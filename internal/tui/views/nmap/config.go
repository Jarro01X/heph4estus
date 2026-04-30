package nmap

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
	"heph4estus/internal/operator"
	"heph4estus/internal/tui/core"
)

// fileReadMsg carries the result of reading a target file from disk.
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
	Tab: key.NewBinding(
		key.WithKeys("tab", "shift+tab"),
		key.WithHelp("tab", "next field"),
	),
	Enter: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "submit"),
	),
	Back: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "back"),
	),
	Quit: key.NewBinding(
		key.WithKeys("ctrl+c"),
		key.WithHelp("ctrl+c", "quit"),
	),
}

const (
	fieldTargetFile = iota
	fieldNmapOptions
	fieldWorkerCount
	fieldComputeMode
	fieldJitterMax
	fieldTimingTemplate
	fieldDNSServers
	fieldCloud
	fieldNoRDNS
	fieldSubmit
	fieldCount
)

// ConfigModel is the nmap configuration form view.
type ConfigModel struct {
	inputs     [8]textinput.Model
	noRDNS     bool
	focusIndex int
	help       help.Model
	width      int
	height     int
	errMsg     string
}

// NewConfig creates a new nmap config view.
func NewConfig() *ConfigModel {
	cfg, _ := operator.LoadConfig()

	targetInput := textinput.New()
	targetInput.Placeholder = "/path/to/targets.txt"
	targetInput.Focus()
	targetInput.CharLimit = 256

	optsInput := textinput.New()
	optsInput.Placeholder = "-sS"
	optsInput.SetValue("-sS")
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

	jitterInput := textinput.New()
	jitterInput.Placeholder = "0"
	jitterInput.SetValue("0")
	jitterInput.CharLimit = 4

	timingInput := textinput.New()
	timingInput.Placeholder = ""
	timingInput.CharLimit = 1

	dnsInput := textinput.New()
	dnsInput.Placeholder = ""
	dnsInput.CharLimit = 128

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
		inputs: [8]textinput.Model{targetInput, optsInput, workerInput, modeInput, jitterInput, timingInput, dnsInput, cloudInput},
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
			if m.focusIndex == fieldNoRDNS {
				m.noRDNS = !m.noRDNS
				return m, nil
			}
			// Move to next field on enter in input fields
			m.focusIndex = (m.focusIndex + 1) % fieldCount
			return m, m.updateFocus()
		case " ":
			if m.focusIndex == fieldNoRDNS {
				m.noRDNS = !m.noRDNS
				return m, nil
			}
		}

	case fileReadMsg:
		if msg.err != nil {
			m.errMsg = fmt.Sprintf("Error reading file: %v", msg.err)
			return m, nil
		}
		workerCount, _ := strconv.Atoi(m.inputs[fieldWorkerCount].Value())
		if workerCount <= 0 {
			workerCount = 10
		}
		computeMode := strings.TrimSpace(m.inputs[fieldComputeMode].Value())
		if computeMode == "" {
			computeMode = "auto"
		}
		cloudKind, cloudErr := cloud.ParseKind(strings.TrimSpace(m.inputs[fieldCloud].Value()))
		if cloudErr != nil {
			m.errMsg = fmt.Sprintf("Invalid cloud: %v", cloudErr)
			return m, nil
		}
		if err := cloud.ValidateComputeMode(cloudKind, computeMode); err != nil {
			m.errMsg = err.Error()
			return m, nil
		}
		jitterMax, _ := strconv.Atoi(strings.TrimSpace(m.inputs[fieldJitterMax].Value()))
		if jitterMax < 0 {
			jitterMax = 0
		}

		opCfg, _ := operator.LoadConfig()
		cleanupPolicy := operator.ResolveCleanupPolicy("", opCfg)
		outputDir := operator.ResolveOutputDir("", opCfg)
		placement, placementErr := operator.ResolvePlacementPolicy(fleet.PlacementPolicy{}, opCfg, workerCount)
		if placementErr != nil {
			m.errMsg = placementErr.Error()
			return m, nil
		}

		if cloudKind.IsSelfhostedFamily() && !cloudKind.IsProviderNative() {
			// Manual selfhosted: bypass deploy view, go directly to status.
			shCfg := factory.SelfhostedConfigFromEnv()
			if shCfg.QueueID == "" || shCfg.Bucket == "" {
				m.errMsg = fmt.Sprintf("%s requires SELFHOSTED_QUEUE_ID and SELFHOSTED_BUCKET environment variables", cloudKind.Canonical())
				return m, nil
			}
			return m, func() tea.Msg {
				return core.NavigateWithDataMsg{
					Target: core.ViewNmapStatus,
					Data: core.InfraOutputs{
						Cloud:              cloudKind,
						SQSQueueURL:        shCfg.QueueID,
						S3BucketName:       shCfg.Bucket,
						TargetsContent:     msg.content,
						NmapOptions:        m.inputs[fieldNmapOptions].Value(),
						WorkerCount:        workerCount,
						ComputeMode:        computeMode,
						Placement:          placement,
						JitterMaxSeconds:   jitterMax,
						NmapTimingTemplate: strings.TrimSpace(m.inputs[fieldTimingTemplate].Value()),
						DNSServers:         strings.TrimSpace(m.inputs[fieldDNSServers].Value()),
						NoRDNS:             m.noRDNS,
						CleanupPolicy:      cleanupPolicy,
						OutputDir:          outputDir,
						Selfhosted: &core.SelfhostedRuntime{
							WorkerHosts: shCfg.WorkerHosts,
							SSHUser:     shCfg.SSHUser,
							DockerImage: shCfg.DockerImage,
						},
					},
				}
			}
		}

		tc, err := infra.ResolveToolConfig("nmap", cloudKind)
		if err != nil {
			m.errMsg = fmt.Sprintf("Error resolving nmap config: %v", err)
			return m, nil
		}
		return m, func() tea.Msg {
			return core.NavigateWithDataMsg{
				Target: core.ViewDeploy,
				Data: core.DeployConfig{
					Cloud:              cloudKind,
					TerraformDir:       tc.TerraformDir,
					Dockerfile:         tc.Dockerfile,
					DockerContext:      tc.DockerCtx,
					DockerTag:          tc.DockerTag,
					ECRRepoName:        tc.ECRRepoName,
					AWSRegion:          infra.AWSRegion(),
					BuildArgs:          tc.BuildArgs,
					TerraformVars:      tc.TerraformVars,
					TargetsContent:     msg.content,
					NmapOptions:        m.inputs[fieldNmapOptions].Value(),
					WorkerCount:        workerCount,
					ComputeMode:        computeMode,
					Placement:          placement,
					JitterMaxSeconds:   jitterMax,
					NmapTimingTemplate: strings.TrimSpace(m.inputs[fieldTimingTemplate].Value()),
					DNSServers:         strings.TrimSpace(m.inputs[fieldDNSServers].Value()),
					NoRDNS:             m.noRDNS,
					CleanupPolicy:      cleanupPolicy,
					OutputDir:          outputDir,
				},
			}
		}
	}

	// Forward to focused textinput
	if m.focusIndex < len(m.inputs) {
		var cmd tea.Cmd
		m.inputs[m.focusIndex], cmd = m.inputs[m.focusIndex].Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *ConfigModel) View() string {
	var b strings.Builder

	titleBar := core.TitleBarStyle.Render("  Nmap Scanner  ")
	b.WriteString(titleBar)
	b.WriteString("\n\n")

	labelStyle := lipgloss.NewStyle().Foreground(core.Gold).Width(18)
	focusedLabel := lipgloss.NewStyle().Foreground(core.Ember).Width(18).Bold(true)

	labels := []string{"Target File:*", "Nmap Options:", "Worker Count:", "Compute Mode:", "Jitter Max (s):", "Timing (-T):", "DNS Servers:", "Cloud:"}
	for i, label := range labels {
		ls := labelStyle
		if m.focusIndex == i {
			ls = focusedLabel
		}
		fmt.Fprintf(&b, "  %s%s\n", ls.Render(label), m.inputs[i].View())
	}

	// Disable rDNS toggle
	rdnsLabel := labelStyle
	if m.focusIndex == fieldNoRDNS {
		rdnsLabel = focusedLabel
	}
	check := "[ ]"
	if m.noRDNS {
		check = "[x]"
	}
	fmt.Fprintf(&b, "  %s%s Disable rDNS (-n)\n", rdnsLabel.Render(""), check)

	b.WriteString("\n")

	// Submit button
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
		if i == m.focusIndex {
			cmds = append(cmds, m.inputs[i].Focus())
		} else {
			m.inputs[i].Blur()
		}
	}
	if len(cmds) == 0 {
		// Focus is on submit button — blur all
		for i := range m.inputs {
			m.inputs[i].Blur()
		}
	}
	return tea.Batch(cmds...)
}

func (m *ConfigModel) submit() tea.Cmd {
	path := strings.TrimSpace(m.inputs[fieldTargetFile].Value())
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

func normalizeCloudValue(value string) string {
	kind, err := cloud.ParseKind(value)
	if err != nil {
		return strings.TrimSpace(value)
	}
	return string(kind.Canonical())
}
