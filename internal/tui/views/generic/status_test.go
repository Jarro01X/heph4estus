package generic

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"heph4estus/internal/cloud"
	"heph4estus/internal/jobs"
	"heph4estus/internal/tui/core"
	"heph4estus/internal/worker"

	tea "charm.land/bubbletea/v2"
)

// mockUploader records chunk uploads.
type mockUploader struct {
	uploaded bool
	err      error
}

func (u *mockUploader) UploadChunks(_ context.Context, _ string, _ *jobs.WordlistPlan) error {
	u.uploaded = true
	return u.err
}

// mockSubmitter records calls and returns configured results.
type mockSubmitter struct {
	enqueuedTasks []worker.Task
	enqueueErr    error
	launchErr     error
	spotErr       error
}

func (s *mockSubmitter) EnqueueTasks(_ context.Context, _ string, tasks []worker.Task) error {
	s.enqueuedTasks = tasks
	return s.enqueueErr
}

func (s *mockSubmitter) LaunchWorkers(_ context.Context, _ cloud.ContainerOpts) (string, error) {
	return "task-123", s.launchErr
}

func (s *mockSubmitter) LaunchSpotWorkers(_ context.Context, _ cloud.SpotOpts) ([]string, error) {
	return []string{"i-123"}, s.spotErr
}

// mockTracker returns a configured count.
type mockTracker struct {
	count int
	err   error
}

func (t *mockTracker) CountResults(_ context.Context, _, _ string) (int, error) {
	return t.count, t.err
}

func testInfra() core.InfraOutputs {
	return core.InfraOutputs{
		SQSQueueURL:    "https://sqs.example.com/q",
		S3BucketName:   "test-bucket",
		ECSClusterName: "test-cluster",
		ToolName:       "httpx",
		ToolOptions:    "-silent",
		TargetsContent: "example.com\n10.0.0.1\n# comment\n\n",
		WorkerCount:    5,
		ComputeMode:    "fargate",
	}
}

func TestGenericStatusInit(t *testing.T) {
	sub := &mockSubmitter{}
	tracker := &mockTracker{}
	m := NewStatusWithDeps(testInfra(), sub, tracker, &mockUploader{})

	cmd := m.Init()
	if cmd == nil {
		t.Fatal("expected init command")
	}
	if m.totalTargets != 2 {
		t.Fatalf("expected 2 targets, got %d", m.totalTargets)
	}
	if m.phase != phaseEnqueuing {
		t.Fatalf("expected phaseEnqueuing, got %d", m.phase)
	}

	// Execute the enqueue command.
	msg := cmd()
	ep, ok := msg.(enqueueProgressMsg)
	if !ok {
		t.Fatalf("expected enqueueProgressMsg, got %T", msg)
	}
	if ep.sent != 2 {
		t.Fatalf("expected 2 sent, got %d", ep.sent)
	}

	// Verify tasks were created with correct fields.
	if len(sub.enqueuedTasks) != 2 {
		t.Fatalf("expected 2 enqueued tasks, got %d", len(sub.enqueuedTasks))
	}
	task := sub.enqueuedTasks[0]
	if task.ToolName != "httpx" {
		t.Errorf("task.ToolName = %q, want httpx", task.ToolName)
	}
	if task.Target != "example.com" {
		t.Errorf("task.Target = %q, want example.com", task.Target)
	}
	if task.Options != "-silent" {
		t.Errorf("task.Options = %q, want -silent", task.Options)
	}
}

func TestGenericStatusEnqueueToLaunch(t *testing.T) {
	sub := &mockSubmitter{}
	tracker := &mockTracker{}
	m := NewStatusWithDeps(testInfra(), sub, tracker, &mockUploader{})
	m.Init()

	_, cmd := m.Update(enqueueProgressMsg{sent: 2, total: 2})
	if m.phase != phaseLaunching {
		t.Fatalf("expected phaseLaunching, got %d", m.phase)
	}
	if cmd == nil {
		t.Fatal("expected launch command")
	}
}

