package generic

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"heph4estus/internal/cloud"
	"heph4estus/internal/jobs"
	"heph4estus/internal/modules"
	"heph4estus/internal/operator"
	targetlisttool "heph4estus/internal/tools/targetlist"
	wordlisttool "heph4estus/internal/tools/wordlist"
	"heph4estus/internal/tui/core"
	"heph4estus/internal/tui/statuscore"
	"heph4estus/internal/worker"
)

type statusPhase = statuscore.Phase

const (
	phaseUploading  = statuscore.PhaseUploading // wordlist only: uploading chunks
	phaseEnqueuing  = statuscore.PhaseEnqueuing
	phaseLaunching  = statuscore.PhaseLaunching
	phaseScanning   = statuscore.PhaseScanning
	phaseExporting  = statuscore.PhaseExporting  // exporting results locally before cleanup
	phaseDestroying = statuscore.PhaseDestroying // auto-destroying infrastructure after export
	phaseComplete   = statuscore.PhaseComplete
)

type enqueueProgressMsg struct {
	sent  int
	total int
	err   error
}

type launchProgressMsg struct {
	launched int
	total    int
	err      error
}

type spotLaunchMsg struct {
	launchProgressMsg
	instanceIDs []string
}

type scanProgressMsg struct {
	completed int
	err       error
}

type exportCompleteMsg struct {
	dir   string
	count int
	err   error
}

// autoDestroyCompleteMsg reports the outcome of auto-destroy in the status view.
type autoDestroyCompleteMsg struct {
	err error
}

type uploadCompleteMsg struct {
	tasks   []worker.Task
	words   int
	targets int
	err     error
}

const SpotThreshold = statuscore.SpotThreshold

// GenericSubmitter abstracts target enqueueing and worker launching for generic tools.
type GenericSubmitter interface {
	EnqueueTasks(ctx context.Context, queueURL string, tasks []worker.Task) error
	LaunchWorkers(ctx context.Context, opts cloud.ContainerOpts) (string, error)
	LaunchSpotWorkers(ctx context.Context, opts cloud.SpotOpts) ([]string, error)
}

// GenericTracker abstracts result counting.
type GenericTracker interface {
	CountResults(ctx context.Context, bucket, prefix string) (int, error)
}

// GenericUploader abstracts chunk uploads to storage.
type GenericUploader interface {
	UploadWordlistChunks(ctx context.Context, bucket string, plan *jobs.WordlistPlan) error
	UploadTargetListChunks(ctx context.Context, bucket string, plan *jobs.TargetListPlan) error
}

type realUploader struct {
	storage cloud.Storage
}

func (u *realUploader) UploadWordlistChunks(ctx context.Context, bucket string, plan *jobs.WordlistPlan) error {
	return jobs.UploadChunks(ctx, u.storage, bucket, plan)
}

func (u *realUploader) UploadTargetListChunks(ctx context.Context, bucket string, plan *jobs.TargetListPlan) error {
	return jobs.UploadTargetListChunks(ctx, u.storage, bucket, plan)
}

type realSubmitter struct {
	queue   cloud.Queue
	compute cloud.Compute
}

func (s *realSubmitter) EnqueueTasks(ctx context.Context, queueURL string, tasks []worker.Task) error {
	_, err := jobs.EnqueueTasks(ctx, s.queue, queueURL, tasks, jobs.EnqueueOptions{})
	return err
}

func (s *realSubmitter) LaunchWorkers(ctx context.Context, opts cloud.ContainerOpts) (string, error) {
	return s.compute.RunContainer(ctx, opts)
}

func (s *realSubmitter) LaunchSpotWorkers(ctx context.Context, opts cloud.SpotOpts) ([]string, error) {
	return s.compute.RunSpotInstances(ctx, opts)
}

type realTracker struct {
	counter    cloud.ProgressCounter
	storage    cloud.Storage
	useCounter bool
}

func (t *realTracker) CountResults(ctx context.Context, bucket, prefix string) (int, error) {
	if t.useCounter {
		return t.counter.Get(ctx, bucket)
	}
	return t.storage.Count(ctx, bucket, prefix)
}

type statusKeyMap struct {
	Back key.Binding
	Quit key.Binding
}

func (k statusKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Back, k.Quit}
}

