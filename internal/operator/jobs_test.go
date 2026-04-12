package operator

import (
	"strings"
	"testing"
	"time"
)

func TestJobStore_CreateAndLoad(t *testing.T) {
	store := NewJobStoreAt(t.TempDir())

	rec := &JobRecord{
		JobID:       "nmap-20260407t120000-abcd",
		ToolName:    "nmap",
		Phase:       PhaseEnqueuing,
		TotalTasks:  100,
		WorkerCount: 10,
		Bucket:      "test-bucket",
	}

	if err := store.Create(rec); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	loaded, err := store.Load("nmap-20260407t120000-abcd")
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if loaded.JobID != rec.JobID {
		t.Errorf("job_id = %q, want %q", loaded.JobID, rec.JobID)
	}
	if loaded.ToolName != "nmap" {
		t.Errorf("tool_name = %q, want nmap", loaded.ToolName)
	}
	if loaded.Phase != PhaseEnqueuing {
		t.Errorf("phase = %q, want enqueuing", loaded.Phase)
	}
	if loaded.TotalTasks != 100 {
		t.Errorf("total_tasks = %d, want 100", loaded.TotalTasks)
	}
	if loaded.CreatedAt.IsZero() {
		t.Error("created_at should be set automatically")
	}
	if loaded.UpdatedAt.IsZero() {
		t.Error("updated_at should be set automatically")
	}
}

