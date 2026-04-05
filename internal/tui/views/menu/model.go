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
	"heph4estus/internal/modules"
	"heph4estus/internal/tui/core"
)

// Block-letter title art — "HEPH4ESTUS" in ANSI Shadow style.
// Rune columns 0-31 = "HEPH", 32-39 = "4", 40+ = "ESTUS".
var titleArt = [6]string{
	"██╗  ██╗███████╗██████╗ ██╗  ██╗██╗  ██╗███████╗███████╗████████╗██╗   ██╗███████╗",
	"██║  ██║██╔════╝██╔══██╗██║  ██║██║  ██║██╔════╝██╔════╝╚══██╔══╝██║   ██║██╔════╝",
	"███████║█████╗  ██████╔╝███████║███████║█████╗  ███████╗   ██║   ██║   ██║███████╗",
	"██╔══██║██╔══╝  ██╔═══╝ ██╔══██║╚════██║██╔══╝  ╚════██║   ██║   ██║   ██║╚════██║",
	"██║  ██║███████╗██║     ██║  ██║     ██║███████╗███████║   ██║   ╚██████╔╝███████║",
	"╚═╝  ╚═╝╚══════╝╚═╝     ╚═╝  ╚═╝     ╚═╝╚══════╝╚══════╝   ╚═╝    ╚═════╝ ╚══════╝",
}

type menuItem struct {
	title    string
	enabled  bool
	target   core.ViewID
	toolName string // non-empty for generic module entries
	hint     string // shown after title in muted style (e.g. "coming soon")
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
	hint := mi.hint
	var line string

	if mi.enabled {
		if selected {
			line = core.SelectedStyle.Render("► " + mi.title)
		} else {
			line = core.NormalStyle.Render("  " + mi.title)
		}
	} else {
		if hint == "" {
			hint = "coming soon"
		}
		label := fmt.Sprintf("  %s (%s)", mi.title, hint)
		if selected {
			label = fmt.Sprintf("► %s (%s)", mi.title, hint)
		}
		line = core.MutedStyle.Render(label)
	}

	fmt.Fprint(w, line) //nolint:errcheck // bubbles list delegate signature doesn't support error returns
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

// New creates a new menu view, populated from the module registry.
func New() *Model {
	items := buildMenuItems()

	delegate := itemDelegate{}
	l := list.New(items, delegate, 50, len(items)+1)
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

// buildMenuItems populates the menu from the module registry.
func buildMenuItems() []list.Item {
	reg, err := modules.NewDefaultRegistry()
	if err != nil {
		// Fallback to hardcoded items if registry fails.
		return []list.Item{
			menuItem{title: "Nmap Scanner", enabled: true, target: core.ViewNmapConfig},
			menuItem{title: "Settings", enabled: true, target: core.ViewSettings},
		}
	}

	var items []list.Item
	for _, mod := range reg.List() {
		switch {
		case mod.Name == "nmap":
			// Route nmap to its dedicated config view.
			items = append(items, menuItem{
				title:   "nmap — " + mod.Description,
				enabled: true,
				target:  core.ViewNmapConfig,
			})
		case mod.InputType == modules.InputTypeWordlist:
			// Wordlist modules are visible but disabled until PR 5.7.
			items = append(items, menuItem{
				title:    mod.Name + " — " + mod.Description,
				enabled:  false,
				toolName: mod.Name,
				hint:     "wordlist — PR 5.7",
			})
		default:
			// target_list modules route to the generic config flow.
			items = append(items, menuItem{
				title:    mod.Name + " — " + mod.Description,
				enabled:  true,
				target:   core.ViewGenericConfig,
				toolName: mod.Name,
			})
		}
	}

	// Settings is always last.
	items = append(items, menuItem{title: "Settings", enabled: true, target: core.ViewSettings})
	return items
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
		// Reserve space for title art (8 lines) and help bar (2 lines).
		listHeight := max(msg.Height-10, len(m.list.Items())+1)
		m.list.SetSize(msg.Width, listHeight)

	case tea.KeyPressMsg:
		switch msg.String() {
		case "q", "Q":
			return m, tea.Quit
		case "enter":
			item, ok := m.list.SelectedItem().(menuItem)
			if ok && item.enabled {
				// Generic modules pass tool name via NavigateWithDataMsg.
				if item.target == core.ViewGenericConfig && item.toolName != "" {
					toolName := item.toolName
					return m, func() tea.Msg {
						return core.NavigateWithDataMsg{
							Target: core.ViewGenericConfig,
							Data:   toolName,
						}
					}
				}
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

	// HEPH4ESTUS title — block art when wide enough, plain text fallback
	goldStyle := lipgloss.NewStyle().Foreground(core.Gold).Bold(true)
	emberStyle := lipgloss.NewStyle().Foreground(core.Ember).Bold(true)
	if m.width == 0 || m.width >= 84 {
		for _, line := range titleArt {
			runes := []rune(line)
			heph := string(runes[:32])
			four := string(runes[32:40])
			estus := string(runes[40:])
			b.WriteString(goldStyle.Render(heph) + emberStyle.Render(four) + goldStyle.Render(estus))
			b.WriteString("\n")
		}
	} else {
		title := goldStyle.Render("HEPH") + emberStyle.Render("4") + goldStyle.Render("ESTUS")
		b.WriteString(title + "\n")
	}
	b.WriteString("\n")

	// Menu list
	b.WriteString(m.list.View())
	b.WriteString("\n")

	// Help bar
	helpBar := core.StatusBarStyle.Render(m.help.View(keys))
	b.WriteString(helpBar)

	// Center when wide enough, left-align for small terminals
	content := b.String()
	if m.width > 0 && m.height > 0 {
		content = lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
	}

	return content
}
