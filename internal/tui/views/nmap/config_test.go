package nmap

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"heph4estus/internal/tui/core"
)

func TestConfigModel_InitialView(t *testing.T) {
	m := NewConfig()
	m.Init()

	v := m.View()
	if !strings.Contains(v, "Nmap Scanner") {
		t.Fatal("expected title")
	}
	if !strings.Contains(v, "Target File:*") {
		t.Fatal("expected target file label with required marker")
	}
	if !strings.Contains(v, "Worker Count") {
		t.Fatal("expected worker count label")
	}
}

func TestConfigModel_EscNavigatesBack(t *testing.T) {
	m := NewConfig()
	m.Init()

	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("expected command from esc")
	}
	msg := cmd()
	nav, ok := msg.(core.NavigateMsg)
	if !ok {
		t.Fatalf("expected NavigateMsg, got %T", msg)
	}
	if nav.Target != core.ViewMenu {
		t.Fatalf("expected ViewMenu, got %v", nav.Target)
	}
}

func TestConfigModel_TabCyclesFocus(t *testing.T) {
	m := NewConfig()
	m.Init()

	if m.focusIndex != 0 {
		t.Fatalf("expected initial focus 0, got %d", m.focusIndex)
	}

	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.focusIndex != 1 {
		t.Fatalf("expected focus 1 after tab, got %d", m.focusIndex)
	}

	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.focusIndex != 2 {
		t.Fatalf("expected focus 2 after second tab, got %d", m.focusIndex)
	}

	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.focusIndex != 3 { // submit
		t.Fatalf("expected focus 3 (submit) after third tab, got %d", m.focusIndex)
	}
}

func TestConfigModel_SubmitEmptyShowsError(t *testing.T) {
	m := NewConfig()
	m.Init()

	// Navigate to submit button
	m.focusIndex = fieldSubmit

	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.errMsg == "" {
		t.Fatal("expected error message for empty target file")
	}
}

func TestConfigModel_SubmitBadFileShowsError(t *testing.T) {
	m := NewConfig()
	m.Init()

	m.inputs[fieldTargetFile].SetValue("/nonexistent/file.txt")
	m.focusIndex = fieldSubmit

	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected cmd for file read")
	}
	msg := cmd()
	frm, ok := msg.(fileReadMsg)
	if !ok {
		t.Fatalf("expected fileReadMsg, got %T", msg)
	}
	if frm.err == nil {
		t.Fatal("expected error for nonexistent file")
	}

	// Process the error message
	m.Update(frm)
	if m.errMsg == "" {
		t.Fatal("expected error message in view")
	}
}
