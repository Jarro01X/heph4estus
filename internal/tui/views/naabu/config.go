package naabu

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"heph4estus/internal/cloud"
	"heph4estus/internal/cloud/factory"
	"heph4estus/internal/fleet"
	"heph4estus/internal/infra"
	"heph4estus/internal/operator"
	naabutool "heph4estus/internal/tools/naabu"
	targetlisttool "heph4estus/internal/tools/targetlist"
	"heph4estus/internal/tui/core"
)

type targetListReadMsg struct {
	path string
	meta *targetlisttool.Metadata
	err  error
}

type configKeyMap struct {
	Tab   key.Binding
	Enter key.Binding
	Back  key.Binding
	Quit  key.Binding
}

func (k configKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Tab, k.Enter, k.Back, k.Quit}
}

func (k configKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Tab, k.Enter, k.Back, k.Quit}}
}

var configKeys = configKeyMap{
	Tab:   key.NewBinding(key.WithKeys("tab", "shift+tab"), key.WithHelp("tab", "next field")),
	Enter: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "submit/toggle")),
	Back:  key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
	Quit:  key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit")),
}

const (
	fieldTargetFile = iota
	fieldMode
	fieldNmapOptions
	fieldWorkerCount
	fieldComputeMode
	fieldCloud
	fieldSubmit
	fieldCount
)

// ConfigModel is the Naabu configuration form view.
type ConfigModel struct {
	inputs     [5]textinput.Model
	mode       naabutool.Mode
	focusIndex int
	help       help.Model
	width      int
	height     int
	errMsg     string
}

// NewConfig creates a Naabu config view with combined mode selected by default.
func NewConfig() *ConfigModel {
	cfg, _ := operator.LoadConfig()

	targetInput := textinput.New()
	targetInput.Placeholder = "/path/to/targets.txt"
	targetInput.Focus()
	targetInput.CharLimit = 256

	optsInput := textinput.New()
	optsInput.Placeholder = "-Pn -T4"
	optsInput.CharLimit = 256

	workers := operator.ResolveWorkers(0, cfg)
	computeMode := operator.ResolveComputeMode("", cfg)

	workerInput := textinput.New()
	workerInput.Placeholder = "10"
	workerInput.SetValue(strconv.Itoa(workers))
	workerInput.CharLimit = 6

	modeInput := textinput.New()
	modeInput.Placeholder = "auto"
	modeInput.SetValue(computeMode)
	modeInput.CharLimit = 7

	savedCloud := ""
	if cfg != nil {
		savedCloud = normalizeCloudValue(cfg.Cloud)
	}
	cloudInput := textinput.New()
	cloudInput.Placeholder = "aws"
	if savedCloud != "" {
		cloudInput.SetValue(savedCloud)
	}
	cloudInput.CharLimit = 12

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

	return &ConfigModel{
		inputs: [5]textinput.Model{targetInput, optsInput, workerInput, modeInput, cloudInput},
		mode:   naabutool.ModeCombined,
		help:   h,
	}
}

