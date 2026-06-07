package jobs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTargetList(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "targets.txt")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write target list: %v", err)
	}
	return path
}

func cleanupTargetListPlan(t *testing.T, plan *TargetListPlan) {
	t.Helper()
	if err := plan.Cleanup(); err != nil {
		t.Fatalf("cleanup target-list plan: %v", err)
	}
}

func TestPlanTargetListFilePerTargetTasks(t *testing.T) {
	path := writeTargetList(t, " example.com \n# skipped\n10.0.0.1\n")
	plan, err := PlanTargetListFile("nmap", "job-123", "-sV", path, "", 4, false)
	if err != nil {
		t.Fatalf("plan target-list file: %v", err)
	}
	if plan.FileBacked {
		t.Fatal("expected per-target plan")
	}
	if len(plan.Tasks) != 2 {
		t.Fatalf("tasks = %d, want 2", len(plan.Tasks))
	}
	if plan.Tasks[0].Target != "example.com" || plan.Tasks[1].Target != "10.0.0.1" {
		t.Fatalf("unexpected targets: %#v", plan.Tasks)
	}
	if plan.Tasks[0].InputKey != "" {
		t.Fatalf("per-target task should not have InputKey: %#v", plan.Tasks[0])
	}
}

func TestPlanTargetListFileBackedChunks(t *testing.T) {
	path := writeTargetList(t, "example.com\n10.0.0.1\n10.0.0.2\n")
	plan, err := PlanTargetListFile("httpx", "job-123", "-silent", path, t.TempDir(), 2, true)
	if err != nil {
		t.Fatalf("plan target-list file: %v", err)
	}
	defer cleanupTargetListPlan(t, plan)

	if !plan.FileBacked {
		t.Fatal("expected file-backed plan")
	}
	if plan.TotalTargets != 3 {
		t.Fatalf("TotalTargets = %d, want 3", plan.TotalTargets)
	}
	if len(plan.Tasks) != 2 {
		t.Fatalf("tasks = %d, want 2", len(plan.Tasks))
	}
	for i, task := range plan.Tasks {
		wantKey := InputKey("httpx", "job-123", i)
		if task.InputKey != wantKey {
			t.Fatalf("task %d InputKey = %q, want %q", i, task.InputKey, wantKey)
		}
		if task.TotalChunks != len(plan.Tasks) {
			t.Fatalf("task %d TotalChunks = %d, want %d", i, task.TotalChunks, len(plan.Tasks))
		}
	}
}

func TestUploadTargetListChunksUsesStreamingStorage(t *testing.T) {
	path := writeTargetList(t, "example.com\n10.0.0.1\n")
	plan, err := PlanTargetListFile("httpx", "job-123", "", path, t.TempDir(), 2, true)
	if err != nil {
		t.Fatalf("plan target-list file: %v", err)
	}
	defer cleanupTargetListPlan(t, plan)

	storage := &streamingRecordingStorage{}
	if err := UploadTargetListChunks(context.Background(), storage, "bucket", plan); err != nil {
		t.Fatalf("upload target-list chunks: %v", err)
	}
	if len(storage.streamUploads) != len(plan.ChunkFiles) {
		t.Fatalf("stream uploads = %d, want %d", len(storage.streamUploads), len(plan.ChunkFiles))
	}
	if len(storage.uploads) != 0 {
		t.Fatalf("byte uploads = %d, want 0", len(storage.uploads))
	}
}

func TestUploadTargetListChunksFailureIncludesChunkIndexAndKey(t *testing.T) {
	path := writeTargetList(t, "example.com\n10.0.0.1\n")
	plan, err := PlanTargetListFile("httpx", "job-123", "", path, t.TempDir(), 2, true)
	if err != nil {
		t.Fatalf("plan target-list file: %v", err)
	}
	defer cleanupTargetListPlan(t, plan)

	failKey := InputKey("httpx", "job-123", 1)
	err = UploadTargetListChunks(context.Background(), &streamingRecordingStorage{streamFailKey: failKey}, "bucket", plan)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "chunk 1") || !strings.Contains(err.Error(), failKey) {
		t.Fatalf("error should include chunk index and key, got %v", err)
	}
}