func TestJobStore_CreateDuplicate(t *testing.T) {
	store := NewJobStoreAt(t.TempDir())

	rec := &JobRecord{JobID: "dup-job", ToolName: "httpx", Phase: PhaseEnqueuing}
	if err := store.Create(rec); err != nil {
		t.Fatal(err)
	}
	err := store.Create(rec)
	if err == nil {
		t.Fatal("expected error for duplicate create")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestJobStore_CreateEmptyID(t *testing.T) {
	store := NewJobStoreAt(t.TempDir())
	err := store.Create(&JobRecord{})
	if err == nil {
		t.Fatal("expected error for empty job ID")
	}
}

func TestJobStore_LoadNotFound(t *testing.T) {
	store := NewJobStoreAt(t.TempDir())
	_, err := store.Load("nonexistent")
	if err == nil {
		t.Fatal("expected error for missing job")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestJobStore_Update(t *testing.T) {
	store := NewJobStoreAt(t.TempDir())

	rec := &JobRecord{
		JobID:    "update-test",
		ToolName: "nmap",
		Phase:    PhaseEnqueuing,
	}
	_ = store.Create(rec)

	rec.Phase = PhaseScanning
	rec.TotalTasks = 50
	if err := store.Update(rec); err != nil {
		t.Fatalf("update failed: %v", err)
	}

	loaded, _ := store.Load("update-test")
	if loaded.Phase != PhaseScanning {
		t.Errorf("phase = %q, want scanning", loaded.Phase)
	}
	if loaded.TotalTasks != 50 {
		t.Errorf("total_tasks = %d, want 50", loaded.TotalTasks)
	}
}

func TestJobStore_UpdateNotFound(t *testing.T) {
	store := NewJobStoreAt(t.TempDir())
	err := store.Update(&JobRecord{JobID: "ghost"})
	if err == nil {
		t.Fatal("expected error for update of nonexistent record")
	}
}

func TestJobStore_List(t *testing.T) {
	store := NewJobStoreAt(t.TempDir())

	_ = store.Create(&JobRecord{JobID: "job-a", ToolName: "nmap", Phase: PhaseEnqueuing})
	_ = store.Create(&JobRecord{JobID: "job-b", ToolName: "httpx", Phase: PhaseComplete})
	_ = store.Create(&JobRecord{JobID: "job-c", ToolName: "ffuf", Phase: PhaseFailed})

	ids, err := store.List()
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(ids) != 3 {
		t.Fatalf("expected 3 jobs, got %d", len(ids))
	}
}

func TestJobStore_ListEmptyDir(t *testing.T) {
	store := NewJobStoreAt(t.TempDir())
	ids, err := store.List()
	if err != nil {
		t.Fatalf("list on empty dir failed: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("expected 0 jobs, got %d", len(ids))
	}
}

func TestJobStore_ListNonexistentDir(t *testing.T) {
	store := NewJobStoreAt("/nonexistent/jobs")
	ids, err := store.List()
	if err != nil {
		t.Fatalf("list on nonexistent dir should not error: %v", err)
	}
	if ids != nil {
		t.Fatalf("expected nil, got %v", ids)
	}
}

func TestJobStore_CreatedAtPreserved(t *testing.T) {
	store := NewJobStoreAt(t.TempDir())

	custom := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	rec := &JobRecord{
		JobID:     "time-test",
		ToolName:  "nmap",
		Phase:     PhaseEnqueuing,
		CreatedAt: custom,
	}
	_ = store.Create(rec)

	loaded, _ := store.Load("time-test")
	if !loaded.CreatedAt.Equal(custom) {
		t.Errorf("created_at = %v, want %v", loaded.CreatedAt, custom)
	}
}

func TestJobRecord_AllFields(t *testing.T) {
	store := NewJobStoreAt(t.TempDir())

	rec := &JobRecord{
		JobID:          "full-test",
		ToolName:       "ffuf",
		Phase:          PhaseUploading,
		TotalTasks:     10,
		TotalWords:     5000,
		WorkerCount:    5,
		ComputeMode:    "spot",
		Cloud:          "selfhosted",
		CleanupPolicy:  "destroy-after",
		Bucket:         "my-bucket",
		ResultPrefix:   "scans/ffuf/full-test/results/",
		ArtifactPrefix: "scans/ffuf/full-test/artifacts/",
		RuntimeTarget:  "https://example.com/FUZZ",
		LastError:      "",
		LocalOutputDir: "/tmp/out",
	}
	_ = store.Create(rec)

	loaded, _ := store.Load("full-test")
	if loaded.TotalWords != 5000 {
		t.Errorf("total_words = %d, want 5000", loaded.TotalWords)
	}
	if loaded.Cloud != "selfhosted" {
		t.Errorf("cloud = %q, want selfhosted", loaded.Cloud)
	}
	if loaded.RuntimeTarget != "https://example.com/FUZZ" {
		t.Errorf("runtime_target = %q", loaded.RuntimeTarget)
	}
	if loaded.LocalOutputDir != "/tmp/out" {
		t.Errorf("local_output_dir = %q", loaded.LocalOutputDir)
	}
}

func TestJobRecord_CloudPersistence(t *testing.T) {
	store := NewJobStoreAt(t.TempDir())

	for _, cloud := range []string{"aws", "selfhosted", ""} {
		jobID := "cloud-" + cloud
		if cloud == "" {
			jobID = "cloud-empty"
		}
		rec := &JobRecord{
			JobID:    jobID,
			ToolName: "nmap",
			Phase:    PhaseEnqueuing,
			Cloud:    cloud,
		}
		if err := store.Create(rec); err != nil {
			t.Fatalf("create %q: %v", jobID, err)
		}
		loaded, err := store.Load(jobID)
		if err != nil {
			t.Fatalf("load %q: %v", jobID, err)
		}
		if loaded.Cloud != cloud {
			t.Errorf("job %q: cloud = %q, want %q", jobID, loaded.Cloud, cloud)
		}
	}
}

func TestJobRecord_CloudUpdatePersists(t *testing.T) {
	store := NewJobStoreAt(t.TempDir())

	rec := &JobRecord{
		JobID:    "cloud-update",
		ToolName: "nmap",
		Phase:    PhaseEnqueuing,
		Cloud:    "aws",
	}
	_ = store.Create(rec)

	rec.Cloud = "selfhosted"
	rec.Phase = PhaseScanning
	if err := store.Update(rec); err != nil {
		t.Fatalf("update: %v", err)
	}

	loaded, _ := store.Load("cloud-update")
	if loaded.Cloud != "selfhosted" {
		t.Errorf("cloud = %q, want selfhosted", loaded.Cloud)
	}
}
