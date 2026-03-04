package jobs

import (
	"context"
	"testing"
	"time"
)

// mockRunner is a test double that satisfies the Runner interface.
type mockRunner struct {
	submitFunc func(ctx context.Context, cfg JobConfig) (string, error)
	statusFunc func(ctx context.Context, jobID string) (*JobStatus, error)
}

func (m *mockRunner) Submit(ctx context.Context, cfg JobConfig) (string, error) {
	return m.submitFunc(ctx, cfg)
}

func (m *mockRunner) Status(ctx context.Context, jobID string) (*JobStatus, error) {
	return m.statusFunc(ctx, jobID)
}

// Compile-time interface check.
var _ Runner = (*mockRunner)(nil)

func TestMockRunnerSubmit(t *testing.T) {
	r := &mockRunner{
		submitFunc: func(_ context.Context, cfg JobConfig) (string, error) {
			if cfg.ToolName != "nmap" {
				t.Fatalf("expected tool nmap, got %s", cfg.ToolName)
			}
			return "job-123", nil
		},
	}

	id, err := r.Submit(context.Background(), JobConfig{ToolName: "nmap"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "job-123" {
		t.Fatalf("expected job-123, got %s", id)
	}
}

func TestMockRunnerStatus(t *testing.T) {
	r := &mockRunner{
		statusFunc: func(_ context.Context, jobID string) (*JobStatus, error) {
			return &JobStatus{
				JobID:          jobID,
				State:          StateRunning,
				TotalTasks:     10,
				CompletedTasks: 3,
				StartedAt:      time.Now(),
			}, nil
		},
	}

	s, err := r.Status(context.Background(), "job-456")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.State != StateRunning {
		t.Fatalf("expected state running, got %s", s.State)
	}
	if s.TotalTasks != 10 {
		t.Fatalf("expected 10 total tasks, got %d", s.TotalTasks)
	}
}

func TestStateConstants(t *testing.T) {
	states := map[State]string{
		StatePending:   "pending",
		StateDeploying: "deploying",
		StateRunning:   "running",
		StateCompleted: "completed",
		StateFailed:    "failed",
	}
	for s, want := range states {
		if string(s) != want {
			t.Errorf("State %v: got %q, want %q", s, string(s), want)
		}
	}
}
