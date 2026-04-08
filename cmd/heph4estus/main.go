package main

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
	"heph4estus/internal/operator"
	"heph4estus/internal/tui"
)

func main() {
	// Apply saved operator defaults (region, profile) when env is unset.
	if cfg, err := operator.LoadConfig(); err == nil {
		operator.ApplyEnvDefaults(cfg)
	}

	app := tui.NewApp()
	p := tea.NewProgram(app)
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
