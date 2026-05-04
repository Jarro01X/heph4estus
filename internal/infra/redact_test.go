package infra

import "testing"

func TestIsSensitiveOutput(t *testing.T) {
	tests := []struct {
		key  string
		want bool
	}{
		// Should be sensitive.
		{"s3_secret_key", true},
		{"s3_access_key", true},
		{"registry_password", true},
		{"minio_root_password", true},
		{"auth_token", true},
		{"api_credential", true},
		{"S3_SECRET_KEY", true}, // case insensitive
		{"controller_ca_pem", true},
		{"nats_operator_client_cert_pem", true},
		{"nats_operator_client_key_pem", true},

		// Should NOT be sensitive.
		{"tool_name", false},
		{"nats_url", true}, // contains embedded auth credentials
		{"s3_endpoint", false},
		{"s3_region", false},
		{"s3_path_style", false},
		{"registry_url", false},
		{"sqs_queue_url", false},
		{"ecr_repo_url", false},
		{"s3_bucket_name", false},
		{"ecs_cluster_name", false},
	}

	for _, tt := range tests {
		got := IsSensitiveOutput(tt.key)
		if got != tt.want {
			t.Errorf("IsSensitiveOutput(%q) = %v, want %v", tt.key, got, tt.want)
		}
	}
}

func TestRedactOutputValue(t *testing.T) {
	if got := RedactOutputValue("s3_secret_key", "my-secret"); got != redactedPlaceholder {
		t.Errorf("expected redacted, got %q", got)
	}
	if got := RedactOutputValue("tool_name", "nmap"); got != "nmap" {
		t.Errorf("expected nmap, got %q", got)
	}
}

func TestRedactOutputValue_SelfhostedOutputs(t *testing.T) {
	// Prove selfhosted controller outputs are redacted when sensitive.
	sensitiveKeys := []string{
		"minio_root_password",
		"registry_password",
		"s3_secret_key",
		"s3_access_key",
	}
	for _, key := range sensitiveKeys {
		got := RedactOutputValue(key, "real-value")
		if got != redactedPlaceholder {
			t.Errorf("RedactOutputValue(%q) = %q, expected redacted", key, got)
		}
	}

	// Non-sensitive selfhosted outputs should pass through.
	safeKeys := map[string]string{
		"s3_endpoint":   "http://10.0.1.5:9000",
		"registry_url":  "10.0.1.5:5000",
		"s3_region":     "us-east-1",
		"s3_path_style": "true",
		"controller_ip": "10.0.1.5",
	}
	for key, val := range safeKeys {
		got := RedactOutputValue(key, val)
		if got != val {
			t.Errorf("RedactOutputValue(%q) = %q, expected pass-through %q", key, got, val)
		}
	}
}

func TestRedactOutputs(t *testing.T) {
	outputs := map[string]string{
		"tool_name":     "nmap",
		"s3_access_key": "AKIAIOSFODNN7EXAMPLE",
		"s3_secret_key": "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		"s3_endpoint":   "http://10.0.1.5:9000",
		"registry_url":  "10.0.1.5:5000",
	}
	redacted := RedactOutputs(outputs)

	// Safe values pass through.
	if redacted["tool_name"] != "nmap" {
		t.Errorf("tool_name = %q, want nmap", redacted["tool_name"])
	}
	if redacted["s3_endpoint"] != "http://10.0.1.5:9000" {
		t.Errorf("s3_endpoint = %q, want http://10.0.1.5:9000", redacted["s3_endpoint"])
	}
	if redacted["registry_url"] != "10.0.1.5:5000" {
		t.Errorf("registry_url = %q, want 10.0.1.5:5000", redacted["registry_url"])
	}

	// Sensitive values are masked.
	if redacted["s3_access_key"] != redactedPlaceholder {
		t.Errorf("s3_access_key = %q, want %q", redacted["s3_access_key"], redactedPlaceholder)
	}
	if redacted["s3_secret_key"] != redactedPlaceholder {
		t.Errorf("s3_secret_key = %q, want %q", redacted["s3_secret_key"], redactedPlaceholder)
	}

	// Original map is not modified.
	if outputs["s3_access_key"] == redactedPlaceholder {
		t.Error("original map was modified")
	}
}
