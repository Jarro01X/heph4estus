package bench

import (
	"path/filepath"
	"testing"
	"time"
)

func TestStoreSaveLoadListAndCompare(t *testing.T) {
	store := NewStoreAt(t.TempDir())
	base := FleetReport{
		Tool:                    "httpx",
		Cloud:                   "hetzner",
		GeneratedAt:             time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC),
		DeployDuration:          2 * time.Minute,
		FirstRegisteredDuration: 30 * time.Second,
		FirstAdmittedDuration:   45 * time.Second,
		SteadyStateDuration:     3 * time.Minute,
		DesiredWorkers:          10,
		UniqueIPv4Count:         10,
		IPv6ReadyCount:          8,
		DiversityEligible:       10,
		ThroughputEligible:      14,
	}
	next := base
	next.GeneratedAt = base.GeneratedAt.Add(10 * time.Minute)
	next.SteadyStateDuration = 2*time.Minute + 30*time.Second
	next.UniqueIPv4Count = 12

	basePath, err := store.Save(base)
	if err != nil {
		t.Fatalf("Save(base): %v", err)
	}
	if filepath.Ext(basePath) != ".json" {
		t.Fatalf("expected json path, got %q", basePath)
	}
	if _, err := store.Save(next); err != nil {
		t.Fatalf("Save(next): %v", err)
	}

	reports, err := store.List("httpx", "hetzner", 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(reports) != 2 {
		t.Fatalf("expected 2 reports, got %d", len(reports))
	}
	if !reports[0].GeneratedAt.After(reports[1].GeneratedAt) {
		t.Fatal("expected reports sorted newest-first")
	}

	loaded, err := store.Load(basePath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Tool != "httpx" || loaded.Cloud != "hetzner" {
		t.Fatalf("unexpected loaded report: %+v", loaded)
	}

	comparison := CompareFleetReports(base, next)
	if comparison.Delta.SteadyStateDuration != -30*time.Second {
		t.Fatalf("steady-state delta = %s, want -30s", comparison.Delta.SteadyStateDuration)
	}
	if comparison.Delta.UniqueIPv4Count != 2 {
		t.Fatalf("unique IPv4 delta = %d, want 2", comparison.Delta.UniqueIPv4Count)
	}
}