func (k statusKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Back, k.Quit}}
}

var statusKeys = statusKeyMap{
	Back: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
	Quit: key.NewBinding(key.WithKeys("q", "Q"), key.WithHelp("q", "quit")),
}

// StatusModel displays enqueue -> launch -> scan progress for generic tools.
type StatusModel struct {
	submitter  GenericSubmitter
	tracker    GenericTracker
	uploader   GenericUploader
	jobTracker *operator.Tracker
	storage    cloud.Storage  // for local export on completion
	destroyer  core.Destroyer // for auto-destroy after export (nil = no destroy)
	infra      core.InfraOutputs

	phase                statusPhase
	isWordlist           bool
	totalTargets         int // task count for progress
	totalWords           int // only for wordlist jobs
	totalTargetEntries   int // original target count for target-list jobs
	targetListFileBacked bool
	enqueueSent          int
	workersUp            int
	completed            int
	startTime            time.Time
	errMsg               string

	spotInstanceIDs []string
	rateSamples     []statuscore.RateSample

	// Cleanup / export state
	cleanupWarning string

	help   help.Model
	width  int
	height int
}

// NewStatus creates a status view with real cloud clients.
func NewStatus(infra core.InfraOutputs, q cloud.Queue, s cloud.Storage, c cloud.Compute, counter cloud.ProgressCounter, jt *operator.Tracker, destroyer core.Destroyer) *StatusModel {
	targetCount := infra.TargetCount
	if targetCount == 0 && infra.TargetsContent != "" {
		targetCount = len(parseTargetLines(infra.TargetsContent))
	}
	useCounter := counter != nil && targetCount >= 10_000

	m := NewStatusWithDeps(infra,
		&realSubmitter{queue: q, compute: c},
		&realTracker{counter: counter, storage: s, useCounter: useCounter},
		&realUploader{storage: s},
		jt,
	)
	m.storage = s
	m.destroyer = destroyer
	return m
}

// NewStatusWithDeps creates a status view with injected dependencies (for testing).
func NewStatusWithDeps(infra core.InfraOutputs, sub GenericSubmitter, tracker GenericTracker, uploader GenericUploader, jt ...*operator.Tracker) *StatusModel {
	h := help.New()
	h.Styles = help.Styles{
		ShortKey:       lipgloss.NewStyle().Foreground(core.Steel),
		ShortDesc:      lipgloss.NewStyle().Foreground(core.Steel),
		ShortSeparator: lipgloss.NewStyle().Foreground(core.Steel),
		FullKey:        lipgloss.NewStyle().Foreground(core.Steel),
		FullDesc:       lipgloss.NewStyle().Foreground(core.Steel),
		FullSeparator:  lipgloss.NewStyle().Foreground(core.Steel),
		Ellipsis:       lipgloss.NewStyle().Foreground(core.Steel),
	}

	isWL := infra.WordlistPath != "" || infra.WordlistContent != ""

	var jobTracker *operator.Tracker
	if len(jt) > 0 && jt[0] != nil {
		jobTracker = jt[0]
	}
	return &StatusModel{
		submitter:  sub,
		tracker:    tracker,
		uploader:   uploader,
		jobTracker: jobTracker,
		infra:      infra,
		isWordlist: isWL,
		startTime:  time.Now(),
		help:       h,
	}
}

func (m *StatusModel) trackPhase(phase operator.Phase) {
	statuscore.TrackPhase(m.jobTracker, m.infra.JobID, phase)
}

// trackFail marks the job as failed if a tracker is available.
func (m *StatusModel) trackFail(err error) {
	statuscore.TrackFail(m.jobTracker, m.infra.JobID, err)
}

