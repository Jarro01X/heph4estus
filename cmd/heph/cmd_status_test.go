package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"heph4estus/internal/operator"
)

func TestRunStatusRequiresJobID(t *testing.T) {
	err := runStatus([]string{}, nil)
	if err == nil {
		t.Fatal("expected error when --job-id is missing")
	}
	if !strings.Contains(err.Error(), "--job-id flag is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunStatusRejectsInvalidFormat(t *testing.T) {
	err := runStatus([]string{"--job-id", "test-123", "--format", "yaml"}, nil)
	if err == nil {
		t.Fatal("expected error for invalid format")
	}
	if !strings.Contains(err.Error(), "--format must be text or json") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunStatusMissingJobRecord(t *testing.T) {
	// Point the job store at an empty temp directory.
	origDir := t.TempDir()
	store := operator.NewJobStoreAt(origDir)

	// Verify Load returns not found.
	_, err := store.Load("nonexistent-job")
	if err == nil {
		t.Fatal("expected error for missing job")
	}
	if !strings.Contains(err.Error(), "job record not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildSnapshotActiveJob(t *testing.T) {
	now := time.Now().UTC()
	rec := &operator.JobRecord{
		JobID:         "nmap-20260407t120000-abcd",
		ToolName:      "nmap",
		Phase:         operator.PhaseScanning,
		CreatedAt:     now.Add(-5 * time.Minute),
		UpdatedAt:     now.Add(-10 * time.Second),
		TotalTasks:    100,
		WorkerCount:   10,
		ComputeMode:   "fargate",
		CleanupPolicy: "reuse",
		Bucket:        "results-bucket",
		ResultPrefix:  "scans/nmap/nmap-20260407t120000-abcd/results/",
	}

	snap := buildSnapshot(rec, 42)

	if snap.JobID != rec.JobID {
		t.Errorf("JobID = %q, want %q", snap.JobID, rec.JobID)
	}
	if snap.Tool != "nmap" {
		t.Errorf("Tool = %q, want nmap", snap.Tool)
	}
	if snap.Phase != operator.PhaseScanning {
		t.Errorf("Phase = %q, want scanning", snap.Phase)
	}
	if snap.Progress.Completed != 42 {
		t.Errorf("Completed = %d, want 42", snap.Progress.Completed)
	}
	if snap.Progress.Total != 100 {
		t.Errorf("Total = %d, want 100", snap.Progress.Total)
	}
	if snap.Progress.Percent != 42.0 {
		t.Errorf("Percent = %.1f, want 42.0", snap.Progress.Percent)
	}
	if snap.CleanupPolicy != "reuse" {
		t.Errorf("CleanupPolicy = %q, want reuse", snap.CleanupPolicy)
	}
}

func TestBuildSnapshotInfersComplete(t *testing.T) {
	now := time.Now().UTC()
	rec := &operator.JobRecord{
		JobID:        "httpx-20260407t120000-abcd",
		ToolName:     "httpx",
		Phase:        operator.PhaseScanning,
		CreatedAt:    now.Add(-10 * time.Minute),
		TotalTasks:   50,
		Bucket:       "bucket",
		ResultPrefix: "scans/httpx/httpx-20260407t120000-abcd/results/",
	}

	snap := buildSnapshot(rec, 50)

	if snap.Phase != operator.PhaseComplete {
		t.Errorf("Phase = %q, want complete when completed == total", snap.Phase)
	}
	if snap.Progress.Percent != 100.0 {
		t.Errorf("Percent = %.1f, want 100.0", snap.Progress.Percent)
	}
}

func TestBuildSnapshotCompleteSynthesizesCompleted(t *testing.T) {
	now := time.Now().UTC()
	rec := &operator.JobRecord{
		JobID:        "done-job",
		ToolName:     "httpx",
		Phase:        operator.PhaseComplete,
		CreatedAt:    now.Add(-10 * time.Minute),
		UpdatedAt:    now.Add(-2 * time.Minute),
		TotalTasks:   50,
		Bucket:       "results-bucket",
		ResultPrefix: "scans/httpx/done-job/results/",
	}

	snap := buildSnapshot(rec, 0)

	if snap.Phase != operator.PhaseComplete {
		t.Errorf("Phase = %q, want complete", snap.Phase)
	}
	if snap.Progress.Completed != 50 {
		t.Errorf("Completed = %d, want 50 for completed job", snap.Progress.Completed)
	}
	if snap.Progress.Percent != 100.0 {
		t.Errorf("Percent = %.1f, want 100.0 for completed job", snap.Progress.Percent)
	}
}

func TestBuildSnapshotInfersScanning(t *testing.T) {
	now := time.Now().UTC()
	rec := &operator.JobRecord{
		JobID:      "nmap-test",
		ToolName:   "nmap",
		Phase:      operator.PhaseLaunching,
		CreatedAt:  now.Add(-2 * time.Minute),
		TotalTasks: 20,
	}

	snap := buildSnapshot(rec, 5)

	if snap.Phase != operator.PhaseScanning {
		t.Errorf("Phase = %q, want scanning when results > 0", snap.Phase)
	}
}

func TestBuildSnapshotPreservesTerminalPhase(t *testing.T) {
	now := time.Now().UTC()
	rec := &operator.JobRecord{
		JobID:     "failed-job",
		ToolName:  "nmap",
		Phase:     operator.PhaseFailed,
		CreatedAt: now.Add(-1 * time.Minute),
		UpdatedAt: now,
		LastError: "launch failed",
	}

	snap := buildSnapshot(rec, 0)

	if snap.Phase != operator.PhaseFailed {
		t.Errorf("Phase = %q, want failed (terminal phases should not be overridden)", snap.Phase)
	}
	if snap.LastError != "launch failed" {
		t.Errorf("LastError = %q, want 'launch failed'", snap.LastError)
	}
}

func TestBuildSnapshotTerminalElapsed(t *testing.T) {
	created := time.Now().UTC().Add(-10 * time.Minute)
	updated := created.Add(3 * time.Minute)
	rec := &operator.JobRecord{
		JobID:     "done-job",
		ToolName:  "httpx",
		Phase:     operator.PhaseComplete,
		CreatedAt: created,
		UpdatedAt: updated,
	}

	snap := buildSnapshot(rec, 0)

	if snap.Elapsed != "3m0s" {
		t.Errorf("Elapsed = %q, want 3m0s for completed job", snap.Elapsed)
	}
}

func TestOutputStatusJSON(t *testing.T) {
	snap := statusSnapshot{
		JobID:    "test-job",
		Tool:     "nmap",
		Phase:    operator.PhaseScanning,
		Bucket:   "results-bucket",
		Progress: statusProgress{Completed: 10, Total: 20, Percent: 50.0},
		Elapsed:  "2m30s",
	}

	// Capture stdout.
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := outputStatusJSON(snap)
	_ = w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("outputStatusJSON error: %v", err)
	}

	var got statusSnapshot
	if err := json.NewDecoder(r).Decode(&got); err != nil {
		t.Fatalf("decode error: %v", err)
	}

	if got.JobID != "test-job" {
		t.Errorf("JobID = %q, want test-job", got.JobID)
	}
	if got.Bucket != "results-bucket" {
		t.Errorf("Bucket = %q, want results-bucket", got.Bucket)
	}
	if got.Phase != operator.PhaseScanning {
		t.Errorf("Phase = %q, want scanning", got.Phase)
	}
	if got.Progress.Completed != 10 {
		t.Errorf("Completed = %d, want 10", got.Progress.Completed)
	}
}

func TestOutputStatusText(t *testing.T) {
	snap := statusSnapshot{
		JobID:          "test-job",
		Tool:           "nmap",
		Phase:          operator.PhaseScanning,
		Bucket:         "results-bucket",
		Progress:       statusProgress{Completed: 10, Total: 20, Percent: 50.0},
		Elapsed:        "2m30s",
		CleanupPolicy:  "destroy-after",
		ResultPrefix:   "scans/nmap/test-job/results/",
		ArtifactPrefix: "scans/nmap/test-job/artifacts/",
		LocalOutputDir: "/tmp/output",
		LastError:      "warning: something",
	}

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := outputStatusText(snap)
	_ = w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("outputStatusText error: %v", err)
	}

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	output := string(buf[:n])

	checks := []string{
		"Job:       test-job",
		"Tool:      nmap",
		"Phase:     scanning",
		"Progress:  10 / 20  (50.0%)",
		"Elapsed:   2m30s",
		"Cleanup:   destroy-after",
		"Results:   s3://results-bucket/scans/nmap/test-job/results/",
		"Artifacts: s3://results-bucket/scans/nmap/test-job/artifacts/",
		"Local:     /tmp/output",
		"Error:     warning: something",
	}
	for _, check := range checks {
		if !strings.Contains(output, check) {
			t.Errorf("text output missing %q\ngot:\n%s", check, output)
		}
	}
}

func TestBuildSnapshotZeroTasks(t *testing.T) {
	rec := &operator.JobRecord{
		JobID:     "empty-job",
		ToolName:  "nmap",
		Phase:     operator.PhaseEnqueuing,
		CreatedAt: time.Now().UTC(),
	}

	snap := buildSnapshot(rec, 0)

	if snap.Progress.Percent != 0.0 {
		t.Errorf("Percent = %.1f, want 0.0 for zero-task job", snap.Progress.Percent)
	}
}

func TestBuildSnapshotIncludesCloud(t *testing.T) {
	rec := &operator.JobRecord{
		JobID:      "hetzner-job",
		ToolName:   "nmap",
		Phase:      operator.PhaseScanning,
		CreatedAt:  time.Now().UTC().Add(-2 * time.Minute),
		TotalTasks: 50,
		Cloud:      "hetzner",
	}

	snap := buildSnapshot(rec, 10)

	if snap.Cloud != "hetzner" {
		t.Errorf("Cloud = %q, want hetzner", snap.Cloud)
	}
}

func TestOutputStatusTextWithFleet(t *testing.T) {
	snap := statusSnapshot{
		JobID:    "fleet-job",
		Tool:     "nmap",
		Phase:    operator.PhaseScanning,
		Cloud:    "hetzner",
		Progress: statusProgress{Completed: 10, Total: 50, Percent: 20.0},
		Elapsed:  "1m0s",
		Fleet: &statusFleet{
			ControllerIP:            "1.2.3.4",
			GenerationID:            "gen-abc",
			CanaryGeneration:        "gen-canary",
			ExpectedWorkerVersion:   "heph-nmap-worker:latest",
			Placement:               "diversity, max 1/IP",
			RolloutPhase:            "canary",
			RollbackReason:          "canary health regression",
			DesiredWorkers:          5,
			RegisteredCount:         5,
			HealthyCount:            4,
			ReadyCount:              4,
			EligibleCount:           3,
			ExcludedCount:           1,
			QuarantinedCount:        1,
			UniqueIPv4Count:         5,
			UniqueEligibleIPv4Count: 3,
			IPv6ReadyCount:          3,
			CanaryWorkerCount:       1,
			PromotedWorkerCount:     2,
			DrainingWorkerCount:     2,
			ExcludedByReason:        map[string]int{"version_mismatch": 1},
		},
	}

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := outputStatusText(snap)
	_ = w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("outputStatusText error: %v", err)
	}

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	output := string(buf[:n])

	checks := []string{
		"Cloud:     hetzner",
		"Fleet:",
		"Controller:  1.2.3.4",
		"Generation:  gen-abc",
		"Canary Gen:  gen-canary",
		"Version:     heph-nmap-worker:latest",
		"Placement:   diversity, max 1/IP",
		"Rollout:     canary",
		"Workers:     5/5 desired, 4 healthy, 4 ready, 3 eligible",
		"Waves:       canary=1 promoted=2 draining=2",
		"Excluded:    1 total, 1 quarantined",
		"Reasons:     version_mismatch=1",
		"Rollback:    canary health regression",
		"IPv4:        5 unique",
		"IPv4 Ready:  3 unique eligible",
		"IPv6:        3 ready",
	}
	for _, check := range checks {
		if !strings.Contains(output, check) {
			t.Errorf("text output missing %q\ngot:\n%s", check, output)
		}
	}
}

