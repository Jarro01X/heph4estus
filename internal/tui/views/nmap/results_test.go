package nmap

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"heph4estus/internal/tui/core"
	"heph4estus/internal/worker"
)

// mockResultsSource implements core.ResultsSource for testing.
type mockResultsSource struct {
	keys    []string
	listErr error
	data    map[string][]byte
	dlErr   error
}

func (s *mockResultsSource) ListKeys(_ context.Context) ([]string, error) {
	return s.keys, s.listErr
}

func (s *mockResultsSource) Download(_ context.Context, key string) ([]byte, error) {
	if s.dlErr != nil {
		return nil, s.dlErr
	}
	if d, ok := s.data[key]; ok {
		return d, nil
	}
	return nil, fmt.Errorf("not found: %s", key)
}

// mockDestroyer implements core.Destroyer for testing.
type mockDestroyer struct {
	called bool
	err    error
}

func (d *mockDestroyer) Destroy(_ context.Context) error {
	d.called = true
	return d.err
}

func testResultsSource() *mockResultsSource {
	keys := []string{
		"192.168.1.1_1000.json",
		"192.168.1.2_1001.json",
		"192.168.1.3_1002.json",
	}

	result := worker.Result{
		ToolName:  "nmap",
		Target:    "192.168.1.1",
		Output:    "Nmap scan output here",
		Timestamp: time.Now(),
	}
	resultBytes, _ := json.Marshal(result)

	data := make(map[string][]byte)
	for _, k := range keys {
		data[k] = resultBytes
	}

	return &mockResultsSource{
		keys: keys,
		data: data,
	}
}

func TestResultsModel_Init(t *testing.T) {
	s := testResultsSource()
	infra := testInfra()
	m := NewResults(infra, s, nil)
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("expected init command")
	}

	// Simulate keys loaded
	msg := cmd()
	m.Update(msg)
	if len(m.allKeys) != 3 {
		t.Fatalf("expected 3 keys, got %d", len(m.allKeys))
	}
}

func TestResultsModel_Navigation(t *testing.T) {
	s := testResultsSource()
	m := NewResults(testInfra(), s, nil)
	cmd := m.Init()
	msg := cmd()
	m.Update(msg)

	// Move down
	m.Update(tea.KeyPressMsg{Code: 'j'})
	if m.cursor != 1 {
		t.Fatalf("expected cursor 1, got %d", m.cursor)
	}

	// Move up
	m.Update(tea.KeyPressMsg{Code: 'k'})
	if m.cursor != 0 {
		t.Fatalf("expected cursor 0, got %d", m.cursor)
	}
}

func TestResultsModel_DetailView(t *testing.T) {
	s := testResultsSource()
	m := NewResults(testInfra(), s, nil)
	cmd := m.Init()
	msg := cmd()
	m.Update(msg)

	// Press enter to load detail
	_, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected detail load command")
	}
	msg = cmd()
	m.Update(msg)

	if !m.detail {
		t.Fatal("expected detail mode")
	}

	// Esc back to list
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.detail {
		t.Fatal("expected list mode after esc")
	}
}