func TestGenericStatusLaunchToScanning(t *testing.T) {
	sub := &mockSubmitter{}
	tracker := &mockTracker{}
	m := NewStatusWithDeps(testInfra(), sub, tracker, &mockUploader{})
	m.Init()
	m.Update(enqueueProgressMsg{sent: 2, total: 2})

	_, cmd := m.Update(launchProgressMsg{launched: 5, total: 5})
	if m.phase != phaseScanning {
		t.Fatalf("expected phaseScanning, got %d", m.phase)
	}
	if m.workersUp != 5 {
		t.Fatalf("expected 5 workers up, got %d", m.workersUp)
	}
	if cmd == nil {
		t.Fatal("expected poll command")
	}
}

func TestGenericStatusScanComplete(t *testing.T) {
	sub := &mockSubmitter{}
	tracker := &mockTracker{}
	m := NewStatusWithDeps(testInfra(), sub, tracker, &mockUploader{})
	m.Init()
	m.totalTargets = 2
	m.phase = phaseScanning

	_, cmd := m.Update(scanProgressMsg{completed: 2})
	if m.phase != phaseComplete {
		t.Fatalf("expected phaseComplete, got %d", m.phase)
	}
	if cmd == nil {
		t.Fatal("expected navigation command")
	}
	msg := cmd()
	nav, ok := msg.(core.NavigateWithDataMsg)
	if !ok {
		t.Fatalf("expected NavigateWithDataMsg, got %T", msg)
	}
	if nav.Target != core.ViewGenericResults {
		t.Fatalf("expected ViewGenericResults, got %v", nav.Target)
	}
}

func TestGenericStatusEnqueueError(t *testing.T) {
	sub := &mockSubmitter{enqueueErr: fmt.Errorf("queue full")}
	tracker := &mockTracker{}
	m := NewStatusWithDeps(testInfra(), sub, tracker, &mockUploader{})
	cmd := m.Init()
	msg := cmd()
	m.Update(msg)

	if !strings.Contains(m.errMsg, "queue full") {
		t.Fatalf("expected error message containing 'queue full', got %q", m.errMsg)
	}
}

func TestGenericStatusViewContainsToolName(t *testing.T) {
	sub := &mockSubmitter{}
	tracker := &mockTracker{}
	m := NewStatusWithDeps(testInfra(), sub, tracker, &mockUploader{})
	m.Init()
	v := m.View()
	if !strings.Contains(v, "httpx") {
		t.Fatal("expected view to contain tool name")
	}
}

