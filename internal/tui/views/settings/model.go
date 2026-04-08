package settings

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"heph4estus/internal/doctor"
	"heph4estus/internal/operator"
	"heph4estus/internal/tui/core"
)

// Field indices for the editable settings form.
const (
	fieldRegion = iota
	fieldProfile
	fieldWorkerCount
	fieldComputeMode
	fieldCleanupPolicy
	fieldOutputDir
	fieldSave
	fieldRefreshDiag
	fieldCount
)

type keyMap struct {
	Tab  key.Binding
	Save key.Binding
	Back key.Binding
	Quit key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Tab, k.Save, k.Back, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Tab, k.Save, k.Back, k.Quit}}
}

var keys = keyMap{
	Tab: key.NewBinding(
		key.WithKeys("tab", "shift+tab"),
		key.WithHelp("tab", "next field"),
	),
	Save: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "select/save"),
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

// diagnosticResultMsg carries doctor check results back to the view.
type diagnosticResultMsg struct {
	results []doctor.CheckResult
}

// configSavedMsg signals a save completed.
type configSavedMsg struct {
	err error
}

// Deps abstracts external dependencies for testability.
type Deps struct {
	LoadConfig func() (*operator.OperatorConfig, error)
	SaveConfig func(*operator.OperatorConfig) error
	RunDoctor  func(ctx context.Context) []doctor.CheckResult
}

// DefaultDeps returns production dependencies.
func DefaultDeps() Deps {
	return Deps{
		LoadConfig: operator.LoadConfig,
		SaveConfig: operator.SaveConfig,
		RunDoctor: func(ctx context.Context) []doctor.CheckResult {
			return doctor.RunAll(ctx, doctor.DefaultDeps())
		},
	}
}

// Model is the editable settings view with diagnostics.
type Model struct {
	deps       Deps
	inputs     [6]textinput.Model
	focusIndex int
	help       help.Model
	width      int
	height     int

	statusMsg string // feedback after save
	diagResults []doctor.CheckResult
	diagLoading bool
}

// New creates a new settings view that loads saved operator defaults.
func New() *Model {
	return NewWithDeps(DefaultDeps())
}

// NewWithDeps creates a settings view with injected dependencies (for testing).
func NewWithDeps(deps Deps) *Model {
	cfg, _ := deps.LoadConfig()
	if cfg == nil {
		cfg = &operator.OperatorConfig{}
	}

	regionInput := textinput.New()
	regionInput.Placeholder = "us-east-1"
	regionInput.CharLimit = 20
	regionInput.SetValue(cfg.Region)
	regionInput.Focus()

	profileInput := textinput.New()
	profileInput.Placeholder = "(default chain)"
	profileInput.CharLimit = 64
	profileInput.SetValue(cfg.Profile)

	workerInput := textinput.New()
	workerInput.Placeholder = "10"
	workerInput.CharLimit = 6
	if cfg.WorkerCount > 0 {
		workerInput.SetValue(strconv.Itoa(cfg.WorkerCount))
	}

	modeInput := textinput.New()
	modeInput.Placeholder = "auto"
	modeInput.CharLimit = 7
	if cfg.ComputeMode != "" {
		modeInput.SetValue(cfg.ComputeMode)
	}

	cleanupInput := textinput.New()
	cleanupInput.Placeholder = "reuse"
	cleanupInput.CharLimit = 14
	if cfg.CleanupPolicy != "" {
		cleanupInput.SetValue(cfg.CleanupPolicy)
	}

	outputInput := textinput.New()
	outputInput.Placeholder = "(results stay in S3)"
	outputInput.CharLimit = 256
	outputInput.SetValue(cfg.OutputDir)

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
		deps:   deps,
		inputs: [6]textinput.Model{regionInput, profileInput, workerInput, modeInput, cleanupInput, outputInput},
		help:   h,
	}
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, m.runDiagnostics())
}

