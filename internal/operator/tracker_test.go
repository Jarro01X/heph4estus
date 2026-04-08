package operator

import (
	"fmt"
	"testing"
)

func TestTracker_CreateAndUpdatePhase(t *testing.T) {
	store := NewJobStoreAt(t.TempDir())
	tr := NewTracker(store)

	rec := &JobRecord{
		JobID:      "tr-job-1",
		ToolName:   "nmap",
		Phase:      PhaseEnqueuing,
		TotalTasks: 10,
		Bucket:     "bucket",
	}

	if err := tr.Create(rec); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Verify prefixes were auto-populated.
	loaded, _ := store.Load("tr-job-1")
	if loaded.ResultPrefix == "" {
		t.Error("expected result_prefix to be auto-populated")
	}
	if loaded.ArtifactPrefix == "" {
		t.Error("expected artifact_prefix to be auto-populated")
	}

	// Update to launching.
	if err := tr.UpdatePhase("tr-job-1", PhaseLaunching); err != nil {
		t.Fatalf("update phase failed: %v", err)
	}
	loaded, _ = store.Load("tr-job-1")
	if loaded.Phase != PhaseLaunching {
		t.Errorf("phase = %q, want launching", loaded.Phase)
	}
	if !loaded.StartedAt.IsZero() {
		t.Error("started_at should not be set until scanning phase")
	}

	// Update to scanning — should set StartedAt.
	if err := tr.UpdatePhase("tr-job-1", PhaseScanning); err != nil {
		t.Fatalf("update phase failed: %v", err)
	}
	loaded, _ = store.Load("tr-job-1")
	if loaded.Phase != PhaseScanning {
		t.Errorf("phase = %q, want scanning", loaded.Phase)
	}
	if loaded.StartedAt.IsZero() {
		t.Error("started_at should be set when entering scanning phase")
	}
}

func TestTracker_Complete(t *testing.T) {
	store := NewJobStoreAt(t.TempDir())
	tr := NewTracker(store)

	store.Create(&JobRecord{
		JobID:     "complete-job",
		ToolName:  "httpx",
		Phase:     PhaseScanning,
		LastError: "transient error",
	})

	if err := tr.Complete("complete-job"); err != nil {
		t.Fatalf("complete failed: %v", err)
	}

	loaded, _ := store.Load("complete-job")
	if loaded.Phase != PhaseComplete {
		t.Errorf("phase = %q, want complete", loaded.Phase)
	}
	if loaded.LastError != "" {
		t.Errorf("last_error should be cleared, got %q", loaded.LastError)
	}
}

func TestTracker_Fail(t *testing.T) {
	store := NewJobStoreAt(t.TempDir())
	tr := NewTracker(store)

	store.Create(&JobRecord{
		JobID:    "fail-job",
		ToolName: "nmap",
		Phase:    PhaseLaunching,
	})

	if err := tr.Fail("fail-job", fmt.Errorf("worker launch timeout")); err != nil {
		t.Fatalf("fail failed: %v", err)
	}

	loaded, _ := store.Load("fail-job")
	if loaded.Phase != PhaseFailed {
		t.Errorf("phase = %q, want failed", loaded.Phase)
	}
	if loaded.LastError != "worker launch timeout" {
		t.Errorf("last_error = %q, want 'worker launch timeout'", loaded.LastError)
	}
}

func TestTracker_FailWithNilError(t *testing.T) {
	store := NewJobStoreAt(t.TempDir())
	tr := NewTracker(store)

	store.Create(&JobRecord{
		JobID:    "fail-nil",
		ToolName: "nmap",
		Phase:    PhaseScanning,
	})

	if err := tr.Fail("fail-nil", nil); err != nil {
		t.Fatalf("fail with nil error failed: %v", err)
	}

	loaded, _ := store.Load("fail-nil")
	if loaded.Phase != PhaseFailed {
		t.Errorf("phase = %q, want failed", loaded.Phase)
	}
}

func TestNoopTracker_AllMethodsSucceed(t *testing.T) {
	tr := NoopTracker()

	if err := tr.Create(&JobRecord{JobID: "noop"}); err != nil {
		t.Errorf("noop create should succeed: %v", err)
	}
	if err := tr.UpdatePhase("noop", PhaseScanning); err != nil {
		t.Errorf("noop update should succeed: %v", err)
	}
	if err := tr.Complete("noop"); err != nil {
		t.Errorf("noop complete should succeed: %v", err)
	}
	if err := tr.Fail("noop", fmt.Errorf("err")); err != nil {
		t.Errorf("noop fail should succeed: %v", err)
	}
}

func TestTracker_UpdatePhaseNotFound(t *testing.T) {
	store := NewJobStoreAt(t.TempDir())
	tr := NewTracker(store)

	err := tr.UpdatePhase("ghost", PhaseScanning)
	if err == nil {
		t.Fatal("expected error for nonexistent job")
	}
}

func TestTracker_CreateAutoPopulatesPrefixes(t *testing.T) {
	store := NewJobStoreAt(t.TempDir())
	tr := NewTracker(store)

	rec := &JobRecord{
		JobID:    "prefix-test",
		ToolName: "httpx",
		Phase:    PhaseEnqueuing,
	}
	tr.Create(rec)

	loaded, _ := store.Load("prefix-test")
	if loaded.ResultPrefix != "scans/httpx/prefix-test/results/" {
		t.Errorf("result_prefix = %q", loaded.ResultPrefix)
	}
	if loaded.ArtifactPrefix != "scans/httpx/prefix-test/artifacts/" {
		t.Errorf("artifact_prefix = %q", loaded.ArtifactPrefix)
	}
}

func TestTracker_CreatePreservesExplicitPrefixes(t *testing.T) {
	store := NewJobStoreAt(t.TempDir())
	tr := NewTracker(store)

	rec := &JobRecord{
		JobID:          "custom-prefix",
		ToolName:       "nmap",
		Phase:          PhaseEnqueuing,
		ResultPrefix:   "custom/results/",
		ArtifactPrefix: "custom/artifacts/",
	}
	tr.Create(rec)

	loaded, _ := store.Load("custom-prefix")
	if loaded.ResultPrefix != "custom/results/" {
		t.Errorf("result_prefix = %q, want custom/results/", loaded.ResultPrefix)
	}
	if loaded.ArtifactPrefix != "custom/artifacts/" {
		t.Errorf("artifact_prefix = %q, want custom/artifacts/", loaded.ArtifactPrefix)
	}
}