func (m *StatusModel) trackCreate() {
	if m.jobTracker == nil || m.infra.JobID == "" {
		return
	}
	rec := &operator.JobRecord{
		JobID:                 m.infra.JobID,
		ToolName:              m.infra.ToolName,
		Phase:                 operator.PhaseEnqueuing,
		TotalTasks:            m.totalTargets,
		TotalTargets:          m.totalTargetEntries,
		TotalWords:            m.totalWords,
		WorkerCount:           m.infra.WorkerCount,
		ComputeMode:           m.infra.ComputeMode,
		Cloud:                 string(m.infra.Cloud),
		Bucket:                m.infra.S3BucketName,
		S3Endpoint:            m.infra.S3Endpoint,
		S3Region:              m.infra.S3Region,
		S3AccessKey:           m.infra.S3AccessKey,
		S3SecretKey:           m.infra.S3SecretKey,
		S3PathStyle:           m.infra.S3PathStyle,
		Placement:             m.infra.Placement,
		ExpectedWorkerVersion: m.infra.ExpectedWorkerVersion,
		RuntimeTarget:         m.infra.RuntimeTarget,
		NATSUrl:               m.infra.NATSUrl,
		ControllerIP:          m.infra.ControllerIP,
		GenerationID:          m.infra.GenerationID,
		ControllerCAPEM:       m.infra.ControllerCAPEM,
		ControllerHost:        m.infra.ControllerHost,
		NATSClientCertPEM:     m.infra.NATSClientCertPEM,
		NATSClientKeyPEM:      m.infra.NATSClientKeyPEM,
	}
	if m.isWordlist || m.targetListFileBacked {
		rec.Phase = operator.PhaseUploading
	}
	_ = m.jobTracker.Create(rec)
}

func (m *StatusModel) Init() tea.Cmd {
	if m.infra.JobID == "" {
		m.infra.JobID = jobs.NewID(m.infra.ToolName)
	}

	if m.isWordlist {
		return m.initWordlist()
	}
	return m.initTargetList()
}

func (m *StatusModel) initTargetList() tea.Cmd {
	infra := m.infra
	uploader := m.uploader

	plan, tempDir, err := m.planTargetList(infra)
	if err != nil {
		if strings.Contains(err.Error(), "no targets found") {
			m.errMsg = "No targets found"
			return nil
		}
		m.errMsg = fmt.Sprintf("Target-list error: %v", err)
		return nil
	}

	m.totalTargets = len(plan.Tasks)
	m.totalTargetEntries = plan.TotalTargets
	m.targetListFileBacked = plan.FileBacked

	if m.totalTargets == 0 {
		m.errMsg = "No targets found"
		return nil
	}

	if plan.FileBacked {
		m.phase = phaseUploading
	} else {
		m.phase = phaseEnqueuing
	}
	m.trackCreate()
	sub := m.submitter

	if plan.FileBacked {
		return func() (msg tea.Msg) {
			defer func() {
				if tempDir != "" {
					if err := os.RemoveAll(tempDir); err != nil {
						msg = uploadCleanupError(msg, fmt.Errorf("removing target-list temp dir: %w", err))
					}
				}
			}()
			defer func() {
				if err := plan.Cleanup(); err != nil {
					msg = uploadCleanupError(msg, fmt.Errorf("cleaning target-list chunks: %w", err))
				}
			}()
			if err := uploader.UploadTargetListChunks(context.Background(), infra.S3BucketName, plan); err != nil {
				return uploadCompleteMsg{err: err}
			}
			return uploadCompleteMsg{tasks: plan.Tasks, targets: plan.TotalTargets}
		}
	}

	tasks := plan.Tasks
	return func() tea.Msg {
		err := sub.EnqueueTasks(context.Background(), infra.SQSQueueURL, tasks)
		return enqueueProgressMsg{sent: len(tasks), total: len(tasks), err: err}
	}
}

func (m *StatusModel) initWordlist() tea.Cmd {
	infra := m.infra
	uploader := m.uploader

	plan, tempDir, err := m.planWordlist(infra)
	if err != nil {
		m.errMsg = fmt.Sprintf("Wordlist error: %v", err)
		return nil
	}

	m.totalTargets = len(plan.Tasks)
	m.totalWords = plan.TotalWords
	m.phase = phaseUploading
	m.trackCreate()

	return func() (msg tea.Msg) {
		defer func() {
			if tempDir != "" {
				if err := os.RemoveAll(tempDir); err != nil {
					msg = uploadCleanupError(msg, fmt.Errorf("removing wordlist temp dir: %w", err))
				}
			}
		}()
		defer func() {
			if err := plan.Cleanup(); err != nil {
				msg = uploadCleanupError(msg, fmt.Errorf("cleaning wordlist chunks: %w", err))
			}
		}()
		if err := uploader.UploadWordlistChunks(context.Background(), infra.S3BucketName, plan); err != nil {
			return uploadCompleteMsg{err: err}
		}
		return uploadCompleteMsg{tasks: plan.Tasks, words: plan.TotalWords}
	}
}

