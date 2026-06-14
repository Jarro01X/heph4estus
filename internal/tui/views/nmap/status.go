package nmap

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"heph4estus/internal/cloud"
	"heph4estus/internal/jobs"
	"heph4estus/internal/operator"
	nmaptool "heph4estus/internal/tools/nmap"
	"heph4estus/internal/tui/core"
	"heph4estus/internal/tui/statuscore"
	"heph4estus/internal/worker"
)

// Phase of the status view lifecycle.
type statusPhase = statuscore.Phase

const (
	phaseEnqueuing  = statuscore.PhaseEnqueuing
	phaseLaunching  = statuscore.PhaseLaunching
	phaseScanning   = statuscore.PhaseScanning
	phaseExporting  = statuscore.PhaseExporting  // exporting results locally before cleanup
	phaseDestroying = statuscore.PhaseDestroying // auto-destroying infrastructure after export
	phaseComplete   = statuscore.PhaseComplete
)

// enqueueProgressMsg reports batch-send progress.
type enqueueProgressMsg struct {
	sent  int
	total int
	err   error
}

// launchProgressMsg reports worker launch progress.
type launchProgressMsg struct {
	launched int
	total    int
	err      error
}

// scanProgressMsg reports S3 result count.
type scanProgressMsg struct {
	completed int
	err       error
}

// exportCompleteMsg reports the outcome of a local result export.
type exportCompleteMsg struct {
	dir   string
	count int
	err   error
}

// autoDestroyCompleteMsg reports the outcome of auto-destroy in the status view.
type autoDestroyCompleteMsg struct {
	err error
}

// SpotThreshold is the worker count at or above which auto mode selects spot
// instances instead of Fargate.
const SpotThreshold = statuscore.SpotThreshold

// JobSubmitter abstracts target enqueueing and worker launching.
type JobSubmitter interface {
	EnqueueTargets(ctx context.Context, queueURL string, tasks []worker.Task) error
	LaunchWorkers(ctx context.Context, opts cloud.ContainerOpts) (string, error)
	LaunchSpotWorkers(ctx context.Context, opts cloud.SpotOpts) ([]string, error)
}

// ProgressTracker abstracts result counting.
type ProgressTracker interface {
	CountResults(ctx context.Context, bucket, prefix string) (int, error)
}

// realSubmitter uses cloud.Queue and cloud.Compute.
type realSubmitter struct {
	queue   cloud.Queue
	compute cloud.Compute
}

func (s *realSubmitter) EnqueueTargets(ctx context.Context, queueURL string, tasks []worker.Task) error {
	_, err := jobs.EnqueueTasks(ctx, s.queue, queueURL, tasks, jobs.EnqueueOptions{})
	return err
}

func (s *realSubmitter) LaunchWorkers(ctx context.Context, opts cloud.ContainerOpts) (string, error) {
	return s.compute.RunContainer(ctx, opts)
}

func (s *realSubmitter) LaunchSpotWorkers(ctx context.Context, opts cloud.SpotOpts) ([]string, error) {
	return s.compute.RunSpotInstances(ctx, opts)
}

// CounterThreshold is the target count above which we automatically use an
// atomic ProgressCounter instead of Storage.Count(). At 10k+ targets,
// Storage.Count() requires 10+ ListObjectsV2 pages per poll — the counter
// is O(1) regardless of scale.
const CounterThreshold = statuscore.CounterThreshold

