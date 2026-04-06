package deploy

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"heph4estus/internal/logger"
	"heph4estus/internal/tui/core"
)

const (
	stageTerraformInit  = "terraform-init"
	stageTerraformPlan  = "terraform-plan"
	stageAwaitApproval  = "await-approval"
	stageTerraformApply = "terraform-apply"
	stageReadOutputs    = "read-outputs"
	stageDockerBuild    = "docker-build"
	stageECRAuth        = "ecr-auth"
	stageDockerTagPush  = "docker-tag-push"
	stageComplete       = "complete"
	stageFailed         = "failed"
	stageRejected       = "rejected"
)

type deployKeyMap struct {
	Approve key.Binding
	Reject  key.Binding
	Back    key.Binding
	Quit    key.Binding
}

func (k deployKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Back, k.Quit}
}

func (k deployKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Approve, k.Reject, k.Back, k.Quit}}
}

var deployKeys = deployKeyMap{
	Approve: key.NewBinding(key.WithKeys("y", "enter"), key.WithHelp("y/enter", "approve")),
	Reject:  key.NewBinding(key.WithKeys("n", "esc"), key.WithHelp("n/esc", "reject")),
	Back:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
	Quit:    key.NewBinding(key.WithKeys("q", "Q"), key.WithHelp("q", "quit")),
}

// Model is the deploy pipeline view.
type Model struct {
	deployer     Deployer
	config       core.DeployConfig
	stage        string
	planSummary  string
	outputs      map[string]string
	streamWriter *core.StreamWriter
	streamLog    string
	viewport     viewport.Model
	errMsg       string
	help         help.Model
	width        int
	height       int
}

// New creates a deploy view with a real deployer.
func New(cfg core.DeployConfig) *Model {
	return NewWithDeployer(cfg, NewRealDeployer(simpleLogger{}))
}

// NewWithDeployer creates a deploy view with an injected Deployer (for testing).
func NewWithDeployer(cfg core.DeployConfig, d Deployer) *Model {
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
	return &Model{
		deployer:     d,
		config:       cfg,
		stage:        stageTerraformInit,
		streamWriter: &core.StreamWriter{},
		help:         h,
	}
}

func (m *Model) Init() tea.Cmd {
	return m.runStage(stageTerraformInit)
}