func (m *Model) Update(msg tea.Msg) (core.View, tea.Cmd) {
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
		case "ctrl+c":
			return m, tea.Quit
		case "tab", "down":
			m.focusIndex = (m.focusIndex + 1) % fieldCount
			return m, m.updateFocus()
		case "shift+tab", "up":
			m.focusIndex = (m.focusIndex - 1 + fieldCount) % fieldCount
			return m, m.updateFocus()
		case "enter":
			if m.focusIndex == fieldSave {
				return m, m.save()
			}
			if m.focusIndex == fieldRefreshDiag {
				m.diagLoading = true
				return m, m.runDiagnostics()
			}
			// Move to next field
			m.focusIndex = (m.focusIndex + 1) % fieldCount
			return m, m.updateFocus()
		}

	case configSavedMsg:
		if msg.err != nil {
			m.statusMsg = fmt.Sprintf("Save failed: %v", msg.err)
		} else {
			m.statusMsg = "Settings saved"
		}
		return m, nil

	case diagnosticResultMsg:
		m.diagResults = msg.results
		m.diagLoading = false
		return m, nil
	}

	// Forward to focused textinput
	if m.focusIndex < len(m.inputs) {
		var cmd tea.Cmd
		m.inputs[m.focusIndex], cmd = m.inputs[m.focusIndex].Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *Model) View() string {
	var b strings.Builder

	titleBar := core.TitleBarStyle.Render("  Settings  ")
	b.WriteString(titleBar)
	b.WriteString("\n\n")

	labelStyle := lipgloss.NewStyle().Foreground(core.Gold).Width(18)
	focusedLabel := lipgloss.NewStyle().Foreground(core.Ember).Width(18).Bold(true)

	// Editable fields
	labels := []string{"Region:", "Profile:", "Worker Count:", "Compute Mode:", "Cleanup Policy:", "Output Dir:"}
	for i, label := range labels {
		ls := labelStyle
		if m.focusIndex == i {
			ls = focusedLabel
		}
		fmt.Fprintf(&b, "  %s%s\n", ls.Render(label), m.inputs[i].View())
	}

	b.WriteString("\n")

	// Save button
	saveStyle := core.MutedStyle
	if m.focusIndex == fieldSave {
		saveStyle = core.SelectedStyle
	}
	b.WriteString("  " + saveStyle.Render("[ Save ]"))

	// Refresh diagnostics button
	diagBtnStyle := core.MutedStyle
	if m.focusIndex == fieldRefreshDiag {
		diagBtnStyle = core.SelectedStyle
	}
	b.WriteString("    " + diagBtnStyle.Render("[ Refresh Diagnostics ]"))
	b.WriteString("\n")

	// Status message
	if m.statusMsg != "" {
		b.WriteString("\n  " + core.SuccessStyle.Render(m.statusMsg) + "\n")
	}

	// Effective environment info
	b.WriteString("\n")
	infoStyle := lipgloss.NewStyle().Foreground(core.Steel)
	region := effectiveRegion(m.inputs[fieldRegion].Value())
	credStatus := detectCredentials()
	fmt.Fprintf(&b, "  %s%s\n", labelStyle.Render("Effective Region:"), infoStyle.Render(region))
	fmt.Fprintf(&b, "  %s%s\n", labelStyle.Render("Credentials:"), infoStyle.Render(credStatus))

	// Diagnostics section
	b.WriteString("\n")
	b.WriteString(core.TitleBarStyle.Render("  Diagnostics  "))
	b.WriteString("\n\n")

	if m.diagLoading {
		b.WriteString("  " + infoStyle.Render("Running checks...") + "\n")
	} else if len(m.diagResults) == 0 {
		b.WriteString("  " + infoStyle.Render("No diagnostics run yet") + "\n")
	} else {
		for _, r := range m.diagResults {
			icon := diagIcon(r.Status)
			line := fmt.Sprintf("  %s %s", icon, r.Summary)
			b.WriteString(line + "\n")
			if r.Fix != "" {
				b.WriteString("    " + infoStyle.Render("Fix: "+r.Fix) + "\n")
			}
		}
	}

	b.WriteString("\n")
	helpBar := core.StatusBarStyle.Render(m.help.View(keys))
	b.WriteString(helpBar)

	content := b.String()
	if m.width > 0 && m.height > 0 {
		content = lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
	}
	return content
}

func (m *Model) updateFocus() tea.Cmd {
	var cmds []tea.Cmd
	for i := range m.inputs {
		if i == m.focusIndex {
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

func (m *Model) save() tea.Cmd {
	cfg := m.buildConfig()
	deps := m.deps
	return func() tea.Msg {
		err := deps.SaveConfig(cfg)
		if err == nil {
			// Apply to current process env so later TUI views see the change.
			operator.ApplyEnvDefaults(cfg)
		}
		return configSavedMsg{err: err}
	}
}

// buildConfig constructs an OperatorConfig from the current input values.
func (m *Model) buildConfig() *operator.OperatorConfig {
	wc, _ := strconv.Atoi(strings.TrimSpace(m.inputs[fieldWorkerCount].Value()))
	return &operator.OperatorConfig{
		Region:        strings.TrimSpace(m.inputs[fieldRegion].Value()),
		Profile:       strings.TrimSpace(m.inputs[fieldProfile].Value()),
		WorkerCount:   wc,
		ComputeMode:   strings.TrimSpace(m.inputs[fieldComputeMode].Value()),
		CleanupPolicy: strings.TrimSpace(m.inputs[fieldCleanupPolicy].Value()),
		OutputDir:     strings.TrimSpace(m.inputs[fieldOutputDir].Value()),
	}
}

func (m *Model) runDiagnostics() tea.Cmd {
	deps := m.deps
	return func() tea.Msg {
		results := deps.RunDoctor(context.Background())
		return diagnosticResultMsg{results: results}
	}
}

func effectiveRegion(saved string) string {
	if v := os.Getenv("AWS_REGION"); v != "" {
		return v + " (env)"
	}
	if v := os.Getenv("AWS_DEFAULT_REGION"); v != "" {
		return v + " (env)"
	}
	if saved != "" {
		return saved + " (saved)"
	}
	return "us-east-1 (default)"
}

func detectCredentials() string {
	if os.Getenv("AWS_ACCESS_KEY_ID") != "" && os.Getenv("AWS_SECRET_ACCESS_KEY") != "" {
		return "environment variables"
	}
	if os.Getenv("AWS_PROFILE") != "" {
		return "profile: " + os.Getenv("AWS_PROFILE")
	}
	home, err := os.UserHomeDir()
	if err == nil {
		if _, err := os.Stat(home + "/.aws/credentials"); err == nil {
			return "~/.aws/credentials"
		}
	}
	return "not found"
}

func diagIcon(s doctor.Status) string {
	switch s {
	case doctor.StatusPass:
		return lipgloss.NewStyle().Foreground(core.Gold).Render("PASS")
	case doctor.StatusWarn:
		return lipgloss.NewStyle().Foreground(core.Molten).Render("WARN")
	case doctor.StatusFail:
		return lipgloss.NewStyle().Foreground(core.Slag).Render("FAIL")
	default:
		return "????"
	}
}
