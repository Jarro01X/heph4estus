package settings

import (
	"fmt"
	"os"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"heph4estus/internal/tui/core"
)

type keyMap struct {
	Back key.Binding
	Quit key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Back, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Back, k.Quit}}
}

var keys = keyMap{
	Back: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "back"),
	),
	Quit: key.NewBinding(
		key.WithKeys("q", "Q"),
		key.WithHelp("q", "quit"),
	),
}

// Model is the settings view.
type Model struct {
	help   help.Model
	width  int
	height int
}

// New creates a new settings view.
func New() *Model {
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
	return &Model{help: h}
}

func (m *Model) Init() tea.Cmd {
	return nil
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
		case "q", "Q":
			return m, tea.Quit
		}
	}

	return m, nil
}

func (m *Model) View() string {
	var b strings.Builder

	// Title bar
	titleBar := core.TitleBarStyle.Render("  Settings  ")
	b.WriteString(titleBar)
	b.WriteString("\n\n")

	// AWS configuration key-value pairs
	labelStyle := lipgloss.NewStyle().Foreground(core.Gold).Width(22)
	valueStyle := lipgloss.NewStyle().Foreground(core.WhiteHot)

	region := envOrDefault("AWS_REGION", os.Getenv("AWS_DEFAULT_REGION"))
	if region == "" {
		region = "(not set)"
	}
	profile := envOrDefault("AWS_PROFILE", "")
	if profile == "" {
		profile = "(not set)"
	}
	credStatus := detectCredentials()

	fmt.Fprintf(&b, "  %s%s\n", labelStyle.Render("AWS Region:"), valueStyle.Render(region))
	fmt.Fprintf(&b, "  %s%s\n", labelStyle.Render("AWS Profile:"), valueStyle.Render(profile))
	fmt.Fprintf(&b, "  %s%s\n", labelStyle.Render("Credentials:"), valueStyle.Render(credStatus))

	b.WriteString("\n")

	// Help bar
	helpBar := core.StatusBarStyle.Render(m.help.View(keys))
	b.WriteString(helpBar)

	content := b.String()
	if m.width > 0 && m.height > 0 {
		content = lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
	}

	return content
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
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
