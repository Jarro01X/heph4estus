package infra

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestGenerateMinIOCredentials(t *testing.T) {
	creds, err := GenerateMinIOCredentials(time.Date(2026, 5, 3, 22, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("GenerateMinIOCredentials: %v", err)
	}
	if !strings.HasPrefix(creds.OperatorAccessKey, "hephop") {
		t.Fatalf("OperatorAccessKey = %q", creds.OperatorAccessKey)
	}
	if !strings.HasPrefix(creds.WorkerAccessKey, "hephwk") {
		t.Fatalf("WorkerAccessKey = %q", creds.WorkerAccessKey)
	}
	if !strings.HasPrefix(creds.Generation, "minio-20260503t220000z-") {
		t.Fatalf("Generation = %q", creds.Generation)
	}
	if creds.RotatedAt != "2026-05-03T22:00:00Z" {
		t.Fatalf("RotatedAt = %q", creds.RotatedAt)
	}
	if len(creds.OperatorSecretKey) != 40 || len(creds.WorkerSecretKey) != 40 {
		t.Fatalf("secret lengths = %d/%d, want 40/40", len(creds.OperatorSecretKey), len(creds.WorkerSecretKey))
	}
}

func TestMinIOTerraformVars(t *testing.T) {
	creds := testMinIOCredentials()
	vars := MinIOTerraformVars(creds)
	want := map[string]string{
		"minio_operator_access_key_override": creds.OperatorAccessKey,
		"minio_operator_secret_key_override": creds.OperatorSecretKey,
		"minio_worker_access_key_override":   creds.WorkerAccessKey,
		"minio_worker_secret_key_override":   creds.WorkerSecretKey,
		"minio_credential_generation":        creds.Generation,
		"minio_credential_rotated_at":        creds.RotatedAt,
	}
	for key, value := range want {
		if vars[key] != value {
			t.Fatalf("vars[%s] = %q, want %q", key, vars[key], value)
		}
	}
}

func TestUpdateControllerMinIOAuthGrace(t *testing.T) {
	runner := &recordingRemoteRunner{}
	err := UpdateControllerMinIOAuth(context.Background(), runner, "1.2.3.4", MinIOControllerAuthUpdate{
		Credentials: testMinIOCredentials(),
		TLSEnabled:  true,
		Bucket:      "heph-results",
		Endpoint:    "https://10.0.1.2:9000",
		Mode:        MinIOAuthUpdateGrace,
	})
	if err != nil {
		t.Fatalf("UpdateControllerMinIOAuth: %v", err)
	}
	if runner.host != "1.2.3.4" {
		t.Fatalf("host = %q", runner.host)
	}
	for _, want := range []string{
		"MINIO_ROOT_USER",
		"/root/.mc/certs/CAs/heph-controller-ca.crt",
		"mc admin user add local",
		"hephopnew",
		"operator-secret",
		"mc admin policy attach local heph-worker --user",
		"hephwknew",
		"mc cp /tmp/heph-minio-probe",
	} {
		if !strings.Contains(runner.cmd, want) {
			t.Fatalf("command missing %q:\n%s", want, runner.cmd)
		}
	}
}

func TestUpdateControllerMinIOAuthFinalRemovesOldUsers(t *testing.T) {
	runner := &recordingRemoteRunner{}
	err := UpdateControllerMinIOAuth(context.Background(), runner, "1.2.3.4", MinIOControllerAuthUpdate{
		Credentials:       testMinIOCredentials(),
		Bucket:            "heph-results",
		OldOperatorKey:    "old-operator",
		PreviousWorkerKey: "old-worker",
		Mode:              MinIOAuthUpdateFinal,
	})
	if err != nil {
		t.Fatalf("UpdateControllerMinIOAuth: %v", err)
	}
	for _, want := range []string{
		"mc admin user remove local",
		"old-operator",
		"old-worker",
	} {
		if !strings.Contains(runner.cmd, want) {
			t.Fatalf("command missing %q:\n%s", want, runner.cmd)
		}
	}
}

func testMinIOCredentials() MinIOCredentials {
	return MinIOCredentials{
		OperatorAccessKey: "hephopnew",
		OperatorSecretKey: "operator-secret",
		WorkerAccessKey:   "hephwknew",
		WorkerSecretKey:   "worker-secret",
		Generation:        "minio-test-generation",
		RotatedAt:         "2026-05-03T22:00:00Z",
	}
}
