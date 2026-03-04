package tui

import (
	tea "charm.land/bubbletea/v2"
	"heph4estus/internal/tui/core"
	"heph4estus/internal/tui/views/menu"
	"heph4estus/internal/tui/views/settings"
)

// App is the root Bubbletea model that manages view switching.
type App struct {
	activeView core.View
	width      int
	height     int
	quitting   bool
}

// NewApp creates a new App starting at the main menu.
func NewApp() *App {
	return &App{}
}

func (a *App) Init() tea.Cmd {
	a.activeView = menu.New()
	return a.activeView.Init()
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			a.quitting = true
			return a, tea.Quit
		}

	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height

	case core.NavigateMsg:
		var newView core.View
		switch msg.Target {
		case core.ViewSettings:
			newView = settings.New()
		case core.ViewMenu:
			newView = menu.New()
		}
		if newView != nil {
			a.activeView = newView
			return a, newView.Init()
		}
	}

	var cmd tea.Cmd
	a.activeView, cmd = a.activeView.Update(msg)
	return a, cmd
}

func (a *App) View() tea.View {
	if a.quitting {
		return tea.NewView("")
	}
	v := tea.NewView(a.activeView.View())
	v.AltScreen = true
	return v
}