func TestOutputStatusJSONWithFleet(t *testing.T) {
	snap := statusSnapshot{
		JobID:    "fleet-job",
		Tool:     "nmap",
		Phase:    operator.PhaseScanning,
		Cloud:    "hetzner",
		Progress: statusProgress{Completed: 10, Total: 50, Percent: 20.0},
		Elapsed:  "1m0s",
		Fleet: &statusFleet{
			ControllerIP:            "1.2.3.4",
			DesiredWorkers:          3,
			RegisteredCount:         3,
			HealthyCount:            3,
			ReadyCount:              3,
			EligibleCount:           2,
			UniqueIPv4Count:         3,
			UniqueEligibleIPv4Count: 2,
			IPv6ReadyCount:          2,
			ExcludedByReason:        map[string]int{"placement_limit_exceeded": 1},
		},
	}

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := outputStatusJSON(snap)
	_ = w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("outputStatusJSON error: %v", err)
	}

	var got statusSnapshot
	if err := json.NewDecoder(r).Decode(&got); err != nil {
		t.Fatalf("decode error: %v", err)
	}

	if got.Fleet == nil {
		t.Fatal("expected fleet field in JSON output")
	}
	if got.Fleet.ControllerIP != "1.2.3.4" {
		t.Errorf("Fleet.ControllerIP = %q, want 1.2.3.4", got.Fleet.ControllerIP)
	}
	if got.Fleet.DesiredWorkers != 3 {
		t.Errorf("Fleet.DesiredWorkers = %d, want 3", got.Fleet.DesiredWorkers)
	}
	if got.Fleet.EligibleCount != 2 {
		t.Errorf("Fleet.EligibleCount = %d, want 2", got.Fleet.EligibleCount)
	}
	if got.Fleet.UniqueIPv4Count != 3 {
		t.Errorf("Fleet.UniqueIPv4Count = %d, want 3", got.Fleet.UniqueIPv4Count)
	}
}
