package factory

import (
	"os"
	"testing"

	"heph4estus/internal/cloud"
	"heph4estus/internal/logger"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
)

func TestBuildAWSDefault(t *testing.T) {
	p, err := Build(Config{
		Kind:   cloud.KindAWS,
		AWS:    &AWSConfig{SDKConfig: awssdk.Config{Region: "us-east-1"}},
		Logger: logger.NewSimpleLogger(),
	})
	if err != nil {
		t.Fatalf("Build aws: %v", err)
	}
	if p == nil || p.Storage() == nil || p.Queue() == nil || p.Compute() == nil {
		t.Fatal("expected fully populated AWS provider")
	}
}

func TestBuildAWSEmptyKindFallsToAWS(t *testing.T) {
	p, err := Build(Config{
		AWS:    &AWSConfig{SDKConfig: awssdk.Config{}},
		Logger: logger.NewSimpleLogger(),
	})
	if err != nil {
		t.Fatalf("Build empty kind: %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
}

func TestBuildAWSMissingConfig(t *testing.T) {
	_, err := Build(Config{Kind: cloud.KindAWS, Logger: logger.NewSimpleLogger()})
	if err == nil {
		t.Fatal("expected error when AWS config is missing")
	}
}

func TestBuildSelfhostedStorageOnly(t *testing.T) {
	// NATSURL is intentionally empty so the queue stays a stub and no
	// real connection is attempted. NATS integration is tested in the
	// selfhosted package with an embedded server.
	p, err := Build(Config{
		Kind: cloud.KindSelfhosted,
		Selfhosted: &SelfhostedConfig{
			S3Endpoint:  "https://minio.example:9000",
			S3Region:    "us-east-1",
			S3AccessKey: "ak",
			S3Secret:    "sk",
			S3PathStyle: true,
		},
		Logger: logger.NewSimpleLogger(),
	})
	if err != nil {
		t.Fatalf("Build selfhosted: %v", err)
	}
	if p == nil || p.Storage() == nil {
		t.Fatal("expected selfhosted provider with storage")
	}
	if p.Queue() == nil || p.Compute() == nil {
		t.Fatal("expected stub queue/compute surfaces")
	}
}

func TestBuildSelfhostedMissingStorageEndpoint(t *testing.T) {
	_, err := Build(Config{
		Kind:       cloud.KindSelfhosted,
		Selfhosted: &SelfhostedConfig{NATSURL: "nats://example:4222"},
		Logger:     logger.NewSimpleLogger(),
	})
	if err == nil {
		t.Fatal("expected error when S3 endpoint is missing")
	}
}

func TestBuildSelfhostedMissingConfig(t *testing.T) {
	_, err := Build(Config{Kind: cloud.KindSelfhosted, Logger: logger.NewSimpleLogger()})
	if err == nil {
		t.Fatal("expected config-required error")
	}
}

func TestBuildRequiresLogger(t *testing.T) {
	_, err := Build(Config{Kind: cloud.KindAWS})
	if err == nil {
		t.Fatal("expected error when logger is missing")
	}
}

func TestBuildUnsupportedKind(t *testing.T) {
	_, err := Build(Config{Kind: "gcp", Logger: logger.NewSimpleLogger()})
	if err == nil {
		t.Fatal("expected error for unsupported kind")
	}
}

func TestSelfhostedConfigFromEnv(t *testing.T) {
	t.Setenv("NATS_URL", "nats://localhost:4222")
	t.Setenv("NATS_STREAM", "custom-stream")
	t.Setenv("S3_ENDPOINT", "https://minio.local:9000")
	t.Setenv("S3_REGION", "eu-west-1")
	t.Setenv("S3_ACCESS_KEY", "mykey")
	t.Setenv("S3_SECRET_KEY", "mysecret")
	t.Setenv("S3_PATH_STYLE", "true")

	cfg := SelfhostedConfigFromEnv()

	if cfg.NATSURL != "nats://localhost:4222" {
		t.Errorf("NATSURL = %q", cfg.NATSURL)
	}
	if cfg.StreamName != "custom-stream" {
		t.Errorf("StreamName = %q", cfg.StreamName)
	}
	if cfg.S3Endpoint != "https://minio.local:9000" {
		t.Errorf("S3Endpoint = %q", cfg.S3Endpoint)
	}
	if cfg.S3Region != "eu-west-1" {
		t.Errorf("S3Region = %q", cfg.S3Region)
	}
	if cfg.S3AccessKey != "mykey" {
		t.Errorf("S3AccessKey = %q", cfg.S3AccessKey)
	}
	if cfg.S3Secret != "mysecret" {
		t.Errorf("S3Secret = %q", cfg.S3Secret)
	}
	if !cfg.S3PathStyle {
		t.Error("S3PathStyle should be true")
	}
}

func TestSelfhostedConfigFromEnv_Defaults(t *testing.T) {
	for _, k := range []string{"NATS_URL", "NATS_STREAM", "S3_ENDPOINT", "S3_REGION", "S3_ACCESS_KEY", "S3_SECRET_KEY", "S3_PATH_STYLE"} {
		os.Unsetenv(k)
	}

	cfg := SelfhostedConfigFromEnv()

	if cfg.NATSURL != "" {
		t.Errorf("NATSURL should be empty, got %q", cfg.NATSURL)
	}
	if cfg.S3Region != "us-east-1" {
		t.Errorf("S3Region should default to us-east-1, got %q", cfg.S3Region)
	}
	if cfg.S3PathStyle {
		t.Error("S3PathStyle should default to false")
	}
}
