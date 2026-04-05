package generic

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"heph4estus/internal/tui/core"
)

func TestGenericConfigShowsToolName(t *testing.T) {
	m := NewConfig("httpx")
	v := m.View()
	if !strings.Contains(v, "httpx") {
		t.Fatal("expected config view to contain tool name")
	}
}

func TestGenericConfigShowsDescription(t *testing.T) {
	m := NewConfig("httpx")
	v := m.View()
	if !strings.Contains(v, "HTTP probe") {
		t.Fatal("expected config view to contain module description")
	}
}

func TestGenericConfigEscNavigatesBack(t *testing.T) {
	m := NewConfig("httpx")
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

func TestGenericConfigSubmitRequiresTargetFile(t *testing.T) {
	m := NewConfig("httpx")
	// Navigate to submit button.
	for i := 0; i < cfgFieldSubmit; i++ {
		m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	}
	// Press enter on submit.
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("expected nil command when submitting without target file")
	}
	if !strings.Contains(m.View(), "Target file is required") {
		t.Fatal("expected error message about target file")
	}
}

func TestGenericConfigWordlistRejection(t *testing.T) {
	m := NewConfig("ffuf")
	// Set a dummy target file value.
	m.inputs[cfgFieldTargetFile].SetValue("/tmp/targets.txt")
	// Navigate to submit.
	m.focus = cfgFieldSubmit
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("expected nil command for wordlist tool submission")
	}
	if !strings.Contains(m.View(), "PR 5.7") {
		t.Fatal("expected PR 5.7 rejection message for wordlist tool")
	}
}

func TestGenericConfigFileRead(t *testing.T) {
	m := NewConfig("httpx")
	// Simulate successful file read.
	_, cmd := m.Update(fileReadMsg{content: "example.com\n10.0.0.1\n"})
	if cmd == nil {
		t.Fatal("expected navigation command after file read")
	}
	msg := cmd()
	nav, ok := msg.(core.NavigateWithDataMsg)
	if !ok {
		t.Fatalf("expected NavigateWithDataMsg, got %T", msg)
	}
	if nav.Target != core.ViewDeploy {
		t.Fatalf("expected ViewDeploy, got %v", nav.Target)
	}
	cfg, ok := nav.Data.(core.DeployConfig)
	if !ok {
		t.Fatalf("expected DeployConfig, got %T", nav.Data)
	}
	if cfg.ToolName != "httpx" {
		t.Fatalf("expected tool httpx, got %q", cfg.ToolName)
	}
	if cfg.PostDeployView != core.ViewGenericStatus {
		t.Fatal("expected PostDeployView to be ViewGenericStatus")
	}
	if cfg.TargetsContent != "example.com\n10.0.0.1\n" {
		t.Fatalf("unexpected targets content: %q", cfg.TargetsContent)
	}
	if cfg.BuildArgs == nil {
		t.Fatal("expected BuildArgs to be set")
	}
}

func TestGenericConfigInvalidComputeMode(t *testing.T) {
	m := NewConfig("httpx")
	m.inputs[cfgFieldComputeMode].SetValue("gpu")
	_, cmd := m.Update(fileReadMsg{content: "example.com\n"})
	if cmd != nil {
		t.Fatal("expected nil command for invalid compute mode")
	}
	if !strings.Contains(m.View(), "Compute mode must be") {
		t.Fatal("expected compute mode error message")
	}
}
