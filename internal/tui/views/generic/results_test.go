package generic

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

func testResultInfra() core.InfraOutputs {
	return core.InfraOutputs{
		S3BucketName: "test-bucket",
		ToolName:     "httpx",
		JobID:        "httpx-20260405t120000-abcd1234",
	}
}

func TestGenericResultsInit(t *testing.T) {
	key1 := "example.com_1700000000.json"
	key2 := "10.0.0.1_1700000001.json"
	r1, _ := json.Marshal(worker.Result{Target: "example.com", Timestamp: time.Now()})
	r2, _ := json.Marshal(worker.Result{Target: "10.0.0.1", Error: "timeout", Timestamp: time.Now()})
	source := &mockResultsSource{
		keys: []string{key1, key2},
		data: map[string][]byte{key1: r1, key2: r2},
	}
	m := NewResults(testResultInfra(), source, nil)
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("expected init command")
	}
	msg := cmd()
	_, cmd = m.Update(msg) // keys loaded -> triggers page status load

	if m.total != 2 {
		t.Fatalf("expected 2 results, got %d", m.total)
	}

	// Execute page status load.
	if cmd != nil {
		msg = cmd()
		m.Update(msg)
	}

	// Statuses should now be populated — verify error is surfaced.
	if r, ok := m.results[key2]; !ok {
		t.Error("expected key2 result to be cached")
	} else if r.Error != "timeout" {
		t.Errorf("expected error 'timeout', got %q", r.Error)
	}
}

func TestGenericResultsViewContainsToolName(t *testing.T) {
	source := &mockResultsSource{keys: []string{}}
	m := NewResults(testResultInfra(), source, nil)
	cmd := m.Init()
	msg := cmd()
	m.Update(msg)

	v := m.View()
	if !strings.Contains(v, "httpx") {
		t.Fatal("expected view to contain tool name")
	}
}

func TestGenericResultsDetailView(t *testing.T) {
	result := worker.Result{
		ToolName:  "httpx",
		Target:    "example.com",
		Output:    "HTTP/1.1 200 OK",
		Timestamp: time.Date(2026, 4, 5, 12, 0, 0, 0, time.UTC),
	}
	data, _ := json.Marshal(result)

	key := "example.com_1700000000.json"
	source := &mockResultsSource{
		keys: []string{key},
		data: map[string][]byte{key: data},
	}
	m := NewResults(testResultInfra(), source, nil)
	cmd := m.Init()
	msg := cmd()
	m.Update(msg) // load keys

	// Press enter to load detail.
	_, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected detail load command")
	}
	msg = cmd()
	m.Update(msg) // load detail

	if !m.detail {
		t.Fatal("expected detail mode to be active")
	}
}

func TestGenericResultsEscNavigatesBack(t *testing.T) {
	source := &mockResultsSource{keys: []string{}}
	m := NewResults(testResultInfra(), source, nil)
	cmd := m.Init()
	msg := cmd()
	m.Update(msg)

	_, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("expected command from esc")
	}
	msg = cmd()
	nav, ok := msg.(core.NavigateMsg)
	if !ok {
		t.Fatalf("expected NavigateMsg, got %T", msg)
	}
	if nav.Target != core.ViewMenu {
		t.Fatalf("expected ViewMenu, got %v", nav.Target)
	}
}

func TestGenericResultsListError(t *testing.T) {
	source := &mockResultsSource{listErr: fmt.Errorf("access denied")}
	m := NewResults(testResultInfra(), source, nil)
	cmd := m.Init()
	msg := cmd()
	m.Update(msg)

	if !strings.Contains(m.errMsg, "access denied") {
		t.Fatalf("expected error message, got %q", m.errMsg)
	}
}

