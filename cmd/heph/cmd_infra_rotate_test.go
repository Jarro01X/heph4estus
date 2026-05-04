package main

import (
	"bytes"
	"strings"
	"testing"

	"heph4estus/internal/cloud"
	"heph4estus/internal/infra"
)

func TestRunInfraRotateRequiresSubcommand(t *testing.T) {
	err := runInfra([]string{"rotate"}, testLogger())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "requires a subcommand") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunInfraRotateCredentialsRejectsAllMutationBeforeTerraform(t *testing.T) {
	err := runInfraRotateCredentials([]string{"--tool", "nmap", "--cloud", "hetzner", "--component", "all"}, testLogger())
	if err == nil {
		t.Fatal("expected unsupported mutation error")
	}
	if !strings.Contains(err.Error(), "one component at a time") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunInfraRotateCredentialsRequiresComponent(t *testing.T) {
	err := runInfraRotateCredentials([]string{"--tool", "nmap", "--cloud", "hetzner", "--dry-run"}, testLogger())
	if err == nil {
		t.Fatal("expected component error")
	}
	if !strings.Contains(err.Error(), "--component flag is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunInfraRotateCredentialsRejectsInvalidComponentBeforeTerraform(t *testing.T) {
	err := runInfraRotateCredentials([]string{"--tool", "nmap", "--cloud", "hetzner", "--component", "docker", "--dry-run"}, testLogger())
	if err == nil {
		t.Fatal("expected component error")
	}
	if !strings.Contains(err.Error(), "--component must be") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseRotationWorkerCount(t *testing.T) {
	got, err := parseRotationWorkerCount(map[string]string{"worker_count": "3"})
	if err != nil {
		t.Fatalf("parseRotationWorkerCount: %v", err)
	}
	if got != 3 {
		t.Fatalf("worker count = %d, want 3", got)
	}
}

func TestParseRotationWorkerCountRejectsInvalid(t *testing.T) {
	_, err := parseRotationWorkerCount(map[string]string{"worker_count": "0"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "positive worker_count") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAllWorkerIndexes(t *testing.T) {
	got := allWorkerIndexes(3)
	want := []int{0, 1, 2}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("index %d = %d, want %d", i, got[i], want[i])
		}
	}
}

func TestOutputCredentialRotationPlanText(t *testing.T) {
	plan := &infra.CredentialRotationPlan{
		Tool:                   "nmap",
		Cloud:                  cloud.KindHetzner,
		Components:             []infra.CredentialRotationComponent{infra.CredentialComponentNATS},
		ControllerServices:     []string{"nats"},
		OperatorOutputKeys:     []string{"nats_url", "nats_password"},
		WorkerRecycleRequired:  true,
		WorkerCount:            "3",
		CredentialScopeVersion: "nats-minio-registry-role-v1",
		ControllerSecurityMode: "tls",
		GenerationID:           "gen-1",
		Actions:                []string{"generate new NATS operator and worker credentials"},
		Verification:           []string{"verify workers resume heartbeats after reconcile"},
	}
	var buf bytes.Buffer
	if err := outputCredentialRotationPlanText(&buf, plan); err != nil {
		t.Fatalf("outputCredentialRotationPlanText: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"Credential rotation dry run",
		"Components:  nats",
		"Controller services affected: nats",
		"Worker reconcile: replace or restart 3 workers",
		"Dry run: no Terraform apply",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}