// realTracker automatically selects the progress tracking strategy based on
// job size. Below CounterThreshold it uses Storage.Count() (simple, no extra
// infra). At or above it, uses ProgressCounter if one was provided.
type realTracker struct {
	counter    cloud.ProgressCounter // nil = no counter backend available
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

// StatusModel displays enqueue → launch → scan progress.
type StatusModel struct {
	submitter  JobSubmitter
	tracker    ProgressTracker
	jobTracker *operator.Tracker
	storage    cloud.Storage  // for local export on completion
	destroyer  core.Destroyer // for auto-destroy after export (nil = no destroy)
	infra      core.InfraOutputs

	phase        statusPhase
	totalTargets int
	enqueueSent  int
	workersUp    int
	completed    int
	startTime    time.Time
	errMsg       string

	spotInstanceIDs []string

	// Cleanup / export state
	cleanupWarning string // shown when destroy-after is gated

	// Rolling rate samples
	rateSamples []statuscore.RateSample

	help   help.Model
	width  int
	height int
}

// NewStatus creates a status view with real cloud clients.
// counter may be nil — falls back to Storage.Count() for progress tracking.
// When counter is provided and the target count is >= CounterThreshold, the
// counter is used automatically for O(1) progress polling.
func NewStatus(infra core.InfraOutputs, q cloud.Queue, s cloud.Storage, c cloud.Compute, counter cloud.ProgressCounter, jt *operator.Tracker, destroyer core.Destroyer) *StatusModel {
	// Pre-count targets to decide tracking strategy.
	scanner := nmaptool.NewScanner(nil)
	targets := scanner.ParseTargets(infra.TargetsContent, infra.NmapOptions)
	useCounter := counter != nil && len(targets) >= CounterThreshold

	m := NewStatusWithDeps(infra,
		&realSubmitter{queue: q, compute: c},
		&realTracker{counter: counter, storage: s, useCounter: useCounter},
		jt,
	)
	m.storage = s
	m.destroyer = destroyer
	return m
}

// NewStatusWithDeps creates a status view with injected dependencies.
func NewStatusWithDeps(infra core.InfraOutputs, sub JobSubmitter, tracker ProgressTracker, jt ...*operator.Tracker) *StatusModel {
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
	var jobTracker *operator.Tracker
	if len(jt) > 0 && jt[0] != nil {
		jobTracker = jt[0]
	}
	return &StatusModel{
		submitter:  sub,
		tracker:    tracker,
		jobTracker: jobTracker,
		infra:      infra,
		startTime:  time.Now(),
		help:       h,
	}
}

// trackPhase updates the job record if a tracker is available.
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
	_ = m.jobTracker.Create(&operator.JobRecord{
		JobID:                 m.infra.JobID,
		ToolName:              "nmap",
		Phase:                 operator.PhaseEnqueuing,
		TotalTasks:            m.totalTargets,
		WorkerCount:           m.infra.WorkerCount,
		ComputeMode:           m.infra.ComputeMode,
		Cloud:                 string(m.infra.Cloud),
		Placement:             m.infra.Placement,
		ExpectedWorkerVersion: m.infra.ExpectedWorkerVersion,
		Bucket:                m.infra.S3BucketName,
		S3Endpoint:            m.infra.S3Endpoint,
		S3Region:              m.infra.S3Region,
		S3AccessKey:           m.infra.S3AccessKey,
		S3SecretKey:           m.infra.S3SecretKey,
		S3PathStyle:           m.infra.S3PathStyle,
		NATSUrl:               m.infra.NATSUrl,
		ControllerIP:          m.infra.ControllerIP,
		GenerationID:          m.infra.GenerationID,
		ControllerCAPEM:       m.infra.ControllerCAPEM,
		ControllerHost:        m.infra.ControllerHost,
		NATSClientCertPEM:     m.infra.NATSClientCertPEM,
		NATSClientKeyPEM:      m.infra.NATSClientKeyPEM,
	})
}

