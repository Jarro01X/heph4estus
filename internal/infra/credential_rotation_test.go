package infra

import (
	"strings"
	"testing"

	"heph4estus/internal/cloud"
)

func TestParseCredentialRotationComponents(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []CredentialRotationComponent
	}{
		{name: "nats", raw: "nats", want: []CredentialRotationComponent{CredentialComponentNATS}},
		{name: "minio", raw: "minio", want: []CredentialRotationComponent{CredentialComponentMinIO}},
		{name: "registry", raw: "registry", want: []CredentialRotationComponent{CredentialComponentRegistry}},
		{name: "all", raw: "all", want: []CredentialRotationComponent{CredentialComponentNATS, CredentialComponentMinIO, CredentialComponentRegistry}},
		{name: "trim and case", raw: " NATS ", want: []CredentialRotationComponent{CredentialComponentNATS}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseCredentialRotationComponents(tt.raw)
			if err != nil {
				t.Fatalf("ParseCredentialRotationComponents(%q): %v", tt.raw, err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("len = %d, want %d (%v)", len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("component[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestParseCredentialRotationComponentsRejectsInvalid(t *testing.T) {
	_, err := ParseCredentialRotationComponents("docker")
	if err == nil {
		t.Fatal("expected invalid component error")
	}
	if !strings.Contains(err.Error(), "nats, minio, registry, or all") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPlanCredentialRotationAll(t *testing.T) {
	plan, err := PlanCredentialRotation(cloud.KindHetzner, "nmap", "all", ProbeResult{
		Status:  StatusReady,
		Outputs: rotationReadyOutputs(),
	})
	if err != nil {
		t.Fatalf("PlanCredentialRotation: %v", err)
	}
	if plan.Tool != "nmap" {
		t.Fatalf("Tool = %q, want nmap", plan.Tool)
	}
	if got := strings.Join(plan.ControllerServices, ","); got != "nats,minio,registry" {
		t.Fatalf("ControllerServices = %q", got)
	}
	if !plan.WorkerRecycleRequired {
		t.Fatal("expected worker recycle for runtime credential rotation")
	}
	if plan.WorkerCount != "3" {
		t.Fatalf("WorkerCount = %q, want 3", plan.WorkerCount)
	}
	if plan.MinIOCredentialGeneration != "bootstrap" || plan.RegistryCredentialGeneration != "bootstrap" {
		t.Fatalf("credential generations = %q/%q, want bootstrap/bootstrap", plan.MinIOCredentialGeneration, plan.RegistryCredentialGeneration)
	}
	for _, key := range []string{"nats_password", "s3_operator_secret_key", "registry_publisher_password"} {
		if !containsString(plan.OperatorOutputKeys, key) {
			t.Fatalf("expected operator output key %q in %v", key, plan.OperatorOutputKeys)
		}
	}
}

func TestPlanCredentialRotationRejectsStaleOutputs(t *testing.T) {
	_, err := PlanCredentialRotation(cloud.KindHetzner, "nmap", "nats", ProbeResult{
		Status:      StatusStale,
		MissingKeys: []string{"registry_password"},
	})
	if err == nil {
		t.Fatal("expected stale output error")
	}
	if !strings.Contains(err.Error(), "missing keys: registry_password") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPlanCredentialRotationRejectsAWS(t *testing.T) {
	_, err := PlanCredentialRotation(cloud.KindAWS, "nmap", "nats", ProbeResult{Status: StatusReady})
	if err == nil {
		t.Fatal("expected provider-native error")
	}
	if !strings.Contains(err.Error(), "provider-native") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func rotationReadyOutputs() map[string]string {
	return map[string]string{
		"tool_name":                      "nmap",
		"cloud":                          "hetzner",
		"credential_scope_version":       "nats-minio-registry-role-v1",
		"nats_credential_generation":     "bootstrap",
		"minio_credential_generation":    "bootstrap",
		"registry_credential_generation": "bootstrap",
		"controller_security_mode":       "tls",
		"generation_id":                  "gen-1",
		"worker_count":                   "3",
		"nats_url":                       "tls://operator:secret@1.2.3.4:4222",
		"nats_user":                      "heph-operator",
		"nats_password":                  "secret",
		"nats_operator_user":             "heph-operator",
		"nats_operator_password":         "secret",
		"s3_access_key":                  "operator",
		"s3_secret_key":                  "operator-secret",
		"s3_operator_access_key":         "operator",
		"s3_operator_secret_key":         "operator-secret",
		"registry_username":              "publisher",
		"registry_password":              "publisher-secret",
		"registry_publisher_username":    "publisher",
		"registry_publisher_password":    "publisher-secret",
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
