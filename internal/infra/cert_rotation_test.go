package infra

import (
	"strings"
	"testing"

	"heph4estus/internal/cloud"
)

func TestParseCertificateRotationComponents(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []CertificateRotationComponent
	}{
		{name: "controller", raw: "controller", want: []CertificateRotationComponent{CertificateComponentController}},
		{name: "worker", raw: "worker", want: []CertificateRotationComponent{CertificateComponentWorker}},
		{name: "ca", raw: "ca", want: []CertificateRotationComponent{CertificateComponentCA}},
		{name: "all", raw: "all", want: []CertificateRotationComponent{CertificateComponentController, CertificateComponentWorker, CertificateComponentCA}},
		{name: "trim and case", raw: " CA ", want: []CertificateRotationComponent{CertificateComponentCA}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseCertificateRotationComponents(tt.raw)
			if err != nil {
				t.Fatalf("ParseCertificateRotationComponents(%q): %v", tt.raw, err)
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

func TestParseCertificateRotationComponentsRejectsInvalid(t *testing.T) {
	_, err := ParseCertificateRotationComponents("registry")
	if err == nil {
		t.Fatal("expected invalid component error")
	}
	if !strings.Contains(err.Error(), "controller, worker, ca, or all") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPlanCertificateRotationAll(t *testing.T) {
	plan, err := PlanCertificateRotation(cloud.KindHetzner, "nmap", "all", ProbeResult{
		Status:  StatusReady,
		Outputs: certificateReadyOutputs(),
	})
	if err != nil {
		t.Fatalf("PlanCertificateRotation: %v", err)
	}
	if got := strings.Join(plan.TLSEnabledServices, ","); got != "nats,minio,registry" {
		t.Fatalf("TLSEnabledServices = %q", got)
	}
	if !plan.WorkerRecycleRequired {
		t.Fatal("expected worker recycle for all certificate rotation")
	}
	if !plan.OperatorTrustRefreshRequired {
		t.Fatal("expected operator trust refresh for CA rotation")
	}
	if plan.ControllerCAFingerprintSHA256 != "abc123" {
		t.Fatalf("ControllerCAFingerprintSHA256 = %q", plan.ControllerCAFingerprintSHA256)
	}
	for _, action := range []string{
		"generate a replacement controller server certificate signed by the current controller CA",
		"replace or restart workers so the replacement CA is trusted",
	} {
		if !containsString(plan.Actions, action) {
			t.Fatalf("expected action %q in %v", action, plan.Actions)
		}
	}
}

func TestPlanCertificateRotationWarnsForPrivateAuth(t *testing.T) {
	outputs := certificateReadyOutputs()
	outputs["controller_security_mode"] = "private-auth"
	outputs["nats_tls_enabled"] = "false"
	outputs["minio_tls_enabled"] = "false"
	outputs["registry_tls_enabled"] = "false"
	outputs["nats_url"] = "nats://operator:secret@1.2.3.4:4222"
	outputs["s3_endpoint"] = "http://1.2.3.4:9000"
	outputs["registry_url"] = "http://1.2.3.4:5000"
	plan, err := PlanCertificateRotation(cloud.KindHetzner, "nmap", "controller", ProbeResult{
		Status:  StatusReady,
		Outputs: outputs,
	})
	if err != nil {
		t.Fatalf("PlanCertificateRotation: %v", err)
	}
	if len(plan.Warnings) == 0 {
		t.Fatal("expected warnings")
	}
	if !strings.Contains(strings.Join(plan.Warnings, "\n"), "private-auth") {
		t.Fatalf("expected private-auth warning, got %v", plan.Warnings)
	}
}

func TestPlanCertificateRotationRejectsAWS(t *testing.T) {
	_, err := PlanCertificateRotation(cloud.KindAWS, "nmap", "controller", ProbeResult{Status: StatusReady})
	if err == nil {
		t.Fatal("expected provider-native error")
	}
	if !strings.Contains(err.Error(), "provider-native") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func certificateReadyOutputs() map[string]string {
	outputs := rotationReadyOutputs()
	outputs["controller_ca_fingerprint_sha256"] = "abc123"
	outputs["controller_cert_not_after"] = "2099-05-03T00:00:00Z"
	outputs["nats_tls_enabled"] = "true"
	outputs["minio_tls_enabled"] = "true"
	outputs["registry_tls_enabled"] = "true"
	outputs["s3_endpoint"] = "https://1.2.3.4:9000"
	outputs["registry_url"] = "https://1.2.3.4:5000"
	return outputs
}