func uploadCleanupError(msg tea.Msg, err error) tea.Msg {
	if err == nil {
		return msg
	}
	if current, ok := msg.(uploadCompleteMsg); ok && current.err != nil {
		return msg
	}
	return uploadCompleteMsg{err: err}
}

func (m *StatusModel) planTargetList(infra core.InfraOutputs) (*jobs.TargetListPlan, string, error) {
	if infra.TargetsPath != "" {
		reg, err := modules.NewDefaultRegistry()
		if err != nil {
			return nil, "", fmt.Errorf("loading module registry: %w", err)
		}
		mod, err := reg.Get(infra.ToolName)
		if err != nil {
			return nil, "", fmt.Errorf("loading module %q: %w", infra.ToolName, err)
		}
		fileBacked := mod.NeedsInput() && !mod.NeedsTarget()
		var tempDir string
		if fileBacked {
			if err := targetlisttool.CleanupStaleTempDirs(targetlisttool.DefaultStaleTempAge); err != nil {
				m.cleanupWarning = fmt.Sprintf("target-list temp cleanup warning: %v", err)
			}
			tempDir, err = os.MkdirTemp("", "heph-targetlist-*")
			if err != nil {
				return nil, "", fmt.Errorf("creating temp dir: %w", err)
			}
		}
		plan, err := jobs.PlanTargetListFile(infra.ToolName, infra.JobID, infra.ToolOptions, infra.TargetsPath, tempDir, infra.WorkerCount, fileBacked)
		if err != nil {
			if tempDir != "" {
				_ = os.RemoveAll(tempDir)
			}
			return nil, "", err
		}
		return plan, tempDir, nil
	}
	plan, err := jobs.PlanTargetListContent(infra.ToolName, infra.JobID, infra.ToolOptions, infra.TargetsContent)
	if err != nil {
		return nil, "", err
	}
	return plan, "", nil
}

func (m *StatusModel) planWordlist(infra core.InfraOutputs) (*jobs.WordlistPlan, string, error) {
	if infra.WordlistPath != "" {
		if err := wordlisttool.CleanupStaleTempDirs(wordlisttool.DefaultStaleTempAge); err != nil {
			m.cleanupWarning = fmt.Sprintf("wordlist temp cleanup warning: %v", err)
		}
		tempDir, err := os.MkdirTemp("", "heph-wordlist-*")
		if err != nil {
			return nil, "", fmt.Errorf("creating temp dir: %w", err)
		}
		plan, err := jobs.PlanWordlistFile(
			infra.ToolName, infra.JobID,
			infra.RuntimeTarget, infra.ToolOptions,
			infra.WordlistPath, tempDir,
			infra.ChunkCount, infra.WorkerCount,
		)
		if err != nil {
			_ = os.RemoveAll(tempDir)
			return nil, "", err
		}
		return plan, tempDir, nil
	}

	chunkCount := infra.ChunkCount
	if chunkCount <= 0 {
		chunkCount = infra.WorkerCount
	}
	plan, err := jobs.PlanWordlistJob(
		infra.ToolName, infra.JobID,
		infra.RuntimeTarget, infra.ToolOptions,
		infra.WordlistContent, chunkCount,
	)
	return plan, "", err
}

