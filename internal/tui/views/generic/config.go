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
	"heph4estus/internal/cloud"
	"heph4estus/internal/cloud/factory"
	"heph4estus/internal/fleet"
	"heph4estus/internal/infra"
	"heph4estus/internal/modules"
	"heph4estus/internal/operator"
	wordlisttool "heph4estus/internal/tools/wordlist"
	"heph4estus/internal/tui/core"
)

type fileReadMsg struct {
	content string
	err     error
}

// wordlistReadMsg carries bounded wordlist metadata separately from target files.
type wordlistReadMsg struct {
	path    string
	meta    *wordlisttool.Metadata
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

// Field indices for target_list modules.
const (
	cfgFieldTargetFile = iota
	cfgFieldOptions
	cfgFieldWorkerCount
	cfgFieldComputeMode
	cfgFieldCloud
	cfgFieldSubmit
)

// Field indices for wordlist modules.
const (
	wlFieldWordlistFile = iota
	wlFieldTarget
	wlFieldOptions
	wlFieldChunks
	wlFieldWorkerCount
	wlFieldComputeMode
	wlFieldCloud
	wlFieldSubmit
)

// ConfigModel is the generic tool configuration form view.
type ConfigModel struct {
	toolName   string
	mod        *modules.ModuleDefinition
	isWordlist bool

	// target_list inputs (5 fields)
	inputs [5]textinput.Model

	// wordlist inputs (7 fields)
	wlInputs [7]textinput.Model

	focus      int
	fieldCount int
	help       help.Model
	width      int
	height     int
	errMsg     string
}

// NewConfig creates a generic config view for the given tool name.
func NewConfig(toolName string) *ConfigModel {
	reg, err := modules.NewDefaultRegistry()
	var mod *modules.ModuleDefinition
	if err == nil {
		mod, _ = reg.Get(toolName)
	}

	isWordlist := mod != nil && mod.InputType == modules.InputTypeWordlist

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

	cfg, _ := operator.LoadConfig()
	workers := operator.ResolveWorkers(0, cfg)
	computeMode := operator.ResolveComputeMode("", cfg)
	savedCloud := ""
	if cfg != nil {
		savedCloud = normalizeCloudValue(cfg.Cloud)
	}

	m := &ConfigModel{
		toolName:   toolName,
		mod:        mod,
		isWordlist: isWordlist,
		help:       h,
	}

	if isWordlist {
		m.fieldCount = wlFieldSubmit + 1
		m.wlInputs = buildWordlistInputs(mod, workers, computeMode, savedCloud)
		m.wlInputs[0].Focus()
	} else {
		m.fieldCount = cfgFieldSubmit + 1
		m.inputs = buildTargetListInputs(workers, computeMode, savedCloud)
		m.inputs[0].Focus()
	}

	return m
}

func buildTargetListInputs(workers int, computeMode, savedCloud string) [5]textinput.Model {
	targetInput := textinput.New()
	targetInput.Placeholder = "/path/to/targets.txt"
	targetInput.CharLimit = 256

	optsInput := textinput.New()
	optsInput.Placeholder = "extra flags"
	optsInput.CharLimit = 256

	workerInput := textinput.New()
	workerInput.Placeholder = "10"
	workerInput.SetValue(strconv.Itoa(workers))
	workerInput.CharLimit = 6

	modeInput := textinput.New()
	modeInput.Placeholder = "auto"
	modeInput.SetValue(computeMode)
	modeInput.CharLimit = 7

	cloudInput := textinput.New()
	cloudInput.Placeholder = "aws"
	if savedCloud != "" {
		cloudInput.SetValue(savedCloud)
	}
	cloudInput.CharLimit = 12

	return [5]textinput.Model{targetInput, optsInput, workerInput, modeInput, cloudInput}
}

func buildWordlistInputs(mod *modules.ModuleDefinition, workers int, computeMode, savedCloud string) [7]textinput.Model {
	wlInput := textinput.New()
	wlInput.Placeholder = "/path/to/wordlist.txt"
	wlInput.CharLimit = 256

	targetInput := textinput.New()
	if mod != nil && mod.NeedsTarget() {
		targetInput.Placeholder = "https://example.com/FUZZ"
	} else {
		targetInput.Placeholder = "(optional)"
	}
	targetInput.CharLimit = 256

	optsInput := textinput.New()
	optsInput.Placeholder = "extra flags"
	optsInput.CharLimit = 256

	chunksInput := textinput.New()
	chunksInput.Placeholder = "auto"
	chunksInput.CharLimit = 6

	workerInput := textinput.New()
	workerInput.Placeholder = "10"
	workerInput.SetValue(strconv.Itoa(workers))
	workerInput.CharLimit = 6

	modeInput := textinput.New()
	modeInput.Placeholder = "auto"
	modeInput.SetValue(computeMode)
	modeInput.CharLimit = 7

	cloudInput := textinput.New()
	cloudInput.Placeholder = "aws"
	if savedCloud != "" {
		cloudInput.SetValue(savedCloud)
	}
	cloudInput.CharLimit = 12

	return [7]textinput.Model{wlInput, targetInput, optsInput, chunksInput, workerInput, modeInput, cloudInput}
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
			m.focus = (m.focus + 1) % m.fieldCount
			return m, m.updateFocus()
		case "shift+tab", "up":
			m.focus = (m.focus - 1 + m.fieldCount) % m.fieldCount
			return m, m.updateFocus()
		case "enter":
			submitIdx := cfgFieldSubmit
			if m.isWordlist {
				submitIdx = wlFieldSubmit
			}
			if m.focus == submitIdx {
				return m, m.submit()
			}
			m.focus = (m.focus + 1) % m.fieldCount
			return m, m.updateFocus()
		}

	case fileReadMsg:
		return m, m.handleTargetListFileRead(msg)

	case wordlistReadMsg:
		return m, m.handleWordlistFileRead(msg)
	}

	// Update the focused text input.
	if m.isWordlist {
		if m.focus < len(m.wlInputs) {
			var cmd tea.Cmd
			m.wlInputs[m.focus], cmd = m.wlInputs[m.focus].Update(msg)
			return m, cmd
		}
	} else {
		if m.focus < len(m.inputs) {
			var cmd tea.Cmd
			m.inputs[m.focus], cmd = m.inputs[m.focus].Update(msg)
			return m, cmd
		}
	}
	return m, nil
}

