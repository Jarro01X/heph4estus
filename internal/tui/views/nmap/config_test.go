package nmap

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"heph4estus/internal/cloud"
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

func TestConfigModel_ProviderNavigatesToStatus(t *testing.T) {
	t.Setenv("SELFHOSTED_QUEUE_ID", "nmap-stream")
	t.Setenv("SELFHOSTED_BUCKET", "nmap-bucket")
	t.Setenv("SELFHOSTED_WORKER_HOSTS", "10.0.0.1")
	t.Setenv("SELFHOSTED_SSH_USER", "heph")
	t.Setenv("SELFHOSTED_DOCKER_IMAGE", "worker:latest")

	m := NewConfig()
	m.Init()
	m.inputs[fieldCloud].SetValue("manual")
	_, cmd := m.Update(fileReadMsg{content: "1.1.1.1\n2.2.2.2\n"})
	if cmd == nil {
		t.Fatal("expected navigation command for manual provider")
	}
	msg := cmd()
	nav, ok := msg.(core.NavigateWithDataMsg)
	if !ok {
		t.Fatalf("expected NavigateWithDataMsg, got %T", msg)
	}
	if nav.Target != core.ViewNmapStatus {
		t.Fatalf("expected ViewNmapStatus (bypass deploy), got %v", nav.Target)
	}
	infra, ok := nav.Data.(core.InfraOutputs)
	if !ok {
		t.Fatalf("expected InfraOutputs, got %T", nav.Data)
	}
	if infra.Cloud != cloud.KindManual {
		t.Fatalf("expected cloud manual, got %q", infra.Cloud)
	}
}

func TestConfigModel_ProviderRejectsSpot(t *testing.T) {
	m := NewConfig()
	m.Init()
	m.inputs[fieldCloud].SetValue("linode")
	m.inputs[fieldComputeMode].SetValue("spot")
	_, cmd := m.Update(fileReadMsg{content: "1.1.1.1\n"})
	if cmd != nil {
		t.Fatal("expected nil command for VPS provider + spot")
	}
	if !strings.Contains(m.View(), `provider "linode" only supports`) {
		t.Fatal("expected provider mode rejection error")
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
