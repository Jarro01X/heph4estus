package operator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"heph4estus/internal/fleet"
)

// Phase represents the current lifecycle phase of a job.
type Phase string

const (
	PhaseUploading Phase = "uploading"
	PhaseEnqueuing Phase = "enqueuing"
	PhaseLaunching Phase = "launching"
	PhaseScanning  Phase = "scanning"
	PhaseComplete  Phase = "complete"
	PhaseFailed    Phase = "failed"
)

// JobRecord persists the metadata needed to reattach to or query a job
// from a later shell session.
type JobRecord struct {
	JobID                 string                `json:"job_id"`
	ToolName              string                `json:"tool_name"`
	Phase                 Phase                 `json:"phase"`
	CreatedAt             time.Time             `json:"created_at"`
	StartedAt             time.Time             `json:"started_at,omitempty"`
	UpdatedAt             time.Time             `json:"updated_at"`
	TotalTasks            int                   `json:"total_tasks"`
	TotalWords            int                   `json:"total_words,omitempty"`
	WorkerCount           int                   `json:"worker_count,omitempty"`
	ComputeMode           string                `json:"compute_mode,omitempty"`
	Cloud                 string                `json:"cloud,omitempty"`
	CleanupPolicy         string                `json:"cleanup_policy,omitempty"`
	Bucket                string                `json:"bucket,omitempty"`
	ResultPrefix          string                `json:"result_prefix,omitempty"`
	ArtifactPrefix        string                `json:"artifact_prefix,omitempty"`
	RuntimeTarget         string                `json:"runtime_target,omitempty"`
	LastError             string                `json:"last_error,omitempty"`
	LocalOutputDir        string                `json:"local_output_dir,omitempty"`
	Placement             fleet.PlacementPolicy `json:"placement,omitempty"`
	ExpectedWorkerVersion string                `json:"expected_worker_version,omitempty"`

	// Fleet metadata for provider-native status reattachment.
	NATSUrl      string `json:"nats_url,omitempty"`
	ControllerIP string `json:"controller_ip,omitempty"`
	GenerationID string `json:"generation_id,omitempty"`
}

// JobStore provides CRUD operations for job records backed by the filesystem.
type JobStore struct {
	dir string
}

// NewJobStore creates a job store at the default path (<config-dir>/heph4estus/jobs/).
func NewJobStore() (*JobStore, error) {
	cfgDir, err := ConfigDir()
	if err != nil {
		return nil, err
	}
	return NewJobStoreAt(filepath.Join(cfgDir, "jobs")), nil
}

// NewJobStoreAt creates a job store at a specific directory.
func NewJobStoreAt(dir string) *JobStore {
	return &JobStore{dir: dir}
}

func (s *JobStore) jobPath(jobID string) string {
	return filepath.Join(s.dir, jobID+".json")
}

// Create persists a new job record. Returns an error if a record already exists.
func (s *JobStore) Create(rec *JobRecord) error {
	if rec.JobID == "" {
		return fmt.Errorf("job ID is required")
	}
	path := s.jobPath(rec.JobID)
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("job record already exists: %s", rec.JobID)
	}

	now := time.Now().UTC()
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = now
	}
	rec.UpdatedAt = now
	return s.write(rec)
}

// Load reads a job record by ID. Returns an error if not found.
func (s *JobStore) Load(jobID string) (*JobRecord, error) {
	data, err := os.ReadFile(s.jobPath(jobID))
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("job record not found: %s — was this job started on this machine?", jobID)
	}
	if err != nil {
		return nil, fmt.Errorf("reading job record: %w", err)
	}

	var rec JobRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("parsing job record %s: %w", jobID, err)
	}
	return &rec, nil
}

// Update overwrites an existing job record. Returns an error if not found.
func (s *JobStore) Update(rec *JobRecord) error {
	if rec.JobID == "" {
		return fmt.Errorf("job ID is required")
	}
	if _, err := os.Stat(s.jobPath(rec.JobID)); os.IsNotExist(err) {
		return fmt.Errorf("job record not found: %s", rec.JobID)
	}
	rec.UpdatedAt = time.Now().UTC()
	return s.write(rec)
}

// List returns all stored job IDs (most recent first is not guaranteed;
// callers should sort by CreatedAt if needed).
func (s *JobStore) List() ([]string, error) {
	entries, err := os.ReadDir(s.dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("listing job records: %w", err)
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if ext := filepath.Ext(name); ext == ".json" {
			ids = append(ids, name[:len(name)-len(ext)])
		}
	}
	return ids, nil
}

func (s *JobStore) write(rec *JobRecord) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("creating job store dir: %w", err)
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling job record: %w", err)
	}

	path := s.jobPath(rec.JobID)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("writing job record: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("committing job record: %w", err)
	}
	return nil
}