func (m *Model) Update(msg tea.Msg) (core.View, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.help.SetWidth(msg.Width)
		m.viewport.SetWidth(msg.Width - 4)
		m.viewport.SetHeight(max(msg.Height-12, 5))

	case tea.KeyPressMsg:
		if m.stage == stageAwaitApproval {
			switch msg.String() {
			case "y", "enter":
				m.stage = stageTerraformApply
				return m, m.runStage(stageTerraformApply)
			case "n", "esc":
				m.stage = stageRejected
				return m, nil
			}
		}
		switch msg.String() {
		case "esc":
			if m.stage == stageFailed || m.stage == stageRejected {
				return m, m.navigateBackToConfig()
			}
		case "q", "Q":
			if m.stage == stageFailed || m.stage == stageRejected {
				return m, tea.Quit
			}
		}

	case core.TickMsg:
		if s := m.streamWriter.Drain(); s != "" {
			m.streamLog += s
			m.viewport.SetContent(m.streamLog)
			m.viewport.GotoBottom()
		}
		if isStreamingStage(m.stage) {
			return m, tickCmd()
		}

	case core.StageCompleteMsg:
		if msg.Error != nil {
			m.stage = stageFailed
			m.errMsg = fmt.Sprintf("Stage %s failed: %v", msg.Stage, msg.Error)
			return m, nil
		}
		m.outputs = mergeOutputs(m.outputs, msg.Outputs)
		return m, m.advanceStage(msg.Stage)
	}

	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m *Model) View() string {
	var b strings.Builder

	titleBar := core.TitleBarStyle.Render("  Deploy Infrastructure  ")
	b.WriteString(titleBar)
	b.WriteString("\n\n")

	// Stage progress
	stages := []struct {
		id    string
		label string
	}{
		{stageTerraformInit, "Terraform Init"},
		{stageTerraformPlan, "Terraform Plan"},
		{stageAwaitApproval, "Approve Plan"},
		{stageTerraformApply, "Terraform Apply"},
		{stageReadOutputs, "Read Outputs"},
		{stageDockerBuild, "Docker Build"},
		{stageECRAuth, "ECR Auth"},
		{stageDockerTagPush, "Docker Push"},
	}

	currentIdx := -1
	for i, s := range stages {
		if s.id == m.stage {
			currentIdx = i
			break
		}
	}
	// Terminal stages
	if m.stage == stageComplete {
		currentIdx = len(stages)
	}

	for i, s := range stages {
		var marker string
		switch {
		case i < currentIdx:
			marker = core.SuccessStyle.Render("  [done]  " + s.label)
		case i == currentIdx:
			marker = core.SelectedStyle.Render("  [>>>>]  " + s.label)
		default:
			marker = core.MutedStyle.Render("  [    ]  " + s.label)
		}
		b.WriteString(marker + "\n")
	}

	b.WriteString("\n")

	// Stage-specific content
	switch m.stage {
	case stageAwaitApproval:
		b.WriteString(core.TitleStyle.Render("  Plan Summary:") + "\n")
		b.WriteString("  " + m.planSummary + "\n\n")
		b.WriteString("  " + core.SelectedStyle.Render("Apply these changes? (y/enter = yes, n/esc = no)") + "\n")

	case stageTerraformApply, stageDockerBuild, stageDockerTagPush:
		b.WriteString(m.viewport.View())
		b.WriteString("\n")

	case stageComplete:
		b.WriteString(core.SuccessStyle.Render("  Infrastructure deployed successfully!") + "\n")
		b.WriteString("  " + core.MutedStyle.Render("Transitioning to scan...") + "\n")

	case stageFailed:
		b.WriteString(core.ErrorStyle.Render("  "+m.errMsg) + "\n\n")
		b.WriteString("  " + core.MutedStyle.Render("esc: back to config  q: quit") + "\n")

	case stageRejected:
		b.WriteString(core.MutedStyle.Render("  Plan rejected.") + "\n\n")
		b.WriteString("  " + core.MutedStyle.Render("esc: back to config  q: quit") + "\n")
	}

	b.WriteString("\n")
	helpBar := core.StatusBarStyle.Render(m.help.View(deployKeys))
	b.WriteString(helpBar)

	content := b.String()
	if m.width > 0 && m.height > 0 {
		content = lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
	}
	return content
}

func (m *Model) runStage(stage string) tea.Cmd {
	ctx := context.Background()
	cfg := m.config
	sw := m.streamWriter
	deployer := m.deployer

	switch stage {
	case stageTerraformInit:
		return func() tea.Msg {
			err := deployer.TerraformInit(ctx, cfg.TerraformDir)
			return core.StageCompleteMsg{Stage: stage, Error: err}
		}

	case stageTerraformPlan:
		return func() tea.Msg {
			summary, err := deployer.TerraformPlan(ctx, cfg.TerraformDir, cfg.TerraformVars)
			return core.StageCompleteMsg{
				Stage:   stage,
				Error:   err,
				Outputs: map[string]string{"plan_summary": summary},
			}
		}

	case stageTerraformApply:
		return tea.Batch(
			func() tea.Msg {
				err := deployer.TerraformApply(ctx, cfg.TerraformDir, cfg.TerraformVars, sw)
				return core.StageCompleteMsg{Stage: stage, Error: err}
			},
			tickCmd(),
		)

	case stageReadOutputs:
		return func() tea.Msg {
			outputs, err := deployer.TerraformReadOutputs(ctx, cfg.TerraformDir)
			return core.StageCompleteMsg{Stage: stage, Error: err, Outputs: outputs}
		}

	case stageDockerBuild:
		return tea.Batch(
			func() tea.Msg {
				var err error
				if len(cfg.BuildArgs) > 0 {
					err = deployer.DockerBuildWithArgs(ctx, cfg.Dockerfile, cfg.DockerContext, cfg.DockerTag, cfg.BuildArgs, sw)
				} else {
					err = deployer.DockerBuild(ctx, cfg.Dockerfile, cfg.DockerContext, cfg.DockerTag, sw)
				}
				return core.StageCompleteMsg{Stage: stage, Error: err}
			},
			tickCmd(),
		)

	case stageECRAuth:
		return func() tea.Msg {
			err := deployer.ECRAuthenticate(ctx, cfg.AWSRegion)
			return core.StageCompleteMsg{Stage: stage, Error: err}
		}

	case stageDockerTagPush:
		ecrURL := m.outputs["ecr_repo_url"]
		return tea.Batch(
			func() tea.Msg {
				if err := deployer.DockerTag(ctx, cfg.DockerTag, ecrURL+":latest"); err != nil {
					return core.StageCompleteMsg{Stage: stage, Error: err}
				}
				err := deployer.DockerPush(ctx, ecrURL+":latest", sw)
				return core.StageCompleteMsg{Stage: stage, Error: err}
			},
			tickCmd(),
		)
	}

	return nil
}

