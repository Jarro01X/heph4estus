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

func TestRunInfraRotateCredentialsRequiresDryRun(t *testing.T) {
	err := runInfraRotateCredentials([]string{"--tool", "nmap", "--cloud", "hetzner", "--component", "nats"}, testLogger())
	if err == nil {
		t.Fatal("expected dry-run guard error")
	}
	if !strings.Contains(err.Error(), "not implemented yet") {
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
