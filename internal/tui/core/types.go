package core

import tea "charm.land/bubbletea/v2"

// View is the interface that all TUI views implement.
// Views return a string from View(); the App wraps it in tea.View with alt screen.
type View interface {
	Init() tea.Cmd
	Update(tea.Msg) (View, tea.Cmd)
	View() string
}

// ViewID identifies a navigable view.
type ViewID int

const (
	ViewMenu ViewID = iota
	ViewSettings
	ViewNmapConfig
	ViewNaabuConfig
)

// NavigateMsg is sent by views to request navigation.
type NavigateMsg struct {
	Target ViewID
}