func (m *StatusModel) Update(msg tea.Msg) (core.View, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.help.SetWidth(msg.Width)

	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			return m, func() tea.Msg {
				return core.NavigateMsg{Target: core.ViewMenu}
			}
		case "q", "Q":
			return m, tea.Quit
		}

	case uploadCompleteMsg:
		if msg.err != nil {
			m.errMsg = fmt.Sprintf("Upload failed: %v", msg.err)
			m.trackFail(msg.err)
			return m, nil
		}
		m.totalWords = msg.words
		if msg.targets > 0 {
			m.totalTargetEntries = msg.targets
		}
		m.totalTargets = len(msg.tasks)
		m.phase = phaseEnqueuing
		m.trackPhase(operator.PhaseEnqueuing)
		infra := m.infra
		sub := m.submitter
		tasks := msg.tasks
		return m, func() tea.Msg {
			err := sub.EnqueueTasks(context.Background(), infra.SQSQueueURL, tasks)
			return enqueueProgressMsg{sent: len(tasks), total: len(tasks), err: err}
		}

	case enqueueProgressMsg:
		if msg.err != nil {
			m.errMsg = fmt.Sprintf("Enqueue failed: %v", msg.err)
			m.trackFail(msg.err)
			return m, nil
		}
		m.enqueueSent = msg.sent
		m.phase = phaseLaunching
		m.trackPhase(operator.PhaseLaunching)
		return m, m.launchWorkers()

	case spotLaunchMsg:
		if msg.err != nil {
			m.errMsg = fmt.Sprintf("Spot launch failed: %v", msg.err)
			m.trackFail(msg.err)
			return m, nil
		}
		m.spotInstanceIDs = msg.instanceIDs
		m.workersUp = msg.launched
		m.phase = phaseScanning
		m.trackPhase(operator.PhaseScanning)
		return m, m.pollProgress()

	case launchProgressMsg:
		if msg.err != nil {
			m.errMsg = fmt.Sprintf("Launch failed: %v", msg.err)
			m.trackFail(msg.err)
			return m, nil
		}
		m.workersUp = msg.launched
		m.phase = phaseScanning
		m.trackPhase(operator.PhaseScanning)
		return m, m.pollProgress()

	case scanProgressMsg:
		if msg.err != nil {
			m.errMsg = fmt.Sprintf("Progress check failed: %v", msg.err)
		} else {
			m.completed = msg.completed
			m.rateSamples = statuscore.UpdateRateSamples(m.rateSamples, msg.completed, time.Now())
		}

		if m.completed >= m.totalTargets {
			if m.jobTracker != nil && m.infra.JobID != "" {
				_ = m.jobTracker.Complete(m.infra.JobID)
			}
			result := statuscore.CompleteScan(m.infra, m.storage)
			if result.Warning != "" {
				m.cleanupWarning = result.Warning
			}
			if result.Action == statuscore.CompletionExport {
				m.phase = phaseExporting
				return m, m.exportResults()
			}
			m.phase = phaseComplete
			return m, m.navigateToResults()
		}
		return m, m.pollProgress()

	case exportCompleteMsg:
		result := statuscore.CompleteExport(&m.infra, m.destroyer, statuscore.ExportResult{
			Dir:   msg.dir,
			Count: msg.count,
			Err:   msg.err,
		})
		if result.Warning != "" {
			m.cleanupWarning = result.Warning
		}
		if result.Action == statuscore.CompletionDestroy {
			m.phase = phaseDestroying
			return m, m.runAutoDestroy()
		}
		m.phase = phaseComplete
		return m, m.navigateToResults()

	case autoDestroyCompleteMsg:
		if warning := statuscore.CompleteDestroy(&m.infra, statuscore.DestroyResult{Err: msg.err}); warning != "" {
			m.cleanupWarning = warning
		}
		m.phase = phaseComplete
		return m, m.navigateToResults()
	}

	return m, nil
}

