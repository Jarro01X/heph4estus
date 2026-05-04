package generic

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"heph4estus/internal/cloud"
	awscloud "heph4estus/internal/cloud/aws"
	"heph4estus/internal/jobs"
	"heph4estus/internal/operator"
	"heph4estus/internal/tui/core"
	"heph4estus/internal/worker"
)

type statusPhase int

const (
	phaseUploading statusPhase = iota // wordlist only: uploading chunks
	phaseEnqueuing
	phaseLaunching
	phaseScanning
	phaseExporting  // exporting results locally before cleanup
	phaseDestroying // auto-destroying infrastructure after export
	phaseComplete
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
	tasks []worker.Task
	words int
	err   error
}

const SpotThreshold = 50

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
	UploadChunks(ctx context.Context, bucket string, plan *jobs.WordlistPlan) error
}

type realUploader struct {
	storage cloud.Storage
}

func (u *realUploader) UploadChunks(ctx context.Context, bucket string, plan *jobs.WordlistPlan) error {
	return jobs.UploadChunks(ctx, u.storage, bucket, plan)
}

type realSubmitter struct {
	queue   cloud.Queue
	compute cloud.Compute
}

func (s *realSubmitter) EnqueueTasks(ctx context.Context, queueURL string, tasks []worker.Task) error {
	bodies := make([]string, len(tasks))
	for i, t := range tasks {
		b, err := json.Marshal(t)
		if err != nil {
			return fmt.Errorf("marshal task %d: %w", i, err)
		}
		bodies[i] = string(b)
	}
	return s.queue.SendBatch(ctx, queueURL, bodies)
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

	phase        statusPhase
	isWordlist   bool
	totalTargets int // for target_list: target count; for wordlist: chunk count
	totalWords   int // only for wordlist jobs
	enqueueSent  int
	workersUp    int
	completed    int
	startTime    time.Time
	errMsg       string

	spotInstanceIDs []string
	rateSamples     []rateSample

	// Cleanup / export state
	cleanupWarning string

	help   help.Model
	width  int
	height int
}

type rateSample struct {
	time  time.Time
	count int
}

// NewStatus creates a status view with real cloud clients.
func NewStatus(infra core.InfraOutputs, q cloud.Queue, s cloud.Storage, c cloud.Compute, counter cloud.ProgressCounter, jt *operator.Tracker, destroyer core.Destroyer) *StatusModel {
	targets := parseTargetLines(infra.TargetsContent)
	useCounter := counter != nil && len(targets) >= 10_000

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

	isWL := infra.WordlistContent != ""

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
	if m.jobTracker != nil && m.infra.JobID != "" {
		_ = m.jobTracker.UpdatePhase(m.infra.JobID, phase)
	}
}