func (m *ConfigModel) handleTargetListFileRead(msg fileReadMsg) tea.Cmd {
	if msg.err != nil {
		m.errMsg = fmt.Sprintf("Error reading file: %v", msg.err)
		return nil
	}
	workerCount, _ := strconv.Atoi(m.inputs[cfgFieldWorkerCount].Value())
	if workerCount <= 0 {
		workerCount = 10
	}
	computeMode := strings.TrimSpace(m.inputs[cfgFieldComputeMode].Value())
	if computeMode == "" {
		computeMode = "auto"
	}
	cloudKind, cloudErr := cloud.ParseKind(strings.TrimSpace(m.inputs[cfgFieldCloud].Value()))
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
	toolOptions := strings.TrimSpace(m.inputs[cfgFieldOptions].Value())

	if cloudKind.IsSelfhostedFamily() && !cloudKind.IsProviderNative() {
		// Manual selfhosted: bypass deploy view, go directly to status.
		shCfg := factory.SelfhostedConfigFromEnv()
		if shCfg.QueueID == "" || shCfg.Bucket == "" {
			m.errMsg = fmt.Sprintf("%s requires SELFHOSTED_QUEUE_ID and SELFHOSTED_BUCKET environment variables", cloudKind.Canonical())
			return nil
		}
		return func() tea.Msg {
			return core.NavigateWithDataMsg{
				Target: core.ViewGenericStatus,
				Data: core.InfraOutputs{
					Cloud:          cloudKind,
					SQSQueueURL:    shCfg.QueueID,
					S3BucketName:   shCfg.Bucket,
					TargetsContent: msg.content,
					WorkerCount:    workerCount,
					ComputeMode:    computeMode,
					Placement:      placement,
					ToolName:       m.toolName,
					ToolOptions:    toolOptions,
					CleanupPolicy:  cleanupPolicy,
					OutputDir:      outputDir,
					Selfhosted: &core.SelfhostedRuntime{
						WorkerHosts: shCfg.WorkerHosts,
						SSHUser:     shCfg.SSHUser,
						DockerImage: shCfg.DockerImage,
					},
				},
			}
		}
	}

	tc, err := infra.ResolveToolConfig(m.toolName, cloudKind)
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
				TargetsContent: msg.content,
				WorkerCount:    workerCount,
				ComputeMode:    computeMode,
				Placement:      placement,
				ToolName:       m.toolName,
				ToolOptions:    toolOptions,
				PostDeployView: core.ViewGenericStatus,
				CleanupPolicy:  cleanupPolicy,
				OutputDir:      outputDir,
			},
		}
	}
}

