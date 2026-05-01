package fleetstate

import (
	"path/filepath"
	"testing"
	"time"
)

func TestReputationStoreSetLookupAndClear(t *testing.T) {
	store := NewReputationStoreAt(filepath.Join(t.TempDir(), "reputation.json"))

	if err := store.SetState("hetzner", "203.0.113.10", "burned", "operator note", ReputationStateCoolingDown, time.Hour); err != nil {
		t.Fatalf("SetState: %v", err)
	}

	rec, err := store.Lookup("hetzner", "203.0.113.10", "", "")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if rec == nil {
		t.Fatal("expected reputation record")
	}
	if rec.State != ReputationStateCoolingDown {
		t.Fatalf("State = %q, want %q", rec.State, ReputationStateCoolingDown)
	}
	if rec.Reason != "burned" {
		t.Fatalf("Reason = %q, want burned", rec.Reason)
	}
	if rec.Notes != "operator note" {
		t.Fatalf("Notes = %q, want operator note", rec.Notes)
	}
	if rec.CooldownUntil.IsZero() {
		t.Fatal("expected cooldown deadline")
	}
	if got := rec.EffectiveState(time.Now().UTC()); got != ReputationStateCoolingDown {
		t.Fatalf("EffectiveState = %q, want %q", got, ReputationStateCoolingDown)
	}

	if err := store.Clear("hetzner", "203.0.113.10"); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	rec, err = store.Lookup("hetzner", "203.0.113.10", "", "")
	if err != nil {
		t.Fatalf("Lookup after clear: %v", err)
	}
	if rec != nil {
		t.Fatalf("expected record to be cleared, got %+v", rec)
	}
}

func TestReputationRecordEffectiveStateExpiresCooldown(t *testing.T) {
	rec := ReputationRecord{
		State:         ReputationStateCoolingDown,
		CooldownUntil: time.Now().UTC().Add(-time.Minute),
	}
	if got := rec.EffectiveState(time.Now().UTC()); got != ReputationStateHealthy {
		t.Fatalf("EffectiveState = %q, want %q", got, ReputationStateHealthy)
	}
}