func TestGenericResultsPagination(t *testing.T) {
	// Create more than pageSize keys.
	keys := make([]string, pageSize+5)
	for i := range keys {
		keys[i] = fmt.Sprintf("target%d_%d.json", i, i)
	}
	source := &mockResultsSource{keys: keys}
	m := NewResults(testResultInfra(), source, nil)
	cmd := m.Init()
	msg := cmd()
	m.Update(msg)

	if m.maxPage() != 1 {
		t.Fatalf("expected 2 pages (maxPage=1), got maxPage=%d", m.maxPage())
	}

	pk := m.pageKeys()
	if len(pk) != pageSize {
		t.Fatalf("expected %d keys on page 0, got %d", pageSize, len(pk))
	}

	// Navigate to next page.
	m.Update(tea.KeyPressMsg{Code: 'n'})
	if m.page != 1 {
		t.Fatalf("expected page 1, got %d", m.page)
	}
	pk = m.pageKeys()
	if len(pk) != 5 {
		t.Fatalf("expected 5 keys on page 1, got %d", len(pk))
	}
}

func TestFormatResult(t *testing.T) {
	r := worker.Result{
		ToolName:  "httpx",
		Target:    "example.com",
		Output:    "200 OK",
		OutputKey: "artifacts/example.com.jsonl",
		Error:     "timeout",
		Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	s := formatResult("test-bucket", r)
	if !strings.Contains(s, "example.com") {
		t.Error("expected target in formatted result")
	}
	if !strings.Contains(s, "httpx") {
		t.Error("expected tool name in formatted result")
	}
	if !strings.Contains(s, "timeout") {
		t.Error("expected error in formatted result")
	}
	if !strings.Contains(s, "200 OK") {
		t.Error("expected output in formatted result")
	}
	if !strings.Contains(s, "s3://test-bucket/artifacts/example.com.jsonl") {
		t.Error("expected full s3 output path in formatted result")
	}
}

func TestGenericResultsDestroyExecutes(t *testing.T) {
	source := &mockResultsSource{keys: []string{}}
	destroyer := &mockDestroyer{}
	infra := testResultInfra()
	infra.CleanupPolicy = "destroy-after"
	infra.Exported = true
	infra.ExportDir = "/tmp/exports/httpx/job1"
	infra.TerraformDir = "/tmp/tf"
	m := NewResults(infra, source, destroyer)
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

	// Execute the destroy command.
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

func TestGenericResultsDestroyBlockedWithoutExport(t *testing.T) {
	source := &mockResultsSource{keys: []string{}}
	destroyer := &mockDestroyer{}
	infra := testResultInfra()
	infra.CleanupPolicy = "destroy-after"
	infra.Exported = false // not exported yet
	m := NewResults(infra, source, destroyer)
	cmd := m.Init()
	msg := cmd()
	m.Update(msg)

	// Press d — should be blocked.
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

func TestGenericResultsDestroyNoDestroyer(t *testing.T) {
	source := &mockResultsSource{keys: []string{}}
	m := NewResults(testResultInfra(), source, nil) // nil destroyer
	cmd := m.Init()
	msg := cmd()
	m.Update(msg)

	_, cmd = m.Update(tea.KeyPressMsg{Code: 'd'})
	if cmd != nil {
		t.Fatal("expected no command (no destroyer)")
	}
	if !strings.Contains(m.destroyMsg, "not available") {
		t.Fatalf("expected not-available message, got %q", m.destroyMsg)
	}
}

func TestGenericResultsDestroyFailure(t *testing.T) {
	source := &mockResultsSource{keys: []string{}}
	destroyer := &mockDestroyer{err: fmt.Errorf("terraform error")}
	infra := testResultInfra()
	infra.Exported = true
	infra.ExportDir = "/tmp/out"
	m := NewResults(infra, source, destroyer)
	cmd := m.Init()
	msg := cmd()
	m.Update(msg)

	_, cmd = m.Update(tea.KeyPressMsg{Code: 'd'})
	if cmd == nil {
		t.Fatal("expected destroy command")
	}
	msg = cmd()
	m.Update(msg)

	if !strings.Contains(m.destroyMsg, "Destroy failed") {
		t.Fatalf("expected failure message, got %q", m.destroyMsg)
	}
}
