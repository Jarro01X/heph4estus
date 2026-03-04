package jobs

import (
	"context"
	"time"
)

// State represents the lifecycle state of a job.
type State string

const (
	StatePending   State = "pending"
	StateDeploying State = "deploying"
	StateRunning   State = "running"
	StateCompleted State = "completed"
	StateFailed    State = "failed"
)

// JobConfig describes what to run.
type JobConfig struct {
	ToolName string
	Targets  []byte
	Options  string
	Metadata map[string]string // tool-specific params
}

// JobStatus reports the current state of a submitted job.
type JobStatus struct {
	JobID          string
	State          State
	TotalTasks     int
	CompletedTasks int
	FailedTasks    int
	StartedAt      time.Time
	Error          string
}

// Runner is the interface that both the TUI and CLI use to submit and
// monitor jobs. Concrete implementations (e.g. AWS Step Functions) will
// be added in Phase 3.
type Runner interface {
	Submit(ctx context.Context, cfg JobConfig) (string, error)
	Status(ctx context.Context, jobID string) (*JobStatus, error)
}
