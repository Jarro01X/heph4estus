package fleetstate

import (
	"strings"
	"testing"
)

func TestBuildRecoveryManifestAndValidate(t *testing.T) {
	manifest := BuildRecoveryManifest("httpx", "hetzner", map[string]string{
		"generation_id": "gen-1",
		"worker_count":  "12",
		"nats_url":      "nats://controller:4222",
	}, &RolloutRecord{ToolName: "httpx", Cloud: "hetzner"}, []ReputationRecord{{Cloud: "hetzner", PublicIPv4: "203.0.113.10"}})

	if err := manifest.Validate("httpx", "hetzner"); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if manifest.ControllerGeneration != "gen-1" {
		t.Fatalf("ControllerGeneration = %q, want gen-1", manifest.ControllerGeneration)
	}
	if manifest.WorkerCount != 12 {
		t.Fatalf("WorkerCount = %d, want 12", manifest.WorkerCount)
	}
	if len(manifest.OutputKeys) != 3 {
		t.Fatalf("expected 3 output keys, got %d", len(manifest.OutputKeys))
	}
	if got := strings.Join(manifest.RecoverableArtifacts, ","); !strings.Contains(got, "rollout_state") || !strings.Contains(got, "reputation_state") {
		t.Fatalf("unexpected recoverable artifacts: %s", got)
	}
	lines := manifest.SummaryLines()
	if len(lines) == 0 {
		t.Fatal("expected summary lines")
	}
}

func TestRecoveryManifestValidateMismatch(t *testing.T) {
	manifest := BuildRecoveryManifest("httpx", "hetzner", map[string]string{"nats_url": "nats://x"}, nil, nil)
	if err := manifest.Validate("nmap", "hetzner"); err == nil {
		t.Fatal("expected tool mismatch error")
	}
	if err := manifest.Validate("httpx", "linode"); err == nil {
		t.Fatal("expected cloud mismatch error")
	}
}