func TestGenericStatusEscNavigatesBack(t *testing.T) {
	sub := &mockSubmitter{}
	tracker := &mockTracker{}
	m := NewStatusWithDeps(testInfra(), sub, tracker, &mockUploader{})
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

func TestGenericStatusNoTargets(t *testing.T) {
	infra := testInfra()
	infra.TargetsContent = "# only comments\n\n"
	sub := &mockSubmitter{}
	tracker := &mockTracker{}
	m := NewStatusWithDeps(infra, sub, tracker, &mockUploader{})
	cmd := m.Init()
	if cmd != nil {
		t.Fatal("expected nil command for no targets")
	}
	if m.errMsg != "No targets found" {
		t.Fatalf("expected 'No targets found' error, got %q", m.errMsg)
	}
}

func testWordlistInfra() core.InfraOutputs {
	return core.InfraOutputs{
		SQSQueueURL:     "https://sqs.example.com/q",
		S3BucketName:    "test-bucket",
		ECSClusterName:  "test-cluster",
		ToolName:        "ffuf",
		ToolOptions:     "-ac",
		WordlistContent: "admin\nlogin\napi\ntest\n# comment\n\n",
		RuntimeTarget:   "https://example.com/FUZZ",
		ChunkCount:      2,
		WorkerCount:     2,
		ComputeMode:     "fargate",
	}
}

func TestGenericStatusWordlistInit(t *testing.T) {
	sub := &mockSubmitter{}
	tracker := &mockTracker{}
	uploader := &mockUploader{}
	m := NewStatusWithDeps(testWordlistInfra(), sub, tracker, uploader)

	cmd := m.Init()
	if cmd == nil {
		t.Fatal("expected init command for wordlist job")
	}
	if !m.isWordlist {
		t.Fatal("expected isWordlist to be true")
	}
	if m.phase != phaseUploading {
		t.Fatalf("expected phaseUploading, got %d", m.phase)
	}
	// 4 entries split into 2 chunks.
	if m.totalTargets != 2 {
		t.Fatalf("expected 2 chunks, got %d", m.totalTargets)
	}

	// Execute the upload command.
	msg := cmd()
	uc, ok := msg.(uploadCompleteMsg)
	if !ok {
		t.Fatalf("expected uploadCompleteMsg, got %T", msg)
	}
	if uc.err != nil {
		t.Fatalf("unexpected upload error: %v", uc.err)
	}
	if !uploader.uploaded {
		t.Fatal("expected uploader to have been called")
	}
	if len(uc.tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(uc.tasks))
	}
	if uc.words != 5 {
		t.Fatalf("expected 5 preserved wordlist entries, got %d", uc.words)
	}

	// Verify chunk metadata on tasks.
	task := uc.tasks[0]
	if task.ToolName != "ffuf" {
		t.Errorf("expected tool ffuf, got %q", task.ToolName)
	}
	if task.Target != "https://example.com/FUZZ" {
		t.Errorf("expected target URL, got %q", task.Target)
	}
	if task.InputKey == "" {
		t.Error("expected InputKey to be set")
	}
	if task.TotalChunks != 2 {
		t.Errorf("expected TotalChunks=2, got %d", task.TotalChunks)
	}
}

func TestGenericStatusWordlistUploadToEnqueue(t *testing.T) {
	sub := &mockSubmitter{}
	tracker := &mockTracker{}
	m := NewStatusWithDeps(testWordlistInfra(), sub, tracker, &mockUploader{})
	m.Init()

	tasks := []worker.Task{
		{ToolName: "ffuf", Target: "https://example.com/FUZZ", InputKey: "key1", ChunkIdx: 0, TotalChunks: 2},
		{ToolName: "ffuf", Target: "https://example.com/FUZZ", InputKey: "key2", ChunkIdx: 1, TotalChunks: 2},
	}
	_, cmd := m.Update(uploadCompleteMsg{tasks: tasks, words: 4})
	if m.phase != phaseEnqueuing {
		t.Fatalf("expected phaseEnqueuing after upload, got %d", m.phase)
	}
	if cmd == nil {
		t.Fatal("expected enqueue command")
	}
}

func TestGenericStatusWordlistViewShowsTarget(t *testing.T) {
	sub := &mockSubmitter{}
	tracker := &mockTracker{}
	m := NewStatusWithDeps(testWordlistInfra(), sub, tracker, &mockUploader{})
	m.Init()

	v := m.View()
	if !strings.Contains(v, "example.com/FUZZ") {
		t.Fatal("expected view to show runtime target")
	}
	if !strings.Contains(v, "chunks") || !strings.Contains(v, "Uploading") {
		t.Fatal("expected view to show uploading chunks status")
	}
}

func TestParseTargetLines(t *testing.T) {
	tests := []struct {
		content string
		want    int
	}{
		{"example.com\n10.0.0.1\n", 2},
		{"# comment\n\nexample.com\n", 1},
		{"", 0},
		{"\n\n\n", 0},
		{"a\nb\nc\n", 3},
	}
	for _, tt := range tests {
		got := parseTargetLines(tt.content)
		if len(got) != tt.want {
			t.Errorf("parseTargetLines(%q) = %d targets, want %d", tt.content, len(got), tt.want)
		}
	}
}