func (m *StatusModel) Init() tea.Cmd {
	scanner := nmaptool.NewScanner(nil)
	nmapTasks := scanner.ParseTargets(m.infra.TargetsContent, m.infra.NmapOptions)
	if m.infra.JobID == "" {
		m.infra.JobID = jobs.NewID("nmap")
	}

	// Convert nmap tasks to generic worker tasks with producer-side option injection.
	tasks := make([]worker.Task, len(nmapTasks))
	for i, t := range nmapTasks {
		opts := t.Options
		if m.infra.NoRDNS {
			opts = "-n " + opts
		}
		if m.infra.NmapTimingTemplate != "" {
			opts = fmt.Sprintf("-T%s %s", m.infra.NmapTimingTemplate, opts)
		}
		if m.infra.DNSServers != "" {
			opts = fmt.Sprintf("--dns-servers %s %s", m.infra.DNSServers, opts)
		}
		tasks[i] = worker.Task{
			ToolName:    "nmap",
			JobID:       m.infra.JobID,
			Target:      t.Target,
			Options:     opts,
			GroupID:     t.GroupID,
			ChunkIdx:    t.ChunkIdx,
			TotalChunks: t.TotalChunks,
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
		err := sub.EnqueueTargets(context.Background(), infra.SQSQueueURL, tasks)
		return enqueueProgressMsg{sent: len(tasks), total: len(tasks), err: err}
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
			// Don't stop — try again
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

	titleBar := core.TitleBarStyle.Render("  Nmap Scan  ")
	b.WriteString(titleBar)
	b.WriteString("\n\n")

	elapsed := time.Since(m.startTime).Truncate(time.Second)
	labelStyle := lipgloss.NewStyle().Foreground(core.Gold).Width(14)

	b.WriteString(statuscore.LifecycleSummary(m.infra, labelStyle))
	b.WriteString("\n")

	switch m.phase {
	case phaseEnqueuing:
		b.WriteString(core.SelectedStyle.Render("  Enqueueing targets...") + "\n\n")
		fmt.Fprintf(&b, "  %s%s\n", labelStyle.Render("Targets:"), fmt.Sprintf("%d", m.totalTargets))
		fmt.Fprintf(&b, "  %s%s\n", labelStyle.Render("Elapsed:"), elapsed.String())

	case phaseLaunching:
		b.WriteString(core.SelectedStyle.Render("  Launching workers...") + "\n\n")
		fmt.Fprintf(&b, "  %s%s\n", labelStyle.Render("Targets:"), fmt.Sprintf("%d enqueued", m.enqueueSent))
		fmt.Fprintf(&b, "  %s%d / %d\n", labelStyle.Render("Workers:"), m.workersUp, m.infra.WorkerCount)
		fmt.Fprintf(&b, "  %s%s\n", labelStyle.Render("Elapsed:"), elapsed.String())

	case phaseScanning:
		pct := float64(m.completed) / float64(m.totalTargets) * 100
		bar := progressBar(m.completed, m.totalTargets, 30)
		rate, eta := m.calcRateETA()

		b.WriteString(core.SelectedStyle.Render("  Scanning") + "\n\n")
		fmt.Fprintf(&b, "  %s%d active\n", labelStyle.Render("Workers:"), m.workersUp)
		if m.infra.Cloud.IsProviderNative() && m.infra.ControllerIP != "" {
			fmt.Fprintf(&b, "  %s%s\n", labelStyle.Render("Controller:"), m.infra.ControllerIP)
			fmt.Fprintf(&b, "  %s%d admitted workers\n", labelStyle.Render("Fleet:"), m.workersUp)
		}
		fmt.Fprintf(&b, "  %s%s %d / %d targets  (%.1f%%)\n", labelStyle.Render("Progress:"), bar, m.completed, m.totalTargets, pct)
		if rate > 0 {
			fmt.Fprintf(&b, "  %s~%.0f targets/min\n", labelStyle.Render("Rate:"), rate)
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

// useSpot returns true if the compute mode resolves to spot instances.
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
			ContainerName: "nmap-worker",
			ToolName:      "nmap",
			WorkerEnv:     statuscore.DefaultWorkerEnv(infra, "nmap"),
		})
		msg := launchProgressMsg{launched: result.Launched, total: result.Total, err: result.Err}
		if result.Spot {
			return spotLaunchMsg{launchProgressMsg: msg, instanceIDs: result.InstanceIDs}
		}
		return msg
	}
}

// spotLaunchMsg extends launchProgressMsg with instance IDs for tracking.
type spotLaunchMsg struct {
	launchProgressMsg
	instanceIDs []string
}

func (m *StatusModel) exportResults() tea.Cmd {
	storage := m.storage
	infra := m.infra
	return func() tea.Msg {
		result := statuscore.ExportResults(context.Background(), storage, infra, "nmap")
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
	return statuscore.NavigateToResults(core.ViewNmapResults, m.infra)
}

func (m *StatusModel) pollProgress() tea.Cmd {
	infra := m.infra
	tracker := m.tracker
	return tea.Tick(statuscore.PollInterval, func(time.Time) tea.Msg {
		result := statuscore.PollProgress(context.Background(), tracker, infra, "nmap")
		return scanProgressMsg{completed: result.Completed, err: result.Err}
	})
}

func (m *StatusModel) calcRateETA() (targetsPerMin float64, remaining time.Duration) {
	return statuscore.CalcRateETA(m.rateSamples, m.totalTargets, m.completed)
}

func progressBar(current, total, width int) string {
	return statuscore.ProgressBar(current, total, width)
}
