package nmap

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"heph4estus/internal/cloud/mock"
	nmaptool "heph4estus/internal/tools/nmap"
	"heph4estus/internal/tui/core"
)

func testResultsStorage() *mock.Storage {
	keys := []string{
		"scans/nmap/job-123/results/192.168.1.1_1000.json",
		"scans/nmap/job-123/results/192.168.1.2_1001.json",
		"scans/nmap/job-123/results/192.168.1.3_1002.json",
	}

	result := nmaptool.ScanResult{
		Target:    "192.168.1.1",
		Output:    "Nmap scan output here",
		Timestamp: time.Now(),
	}
	resultBytes, _ := json.Marshal(result)

	return &mock.Storage{
		ListFunc: func(_ context.Context, _, _ string) ([]string, error) {
			return keys, nil
		},
		DownloadFunc: func(_ context.Context, _, _ string) ([]byte, error) {
			return resultBytes, nil
		},
		CountFunc: func(_ context.Context, _, _ string) (int, error) {
			return len(keys), nil
		},
	}
}

func TestResultsModel_Init(t *testing.T) {
	s := testResultsStorage()
	infra := testInfra()
	m := NewResults(infra, s)
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
	s := testResultsStorage()
	m := NewResults(testInfra(), s)
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
	s := testResultsStorage()
	m := NewResults(testInfra(), s)
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
	s := testResultsStorage()
	m := NewResults(testInfra(), s)
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
	s := testResultsStorage()
	m := NewResults(testInfra(), s)
	cmd := m.Init()
	msg := cmd()
	m.Update(msg)

	v := m.View()
	if !strings.Contains(v, "Nmap Scan Results") {
		t.Fatal("expected title")
	}
	if !strings.Contains(v, "192.168.1.1") {
		t.Fatal("expected target in view")
	}
}

func TestExtractTarget(t *testing.T) {
	tests := []struct {
		key    string
		target string
	}{
		{"scans/nmap/job-123/results/192.168.1.1_1000.json", "192.168.1.1"},
		{"scans/nmap/job-123/results/example.com_1234.json", "example.com"},
		{"scans/nmap/job-123/results/example.com_line1/example.com_chunk0_of_5_1234.json", "example.com"},
	}
	for _, tt := range tests {
		got := extractTarget(tt.key)
		if got != tt.target {
			t.Errorf("extractTarget(%q) = %q, want %q", tt.key, got, tt.target)
		}
	}
}
