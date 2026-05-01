package fleetstate

import (
	"path/filepath"
	"testing"
)

func TestRolloutStoreSaveLoadDelete(t *testing.T) {
	store := NewRolloutStoreAt(filepath.Join(t.TempDir(), "rollouts.json"))
	rec := &RolloutRecord{
		ToolName:          "httpx",
		Cloud:             "hetzner",
		Phase:             RolloutPhaseCanary,
		DesiredGeneration: "gen-2",
		ActiveGeneration:  "gen-1",
		TargetVersion:     "registry/heph-httpx:2",
		PreviousVersion:   "registry/heph-httpx:1",
		CanaryCount:       1,
		CanaryWorkerIDs:   []string{"heph-worker-0"},
	}

	if err := store.Save(rec); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := store.Load("hetzner", "httpx")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got == nil {
		t.Fatal("expected rollout record")
	}
	if got.Phase != RolloutPhaseCanary {
		t.Fatalf("Phase = %q, want %q", got.Phase, RolloutPhaseCanary)
	}
	if got.TargetVersion != rec.TargetVersion {
		t.Fatalf("TargetVersion = %q, want %q", got.TargetVersion, rec.TargetVersion)
	}
	if got.StartedAt.IsZero() || got.UpdatedAt.IsZero() {
		t.Fatal("expected timestamps to be populated")
	}

	if err := store.Delete("hetzner", "httpx"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	got, err = store.Load("hetzner", "httpx")
	if err != nil {
		t.Fatalf("Load after delete: %v", err)
	}
	if got != nil {
		t.Fatalf("expected record to be deleted, got %+v", got)
	}
}

func TestRecoveryManifestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recovery.json")
	manifest := &RecoveryManifest{
		ToolName: "nmap",
		Cloud:    "vultr",
		Outputs: map[string]string{
			"controller_ip": "203.0.113.20",
		},
		Rollout: &RolloutRecord{
			ToolName:      "nmap",
			Cloud:         "vultr",
			Phase:         RolloutPhaseStable,
			TargetVersion: "registry/heph-nmap:3",
		},
		Reputation: []ReputationRecord{{
			Cloud:      "vultr",
			PublicIPv4: "203.0.113.21",
			State:      ReputationStateQuarantined,
			Reason:     "burned",
		}},
	}

	if err := WriteRecoveryManifest(path, manifest); err != nil {
		t.Fatalf("WriteRecoveryManifest: %v", err)
	}

	got, err := ReadRecoveryManifest(path)
	if err != nil {
		t.Fatalf("ReadRecoveryManifest: %v", err)
	}
	if got.Version != 1 {
		t.Fatalf("Version = %d, want 1", got.Version)
	}
	if got.ToolName != "nmap" || got.Cloud != "vultr" {
		t.Fatalf("unexpected manifest identity: %+v", got)
	}
	if got.Rollout == nil || got.Rollout.TargetVersion != "registry/heph-nmap:3" {
		t.Fatalf("unexpected rollout payload: %+v", got.Rollout)
	}
	if len(got.Reputation) != 1 || got.Reputation[0].Reason != "burned" {
		t.Fatalf("unexpected reputation payload: %+v", got.Reputation)
	}
}
