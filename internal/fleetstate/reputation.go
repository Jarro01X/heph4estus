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

type ReputationState string

const (
	ReputationStateHealthy     ReputationState = "healthy"
	ReputationStateCoolingDown ReputationState = "cooling_down"
	ReputationStateQuarantined ReputationState = "quarantined"
	ReputationStateRetired     ReputationState = "retired"
)

type ReputationRecord struct {
	Cloud         string          `json:"cloud"`
	Region        string          `json:"region,omitempty"`
	PublicIPv4    string          `json:"public_ipv4,omitempty"`
	PublicIPv6    string          `json:"public_ipv6,omitempty"`
	Host          string          `json:"host,omitempty"`
	WorkerID      string          `json:"worker_id,omitempty"`
	State         ReputationState `json:"state"`
	Reason        string          `json:"reason,omitempty"`
	FailureCount  int             `json:"failure_count,omitempty"`
	LastHealthyAt time.Time       `json:"last_healthy_at,omitempty"`
	CooldownUntil time.Time       `json:"cooldown_until,omitempty"`
	RetiredAt     time.Time       `json:"retired_at,omitempty"`
	UpdatedAt     time.Time       `json:"updated_at"`
	Notes         string          `json:"notes,omitempty"`
	Source        string          `json:"source,omitempty"`
}

func (r ReputationRecord) EffectiveState(now time.Time) ReputationState {
	switch r.State {
	case ReputationStateCoolingDown:
		if r.CooldownUntil.IsZero() || now.Before(r.CooldownUntil) {
			return ReputationStateCoolingDown
		}
		return ReputationStateHealthy
	default:
		return r.State
	}
}

type ReputationStore struct {
	path string
}

func NewReputationStore() (*ReputationStore, error) {
	path, err := stateFile("reputation.json")
	if err != nil {
		return nil, err
	}
	return &ReputationStore{path: path}, nil
}

func NewReputationStoreAt(path string) *ReputationStore {
	return &ReputationStore{path: path}
}

func (s *ReputationStore) LoadAll() (map[string]ReputationRecord, error) {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return map[string]ReputationRecord{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading reputation store: %w", err)
	}
	var records map[string]ReputationRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, fmt.Errorf("parsing reputation store: %w", err)
	}
	if records == nil {
		records = map[string]ReputationRecord{}
	}
	return records, nil
}

func (s *ReputationStore) SaveAll(records map[string]ReputationRecord) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("creating reputation dir: %w", err)
	}
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling reputation store: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("writing reputation store: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("committing reputation store: %w", err)
	}
	return nil
}

func (s *ReputationStore) Upsert(rec ReputationRecord) error {
	records, err := s.LoadAll()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if rec.UpdatedAt.IsZero() {
		rec.UpdatedAt = now
	}
	if rec.State == "" {
		rec.State = ReputationStateHealthy
	}
	records[recordKey(rec.Cloud, rec.PublicIPv4, rec.PublicIPv6, rec.WorkerID)] = rec
	return s.SaveAll(records)
}

func (s *ReputationStore) SetState(cloud, ip, reason, notes string, state ReputationState, duration time.Duration) error {
	rec, err := s.Lookup(cloud, ip, "", "")
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if rec == nil {
		rec = &ReputationRecord{
			Cloud:      cloud,
			PublicIPv4: ip,
		}
	}
	rec.State = state
	rec.Reason = strings.TrimSpace(reason)
	rec.Notes = strings.TrimSpace(notes)
	rec.UpdatedAt = now
	switch state {
	case ReputationStateCoolingDown, ReputationStateQuarantined:
		if duration > 0 {
			rec.CooldownUntil = now.Add(duration)
		}
	case ReputationStateRetired:
		rec.RetiredAt = now
	case ReputationStateHealthy:
		rec.CooldownUntil = time.Time{}
		rec.RetiredAt = time.Time{}
		rec.Reason = ""
	}
	return s.Upsert(*rec)
}

func (s *ReputationStore) Clear(cloud, ip string) error {
	records, err := s.LoadAll()
	if err != nil {
		return err
	}
	delete(records, recordKey(cloud, ip, "", ""))
	return s.SaveAll(records)
}

func (s *ReputationStore) Lookup(cloud, ip4, ip6, workerID string) (*ReputationRecord, error) {
	records, err := s.LoadAll()
	if err != nil {
		return nil, err
	}
	keys := []string{
		recordKey(cloud, ip4, ip6, workerID),
		recordKey(cloud, ip4, "", ""),
		recordKey(cloud, "", ip6, ""),
		recordKey(cloud, "", "", workerID),
	}
	for _, key := range keys {
		if key == "" {
			continue
		}
		if rec, ok := records[key]; ok {
			cp := rec
			return &cp, nil
		}
	}
	return nil, nil
}

func (s *ReputationStore) List(cloud string) ([]ReputationRecord, error) {
	records, err := s.LoadAll()
	if err != nil {
		return nil, err
	}
	list := make([]ReputationRecord, 0, len(records))
	for _, rec := range records {
		if cloud == "" || rec.Cloud == cloud {
			list = append(list, rec)
		}
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Cloud != list[j].Cloud {
			return list[i].Cloud < list[j].Cloud
		}
		if list[i].PublicIPv4 != list[j].PublicIPv4 {
			return list[i].PublicIPv4 < list[j].PublicIPv4
		}
		return list[i].UpdatedAt.Before(list[j].UpdatedAt)
	})
	return list, nil
}

func recordKey(cloud, ip4, ip6, workerID string) string {
	cloud = strings.TrimSpace(cloud)
	switch {
	case strings.TrimSpace(ip4) != "":
		return cloud + "|ipv4|" + strings.TrimSpace(ip4)
	case strings.TrimSpace(ip6) != "":
		return cloud + "|ipv6|" + strings.TrimSpace(ip6)
	case strings.TrimSpace(workerID) != "":
		return cloud + "|worker|" + strings.TrimSpace(workerID)
	default:
		return ""
	}
}
