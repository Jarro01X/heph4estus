package generic

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"heph4estus/internal/cloud"
	"heph4estus/internal/jobs"
	"heph4estus/internal/operator"
	"heph4estus/internal/tui/core"
	"heph4estus/internal/worker"

	tea "charm.land/bubbletea/v2"
)

// mockUploader records chunk uploads.
type mockUploader struct {
	uploaded bool
	err      error
	plan     *jobs.WordlistPlan
}

func (u *mockUploader) UploadChunks(_ context.Context, _ string, plan *jobs.WordlistPlan) error {
	u.uploaded = true
	u.plan = plan
	return u.err
}

// mockSubmitter records calls and returns configured results.
type mockSubmitter struct {
	enqueuedTasks []worker.Task
	enqueueErr    error
	launchErr     error
	spotErr       error
	launchCalls   int
	spotCalls     int
}

func (s *mockSubmitter) EnqueueTasks(_ context.Context, _ string, tasks []worker.Task) error {
	s.enqueuedTasks = tasks
	return s.enqueueErr
}

func (s *mockSubmitter) LaunchWorkers(_ context.Context, _ cloud.ContainerOpts) (string, error) {
	s.launchCalls++
	return "task-123", s.launchErr
}

func (s *mockSubmitter) LaunchSpotWorkers(_ context.Context, _ cloud.SpotOpts) ([]string, error) {
	s.spotCalls++
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

func TestGenericStatusTrackCreatePersistsNATSClientIdentity(t *testing.T) {
	infra := testInfra()
	infra.JobID = "httpx-job"
	infra.NATSUrl = "tls://controller:4222"
	infra.ControllerCAPEM = "ca-pem"
	infra.ControllerHost = "heph-controller"
	infra.NATSClientCertPEM = "operator-cert"
	infra.NATSClientKeyPEM = "operator-key"

	store := operator.NewJobStoreAt(t.TempDir())
	tracker := operator.NewTracker(store)
	m := NewStatusWithDeps(infra, &mockSubmitter{}, &mockTracker{}, &mockUploader{}, tracker)
	_ = m.Init()

	rec, err := store.Load(infra.JobID)
	if err != nil {
		t.Fatalf("load job record: %v", err)
	}
	if rec.NATSClientCertPEM != "operator-cert" || rec.NATSClientKeyPEM != "operator-key" {
		t.Fatalf("NATS client identity = %q/%q", rec.NATSClientCertPEM, rec.NATSClientKeyPEM)
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

func testWordlistInfra(t *testing.T) core.InfraOutputs {
	t.Helper()
	path := filepath.Join(t.TempDir(), "words.txt")
	if err := os.WriteFile(path, []byte("admin\nlogin\napi\ntest\n# comment\n\n"), 0o644); err != nil {
		t.Fatalf("write wordlist: %v", err)
	}
	return core.InfraOutputs{
		SQSQueueURL:    "https://sqs.example.com/q",
		S3BucketName:   "test-bucket",
		ECSClusterName: "test-cluster",
		ToolName:       "ffuf",
		ToolOptions:    "-ac",
		WordlistPath:   path,
		RuntimeTarget:  "https://example.com/FUZZ",
		ChunkCount:     2,
		WorkerCount:    2,
		ComputeMode:    "fargate",
	}
}

func TestGenericStatusWordlistInit(t *testing.T) {
	sub := &mockSubmitter{}
	tracker := &mockTracker{}
	uploader := &mockUploader{}
	m := NewStatusWithDeps(testWordlistInfra(t), sub, tracker, uploader)

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
	if uploader.plan == nil || len(uploader.plan.ChunkFiles) != 2 {
		t.Fatalf("expected file-based chunk plan, got %#v", uploader.plan)
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
	m := NewStatusWithDeps(testWordlistInfra(t), sub, tracker, &mockUploader{})
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
	m := NewStatusWithDeps(testWordlistInfra(t), sub, tracker, &mockUploader{})
	m.Init()

	v := m.View()
	if !strings.Contains(v, "example.com/FUZZ") {
		t.Fatal("expected view to show runtime target")
	}
	if !strings.Contains(v, "chunks") || !strings.Contains(v, "Uploading") {
		t.Fatal("expected view to show uploading chunks status")
	}
	if !strings.Contains(v, "Words") {
		t.Fatal("expected view to show total words after planning")
	}
}

func TestGenericStatusWordlistContentFallback(t *testing.T) {
	infra := testWordlistInfra(t)
	infra.WordlistPath = ""
	infra.WordlistContent = "admin\nlogin\n"

	uploader := &mockUploader{}
	m := NewStatusWithDeps(infra, &mockSubmitter{}, &mockTracker{}, uploader)
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("expected init command")
	}
	msg := cmd()
	uc, ok := msg.(uploadCompleteMsg)
	if !ok {
		t.Fatalf("expected uploadCompleteMsg, got %T", msg)
	}
	if uc.err != nil {
		t.Fatalf("unexpected upload error: %v", uc.err)
	}
	if uploader.plan == nil || len(uploader.plan.ChunkData) == 0 {
		t.Fatal("expected fallback in-memory chunk data")
	}
}

// --- Track 6B: reuse/cleanup summary and export gating tests ---

func TestGenericStatusViewShowsReused(t *testing.T) {
	infra := testInfra()
	infra.Reused = true
	infra.CleanupPolicy = "reuse"
	m := NewStatusWithDeps(infra, &mockSubmitter{}, &mockTracker{}, &mockUploader{})
	m.totalTargets = 10
	m.phase = phaseScanning

	v := m.View()
	if !strings.Contains(v, "reused") {
		t.Fatal("expected 'reused' in view")
	}
}

func TestGenericStatusViewShowsFreshlyDeployed(t *testing.T) {
	infra := testInfra()
	infra.Reused = false
	infra.CleanupPolicy = "destroy-after"
	m := NewStatusWithDeps(infra, &mockSubmitter{}, &mockTracker{}, &mockUploader{})
	m.totalTargets = 10
	m.phase = phaseScanning

	v := m.View()
	if !strings.Contains(v, "freshly deployed") {
		t.Fatal("expected 'freshly deployed' in view")
	}
	if !strings.Contains(v, "destroy-after") {
		t.Fatal("expected cleanup policy in view")
	}
}

func TestGenericStatusDestroyAfterWithoutOutputDir(t *testing.T) {
	infra := testInfra()
	infra.CleanupPolicy = "destroy-after"
	infra.OutputDir = ""
	m := NewStatusWithDeps(infra, &mockSubmitter{}, &mockTracker{}, &mockUploader{})
	m.totalTargets = 2
	m.phase = phaseScanning

	m.Update(scanProgressMsg{completed: 2})
	if m.phase != phaseComplete {
		t.Fatalf("expected phaseComplete, got %d", m.phase)
	}
	if !strings.Contains(m.cleanupWarning, "no output directory") {
		t.Fatalf("expected warning, got %q", m.cleanupWarning)
	}
}

func TestGenericStatusDestroyAfterWithOutputDir(t *testing.T) {
	infra := testInfra()
	infra.CleanupPolicy = "destroy-after"
	infra.OutputDir = "/tmp/export"
	m := NewStatusWithDeps(infra, &mockSubmitter{}, &mockTracker{}, &mockUploader{})
	m.totalTargets = 2
	m.phase = phaseScanning
	m.storage = &mockExportStorage{}

	m.Update(scanProgressMsg{completed: 2})
	if m.phase != phaseExporting {
		t.Fatalf("expected phaseExporting, got %d", m.phase)
	}
}

func TestGenericStatusExportComplete(t *testing.T) {
	infra := testInfra()
	infra.CleanupPolicy = "destroy-after"
	m := NewStatusWithDeps(infra, &mockSubmitter{}, &mockTracker{}, &mockUploader{})
	m.phase = phaseExporting

	m.Update(exportCompleteMsg{dir: "/tmp/export/httpx/job-1", count: 3})
	if !m.infra.Exported {
		t.Fatal("expected Exported to be true")
	}
	if m.infra.ExportDir != "/tmp/export/httpx/job-1" {
		t.Fatalf("expected ExportDir, got %q", m.infra.ExportDir)
	}
}

func TestGenericStatusExportFailed(t *testing.T) {
	infra := testInfra()
	infra.CleanupPolicy = "destroy-after"
	m := NewStatusWithDeps(infra, &mockSubmitter{}, &mockTracker{}, &mockUploader{})
	m.phase = phaseExporting

	m.Update(exportCompleteMsg{err: fmt.Errorf("download error")})
	if m.infra.Exported {
		t.Fatal("expected Exported to remain false")
	}
	if !strings.Contains(m.cleanupWarning, "export failed") {
		t.Fatalf("expected export failure warning, got %q", m.cleanupWarning)
	}
}

func TestGenericStatusViewShowsExportingPhase(t *testing.T) {
	infra := testInfra()
	infra.OutputDir = "/tmp/out"
	m := NewStatusWithDeps(infra, &mockSubmitter{}, &mockTracker{}, &mockUploader{})
	m.totalTargets = 10
	m.completed = 10
	m.phase = phaseExporting

	v := m.View()
	if !strings.Contains(v, "Exporting") {
		t.Fatal("expected 'Exporting' in view")
	}
}

// mockExportStorage is a minimal cloud.Storage for export gating tests.
type mockExportStorage struct{}

func (s *mockExportStorage) Upload(context.Context, string, string, []byte) error { return nil }
func (s *mockExportStorage) Download(context.Context, string, string) ([]byte, error) {
	return nil, nil
}
func (s *mockExportStorage) List(context.Context, string, string) ([]string, error) { return nil, nil }
func (s *mockExportStorage) Count(context.Context, string, string) (int, error)     { return 0, nil }

// --- Track 1 PR 5.12: auto-destroy lifecycle tests ---

func TestGenericStatusExportSuccess_DestroyAfter_TriggersDestroy(t *testing.T) {
	infra := testInfra()
	infra.CleanupPolicy = "destroy-after"
	infra.OutputDir = "/tmp/export"
	m := NewStatusWithDeps(infra, &mockSubmitter{}, &mockTracker{}, &mockUploader{})
	m.storage = &mockExportStorage{}
	m.destroyer = &mockDestroyer{}
	m.phase = phaseExporting

	_, cmd := m.Update(exportCompleteMsg{dir: "/tmp/export/httpx/job-1", count: 3})
	if m.phase != phaseDestroying {
		t.Fatalf("expected phaseDestroying, got %d", m.phase)
	}
	if !m.infra.Exported {
		t.Fatal("expected Exported to be true")
	}
	if cmd == nil {
		t.Fatal("expected destroy command")
	}
}

func TestGenericStatusExportSuccess_NoDestroyer_ShowsWarning(t *testing.T) {
	infra := testInfra()
	infra.CleanupPolicy = "destroy-after"
	infra.OutputDir = "/tmp/export"
	m := NewStatusWithDeps(infra, &mockSubmitter{}, &mockTracker{}, &mockUploader{})
	m.storage = &mockExportStorage{}
	// destroyer is nil
	m.phase = phaseExporting

	_, cmd := m.Update(exportCompleteMsg{dir: "/tmp/export/httpx/job-1", count: 3})
	if m.phase != phaseComplete {
		t.Fatalf("expected phaseComplete when destroyer is nil, got %d", m.phase)
	}
	if !strings.Contains(m.cleanupWarning, "no terraform directory") {
		t.Fatalf("expected terraform warning, got %q", m.cleanupWarning)
	}
	if cmd == nil {
		t.Fatal("expected navigate command")
	}
}

func TestGenericStatusDestroySuccess_SetsDestroyed(t *testing.T) {
	infra := testInfra()
	infra.CleanupPolicy = "destroy-after"
	m := NewStatusWithDeps(infra, &mockSubmitter{}, &mockTracker{}, &mockUploader{})
	m.phase = phaseDestroying

	_, cmd := m.Update(autoDestroyCompleteMsg{err: nil})
	if m.phase != phaseComplete {
		t.Fatalf("expected phaseComplete, got %d", m.phase)
	}
	if !m.infra.Destroyed {
		t.Fatal("expected Destroyed to be true")
	}
	if m.infra.DestroyErr != "" {
		t.Fatalf("expected no DestroyErr, got %q", m.infra.DestroyErr)
	}
	if cmd == nil {
		t.Fatal("expected navigate command")
	}
}

func TestGenericStatusDestroyFailure_SetsDestroyErr(t *testing.T) {
	infra := testInfra()
	infra.CleanupPolicy = "destroy-after"
	m := NewStatusWithDeps(infra, &mockSubmitter{}, &mockTracker{}, &mockUploader{})
	m.phase = phaseDestroying

	_, cmd := m.Update(autoDestroyCompleteMsg{err: fmt.Errorf("terraform timeout")})
	if m.phase != phaseComplete {
		t.Fatalf("expected phaseComplete, got %d", m.phase)
	}
	if m.infra.Destroyed {
		t.Fatal("expected Destroyed to remain false")
	}
	if m.infra.DestroyErr == "" {
		t.Fatal("expected DestroyErr to be set")
	}
	if !strings.Contains(m.cleanupWarning, "destroy failed") {
		t.Fatalf("expected destroy failure warning, got %q", m.cleanupWarning)
	}
	if cmd == nil {
		t.Fatal("expected navigate command")
	}
}

func TestGenericStatusViewShowsDestroyingPhase(t *testing.T) {
	infra := testInfra()
	infra.CleanupPolicy = "destroy-after"
	infra.Exported = true
	infra.ExportDir = "/tmp/export/httpx/job-1"
	m := NewStatusWithDeps(infra, &mockSubmitter{}, &mockTracker{}, &mockUploader{})
	m.totalTargets = 10
	m.completed = 10
	m.phase = phaseDestroying

	v := m.View()
	if !strings.Contains(v, "Destroying") {
		t.Fatal("expected 'Destroying' in view during phaseDestroying")
	}
	if !strings.Contains(v, "/tmp/export/httpx/job-1") {
		t.Fatal("expected export dir in view during phaseDestroying")
	}
}

func TestGenericStatusViewShowsDestroyedOnComplete(t *testing.T) {
	infra := testInfra()
	infra.Exported = true
	infra.ExportDir = "/tmp/export/httpx/job-1"
	infra.Destroyed = true
	m := NewStatusWithDeps(infra, &mockSubmitter{}, &mockTracker{}, &mockUploader{})
	m.totalTargets = 10
	m.completed = 10
	m.phase = phaseComplete

	v := m.View()
	if !strings.Contains(v, "destroyed") {
		t.Fatal("expected 'destroyed' label in view")
	}
}

func TestGenericStatusAutoDestroyEndToEnd(t *testing.T) {
	infra := testInfra()
	infra.CleanupPolicy = "destroy-after"
	infra.OutputDir = "/tmp/export"
	destroyer := &mockDestroyer{}
	m := NewStatusWithDeps(infra, &mockSubmitter{}, &mockTracker{}, &mockUploader{})
	m.storage = &mockExportStorage{}
	m.destroyer = destroyer
	m.totalTargets = 2
	m.phase = phaseScanning

	// Scan complete triggers export.
	_, _ = m.Update(scanProgressMsg{completed: 2})
	if m.phase != phaseExporting {
		t.Fatalf("expected phaseExporting, got %d", m.phase)
	}

	// Export succeeds, triggers destroy.
	_, cmd := m.Update(exportCompleteMsg{dir: "/tmp/export/httpx/job-1", count: 2})
	if m.phase != phaseDestroying {
		t.Fatalf("expected phaseDestroying, got %d", m.phase)
	}

	// Execute the destroy command.
	msg := cmd()
	destroyMsg, ok := msg.(autoDestroyCompleteMsg)
	if !ok {
		t.Fatalf("expected autoDestroyCompleteMsg, got %T", msg)
	}
	if !destroyer.called {
		t.Fatal("expected destroyer to be called")
	}

	// Destroy complete, navigates to results.
	_, cmd = m.Update(destroyMsg)
	if m.phase != phaseComplete {
		t.Fatalf("expected phaseComplete, got %d", m.phase)
	}
	if !m.infra.Destroyed {
		t.Fatal("expected Destroyed to be true")
	}
	if cmd == nil {
		t.Fatal("expected navigate command")
	}
	navMsg := cmd()
	nav, ok := navMsg.(core.NavigateWithDataMsg)
	if !ok {
		t.Fatalf("expected NavigateWithDataMsg, got %T", navMsg)
	}
	if nav.Target != core.ViewGenericResults {
		t.Fatalf("expected ViewGenericResults, got %v", nav.Target)
	}
	navInfra, ok := nav.Data.(core.InfraOutputs)
	if !ok {
		t.Fatalf("expected InfraOutputs in nav data, got %T", nav.Data)
	}
	if !navInfra.Destroyed {
		t.Fatal("expected nav data to carry Destroyed=true")
	}
}

func TestGenericStatusProviderNativeDestroyAfterUsesDestroyer(t *testing.T) {
	infra := testInfra()
	infra.Cloud = cloud.KindHetzner
	infra.CleanupPolicy = "destroy-after"
	infra.OutputDir = "/tmp/export"

	destroyer := &mockDestroyer{}
	m := NewStatusWithDeps(infra, &mockSubmitter{}, &mockTracker{}, &mockUploader{})
	m.storage = &mockExportStorage{}
	m.destroyer = destroyer
	m.phase = phaseExporting

	_, cmd := m.Update(exportCompleteMsg{dir: "/tmp/export/httpx/job-1", count: 2})
	if m.phase != phaseDestroying {
		t.Fatalf("expected phaseDestroying, got %d", m.phase)
	}
	if cmd == nil {
		t.Fatal("expected destroy command")
	}
}

func testSelfhostedInfra() core.InfraOutputs {
	return core.InfraOutputs{
		Cloud:          cloud.KindManual,
		SQSQueueURL:    "test-stream",
		S3BucketName:   "test-bucket",
		ToolName:       "httpx",
		ToolOptions:    "-silent",
		TargetsContent: "example.com\n10.0.0.1\n",
		WorkerCount:    5,
		ComputeMode:    "auto",
	}
}

func TestGenericStatusSelfhostedUsesLaunchWorkers(t *testing.T) {
	sub := &mockSubmitter{}
	tracker := &mockTracker{}
	m := NewStatusWithDeps(testSelfhostedInfra(), sub, tracker, &mockUploader{})
	m.Init()

	// Enqueue succeeds → launch phase.
	_, cmd := m.Update(enqueueProgressMsg{sent: 2, total: 2})
	if m.phase != phaseLaunching {
		t.Fatalf("expected phaseLaunching, got %d", m.phase)
	}
	if cmd == nil {
		t.Fatal("expected launch command")
	}
	// Execute the launch command — should call LaunchWorkers, not LaunchSpotWorkers.
	msg := cmd()
	_, ok := msg.(launchProgressMsg)
	if !ok {
		t.Fatalf("expected launchProgressMsg (not spotLaunchMsg), got %T", msg)
	}
	if sub.launchCalls != 1 {
		t.Fatalf("expected LaunchWorkers to be called once, got %d", sub.launchCalls)
	}
}

func TestGenericStatusSelfhostedNeverUsesSpot(t *testing.T) {
	infra := testSelfhostedInfra()
	infra.WorkerCount = 200 // above SpotThreshold
	infra.ComputeMode = "auto"
	if useSpot(infra) {
		t.Fatal("selfhosted should never use spot even with high worker count")
	}
}

func TestGenericStatusSelfhostedDestroyAfterSkipped(t *testing.T) {
	infra := testSelfhostedInfra()
	infra.CleanupPolicy = "destroy-after"
	infra.OutputDir = ""
	m := NewStatusWithDeps(infra, &mockSubmitter{}, &mockTracker{}, &mockUploader{})
	m.totalTargets = 2
	m.phase = phaseScanning

	m.Update(scanProgressMsg{completed: 2})
	if m.phase != phaseComplete {
		t.Fatalf("expected phaseComplete, got %d", m.phase)
	}
	if !strings.Contains(m.cleanupWarning, "selfhosted does not support auto-destroy") {
		t.Fatalf("expected selfhosted destroy warning, got %q", m.cleanupWarning)
	}
}

func TestGenericStatusSelfhostedExportComplete_SkipsDestroy(t *testing.T) {
	infra := testSelfhostedInfra()
	infra.CleanupPolicy = "destroy-after"
	infra.OutputDir = "/tmp/export"
	m := NewStatusWithDeps(infra, &mockSubmitter{}, &mockTracker{}, &mockUploader{})
	m.storage = &mockExportStorage{}
	m.destroyer = &mockDestroyer{} // destroyer exists but should be skipped
	m.phase = phaseExporting

	_, cmd := m.Update(exportCompleteMsg{dir: "/tmp/export/httpx/job-sh", count: 2})
	// Selfhosted should skip destroy and go to complete.
	if m.phase != phaseComplete {
		t.Fatalf("expected phaseComplete (selfhosted skips destroy), got %d", m.phase)
	}
	if !strings.Contains(m.cleanupWarning, "selfhosted does not support auto-destroy") {
		t.Fatalf("expected selfhosted destroy warning, got %q", m.cleanupWarning)
	}
	if cmd == nil {
		t.Fatal("expected navigate command")
	}
}

func TestGenericStatusProviderNativeSkipsLaunchWorkers(t *testing.T) {
	infra := testInfra()
	infra.Cloud = cloud.KindHetzner
	infra.FleetWorkerCount = 3

	sub := &mockSubmitter{}
	m := NewStatusWithDeps(infra, sub, &mockTracker{}, &mockUploader{})
	m.Init()

	_, cmd := m.Update(enqueueProgressMsg{sent: 2, total: 2})
	if cmd == nil {
		t.Fatal("expected launch command")
	}
	msg := cmd()
	if _, ok := msg.(launchProgressMsg); !ok {
		t.Fatalf("expected launchProgressMsg, got %T", msg)
	}
	if sub.launchCalls != 0 {
		t.Fatalf("expected provider-native path to skip LaunchWorkers, got %d calls", sub.launchCalls)
	}
	if sub.spotCalls != 0 {
		t.Fatalf("expected provider-native path to skip LaunchSpotWorkers, got %d calls", sub.spotCalls)
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
