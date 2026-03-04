package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"heph4estus/internal/tui/core"
)

func TestAppInitializesToMenu(t *testing.T) {
	app := NewApp()
	app.Init()

	v := app.View()
	if v.Content == "" {
		t.Fatal("expected non-empty view content after Init")
	}
	if !strings.Contains(v.Content, "Nmap Scanner") {
		t.Fatal("expected menu view to contain menu items")
	}
}

func TestWindowResizeDoesNotPanic(t *testing.T) {
	app := NewApp()
	app.Init()

	msg := tea.WindowSizeMsg{Width: 120, Height: 40}
	_, cmd := app.Update(msg)
	_ = cmd
	// No panic = success
}

func TestNavigateToSettings(t *testing.T) {
	app := NewApp()
	app.Init()

	msg := core.NavigateMsg{Target: core.ViewSettings}
	_, _ = app.Update(msg)

	v := app.View()
	if !strings.Contains(v.Content, "Settings") {
		t.Fatal("expected settings view to contain 'Settings'")
	}
}

func TestNavigateBackToMenu(t *testing.T) {
	app := NewApp()
	app.Init()

	// Navigate to settings first
	_, _ = app.Update(core.NavigateMsg{Target: core.ViewSettings})
	// Navigate back to menu
	_, _ = app.Update(core.NavigateMsg{Target: core.ViewMenu})

	v := app.View()
	if !strings.Contains(v.Content, "Nmap Scanner") {
		t.Fatal("expected menu view to contain menu items after navigating back")
	}
}

func TestCtrlCReturnsQuit(t *testing.T) {
	app := NewApp()
	app.Init()

	msg := tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	_, cmd := app.Update(msg)

	if cmd == nil {
		t.Fatal("expected quit command from ctrl+c")
	}
	// Call the command and check it returns QuitMsg
	result := cmd()
	if _, ok := result.(tea.QuitMsg); !ok {
		t.Fatalf("expected QuitMsg, got %T", result)
	}
}

func TestEnterOnDisabledItemDoesNotNavigate(t *testing.T) {
	app := NewApp()
	app.Init()

	// The first item (Nmap Scanner) is disabled; press enter
	enterMsg := tea.KeyPressMsg{Code: tea.KeyEnter}
	_, cmd := app.Update(enterMsg)

	// Should remain on menu view — no NavigateMsg emitted
	v := app.View()
	if !strings.Contains(v.Content, "Nmap Scanner") {
		t.Fatal("expected menu view to still contain menu items after enter on disabled item")
	}

	// The cmd, if any, should not produce a NavigateMsg
	if cmd != nil {
		result := cmd()
		if _, ok := result.(core.NavigateMsg); ok {
			t.Fatal("expected no NavigateMsg from disabled item")
		}
	}
}
