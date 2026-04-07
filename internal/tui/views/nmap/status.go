package nmap

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
	nmaptool "heph4estus/internal/tools/nmap"
	"heph4estus/internal/tui/core"
	"heph4estus/internal/worker"
)

// Phase of the status view lifecycle.
type statusPhase int

const (
	phaseEnqueuing statusPhase = iota
	phaseLaunching
	phaseScanning
	phaseComplete
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

// SpotThreshold is the worker count at or above which auto mode selects spot
// instances instead of Fargate.
const SpotThreshold = 50

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

// CounterThreshold is the target count above which we automatically use an
// atomic ProgressCounter instead of Storage.Count(). At 10k+ targets,
// Storage.Count() requires 10+ ListObjectsV2 pages per poll — the counter
// is O(1) regardless of scale.
const CounterThreshold = 10_000

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
	submitter JobSubmitter
	tracker   ProgressTracker
	infra     core.InfraOutputs

	phase        statusPhase
	totalTargets int
	enqueueSent  int
	workersUp    int
	completed    int
	startTime    time.Time
	errMsg       string

	spotInstanceIDs []string

	// Rolling rate samples
	rateSamples []rateSample

	help   help.Model
	width  int
	height int
}

type rateSample struct {
	time  time.Time
	count int
}

// NewStatus creates a status view with real cloud clients.
// counter may be nil — falls back to Storage.Count() for progress tracking.
// When counter is provided and the target count is >= CounterThreshold, the
// counter is used automatically for O(1) progress polling.
func NewStatus(infra core.InfraOutputs, q cloud.Queue, s cloud.Storage, c cloud.Compute, counter cloud.ProgressCounter) *StatusModel {
	// Pre-count targets to decide tracking strategy.
	scanner := nmaptool.NewScanner(nil)
	targets := scanner.ParseTargets(infra.TargetsContent, infra.NmapOptions)
	useCounter := counter != nil && len(targets) >= CounterThreshold

	return NewStatusWithDeps(infra,
		&realSubmitter{queue: q, compute: c},
		&realTracker{counter: counter, storage: s, useCounter: useCounter},
	)
}

// NewStatusWithDeps creates a status view with injected dependencies.
func NewStatusWithDeps(infra core.InfraOutputs, sub JobSubmitter, tracker ProgressTracker) *StatusModel {
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
	return &StatusModel{
		submitter: sub,
		tracker:   tracker,
		infra:     infra,
		startTime: time.Now(),
		help:      h,
	}
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
			return m, nil
		}
		m.enqueueSent = msg.sent
		m.phase = phaseLaunching
		return m, m.launchWorkers()

	case spotLaunchMsg:
		if msg.err != nil {
			m.errMsg = fmt.Sprintf("Spot launch failed: %v", msg.err)
			return m, nil
		}
		m.spotInstanceIDs = msg.instanceIDs
		m.workersUp = msg.launched
		m.phase = phaseScanning
		return m, m.pollProgress()

	case launchProgressMsg:
		if msg.err != nil {
			m.errMsg = fmt.Sprintf("Launch failed: %v", msg.err)
			return m, nil
		}
		m.workersUp = msg.launched
		m.phase = phaseScanning
		return m, m.pollProgress()

	case scanProgressMsg:
		if msg.err != nil {
			m.errMsg = fmt.Sprintf("Progress check failed: %v", msg.err)
			// Don't stop — try again
		} else {
			m.completed = msg.completed
			m.rateSamples = append(m.rateSamples, rateSample{time: time.Now(), count: msg.completed})
			// Keep only last 30s of samples
			cutoff := time.Now().Add(-30 * time.Second)
			for len(m.rateSamples) > 1 && m.rateSamples[0].time.Before(cutoff) {
				m.rateSamples = m.rateSamples[1:]
			}
		}

		if m.completed >= m.totalTargets {
			m.phase = phaseComplete
			return m, func() tea.Msg {
				return core.NavigateWithDataMsg{
					Target: core.ViewNmapResults,
					Data:   m.infra,
				}
			}
		}
		return m, m.pollProgress()
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
		fmt.Fprintf(&b, "  %s%s %d / %d targets  (%.1f%%)\n", labelStyle.Render("Progress:"), bar, m.completed, m.totalTargets, pct)
		if rate > 0 {
			fmt.Fprintf(&b, "  %s~%.0f targets/min\n", labelStyle.Render("Rate:"), rate)
		}
		fmt.Fprintf(&b, "  %s%s\n", labelStyle.Render("Elapsed:"), elapsed.String())
		if eta > 0 {
			fmt.Fprintf(&b, "  %s~%s\n", labelStyle.Render("Remaining:"), eta.Truncate(time.Second).String())
		}

	case phaseComplete:
		b.WriteString(core.SuccessStyle.Render("  Scan complete!") + "\n\n")
		fmt.Fprintf(&b, "  %s%d / %d\n", labelStyle.Render("Completed:"), m.completed, m.totalTargets)
		fmt.Fprintf(&b, "  %s%s\n", labelStyle.Render("Elapsed:"), elapsed.String())
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
	switch infra.ComputeMode {
	case "spot":
		return true
	case "fargate":
		return false
	default: // "auto" or empty
		return infra.WorkerCount >= SpotThreshold
	}
}

func (m *StatusModel) launchWorkers() tea.Cmd {
	infra := m.infra
	sub := m.submitter

	if useSpot(infra) {
		return m.launchSpotWorkers()
	}

	return func() tea.Msg {
		_, err := sub.LaunchWorkers(context.Background(), cloud.ContainerOpts{
			Cluster:        infra.ECSClusterName,
			TaskDefinition: infra.TaskDefinitionARN,
			ContainerName:  "nmap-worker",
			Subnets:        infra.SubnetIDs,
			SecurityGroups: []string{infra.SecurityGroupID},
			Env: map[string]string{
				"QUEUE_URL":          infra.SQSQueueURL,
				"S3_BUCKET":          infra.S3BucketName,
				"JITTER_MAX_SECONDS": strconv.Itoa(infra.JitterMaxSeconds),
				"TOOL_NAME":          "nmap",
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
				"JITTER_MAX_SECONDS": strconv.Itoa(infra.JitterMaxSeconds),
				"TOOL_NAME":          "nmap",
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
				"Tool":    "nmap",
			},
		})
		msg := launchProgressMsg{launched: len(ids), total: infra.WorkerCount, err: err}
		return spotLaunchMsg{launchProgressMsg: msg, instanceIDs: ids}
	}
}

// spotLaunchMsg extends launchProgressMsg with instance IDs for tracking.
type spotLaunchMsg struct {
	launchProgressMsg
	instanceIDs []string
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

func (m *StatusModel) pollProgress() tea.Cmd {
	infra := m.infra
	tracker := m.tracker
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg {
		count, err := tracker.CountResults(context.Background(), infra.S3BucketName, jobs.ResultPrefix("nmap", infra.JobID))
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