func TestResultsModel_EscNavigatesBack(t *testing.T) {
	s := testResultsSource()
	m := NewResults(testInfra(), s, nil)
	m.Init()

	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("expected navigate command")
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

func TestResultsModel_View(t *testing.T) {
	s := testResultsSource()
	m := NewResults(testInfra(), s, nil)
	cmd := m.Init()
	msg := cmd()
	m.Update(msg)

	v := m.View()
	if !strings.Contains(v, "Nmap Scan Results") {
		t.Fatal("expected title")
	}
}

func TestExtractTarget(t *testing.T) {
	tests := []struct {
		key    string
		target string
	}{
		{"192.168.1.1_1000.json", "192.168.1.1"},
		{"example.com_1234.json", "example.com"},
	}
	for _, tt := range tests {
		got := extractTarget(tt.key)
		if got != tt.target {
			t.Errorf("extractTarget(%q) = %q, want %q", tt.key, got, tt.target)
		}
	}
}

func TestResultsModel_DestroyExecutes(t *testing.T) {
	s := testResultsSource()
	destroyer := &mockDestroyer{}
	infra := testInfra()
	infra.CleanupPolicy = "destroy-after"
	infra.Exported = true
	infra.ExportDir = "/tmp/exports/nmap/job1"
	infra.TerraformDir = "/tmp/tf"
	m := NewResults(infra, s, destroyer)
	cmd := m.Init()
	msg := cmd()
	m.Update(msg)

	// Press d to trigger destroy.
	_, cmd = m.Update(tea.KeyPressMsg{Code: 'd'})
	if cmd == nil {
		t.Fatal("expected destroy command")
	}
	if !m.destroying {
		t.Fatal("expected destroying=true")
	}

	msg = cmd()
	m.Update(msg)

	if !destroyer.called {
		t.Fatal("expected destroyer to be called")
	}
	if !m.destroyed {
		t.Fatal("expected destroyed=true")
	}
	if !strings.Contains(m.destroyMsg, "destroyed successfully") {
		t.Fatalf("expected success message, got %q", m.destroyMsg)
	}
}

func TestResultsModel_DestroyFailureAllowsRetry(t *testing.T) {
	s := testResultsSource()
	destroyer := &mockDestroyer{err: fmt.Errorf("terraform error")}
	infra := testInfra()
	infra.Exported = true
	infra.ExportDir = "/tmp/out"
	m := NewResults(infra, s, destroyer)
	cmd := m.Init()
	msg := cmd()
	m.Update(msg)

	_, cmd = m.Update(tea.KeyPressMsg{Code: 'd'})
	msg = cmd()
	m.Update(msg)

	if m.destroyed {
		t.Fatal("expected destroyed=false after failure")
	}
	if !strings.Contains(m.destroyMsg, "press d to retry") {
		t.Fatalf("expected retry hint, got %q", m.destroyMsg)
	}

	// Retry should work.
	destroyer.err = nil
	destroyer.called = false
	_, cmd = m.Update(tea.KeyPressMsg{Code: 'd'})
	if cmd == nil {
		t.Fatal("expected destroy command on retry")
	}
	msg = cmd()
	m.Update(msg)

	if !m.destroyed {
		t.Fatal("expected destroyed=true after successful retry")
	}
}

func TestResultsModel_AutoDestroyedHidesKey(t *testing.T) {
	s := testResultsSource()
	infra := testInfra()
	infra.Destroyed = true
	infra.Exported = true
	infra.ExportDir = "/tmp/out"
	infra.CleanupPolicy = "destroy-after"
	m := NewResults(infra, s, nil)
	cmd := m.Init()
	msg := cmd()
	m.Update(msg)

	if !m.destroyed {
		t.Fatal("expected destroyed=true from InfraOutputs")
	}

	v := m.View()
	if !strings.Contains(v, "infra: destroyed") {
		t.Fatal("expected 'infra: destroyed' in summary")
	}

	// Press d — should be a no-op.
	_, cmd = m.Update(tea.KeyPressMsg{Code: 'd'})
	if cmd != nil {
		t.Fatal("expected no command (already destroyed)")
	}
}

func TestResultsModel_AutoDestroyFailureShowsMessage(t *testing.T) {
	s := testResultsSource()
	destroyer := &mockDestroyer{}
	infra := testInfra()
	infra.DestroyErr = "timeout waiting for resources"
	infra.Exported = true
	infra.ExportDir = "/tmp/out"
	m := NewResults(infra, s, destroyer)
	cmd := m.Init()
	msg := cmd()
	m.Update(msg)

	if !strings.Contains(m.destroyMsg, "Auto-destroy failed") {
		t.Fatalf("expected auto-destroy failure message, got %q", m.destroyMsg)
	}

	v := m.View()
	if !strings.Contains(v, "infra: retained") {
		t.Fatal("expected 'infra: retained' in summary")
	}

	// Manual retry via d should work.
	_, cmd = m.Update(tea.KeyPressMsg{Code: 'd'})
	if cmd == nil {
		t.Fatal("expected destroy command (manual retry)")
	}
	msg = cmd()
	m.Update(msg)

	if !destroyer.called {
		t.Fatal("expected destroyer to be called on retry")
	}
	if !m.destroyed {
		t.Fatal("expected destroyed=true after successful retry")
	}
}

func TestResultsModel_DestroyBlockedWithoutExport(t *testing.T) {
	s := testResultsSource()
	destroyer := &mockDestroyer{}
	infra := testInfra()
	infra.CleanupPolicy = "destroy-after"
	infra.Exported = false
	m := NewResults(infra, s, destroyer)
	cmd := m.Init()
	msg := cmd()
	m.Update(msg)

	_, cmd = m.Update(tea.KeyPressMsg{Code: 'd'})
	if cmd != nil {
		t.Fatal("expected no command (destroy blocked)")
	}
	if destroyer.called {
		t.Fatal("destroyer should not have been called")
	}
	if !strings.Contains(m.destroyMsg, "not exported") {
		t.Fatalf("expected blocked message, got %q", m.destroyMsg)
	}
}
