package infra

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestGenerateRegistryCredentials(t *testing.T) {
	creds, err := GenerateRegistryCredentials(time.Date(2026, 5, 3, 22, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("GenerateRegistryCredentials: %v", err)
	}
	if !strings.HasPrefix(creds.PublisherUsername, "heph-registry-publisher-") {
		t.Fatalf("PublisherUsername = %q", creds.PublisherUsername)
	}
	if !strings.HasPrefix(creds.WorkerUsername, "heph-registry-worker-") {
		t.Fatalf("WorkerUsername = %q", creds.WorkerUsername)
	}
	if !strings.HasPrefix(creds.Generation, "registry-20260503t220000z-") {
		t.Fatalf("Generation = %q", creds.Generation)
	}
	if creds.RotatedAt != "2026-05-03T22:00:00Z" {
		t.Fatalf("RotatedAt = %q", creds.RotatedAt)
	}
	if len(creds.PublisherPassword) != 32 || len(creds.WorkerPassword) != 32 {
		t.Fatalf("password lengths = %d/%d, want 32/32", len(creds.PublisherPassword), len(creds.WorkerPassword))
	}
}

func TestRegistryTerraformVars(t *testing.T) {
	creds := testRegistryCredentials()
	vars := RegistryTerraformVars(creds)
	want := map[string]string{
		"registry_publisher_username_override": creds.PublisherUsername,
		"registry_publisher_password_override": creds.PublisherPassword,
		"registry_worker_username_override":    creds.WorkerUsername,
		"registry_worker_password_override":    creds.WorkerPassword,
		"registry_credential_generation":       creds.Generation,
		"registry_credential_rotated_at":       creds.RotatedAt,
	}
	for key, value := range want {
		if vars[key] != value {
			t.Fatalf("vars[%s] = %q, want %q", key, vars[key], value)
		}
	}
}

func TestUpdateControllerRegistryAuthGrace(t *testing.T) {
	runner := &recordingRemoteRunner{}
	err := UpdateControllerRegistryAuth(context.Background(), runner, "1.2.3.4", RegistryControllerAuthUpdate{
		Credentials: testRegistryCredentials(),
		TLSEnabled:  true,
		RegistryURL: "https://10.0.1.2:5000",
		Mode:        RegistryAuthUpdateGrace,
	})
	if err != nil {
		t.Fatalf("UpdateControllerRegistryAuth: %v", err)
	}
	if runner.host != "1.2.3.4" {
		t.Fatalf("host = %q", runner.host)
	}
	for _, want := range []string{
		"registry_upsert_user",
		"heph-registry-publisher-new",
		"heph-registry-worker-new",
		"systemctl restart registry",
		"--cacert /data/tls/ca.crt",
		"https://localhost:5000/v2/",
	} {
		if !strings.Contains(runner.cmd, want) {
			t.Fatalf("command missing %q:\n%s", want, runner.cmd)
		}
	}
}

func TestUpdateControllerRegistryAuthFinalRewritesFile(t *testing.T) {
	runner := &recordingRemoteRunner{}
	err := UpdateControllerRegistryAuth(context.Background(), runner, "1.2.3.4", RegistryControllerAuthUpdate{
		Credentials: testRegistryCredentials(),
		RegistryURL: "10.0.1.2:5000",
		Mode:        RegistryAuthUpdateFinal,
	})
	if err != nil {
		t.Fatalf("UpdateControllerRegistryAuth: %v", err)
	}
	for _, want := range []string{
		"htpasswd -Bbn 'heph-registry-publisher-new'",
		"htpasswd -Bbn 'heph-registry-worker-new'",
		"install -m 0600",
		"http://localhost:5000/v2/",
	} {
		if !strings.Contains(runner.cmd, want) {
			t.Fatalf("command missing %q:\n%s", want, runner.cmd)
		}
	}
}

func testRegistryCredentials() RegistryCredentials {
	return RegistryCredentials{
		PublisherUsername: "heph-registry-publisher-new",
		PublisherPassword: "publisher-secret",
		WorkerUsername:    "heph-registry-worker-new",
		WorkerPassword:    "worker-secret",
		Generation:        "registry-test-generation",
		RotatedAt:         "2026-05-03T22:00:00Z",
	}
}