func (m *ConfigModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m *ConfigModel) Update(msg tea.Msg) (core.View, tea.Cmd) {
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
		case "tab", "down":
			m.focusIndex = (m.focusIndex + 1) % fieldCount
			return m, m.updateFocus()
		case "shift+tab", "up":
			m.focusIndex = (m.focusIndex - 1 + fieldCount) % fieldCount
			return m, m.updateFocus()
		case "enter":
			if m.focusIndex == fieldSubmit {
				return m, m.submit()
			}
			if m.focusIndex == fieldMode {
				m.toggleMode()
				return m, nil
			}
			m.focusIndex = (m.focusIndex + 1) % fieldCount
			return m, m.updateFocus()
		case " ":
			if m.focusIndex == fieldMode {
				m.toggleMode()
				return m, nil
			}
		}

	case targetListReadMsg:
		return m, m.handleTargetListFileRead(msg)
	}

	inputIdx, ok := inputIndexForField(m.focusIndex)
	if ok {
		var cmd tea.Cmd
		m.inputs[inputIdx], cmd = m.inputs[inputIdx].Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *ConfigModel) View() string {
	var b strings.Builder

	titleBar := core.TitleBarStyle.Render("  Naabu Scanner  ")
	b.WriteString(titleBar)
	b.WriteString("\n\n")

	labelStyle := lipgloss.NewStyle().Foreground(core.Gold).Width(18)
	focusedLabel := lipgloss.NewStyle().Foreground(core.Ember).Width(18).Bold(true)

	m.renderInputRow(&b, labelStyle, focusedLabel, fieldTargetFile, "Target File:*", inputTargetFile)
	m.renderModeRow(&b, labelStyle, focusedLabel)
	m.renderInputRow(&b, labelStyle, focusedLabel, fieldNmapOptions, "Nmap Options:", inputNmapOptions)
	m.renderInputRow(&b, labelStyle, focusedLabel, fieldWorkerCount, "Worker Count:", inputWorkerCount)
	m.renderInputRow(&b, labelStyle, focusedLabel, fieldComputeMode, "Compute Mode:", inputComputeMode)
	m.renderInputRow(&b, labelStyle, focusedLabel, fieldCloud, "Cloud:", inputCloud)

	b.WriteString("\n")
	submitStyle := core.MutedStyle
	if m.focusIndex == fieldSubmit {
		submitStyle = core.SelectedStyle
	}
	b.WriteString("  " + submitStyle.Render("[ Submit ]"))
	b.WriteString("\n")

	if m.errMsg != "" {
		b.WriteString("\n")
		b.WriteString("  " + core.ErrorStyle.Render(m.errMsg))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(core.StatusBarStyle.Render(m.help.View(configKeys)))

	content := b.String()
	if m.width > 0 && m.height > 0 {
		content = lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
	}
	return content
}

func (m *ConfigModel) renderInputRow(b *strings.Builder, labelStyle, focusedLabel lipgloss.Style, field int, label string, inputIdx int) {
	ls := labelStyle
	if m.focusIndex == field {
		ls = focusedLabel
	}
	fmt.Fprintf(b, "  %s%s", ls.Render(label), m.inputs[inputIdx].View())
	if field == fieldNmapOptions && m.mode == naabutool.ModeDiscovery {
		b.WriteString(" " + core.MutedStyle.Render("(ignored in discovery)"))
	}
	b.WriteString("\n")
}

func (m *ConfigModel) renderModeRow(b *strings.Builder, labelStyle, focusedLabel lipgloss.Style) {
	ls := labelStyle
	if m.focusIndex == fieldMode {
		ls = focusedLabel
	}
	combined := "[ ] combined"
	discovery := "[ ] discovery"
	if m.mode == naabutool.ModeCombined {
		combined = "[x] combined"
	} else {
		discovery = "[x] discovery"
	}
	fmt.Fprintf(b, "  %s%s  %s\n", ls.Render("Mode:"), combined, discovery)
}

func (m *ConfigModel) submit() tea.Cmd {
	path := strings.TrimSpace(m.inputs[inputTargetFile].Value())
	if path == "" {
		m.errMsg = "Target file is required"
		return nil
	}
	workerCount, _ := strconv.Atoi(m.inputs[inputWorkerCount].Value())
	if workerCount <= 0 {
		workerCount = 10
	}
	m.errMsg = ""
	return func() tea.Msg {
		meta, err := targetlisttool.InspectFile(path, targetlisttool.Policy{WorkerCount: workerCount})
		if err != nil {
			return targetListReadMsg{err: err}
		}
		return targetListReadMsg{path: path, meta: meta}
	}
}

func (m *ConfigModel) handleTargetListFileRead(msg targetListReadMsg) tea.Cmd {
	if msg.err != nil {
		m.errMsg = fmt.Sprintf("Error reading target file: %v", msg.err)
		return nil
	}

	workerCount, _ := strconv.Atoi(m.inputs[inputWorkerCount].Value())
	if workerCount <= 0 {
		workerCount = 10
	}
	computeMode := strings.TrimSpace(m.inputs[inputComputeMode].Value())
	if computeMode == "" {
		computeMode = "auto"
	}
	cloudKind, cloudErr := cloud.ParseKind(strings.TrimSpace(m.inputs[inputCloud].Value()))
	if cloudErr != nil {
		m.errMsg = fmt.Sprintf("Invalid cloud: %v", cloudErr)
		return nil
	}
	if err := cloud.ValidateComputeMode(cloudKind, computeMode); err != nil {
		m.errMsg = err.Error()
		return nil
	}

	opCfg, _ := operator.LoadConfig()
	cleanupPolicy := operator.ResolveCleanupPolicy("", opCfg)
	outputDir := operator.ResolveOutputDir("", opCfg)
	placement, placementErr := operator.ResolvePlacementPolicy(fleet.PlacementPolicy{}, opCfg, workerCount)
	if placementErr != nil {
		m.errMsg = placementErr.Error()
		return nil
	}

	toolName := m.mode.ModuleName()
	toolOptions := ""
	if m.mode == naabutool.ModeCombined {
		toolOptions = strings.TrimSpace(m.inputs[inputNmapOptions].Value())
	}

	if cloudKind.IsSelfhostedFamily() && !cloudKind.IsProviderNative() {
		shCfg := factory.SelfhostedConfigFromEnv()
		if shCfg.QueueID == "" || shCfg.Bucket == "" {
			m.errMsg = fmt.Sprintf("%s requires SELFHOSTED_QUEUE_ID and SELFHOSTED_BUCKET environment variables", cloudKind.Canonical())
			return nil
		}
		return func() tea.Msg {
			return core.NavigateWithDataMsg{
				Target: core.ViewGenericStatus,
				Data: core.InfraOutputs{
					Cloud:         cloudKind,
					SQSQueueURL:   shCfg.QueueID,
					S3BucketName:  shCfg.Bucket,
					TargetsPath:   msg.path,
					TargetCount:   msg.meta.TotalTargets,
					TargetChunks:  msg.meta.EffectiveChunks,
					WorkerCount:   workerCount,
					ComputeMode:   computeMode,
					Placement:     placement,
					ToolName:      toolName,
					ToolOptions:   toolOptions,
					CleanupPolicy: cleanupPolicy,
					OutputDir:     outputDir,
					Selfhosted: &core.SelfhostedRuntime{
						WorkerHosts: shCfg.WorkerHosts,
						SSHUser:     shCfg.SSHUser,
						DockerImage: shCfg.DockerImage,
					},
				},
			}
		}
	}

	tc, err := infra.ResolveToolConfig(toolName, cloudKind)
	if err != nil {
		m.errMsg = fmt.Sprintf("Error resolving tool config: %v", err)
		return nil
	}
	return func() tea.Msg {
		return core.NavigateWithDataMsg{
			Target: core.ViewDeploy,
			Data: core.DeployConfig{
				Cloud:          cloudKind,
				TerraformDir:   tc.TerraformDir,
				Dockerfile:     tc.Dockerfile,
				DockerContext:  tc.DockerCtx,
				DockerTag:      tc.DockerTag,
				ECRRepoName:    tc.ECRRepoName,
				AWSRegion:      infra.AWSRegion(),
				BuildArgs:      tc.BuildArgs,
				TerraformVars:  tc.TerraformVars,
				TargetsPath:    msg.path,
				TargetCount:    msg.meta.TotalTargets,
				TargetChunks:   msg.meta.EffectiveChunks,
				WorkerCount:    workerCount,
				ComputeMode:    computeMode,
				Placement:      placement,
				ToolName:       toolName,
				ToolOptions:    toolOptions,
				PostDeployView: core.ViewGenericStatus,
				CleanupPolicy:  cleanupPolicy,
				OutputDir:      outputDir,
			},
		}
	}
}

func (m *ConfigModel) updateFocus() tea.Cmd {
	var cmds []tea.Cmd
	for i := range m.inputs {
		m.inputs[i].Blur()
	}
	if inputIdx, ok := inputIndexForField(m.focusIndex); ok {
		cmds = append(cmds, m.inputs[inputIdx].Focus())
	}
	return tea.Batch(cmds...)
}

func (m *ConfigModel) toggleMode() {
	if m.mode == naabutool.ModeCombined {
		m.mode = naabutool.ModeDiscovery
	} else {
		m.mode = naabutool.ModeCombined
	}
}

const (
	inputTargetFile = iota
	inputNmapOptions
	inputWorkerCount
	inputComputeMode
	inputCloud
)

func inputIndexForField(field int) (int, bool) {
	switch field {
	case fieldTargetFile:
		return inputTargetFile, true
	case fieldNmapOptions:
		return inputNmapOptions, true
	case fieldWorkerCount:
		return inputWorkerCount, true
	case fieldComputeMode:
		return inputComputeMode, true
	case fieldCloud:
		return inputCloud, true
	default:
		return 0, false
	}
}

func normalizeCloudValue(value string) string {
	kind, err := cloud.ParseKind(value)
	if err != nil {
		return strings.TrimSpace(value)
	}
	return string(kind.Canonical())
}
