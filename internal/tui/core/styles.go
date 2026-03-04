package core

import "charm.land/lipgloss/v2"

// Forge theme colors.
var (
	Ember    = lipgloss.Color("#C75B39")
	Gold     = lipgloss.Color("#D4A04A")
	Molten   = lipgloss.Color("#E8873A")
	Iron     = lipgloss.Color("#43464B")
	Steel    = lipgloss.Color("#6E7C7F")
	Charcoal = lipgloss.Color("#2B3338")
	Slag     = lipgloss.Color("#5C2E1A")
	WhiteHot = lipgloss.Color("#F5E6C8")
)

// Reusable styles.
var (
	BorderStyle       = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(Steel)
	ActiveBorderStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(Ember)
	TitleStyle        = lipgloss.NewStyle().Foreground(Gold).Bold(true)
	SelectedStyle     = lipgloss.NewStyle().Foreground(Ember).Bold(true)
	NormalStyle       = lipgloss.NewStyle().Foreground(Iron)
	MutedStyle        = lipgloss.NewStyle().Foreground(Steel)
	ErrorStyle        = lipgloss.NewStyle().Foreground(Slag).Bold(true)
	SuccessStyle      = lipgloss.NewStyle().Foreground(Gold)
	StatusBarStyle    = lipgloss.NewStyle().Foreground(Steel).Background(Charcoal)
	TitleBarStyle     = lipgloss.NewStyle().Foreground(Gold).Bold(true)
)
