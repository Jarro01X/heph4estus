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
