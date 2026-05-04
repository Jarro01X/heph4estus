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

func TestRunInfraRotateCertsRejectsNonControllerMutationBeforeTerraform(t *testing.T) {
	err := runInfraRotateCerts([]string{"--tool", "nmap", "--cloud", "hetzner", "--component", "worker"}, testLogger())
	if err == nil {
		t.Fatal("expected unsupported mutation error")
	}
	if !strings.Contains(err.Error(), "only --component controller") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunInfraRotateCertsRequiresComponent(t *testing.T) {
	err := runInfraRotateCerts([]string{"--tool", "nmap", "--cloud", "hetzner", "--dry-run"}, testLogger())
	if err == nil {
		t.Fatal("expected component error")
	}
	if !strings.Contains(err.Error(), "--component flag is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunInfraRotateCertsRejectsInvalidComponentBeforeTerraform(t *testing.T) {
	err := runInfraRotateCerts([]string{"--tool", "nmap", "--cloud", "hetzner", "--component", "registry", "--dry-run"}, testLogger())
	if err == nil {
		t.Fatal("expected component error")
	}
	if !strings.Contains(err.Error(), "--component must be") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSupportedCertificateMutationComponent(t *testing.T) {
	if !supportedCertificateMutationComponent(infra.CertificateComponentController) {
		t.Fatal("controller certificate mutation should be supported")
	}
	if !supportedCertificateMutationComponent(infra.CertificateComponentCA) {
		t.Fatal("CA certificate mutation should be supported")
	}
	if supportedCertificateMutationComponent(infra.CertificateComponentWorker) {
		t.Fatal("worker-only certificate mutation should not be supported yet")
	}
}

func TestControllerCAPrivateKeyForRotationUsesRotationVars(t *testing.T) {
	dir := t.TempDir()
	if err := infra.WriteRotationAutoVars(dir, map[string]string{"controller_ca_key_pem_override": "ca-key"}); err != nil {
		t.Fatalf("WriteRotationAutoVars: %v", err)
	}
	key, err := controllerCAPrivateKeyForRotation(rotationMutationOpts{
		ToolConfig: &infra.ToolConfig{TerraformDir: dir},
		Log:        testLogger(),
	})
	if err != nil {
		t.Fatalf("controllerCAPrivateKeyForRotation: %v", err)
	}
	if key != "ca-key" {
		t.Fatalf("key = %q, want ca-key", key)
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

func TestOutputCertificateRotationPlanText(t *testing.T) {
	plan := &infra.CertificateRotationPlan{
		Tool:                          "nmap",
		Cloud:                         cloud.KindHetzner,
		Components:                    []infra.CertificateRotationComponent{infra.CertificateComponentController, infra.CertificateComponentCA},
		ControllerSecurityMode:        "tls",
		GenerationID:                  "gen-1",
		WorkerCount:                   "3",
		CertificateGeneration:         "bootstrap",
		CertificateRotatedAt:          "unknown",
		ControllerCAFingerprintSHA256: "abc123",
		ControllerCertNotAfter:        "2035-05-03T00:00:00Z",
		TLSEnabledServices:            []string{"nats", "minio", "registry"},
		ControllerServices:            []string{"nats", "minio", "registry"},
		WorkerRecycleRequired:         true,
		OperatorTrustRefreshRequired:  true,
		Actions:                       []string{"generate a replacement controller CA and controller server certificate chain"},
		Verification:                  []string{"verify operator clients trust the replacement controller CA"},
		Warnings:                      []string{"controller certificate expires soon"},
	}
	var buf bytes.Buffer
	if err := outputCertificateRotationPlanText(&buf, plan); err != nil {
		t.Fatalf("outputCertificateRotationPlanText: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"Certificate rotation dry run",
		"Components:  controller, ca",
		"Cert gen:    bootstrap",
		"CA sha256:   abc123",
		"Controller services restarted: nats, minio, registry",
		"Operator trust refresh: refresh operator-side controller CA trust",
		"Dry run: no Terraform apply",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
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
