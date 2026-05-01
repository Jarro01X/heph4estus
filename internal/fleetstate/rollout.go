package fleetstate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type RolloutPhase string

const (
	RolloutPhaseIdle        RolloutPhase = "idle"
	RolloutPhaseCanary      RolloutPhase = "canary"
	RolloutPhasePromoting   RolloutPhase = "promoting"
	RolloutPhaseDrainingOld RolloutPhase = "draining-old"
	RolloutPhaseRolledBack  RolloutPhase = "rolled-back"
	RolloutPhaseStable      RolloutPhase = "stable"
)

type RolloutRecord struct {
	ToolName              string       `json:"tool_name"`
	Cloud                 string       `json:"cloud"`
	Phase                 RolloutPhase `json:"phase"`
	DesiredGeneration     string       `json:"desired_generation,omitempty"`
	ActiveGeneration      string       `json:"active_generation,omitempty"`
	CanaryGeneration      string       `json:"canary_generation,omitempty"`
	TargetVersion         string       `json:"target_version,omitempty"`
	PreviousVersion       string       `json:"previous_version,omitempty"`
	CanaryCount           int          `json:"canary_count,omitempty"`
	CanaryWorkerIDs       []string     `json:"canary_worker_ids,omitempty"`
	CanaryWorkerIndexes   []int        `json:"canary_worker_indexes,omitempty"`
	PromotedWorkerIndexes []int        `json:"promoted_worker_indexes,omitempty"`
	DrainingWorkerIndexes []int        `json:"draining_worker_indexes,omitempty"`
	RollbackReason        string       `json:"rollback_reason,omitempty"`
	StartedAt             time.Time    `json:"started_at"`
	UpdatedAt             time.Time    `json:"updated_at"`
}

type RolloutStore struct {
	path string
}

func NewRolloutStore() (*RolloutStore, error) {
	path, err := stateFile("rollouts.json")
	if err != nil {
		return nil, err
	}
	return &RolloutStore{path: path}, nil
}

func NewRolloutStoreAt(path string) *RolloutStore {
	return &RolloutStore{path: path}
}

func (s *RolloutStore) LoadAll() (map[string]RolloutRecord, error) {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return map[string]RolloutRecord{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading rollout store: %w", err)
	}
	var records map[string]RolloutRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, fmt.Errorf("parsing rollout store: %w", err)
	}
	if records == nil {
		records = map[string]RolloutRecord{}
	}
	return records, nil
}

func (s *RolloutStore) SaveAll(records map[string]RolloutRecord) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("creating rollout dir: %w", err)
	}
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling rollout store: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("writing rollout store: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("committing rollout store: %w", err)
	}
	return nil
}

func (s *RolloutStore) Load(cloud, tool string) (*RolloutRecord, error) {
	records, err := s.LoadAll()
	if err != nil {
		return nil, err
	}
	rec, ok := records[rolloutKey(cloud, tool)]
	if !ok {
		return nil, nil
	}
	cp := rec
	return &cp, nil
}

func (s *RolloutStore) Save(rec *RolloutRecord) error {
	if rec == nil {
		return fmt.Errorf("rollout record is required")
	}
	records, err := s.LoadAll()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if rec.StartedAt.IsZero() {
		rec.StartedAt = now
	}
	rec.UpdatedAt = now
	records[rolloutKey(rec.Cloud, rec.ToolName)] = *rec
	return s.SaveAll(records)
}

func (s *RolloutStore) Delete(cloud, tool string) error {
	records, err := s.LoadAll()
	if err != nil {
		return err
	}
	delete(records, rolloutKey(cloud, tool))
	return s.SaveAll(records)
}

func (s *RolloutStore) List() ([]RolloutRecord, error) {
	records, err := s.LoadAll()
	if err != nil {
		return nil, err
	}
	list := make([]RolloutRecord, 0, len(records))
	for _, rec := range records {
		list = append(list, rec)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Cloud != list[j].Cloud {
			return list[i].Cloud < list[j].Cloud
		}
		if list[i].ToolName != list[j].ToolName {
			return list[i].ToolName < list[j].ToolName
		}
		return list[i].UpdatedAt.Before(list[j].UpdatedAt)
	})
	return list, nil
}

func rolloutKey(cloud, tool string) string {
	return strings.TrimSpace(cloud) + "|" + strings.TrimSpace(tool)
}