// trackFail marks the job as failed if a tracker is available.
func (m *StatusModel) trackFail(err error) {
	if m.jobTracker != nil && m.infra.JobID != "" {
		_ = m.jobTracker.Fail(m.infra.JobID, err)
	}
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
		TotalWords:            m.totalWords,
		WorkerCount:           m.infra.WorkerCount,
		ComputeMode:           m.infra.ComputeMode,
		Cloud:                 string(m.infra.Cloud),
		Bucket:                m.infra.S3BucketName,
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
	if m.isWordlist {
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
	targets := parseTargetLines(m.infra.TargetsContent)

	tasks := make([]worker.Task, len(targets))
	for i, t := range targets {
		tasks[i] = worker.Task{
			ToolName: m.infra.ToolName,
			JobID:    m.infra.JobID,
			Target:   t,
			Options:  m.infra.ToolOptions,
		}
	}
	m.totalTargets = len(tasks)

	if m.totalTargets == 0 {
		m.errMsg = "No targets found"
		return nil
	}

	m.phase = phaseEnqueuing
	m.trackCreate()
	infra := m.infra
	sub := m.submitter
	return func() tea.Msg {
		err := sub.EnqueueTasks(context.Background(), infra.SQSQueueURL, tasks)
		return enqueueProgressMsg{sent: len(tasks), total: len(tasks), err: err}
	}
}

func (m *StatusModel) initWordlist() tea.Cmd {
	infra := m.infra
	uploader := m.uploader

	chunkCount := infra.ChunkCount
	if chunkCount <= 0 {
		chunkCount = infra.WorkerCount
	}

	plan, err := jobs.PlanWordlistJob(
		infra.ToolName, infra.JobID,
		infra.RuntimeTarget, infra.ToolOptions,
		infra.WordlistContent, chunkCount,
	)
	if err != nil {
		m.errMsg = fmt.Sprintf("Wordlist error: %v", err)
		return nil
	}

	m.totalTargets = len(plan.Tasks)
	m.totalWords = plan.TotalWords
	m.phase = phaseUploading
	m.trackCreate()

	return func() tea.Msg {
		if err := uploader.UploadChunks(context.Background(), infra.S3BucketName, plan); err != nil {
			return uploadCompleteMsg{err: err}
		}
		return uploadCompleteMsg{tasks: plan.Tasks, words: plan.TotalWords}
	}
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
			m.rateSamples = append(m.rateSamples, rateSample{time: time.Now(), count: msg.completed})
			cutoff := time.Now().Add(-30 * time.Second)
			for len(m.rateSamples) > 1 && m.rateSamples[0].time.Before(cutoff) {
				m.rateSamples = m.rateSamples[1:]
			}
		}

		if m.completed >= m.totalTargets {
			if m.jobTracker != nil && m.infra.JobID != "" {
				_ = m.jobTracker.Complete(m.infra.JobID)
			}
			if m.shouldExport() {
				m.phase = phaseExporting
				return m, m.exportResults()
			}
			if m.infra.CleanupPolicy == "destroy-after" {
				if m.infra.Cloud.IsSelfhostedFamily() && !m.infra.Cloud.IsProviderNative() {
					m.cleanupWarning = "destroy-after skipped: selfhosted does not support auto-destroy"
				} else if m.infra.OutputDir == "" {
					m.cleanupWarning = "destroy-after skipped: no output directory configured"
				}
			}
			m.phase = phaseComplete
			return m, m.navigateToResults()
		}
		return m, m.pollProgress()

	case exportCompleteMsg:
		if msg.err != nil {
			m.cleanupWarning = fmt.Sprintf("destroy-after skipped: export failed (%v)", msg.err)
			m.phase = phaseComplete
			return m, m.navigateToResults()
		}
		m.infra.Exported = true
		m.infra.ExportDir = msg.dir
		// Auto-destroy if destroy-after policy and destroyer is available.
		if m.infra.Cloud.IsSelfhostedFamily() && !m.infra.Cloud.IsProviderNative() {
			m.cleanupWarning = "destroy-after skipped: selfhosted does not support auto-destroy"
			m.phase = phaseComplete
			return m, m.navigateToResults()
		}
		if m.destroyer != nil {
			m.phase = phaseDestroying
			return m, m.runAutoDestroy()
		}
		m.cleanupWarning = "destroy-after skipped: no terraform directory"
		m.phase = phaseComplete
		return m, m.navigateToResults()

	case autoDestroyCompleteMsg:
		if msg.err != nil {
			m.infra.DestroyErr = msg.err.Error()
			m.cleanupWarning = fmt.Sprintf("destroy failed: %v", msg.err)
		} else {
			m.infra.Destroyed = true
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

	// Lifecycle summary — shown in all phases.
	infraLabel := "freshly deployed"
	if m.infra.Reused {
		infraLabel = "reused"
	}
	fmt.Fprintf(&b, "  %s%s\n", labelStyle.Render("Infra:"), infraLabel)
	if m.infra.CleanupPolicy != "" {
		fmt.Fprintf(&b, "  %s%s\n", labelStyle.Render("Cleanup:"), m.infra.CleanupPolicy)
	}
	if m.infra.Placement.Summary() != "" {
		fmt.Fprintf(&b, "  %s%s\n", labelStyle.Render("Placement:"), m.infra.Placement.Summary())
	}
	b.WriteString("\n")

	unitLabel := "targets"
	if m.isWordlist {
		unitLabel = "chunks"
	}

	switch m.phase {
	case phaseUploading:
		b.WriteString(core.SelectedStyle.Render("  Uploading wordlist chunks...") + "\n\n")
		if m.infra.RuntimeTarget != "" {
			fmt.Fprintf(&b, "  %s%s\n", labelStyle.Render("Target:"), m.infra.RuntimeTarget)
		}
		fmt.Fprintf(&b, "  %s%d\n", labelStyle.Render("Chunks:"), m.totalTargets)
		fmt.Fprintf(&b, "  %s%s\n", labelStyle.Render("Elapsed:"), elapsed.String())

	case phaseEnqueuing:
		b.WriteString(core.SelectedStyle.Render("  Enqueueing "+unitLabel+"...") + "\n\n")
		if m.isWordlist && m.infra.RuntimeTarget != "" {
			fmt.Fprintf(&b, "  %s%s\n", labelStyle.Render("Target:"), m.infra.RuntimeTarget)
			fmt.Fprintf(&b, "  %s%d\n", labelStyle.Render("Words:"), m.totalWords)
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
		b.WriteString("\n  " + core.MutedStyle.Render(m.cleanupWarning) + "\n")
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
	// Selfhosted only supports RunContainer (no spot instances).
	if infra.Cloud.IsSelfhostedFamily() {
		return false
	}
	switch infra.ComputeMode {
	case "spot":
		return true
	case "fargate":
		return false
	default:
		return infra.WorkerCount >= SpotThreshold
	}
}

func (m *StatusModel) launchWorkers() tea.Cmd {
	infra := m.infra
	sub := m.submitter

	if useSpot(infra) {
		return m.launchSpotWorkers()
	}
	if infra.Cloud.IsProviderNative() {
		return func() tea.Msg {
			launched := infra.FleetWorkerCount
			if launched <= 0 {
				launched = infra.WorkerCount
			}
			if launched <= 0 {
				launched = 1
			}
			return launchProgressMsg{launched: launched, total: launched}
		}
	}

	return func() tea.Msg {
		_, err := sub.LaunchWorkers(context.Background(), cloud.ContainerOpts{
			Cluster:        infra.ECSClusterName,
			TaskDefinition: infra.TaskDefinitionARN,
			ContainerName:  fmt.Sprintf("%s-worker", infra.ToolName),
			Subnets:        infra.SubnetIDs,
			SecurityGroups: []string{infra.SecurityGroupID},
			Env: map[string]string{
				"QUEUE_URL":          infra.SQSQueueURL,
				"S3_BUCKET":          infra.S3BucketName,
				"TOOL_NAME":          infra.ToolName,
				"JITTER_MAX_SECONDS": strconv.Itoa(infra.JitterMaxSeconds),
			},
			Count: infra.WorkerCount,
		})
		return launchProgressMsg{launched: infra.WorkerCount, total: infra.WorkerCount, err: err}
	}
}

func (m *StatusModel) launchSpotWorkers() tea.Cmd {
	infra := m.infra
	sub := m.submitter
	return func() tea.Msg {
		userData := awscloud.GenerateUserData(awscloud.UserDataOpts{
			ECRRepoURL: infra.ECRRepoURL,
			ImageTag:   "latest",
			Region:     regionFromECR(infra.ECRRepoURL),
			EnvVars: map[string]string{
				"QUEUE_URL":          infra.SQSQueueURL,
				"S3_BUCKET":          infra.S3BucketName,
				"TOOL_NAME":          infra.ToolName,
				"JITTER_MAX_SECONDS": strconv.Itoa(infra.JitterMaxSeconds),
			},
		})
		ids, err := sub.LaunchSpotWorkers(context.Background(), cloud.SpotOpts{
			AMI:             infra.AMIID,
			Count:           infra.WorkerCount,
			SecurityGroups:  []string{infra.SecurityGroupID},
			SubnetIDs:       infra.SubnetIDs,
			InstanceProfile: infra.InstanceProfileARN,
			UserData:        userData,
			Tags: map[string]string{
				"Project": "heph4estus",
				"Tool":    infra.ToolName,
			},
		})
		msg := launchProgressMsg{launched: len(ids), total: infra.WorkerCount, err: err}
		return spotLaunchMsg{launchProgressMsg: msg, instanceIDs: ids}
	}
}

func (m *StatusModel) shouldExport() bool {
	return m.infra.CleanupPolicy == "destroy-after" && m.infra.OutputDir != "" && m.storage != nil
}

func (m *StatusModel) exportResults() tea.Cmd {
	storage := m.storage
	infra := m.infra
	return func() tea.Msg {
		result, err := operator.ExportJob(
			context.Background(), storage,
			infra.S3BucketName, infra.ToolName, infra.JobID, infra.OutputDir,
		)
		if err != nil {
			return exportCompleteMsg{err: err}
		}
		return exportCompleteMsg{dir: result.Dir, count: result.ResultCount + result.ArtifactCount}
	}
}

func (m *StatusModel) runAutoDestroy() tea.Cmd {
	d := m.destroyer
	return func() tea.Msg {
		err := d.Destroy(context.Background())
		return autoDestroyCompleteMsg{err: err}
	}
}

func (m *StatusModel) navigateToResults() tea.Cmd {
	infra := m.infra
	return func() tea.Msg {
		return core.NavigateWithDataMsg{
			Target: core.ViewGenericResults,
			Data:   infra,
		}
	}
}

func (m *StatusModel) pollProgress() tea.Cmd {
	infra := m.infra
	tracker := m.tracker
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg {
		count, err := tracker.CountResults(context.Background(), infra.S3BucketName, jobs.ResultPrefix(infra.ToolName, infra.JobID))
		return scanProgressMsg{completed: count, err: err}
	})
}

func (m *StatusModel) calcRateETA() (targetsPerMin float64, remaining time.Duration) {
	if len(m.rateSamples) < 2 {
		return 0, 0
	}
	first := m.rateSamples[0]
	last := m.rateSamples[len(m.rateSamples)-1]
	dt := last.time.Sub(first.time).Minutes()
	if dt <= 0 {
		return 0, 0
	}
	dc := float64(last.count - first.count)
	rate := dc / dt
	if rate <= 0 {
		return 0, 0
	}
	left := float64(m.totalTargets - m.completed)
	eta := time.Duration(left/rate*60) * time.Second
	return rate, eta
}

func progressBar(current, total, width int) string {
	if total <= 0 {
		return strings.Repeat("░", width)
	}
	filled := min(current*width/total, width)
	return "[" + strings.Repeat("█", filled) + strings.Repeat("░", width-filled) + "]"
}

func regionFromECR(url string) string {
	parts := strings.Split(url, ".")
	for i, p := range parts {
		if p == "ecr" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return "us-east-1"
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