func (m *StatusModel) View() string {
	var b strings.Builder

	titleBar := core.TitleBarStyle.Render(fmt.Sprintf("  %s Scan  ", m.infra.ToolName))
	b.WriteString(titleBar)
	b.WriteString("\n\n")

	elapsed := time.Since(m.startTime).Truncate(time.Second)
	labelStyle := lipgloss.NewStyle().Foreground(core.Gold).Width(14)

	b.WriteString(statuscore.LifecycleSummary(m.infra, labelStyle))
	b.WriteString("\n")

	unitLabel := "targets"
	if m.isWordlist || m.targetListFileBacked {
		unitLabel = "chunks"
	}

	switch m.phase {
	case phaseUploading:
		if m.isWordlist {
			b.WriteString(core.SelectedStyle.Render("  Uploading wordlist chunks...") + "\n\n")
			if m.infra.RuntimeTarget != "" {
				fmt.Fprintf(&b, "  %s%s\n", labelStyle.Render("Target:"), m.infra.RuntimeTarget)
			}
			fmt.Fprintf(&b, "  %s%d\n", labelStyle.Render("Words:"), m.totalWords)
		} else {
			b.WriteString(core.SelectedStyle.Render("  Uploading target-list chunks...") + "\n\n")
			fmt.Fprintf(&b, "  %s%d\n", labelStyle.Render("Targets:"), m.totalTargetEntries)
		}
		fmt.Fprintf(&b, "  %s%d\n", labelStyle.Render("Chunks:"), m.totalTargets)
		fmt.Fprintf(&b, "  %s%s\n", labelStyle.Render("Elapsed:"), elapsed.String())

	case phaseEnqueuing:
		b.WriteString(core.SelectedStyle.Render("  Enqueueing "+unitLabel+"...") + "\n\n")
		if m.isWordlist && m.infra.RuntimeTarget != "" {
			fmt.Fprintf(&b, "  %s%s\n", labelStyle.Render("Target:"), m.infra.RuntimeTarget)
			fmt.Fprintf(&b, "  %s%d\n", labelStyle.Render("Words:"), m.totalWords)
		}
		if m.targetListFileBacked {
			fmt.Fprintf(&b, "  %s%d\n", labelStyle.Render("Targets:"), m.totalTargetEntries)
		}
		fmt.Fprintf(&b, "  %s%d\n", labelStyle.Render("Tasks:"), m.totalTargets)
		fmt.Fprintf(&b, "  %s%s\n", labelStyle.Render("Elapsed:"), elapsed.String())

	case phaseLaunching:
		b.WriteString(core.SelectedStyle.Render("  Launching workers...") + "\n\n")
		fmt.Fprintf(&b, "  %s%s\n", labelStyle.Render("Tasks:"), fmt.Sprintf("%d enqueued", m.enqueueSent))
		fmt.Fprintf(&b, "  %s%d / %d\n", labelStyle.Render("Workers:"), m.workersUp, m.infra.WorkerCount)
		fmt.Fprintf(&b, "  %s%s\n", labelStyle.Render("Elapsed:"), elapsed.String())

	case phaseScanning:
		pct := float64(m.completed) / float64(m.totalTargets) * 100
		bar := progressBar(m.completed, m.totalTargets, 30)
		rate, eta := m.calcRateETA()

		b.WriteString(core.SelectedStyle.Render("  Scanning") + "\n\n")
		if m.isWordlist && m.infra.RuntimeTarget != "" {
			fmt.Fprintf(&b, "  %s%s\n", labelStyle.Render("Target:"), m.infra.RuntimeTarget)
			fmt.Fprintf(&b, "  %s%d\n", labelStyle.Render("Words:"), m.totalWords)
		}
		fmt.Fprintf(&b, "  %s%d active\n", labelStyle.Render("Workers:"), m.workersUp)
		if m.infra.Cloud.IsProviderNative() && m.infra.ControllerIP != "" {
			fmt.Fprintf(&b, "  %s%s\n", labelStyle.Render("Controller:"), m.infra.ControllerIP)
			fmt.Fprintf(&b, "  %s%d admitted workers\n", labelStyle.Render("Fleet:"), m.workersUp)
		}
		fmt.Fprintf(&b, "  %s%s %d / %d %s  (%.1f%%)\n", labelStyle.Render("Progress:"), bar, m.completed, m.totalTargets, unitLabel, pct)
		if rate > 0 {
			fmt.Fprintf(&b, "  %s~%.0f %s/min\n", labelStyle.Render("Rate:"), rate, unitLabel)
		}
		fmt.Fprintf(&b, "  %s%s\n", labelStyle.Render("Elapsed:"), elapsed.String())
		if eta > 0 {
			fmt.Fprintf(&b, "  %s~%s\n", labelStyle.Render("Remaining:"), eta.Truncate(time.Second).String())
		}

	case phaseExporting:
		b.WriteString(core.SelectedStyle.Render("  Exporting results locally...") + "\n\n")
		fmt.Fprintf(&b, "  %s%d / %d\n", labelStyle.Render("Completed:"), m.completed, m.totalTargets)
		fmt.Fprintf(&b, "  %s%s\n", labelStyle.Render("Output:"), m.infra.OutputDir)
		fmt.Fprintf(&b, "  %s%s\n", labelStyle.Render("Elapsed:"), elapsed.String())

	case phaseDestroying:
		b.WriteString(core.SelectedStyle.Render("  Destroying infrastructure...") + "\n\n")
		fmt.Fprintf(&b, "  %s%d / %d\n", labelStyle.Render("Completed:"), m.completed, m.totalTargets)
		if m.infra.Exported {
			fmt.Fprintf(&b, "  %s%s\n", labelStyle.Render("Exported:"), m.infra.ExportDir)
		}
		fmt.Fprintf(&b, "  %s%s\n", labelStyle.Render("Elapsed:"), elapsed.String())

	case phaseComplete:
		b.WriteString(core.SuccessStyle.Render("  Scan complete!") + "\n\n")
		fmt.Fprintf(&b, "  %s%d / %d\n", labelStyle.Render("Completed:"), m.completed, m.totalTargets)
		fmt.Fprintf(&b, "  %s%s\n", labelStyle.Render("Elapsed:"), elapsed.String())
		if m.infra.Exported {
			fmt.Fprintf(&b, "  %s%s\n", labelStyle.Render("Exported:"), m.infra.ExportDir)
		}
		if m.infra.Destroyed {
			fmt.Fprintf(&b, "  %s%s\n", labelStyle.Render("Infra:"), "destroyed")
		}
	}

	if m.cleanupWarning != "" {
		b.WriteString(statuscore.WarningText(m.cleanupWarning))
	}
	if m.errMsg != "" {
		b.WriteString("\n  " + core.ErrorStyle.Render(m.errMsg) + "\n")
	}

	b.WriteString("\n")
	helpBar := core.StatusBarStyle.Render(m.help.View(statusKeys))
	b.WriteString(helpBar)

	content := b.String()
	if m.width > 0 && m.height > 0 {
		content = lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
	}
	return content
}