func (m *Model) advanceStage(completed string) tea.Cmd {
	switch completed {
	case stageTerraformInit:
		m.stage = stageTerraformPlan
		return m.runStage(stageTerraformPlan)

	case stageTerraformPlan:
		m.planSummary = m.outputs["plan_summary"]
		m.stage = stageAwaitApproval
		return nil

	case stageTerraformApply:
		m.streamLog = ""
		m.stage = stageReadOutputs
		return m.runStage(stageReadOutputs)

	case stageReadOutputs:
		m.stage = stageDockerBuild
		return m.runStage(stageDockerBuild)

	case stageDockerBuild:
		m.streamLog = ""
		m.stage = stageECRAuth
		return m.runStage(stageECRAuth)

	case stageECRAuth:
		m.stage = stageDockerTagPush
		return m.runStage(stageDockerTagPush)

	case stageDockerTagPush:
		m.stage = stageComplete
		return m.emitNavigateToStatus()
	}

	return nil
}

func (m *Model) emitNavigateToStatus() tea.Cmd {
	outputs := m.outputs
	cfg := m.config

	target := cfg.PostDeployView
	if target == 0 {
		target = core.ViewNmapStatus
	}

	return func() tea.Msg {
		return core.NavigateWithDataMsg{
			Target: target,
			Data: core.InfraOutputs{
				SQSQueueURL:        outputs["sqs_queue_url"],
				ECRRepoURL:         outputs["ecr_repo_url"],
				S3BucketName:       outputs["s3_bucket_name"],
				ECSClusterName:     outputs["ecs_cluster_name"],
				TaskDefinitionARN:  outputs["task_definition_arn"],
				SubnetIDs:          splitCSV(outputs["subnet_ids"]),
				SecurityGroupID:    outputs["security_group_id"],
				TargetsContent:     cfg.TargetsContent,
				NmapOptions:        cfg.NmapOptions,
				WorkerCount:        cfg.WorkerCount,
				ComputeMode:        cfg.ComputeMode,
				JitterMaxSeconds:   cfg.JitterMaxSeconds,
				NmapTimingTemplate: cfg.NmapTimingTemplate,
				DNSServers:         cfg.DNSServers,
				NoRDNS:             cfg.NoRDNS,
				InstanceProfileARN: outputs["instance_profile_arn"],
				AMIID:              outputs["ami_id"],
				ToolName:           cfg.ToolName,
				ToolOptions:        cfg.ToolOptions,
				WordlistContent:    cfg.WordlistContent,
				RuntimeTarget:      cfg.RuntimeTarget,
				ChunkCount:         cfg.ChunkCount,
			},
		}
	}
}

func (m *Model) navigateBackToConfig() tea.Cmd {
	if m.config.PostDeployView == core.ViewGenericStatus && m.config.ToolName != "" {
		toolName := m.config.ToolName
		return func() tea.Msg {
			return core.NavigateWithDataMsg{
				Target: core.ViewGenericConfig,
				Data:   toolName,
			}
		}
	}

	return func() tea.Msg {
		return core.NavigateMsg{Target: core.ViewNmapConfig}
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg {
		return core.TickMsg{}
	})
}

func isStreamingStage(stage string) bool {
	return stage == stageTerraformApply || stage == stageDockerBuild || stage == stageDockerTagPush
}

func mergeOutputs(existing, new map[string]string) map[string]string {
	if existing == nil {
		existing = make(map[string]string)
	}
	for k, v := range new {
		existing[k] = v
	}
	return existing
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	// Handle terraform list output format like [subnet-xxx subnet-yyy]
	s = strings.Trim(s, "[]")
	parts := strings.Split(s, " ")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// simpleLogger satisfies logger.Logger for the default deployer.
type simpleLogger struct{}

func (simpleLogger) Info(string, ...interface{})  {}
func (simpleLogger) Error(string, ...interface{}) {}
func (simpleLogger) Fatal(string, ...interface{}) {}

// Compile-time check.
var _ logger.Logger = simpleLogger{}
