package operator

import (
	"time"

	"heph4estus/internal/jobs"
)

// Tracker provides a narrow API for recording job lifecycle transitions.
// Both CLI and TUI call these methods at key boundaries so that
// `heph status` can later reconstruct the job state.
type Tracker struct {
	store *JobStore
}

// NewTracker creates a tracker backed by the given job store.
func NewTracker(store *JobStore) *Tracker {
	return &Tracker{store: store}
}

// Create persists a new job record at the start of a run.
func (t *Tracker) Create(rec *JobRecord) error {
	if t.isNoop() {
		return nil
	}
	if rec.ResultPrefix == "" && rec.ToolName != "" && rec.JobID != "" {
		rec.ResultPrefix = jobs.ResultPrefix(rec.ToolName, rec.JobID)
	}
	if rec.ArtifactPrefix == "" && rec.ToolName != "" && rec.JobID != "" {
		rec.ArtifactPrefix = jobs.ArtifactPrefix(rec.ToolName, rec.JobID)
	}
	return t.store.Create(rec)
}

// UpdatePhase transitions the job to a new phase.
func (t *Tracker) UpdatePhase(jobID string, phase Phase) error {
	if t.isNoop() {
		return nil
	}
	rec, err := t.store.Load(jobID)
	if err != nil {
		return err
	}
	rec.Phase = phase
	if phase == PhaseScanning && rec.StartedAt.IsZero() {
		rec.StartedAt = time.Now().UTC()
	}
	return t.store.Update(rec)
}

// Complete marks the job as successfully finished.
func (t *Tracker) Complete(jobID string) error {
	if t.isNoop() {
		return nil
	}
	rec, err := t.store.Load(jobID)
	if err != nil {
		return err
	}
	rec.Phase = PhaseComplete
	rec.LastError = ""
	return t.store.Update(rec)
}

// Fail marks the job as failed with an error message.
func (t *Tracker) Fail(jobID string, reason error) error {
	if t.isNoop() {
		return nil
	}
	rec, err := t.store.Load(jobID)
	if err != nil {
		return err
	}
	rec.Phase = PhaseFailed
	if reason != nil {
		rec.LastError = reason.Error()
	}
	return t.store.Update(rec)
}

// NoopTracker returns a Tracker with a nil store. All methods are no-ops
// that silently succeed. Use this when job tracking is unavailable (e.g.
// config dir unresolvable) to avoid cluttering error paths.
func NoopTracker() *Tracker {
	return &Tracker{store: nil}
}

// Store returns the underlying job store, or nil for noop trackers.
func (t *Tracker) Store() *JobStore {
	return t.store
}

// isNoop returns true if the tracker has no backing store.
func (t *Tracker) isNoop() bool {
	return t.store == nil
}