func useSpot(infra core.InfraOutputs) bool {
	return statuscore.UseSpot(infra)
}

func (m *StatusModel) launchWorkers() tea.Cmd {
	infra := m.infra
	sub := m.submitter
	return func() tea.Msg {
		result := statuscore.Launch(context.Background(), statuscore.LaunchOptions{
			Infra:         infra,
			Launcher:      sub,
			ContainerName: fmt.Sprintf("%s-worker", infra.ToolName),
			ToolName:      infra.ToolName,
			WorkerEnv:     statuscore.DefaultWorkerEnv(infra, infra.ToolName),
		})
		msg := launchProgressMsg{launched: result.Launched, total: result.Total, err: result.Err}
		if result.Spot {
			return spotLaunchMsg{launchProgressMsg: msg, instanceIDs: result.InstanceIDs}
		}
		return msg
	}
}

func (m *StatusModel) exportResults() tea.Cmd {
	storage := m.storage
	infra := m.infra
	return func() tea.Msg {
		result := statuscore.ExportResults(context.Background(), storage, infra, infra.ToolName)
		return exportCompleteMsg{dir: result.Dir, count: result.Count, err: result.Err}
	}
}

func (m *StatusModel) runAutoDestroy() tea.Cmd {
	d := m.destroyer
	return func() tea.Msg {
		result := statuscore.RunAutoDestroy(context.Background(), d)
		return autoDestroyCompleteMsg{err: result.Err}
	}
}

func (m *StatusModel) navigateToResults() tea.Cmd {
	return statuscore.NavigateToResults(core.ViewGenericResults, m.infra)
}

func (m *StatusModel) pollProgress() tea.Cmd {
	infra := m.infra
	tracker := m.tracker
	return tea.Tick(statuscore.PollInterval, func(time.Time) tea.Msg {
		result := statuscore.PollProgress(context.Background(), tracker, infra, infra.ToolName)
		return scanProgressMsg{completed: result.Completed, err: result.Err}
	})
}

func (m *StatusModel) calcRateETA() (targetsPerMin float64, remaining time.Duration) {
	return statuscore.CalcRateETA(m.rateSamples, m.totalTargets, m.completed)
}

func progressBar(current, total, width int) string {
	return statuscore.ProgressBar(current, total, width)
}

// parseTargetLines splits content into non-empty, non-comment target lines.
func parseTargetLines(content string) []string {
	var targets []string
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		targets = append(targets, line)
	}
	return targets
}
