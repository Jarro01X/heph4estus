package generic

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"heph4estus/internal/modules"
	"heph4estus/internal/tui/core"
)

type fileReadMsg struct {
	content string
	err     error
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
	Enter: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "submit")),
	Back:  key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
	Quit:  key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit")),
}

const (
	cfgFieldTargetFile = iota
	cfgFieldOptions
	cfgFieldWorkerCount
	cfgFieldComputeMode
	cfgFieldSubmit
	cfgFieldCount
)

// ConfigModel is the generic tool configuration form view.
type ConfigModel struct {
	toolName string
	mod      *modules.ModuleDefinition
	inputs   [4]textinput.Model
	focus    int
	help     help.Model
	width    int
	height   int
	errMsg   string
}

// NewConfig creates a generic config view for the given tool name.
func NewConfig(toolName string) *ConfigModel {
	reg, err := modules.NewDefaultRegistry()
	var mod *modules.ModuleDefinition
	if err == nil {
		mod, _ = reg.Get(toolName)
	}

	targetInput := textinput.New()
	targetInput.Placeholder = "/path/to/targets.txt"
	targetInput.Focus()
	targetInput.CharLimit = 256

	optsInput := textinput.New()
	optsInput.Placeholder = "extra flags"
	optsInput.CharLimit = 256

	workerInput := textinput.New()
	workerInput.Placeholder = "10"
	workerInput.SetValue("10")
	workerInput.CharLimit = 6

	modeInput := textinput.New()
	modeInput.Placeholder = "auto"
	modeInput.SetValue("auto")
	modeInput.CharLimit = 7

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
		toolName: toolName,
		mod:      mod,
		inputs:   [4]textinput.Model{targetInput, optsInput, workerInput, modeInput},
		help:     h,
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
			m.focus = (m.focus + 1) % cfgFieldCount
			return m, m.updateFocus()
		case "shift+tab", "up":
			m.focus = (m.focus - 1 + cfgFieldCount) % cfgFieldCount
			return m, m.updateFocus()
		case "enter":
			if m.focus == cfgFieldSubmit {
				return m, m.submit()
			}
			m.focus = (m.focus + 1) % cfgFieldCount
			return m, m.updateFocus()
		}

	case fileReadMsg:
		if msg.err != nil {
			m.errMsg = fmt.Sprintf("Error reading file: %v", msg.err)
			return m, nil
		}
		workerCount, _ := strconv.Atoi(m.inputs[cfgFieldWorkerCount].Value())
		if workerCount <= 0 {
			workerCount = 10
		}
		computeMode := strings.TrimSpace(m.inputs[cfgFieldComputeMode].Value())
		if computeMode == "" {
			computeMode = "auto"
		}
		if computeMode != "auto" && computeMode != "fargate" && computeMode != "spot" {
			m.errMsg = "Compute mode must be auto, fargate, or spot"
			return m, nil
		}

		buildArgs := installCmdToBuildArgs(m.mod)
		tfVars := map[string]string{"tool_name": m.toolName}
		if m.mod != nil {
			tfVars["task_cpu"] = fmt.Sprintf("%d", m.mod.DefaultCPU)
			tfVars["task_memory"] = fmt.Sprintf("%d", m.mod.DefaultMemory)
		}
		return m, func() tea.Msg {
			return core.NavigateWithDataMsg{
				Target: core.ViewDeploy,
				Data: core.DeployConfig{
					TerraformDir:  "deployments/aws/generic/environments/dev",
					Dockerfile:    "containers/generic/Dockerfile",
					DockerContext: ".",
					DockerTag:     fmt.Sprintf("heph-%s-worker:latest", m.toolName),
					ECRRepoName:   fmt.Sprintf("heph-dev-%s", m.toolName),
					AWSRegion:     awsRegion(),
					BuildArgs:     buildArgs,
					TerraformVars: tfVars,
					TargetsContent: msg.content,
					WorkerCount:    workerCount,
					ComputeMode:    computeMode,
					ToolName:       m.toolName,
					ToolOptions:    strings.TrimSpace(m.inputs[cfgFieldOptions].Value()),
					PostDeployView: core.ViewGenericStatus,
				},
			}
		}
	}

	if m.focus < len(m.inputs) {
		var cmd tea.Cmd
		m.inputs[m.focus], cmd = m.inputs[m.focus].Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *ConfigModel) View() string {
	var b strings.Builder

	title := m.toolName
	if m.mod != nil {
		title = fmt.Sprintf("%s — %s", m.toolName, m.mod.Description)
	}
	titleBar := core.TitleBarStyle.Render("  " + title + "  ")
	b.WriteString(titleBar)
	b.WriteString("\n\n")

	labelStyle := lipgloss.NewStyle().Foreground(core.Gold).Width(18)
	focusedLabel := lipgloss.NewStyle().Foreground(core.Ember).Width(18).Bold(true)

	labels := []string{"Target File:*", "Extra Options:", "Worker Count:", "Compute Mode:"}
	for i, label := range labels {
		ls := labelStyle
		if m.focus == i {
			ls = focusedLabel
		}
		fmt.Fprintf(&b, "  %s%s\n", ls.Render(label), m.inputs[i].View())
	}

	b.WriteString("\n")

	submitStyle := core.MutedStyle
	if m.focus == cfgFieldSubmit {
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
	helpBar := core.StatusBarStyle.Render(m.help.View(configKeys))
	b.WriteString(helpBar)

	content := b.String()
	if m.width > 0 && m.height > 0 {
		content = lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
	}
	return content
}

func (m *ConfigModel) updateFocus() tea.Cmd {
	var cmds []tea.Cmd
	for i := range m.inputs {
		if i == m.focus {
			cmds = append(cmds, m.inputs[i].Focus())
		} else {
			m.inputs[i].Blur()
		}
	}
	if len(cmds) == 0 {
		for i := range m.inputs {
			m.inputs[i].Blur()
		}
	}
	return tea.Batch(cmds...)
}

func (m *ConfigModel) submit() tea.Cmd {
	if m.mod != nil && m.mod.InputType == modules.InputTypeWordlist {
		m.errMsg = fmt.Sprintf("%s requires wordlist input — planned for PR 5.7", m.toolName)
		return nil
	}

	path := strings.TrimSpace(m.inputs[cfgFieldTargetFile].Value())
	if path == "" {
		m.errMsg = "Target file is required"
		return nil
	}
	m.errMsg = ""
	return func() tea.Msg {
		data, err := os.ReadFile(path)
		if err != nil {
			return fileReadMsg{err: err}
		}
		return fileReadMsg{content: string(data)}
	}
}

func installCmdToBuildArgs(mod *modules.ModuleDefinition) map[string]string {
	if mod == nil {
		return nil
	}
	if strings.HasPrefix(mod.InstallCmd, "go install ") {
		return map[string]string{"GO_INSTALL_CMD": mod.InstallCmd}
	}
	return map[string]string{"RUNTIME_INSTALL_CMD": mod.InstallCmd}
}

func awsRegion() string {
	if r := os.Getenv("AWS_REGION"); r != "" {
		return r
	}
	if r := os.Getenv("AWS_DEFAULT_REGION"); r != "" {
		return r
	}
	return "us-east-1"
}
