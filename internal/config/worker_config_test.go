package config

import (
	"testing"
)

func TestNewWorkerConfig_DefaultCloud(t *testing.T) {
	t.Setenv("QUEUE_URL", "https://sqs.example.com/queue")
	t.Setenv("S3_BUCKET", "test-bucket")
	t.Setenv("TOOL_NAME", "nmap")
	t.Setenv("CLOUD", "")

	cfg, err := NewWorkerConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Cloud != "aws" {
		t.Errorf("Cloud = %q, want aws", cfg.Cloud)
	}
}

func TestNewWorkerConfig_ExplicitCloud(t *testing.T) {
	t.Setenv("QUEUE_URL", "tasks.>")
	t.Setenv("S3_BUCKET", "results")
	t.Setenv("TOOL_NAME", "httpx")
	t.Setenv("CLOUD", "selfhosted")

	cfg, err := NewWorkerConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Cloud != "selfhosted" {
		t.Errorf("Cloud = %q, want selfhosted", cfg.Cloud)
	}
}

func TestNewWorkerConfig_ProviderNeutralFieldNames(t *testing.T) {
	t.Setenv("QUEUE_URL", "my-queue")
	t.Setenv("S3_BUCKET", "my-bucket")
	t.Setenv("TOOL_NAME", "nmap")

	cfg, err := NewWorkerConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.QueueID != "my-queue" {
		t.Errorf("QueueID = %q, want my-queue", cfg.QueueID)
	}
	if cfg.Bucket != "my-bucket" {
		t.Errorf("Bucket = %q, want my-bucket", cfg.Bucket)
	}
}

func TestNewWorkerConfig_JitterParsing(t *testing.T) {
	t.Setenv("QUEUE_URL", "q")
	t.Setenv("S3_BUCKET", "b")
	t.Setenv("TOOL_NAME", "nmap")
	t.Setenv("JITTER_MAX_SECONDS", "30")

	cfg, err := NewWorkerConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.JitterMaxSeconds != 30 {
		t.Errorf("JitterMaxSeconds = %d, want 30", cfg.JitterMaxSeconds)
	}
}

func TestNewWorkerConfig_JitterInvalidIgnored(t *testing.T) {
	t.Setenv("QUEUE_URL", "q")
	t.Setenv("S3_BUCKET", "b")
	t.Setenv("TOOL_NAME", "nmap")
	t.Setenv("JITTER_MAX_SECONDS", "abc")

	cfg, err := NewWorkerConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.JitterMaxSeconds != 0 {
		t.Errorf("JitterMaxSeconds = %d, want 0 for invalid input", cfg.JitterMaxSeconds)
	}
}

func TestNewWorkerConfig_SelfhostedScanRuntime(t *testing.T) {
	// Prove a selfhosted worker reads env-driven queue/bucket exactly like AWS.
	t.Setenv("QUEUE_URL", "nats-subject")
	t.Setenv("S3_BUCKET", "minio-bucket")
	t.Setenv("TOOL_NAME", "httpx")
	t.Setenv("CLOUD", "selfhosted")
	t.Setenv("JITTER_MAX_SECONDS", "")

	cfg, err := NewWorkerConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Cloud != "selfhosted" {
		t.Errorf("Cloud = %q, want selfhosted", cfg.Cloud)
	}
	if cfg.QueueID != "nats-subject" {
		t.Errorf("QueueID = %q, want nats-subject", cfg.QueueID)
	}
	if cfg.Bucket != "minio-bucket" {
		t.Errorf("Bucket = %q, want minio-bucket", cfg.Bucket)
	}
}

func TestNewWorkerConfig_FleetMetadata(t *testing.T) {
	t.Setenv("QUEUE_URL", "heph-tasks")
	t.Setenv("S3_BUCKET", "heph-results")
	t.Setenv("TOOL_NAME", "nmap")
	t.Setenv("CLOUD", "hetzner")
	t.Setenv("FLEET_HEARTBEAT", "true")
	t.Setenv("WORKER_ID", "heph-worker-0")
	t.Setenv("WORKER_HOST", "10.0.1.10")
	t.Setenv("NATS_URL", "nats://10.0.1.2:4222")
	t.Setenv("WORKER_VERSION", "heph-nmap-worker:latest")
	t.Setenv("FLEET_GENERATION_ID", "gen-123")

	cfg, err := NewWorkerConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.FleetHeartbeat {
		t.Fatal("expected FleetHeartbeat=true")
	}
	if cfg.WorkerVersion != "heph-nmap-worker:latest" {
		t.Fatalf("WorkerVersion = %q", cfg.WorkerVersion)
	}
	if cfg.GenerationID != "gen-123" {
		t.Fatalf("GenerationID = %q", cfg.GenerationID)
	}
}

func TestNewWorkerConfig_ControllerTLSMetadata(t *testing.T) {
	t.Setenv("QUEUE_URL", "heph-tasks")
	t.Setenv("S3_BUCKET", "heph-results")
	t.Setenv("TOOL_NAME", "nmap")
	t.Setenv("HEPH_CONTROLLER_CA_FILE", "/etc/heph/controller-ca.crt")
	t.Setenv("HEPH_CONTROLLER_SERVER_NAME", "heph-controller")
	t.Setenv("HEPH_NATS_CLIENT_CERT_FILE", "/etc/heph/nats-client.crt")
	t.Setenv("HEPH_NATS_CLIENT_KEY_FILE", "/etc/heph/nats-client.key")

	cfg, err := NewWorkerConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ControllerCAFile != "/etc/heph/controller-ca.crt" {
		t.Fatalf("ControllerCAFile = %q", cfg.ControllerCAFile)
	}
	if cfg.ControllerServerName != "heph-controller" {
		t.Fatalf("ControllerServerName = %q", cfg.ControllerServerName)
	}
	if cfg.NATSClientCertFile != "/etc/heph/nats-client.crt" {
		t.Fatalf("NATSClientCertFile = %q", cfg.NATSClientCertFile)
	}
	if cfg.NATSClientKeyFile != "/etc/heph/nats-client.key" {
		t.Fatalf("NATSClientKeyFile = %q", cfg.NATSClientKeyFile)
	}
}

func TestNewWorkerConfig_MissingRequired(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{"missing queue", map[string]string{"S3_BUCKET": "b", "TOOL_NAME": "t"}, "QUEUE_URL"},
		{"missing bucket", map[string]string{"QUEUE_URL": "q", "TOOL_NAME": "t"}, "S3_BUCKET"},
		{"missing tool", map[string]string{"QUEUE_URL": "q", "S3_BUCKET": "b"}, "TOOL_NAME"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("QUEUE_URL", "")
			t.Setenv("S3_BUCKET", "")
			t.Setenv("TOOL_NAME", "")
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			_, err := NewWorkerConfig()
			if err == nil {
				t.Fatal("expected error")
			}
			if got := err.Error(); got != tt.wantErr+" environment variable is required" {
				t.Fatalf("error = %q, want to contain %q", got, tt.wantErr)
			}
		})
	}
}