func (m *ConfigModel) handleWordlistFileRead(msg wordlistReadMsg) tea.Cmd {
	if msg.err != nil {
		m.errMsg = fmt.Sprintf("Error reading wordlist: %v", msg.err)
		return nil
	}

	runtimeTarget := strings.TrimSpace(m.wlInputs[wlFieldTarget].Value())
	if m.mod != nil && m.mod.NeedsTarget() && runtimeTarget == "" {
		m.errMsg = "Target / URL is required for this tool"
		return nil
	}

	workerCount, _ := strconv.Atoi(m.wlInputs[wlFieldWorkerCount].Value())
	if workerCount <= 0 {
		workerCount = 10
	}

	chunkCount, _ := strconv.Atoi(strings.TrimSpace(m.wlInputs[wlFieldChunks].Value()))

	computeMode := strings.TrimSpace(m.wlInputs[wlFieldComputeMode].Value())
	if computeMode == "" {
		computeMode = "auto"
	}
	cloudKind, cloudErr := cloud.ParseKind(strings.TrimSpace(m.wlInputs[wlFieldCloud].Value()))
	if cloudErr != nil {
		m.errMsg = fmt.Sprintf("Invalid cloud: %v", cloudErr)
		return nil
	}
	if err := cloud.ValidateComputeMode(cloudKind, computeMode); err != nil {
		m.errMsg = err.Error()
		return nil
	}

	wlCfg, _ := operator.LoadConfig()
	wlCleanup := operator.ResolveCleanupPolicy("", wlCfg)
	wlOutDir := operator.ResolveOutputDir("", wlCfg)
	placement, placementErr := operator.ResolvePlacementPolicy(fleet.PlacementPolicy{}, wlCfg, workerCount)
	if placementErr != nil {
		m.errMsg = placementErr.Error()
		return nil
	}
	toolOptions := strings.TrimSpace(m.wlInputs[wlFieldOptions].Value())

	if cloudKind.IsSelfhostedFamily() && !cloudKind.IsProviderNative() {
		// Manual selfhosted: bypass deploy view, go directly to status.
		shCfg := factory.SelfhostedConfigFromEnv()
		if shCfg.QueueID == "" || shCfg.Bucket == "" {
			m.errMsg = fmt.Sprintf("%s requires SELFHOSTED_QUEUE_ID and SELFHOSTED_BUCKET environment variables", cloudKind.Canonical())
			return nil
		}
		return func() tea.Msg {
			return core.NavigateWithDataMsg{
				Target: core.ViewGenericStatus,
				Data: core.InfraOutputs{
					Cloud:           cloudKind,
					SQSQueueURL:     shCfg.QueueID,
					S3BucketName:    shCfg.Bucket,
					WorkerCount:     workerCount,
					ComputeMode:     computeMode,
					Placement:       placement,
					ToolName:        m.toolName,
					ToolOptions:     toolOptions,
					WordlistPath:    msg.path,
					WordlistContent: msg.content,
					RuntimeTarget:   runtimeTarget,
					ChunkCount:      chunkCount,
					CleanupPolicy:   wlCleanup,
					OutputDir:       wlOutDir,
					Selfhosted: &core.SelfhostedRuntime{
						WorkerHosts: shCfg.WorkerHosts,
						SSHUser:     shCfg.SSHUser,
						DockerImage: shCfg.DockerImage,
					},
				},
			}
		}
	}

	tc, err := infra.ResolveToolConfig(m.toolName, cloudKind)
	if err != nil {
		m.errMsg = fmt.Sprintf("Error resolving tool config: %v", err)
		return nil
	}
	return func() tea.Msg {
		return core.NavigateWithDataMsg{
			Target: core.ViewDeploy,
			Data: core.DeployConfig{
				Cloud:           cloudKind,
				TerraformDir:    tc.TerraformDir,
				Dockerfile:      tc.Dockerfile,
				DockerContext:   tc.DockerCtx,
				DockerTag:       tc.DockerTag,
				ECRRepoName:     tc.ECRRepoName,
				AWSRegion:       infra.AWSRegion(),
				BuildArgs:       tc.BuildArgs,
				TerraformVars:   tc.TerraformVars,
				WorkerCount:     workerCount,
				ComputeMode:     computeMode,
				Placement:       placement,
				ToolName:        m.toolName,
				ToolOptions:     toolOptions,
				PostDeployView:  core.ViewGenericStatus,
				WordlistPath:    msg.path,
				WordlistContent: msg.content,
				RuntimeTarget:   runtimeTarget,
				ChunkCount:      chunkCount,
				CleanupPolicy:   wlCleanup,
				OutputDir:       wlOutDir,
			},
		}
	}
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

	if m.isWordlist {
		labels := []string{"Wordlist File:*", "Target / URL:*", "Extra Options:", "Chunks:", "Worker Count:", "Compute Mode:", "Cloud:"}
		if m.mod != nil && !m.mod.NeedsTarget() {
			labels[1] = "Target / URL:"
		}
		for i, label := range labels {
			ls := labelStyle
			if m.focus == i {
				ls = focusedLabel
			}
			fmt.Fprintf(&b, "  %s%s\n", ls.Render(label), m.wlInputs[i].View())
		}
	} else {
		labels := []string{"Target File:*", "Extra Options:", "Worker Count:", "Compute Mode:", "Cloud:"}
		for i, label := range labels {
			ls := labelStyle
			if m.focus == i {
				ls = focusedLabel
			}
			fmt.Fprintf(&b, "  %s%s\n", ls.Render(label), m.inputs[i].View())
		}
	}

	b.WriteString("\n")

	submitIdx := cfgFieldSubmit
	if m.isWordlist {
		submitIdx = wlFieldSubmit
	}
	submitStyle := core.MutedStyle
	if m.focus == submitIdx {
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
	if m.isWordlist {
		for i := range m.wlInputs {
			if i == m.focus {
				cmds = append(cmds, m.wlInputs[i].Focus())
			} else {
				m.wlInputs[i].Blur()
			}
		}
	} else {
		for i := range m.inputs {
			if i == m.focus {
				cmds = append(cmds, m.inputs[i].Focus())
			} else {
				m.inputs[i].Blur()
			}
		}
	}
	if len(cmds) == 0 {
		if m.isWordlist {
			for i := range m.wlInputs {
				m.wlInputs[i].Blur()
			}
		} else {
			for i := range m.inputs {
				m.inputs[i].Blur()
			}
		}
	}
	return tea.Batch(cmds...)
}

func normalizeCloudValue(value string) string {
	kind, err := cloud.ParseKind(value)
	if err != nil {
		return strings.TrimSpace(value)
	}
	return string(kind.Canonical())
}

func (m *ConfigModel) submit() tea.Cmd {
	if m.isWordlist {
		return m.submitWordlist()
	}
	return m.submitTargetList()
}

func (m *ConfigModel) submitTargetList() tea.Cmd {
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

func (m *ConfigModel) submitWordlist() tea.Cmd {
	wlPath := strings.TrimSpace(m.wlInputs[wlFieldWordlistFile].Value())
	if wlPath == "" {
		m.errMsg = "Wordlist file is required"
		return nil
	}

	runtimeTarget := strings.TrimSpace(m.wlInputs[wlFieldTarget].Value())
	if m.mod != nil && m.mod.NeedsTarget() && runtimeTarget == "" {
		m.errMsg = "Target / URL is required for this tool"
		return nil
	}

	chunksStr := strings.TrimSpace(m.wlInputs[wlFieldChunks].Value())
	requestedChunks := 0
	if chunksStr != "" {
		v, err := strconv.Atoi(chunksStr)
		if err != nil || v <= 0 {
			m.errMsg = "Chunks must be a positive number"
			return nil
		}
		requestedChunks = v
	}

	workerCount, _ := strconv.Atoi(m.wlInputs[wlFieldWorkerCount].Value())
	if workerCount <= 0 {
		workerCount = 10
	}

	m.errMsg = ""
	return func() tea.Msg {
		meta, err := wordlisttool.InspectFile(wlPath, wordlisttool.Policy{
			RequestedChunks: requestedChunks,
			WorkerCount:     workerCount,
		})
		if err != nil {
			return wordlistReadMsg{err: err}
		}
		return wordlistReadMsg{path: wlPath, meta: meta}
	}
}
