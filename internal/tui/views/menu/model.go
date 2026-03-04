package menu

import (
	"fmt"
	"io"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"heph4estus/internal/tui/core"
)

var anvil = strings.TrimRight(`
          ╔═══╗
         ╔╝   ╚╗
         ║     ║
    ╔════╩═════╩════╗
    ║               ║
    ╚═══╗       ╔═══╝
   ═════╩═══════╩═════`, "\n")

type menuItem struct {
	title   string
	enabled bool
	target  core.ViewID
}

func (i menuItem) FilterValue() string { return i.title }

type itemDelegate struct{}

func (d itemDelegate) Height() int                             { return 1 }
func (d itemDelegate) Spacing() int                            { return 0 }
func (d itemDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d itemDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	mi, ok := item.(menuItem)
	if !ok {
		return
	}

	selected := index == m.Index()
	var line string

	if mi.enabled {
		if selected {
			line = core.SelectedStyle.Render("► " + mi.title)
		} else {
			line = core.NormalStyle.Render("  " + mi.title)
		}
	} else {
		label := "  " + mi.title + " (coming soon)"
		if selected {
			label = "► " + mi.title + " (coming soon)"
		}
		line = core.MutedStyle.Render(label)
	}

	fmt.Fprint(w, line)
}

type keyMap struct {
	Up    key.Binding
	Down  key.Binding
	Enter key.Binding
	Quit  key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Enter, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Up, k.Down, k.Enter, k.Quit}}
}

var keys = keyMap{
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "down"),
	),
	Enter: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "select"),
	),
	Quit: key.NewBinding(
		key.WithKeys("q", "Q"),
		key.WithHelp("q", "quit"),
	),
}

// Model is the main menu view.
type Model struct {
	list   list.Model
	help   help.Model
	width  int
	height int
}

// New creates a new menu view.
func New() *Model {
	items := []list.Item{
		menuItem{title: "Nmap Scanner", enabled: false, target: core.ViewNmapConfig},
		menuItem{title: "Naabu + Nmap", enabled: false, target: core.ViewNaabuConfig},
		menuItem{title: "Settings", enabled: true, target: core.ViewSettings},
	}

	delegate := itemDelegate{}
	l := list.New(items, delegate, 40, 6)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowFilter(false)
	l.SetShowPagination(false)
	l.SetShowHelp(false)
	l.SetFilteringEnabled(false)

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
		list: l,
		help: h,
	}
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
		m.list.SetSize(msg.Width, 6)

	case tea.KeyPressMsg:
		switch msg.String() {
		case "q", "Q":
			return m, tea.Quit
		case "enter":
			item, ok := m.list.SelectedItem().(menuItem)
			if ok && item.enabled {
				return m, func() tea.Msg {
					return core.NavigateMsg{Target: item.target}
				}
			}
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m *Model) View() string {
	var b strings.Builder

	// Anvil in Ember
	art := lipgloss.NewStyle().Foreground(core.Ember).Render(anvil)
	b.WriteString(art)
	b.WriteString("\n\n")

	// HEPH4ESTUS title — "4" in Ember, rest in Gold
	title := core.TitleStyle.Render("HEPH") +
		lipgloss.NewStyle().Foreground(core.Ember).Bold(true).Render("4") +
		core.TitleStyle.Render("ESTUS")
	b.WriteString(title)
	b.WriteString("\n\n")

	// Menu list
	b.WriteString(m.list.View())
	b.WriteString("\n")

	// Help bar
	helpBar := core.StatusBarStyle.Render(m.help.View(keys))
	b.WriteString(helpBar)

	// Center the whole block
	content := b.String()
	if m.width > 0 && m.height > 0 {
		content = lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
	}

	return content
}
