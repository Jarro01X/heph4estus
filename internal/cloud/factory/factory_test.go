package factory

import (
	"context"
	"strings"
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
	t.Setenv("HEPH_NATS_CLIENT_CERT_FILE", "/etc/heph/nats-client.crt")
	t.Setenv("HEPH_NATS_CLIENT_KEY_FILE", "/etc/heph/nats-client.key")

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
	if cfg.NATSClientCertFile != "/etc/heph/nats-client.crt" || cfg.NATSClientKeyFile != "/etc/heph/nats-client.key" {
		t.Errorf("NATS client cert files = %q/%q", cfg.NATSClientCertFile, cfg.NATSClientKeyFile)
	}
	if cfg.S3Secret != "mysecret" {
		t.Errorf("S3Secret = %q", cfg.S3Secret)
	}
	if !cfg.S3PathStyle {
		t.Error("S3PathStyle should be true")
	}
}

func TestSelfhostedConfigFromEnv_Defaults(t *testing.T) {
	for _, k := range []string{
		"NATS_URL", "NATS_STREAM", "S3_ENDPOINT", "S3_REGION", "S3_ACCESS_KEY", "S3_SECRET_KEY", "S3_PATH_STYLE",
		"SELFHOSTED_WORKER_HOSTS", "SELFHOSTED_SSH_USER", "SELFHOSTED_SSH_KEY_PATH", "SELFHOSTED_SSH_PORT", "SELFHOSTED_DOCKER_IMAGE",
	} {
		t.Setenv(k, "")
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
	if len(cfg.WorkerHosts) != 0 {
		t.Errorf("WorkerHosts should be empty, got %v", cfg.WorkerHosts)
	}
	if cfg.SSHUser != "" {
		t.Errorf("SSHUser should be empty, got %q", cfg.SSHUser)
	}
	if cfg.SSHKeyPath != "" {
		t.Errorf("SSHKeyPath should be empty, got %q", cfg.SSHKeyPath)
	}
	if cfg.SSHPort != 0 {
		t.Errorf("SSHPort should be 0, got %d", cfg.SSHPort)
	}
	if cfg.DockerImage != "" {
		t.Errorf("DockerImage should be empty, got %q", cfg.DockerImage)
	}
}

func TestSelfhostedConfigFromEnv_ComputeFields(t *testing.T) {
	t.Setenv("SELFHOSTED_WORKER_HOSTS", "10.0.0.1, 10.0.0.2, 10.0.0.3")
	t.Setenv("SELFHOSTED_SSH_USER", "heph")
	t.Setenv("SELFHOSTED_SSH_KEY_PATH", "/home/heph/.ssh/id_ed25519")
	t.Setenv("SELFHOSTED_SSH_PORT", "2222")
	t.Setenv("SELFHOSTED_DOCKER_IMAGE", "ghcr.io/heph/worker:latest")

	// Clear storage/queue vars to isolate the test.
	for _, k := range []string{"NATS_URL", "NATS_STREAM", "S3_ENDPOINT", "S3_REGION", "S3_ACCESS_KEY", "S3_SECRET_KEY", "S3_PATH_STYLE"} {
		t.Setenv(k, "")
	}

	cfg := SelfhostedConfigFromEnv()

	if len(cfg.WorkerHosts) != 3 {
		t.Fatalf("WorkerHosts = %v, want 3 entries", cfg.WorkerHosts)
	}
	if cfg.WorkerHosts[0] != "10.0.0.1" || cfg.WorkerHosts[1] != "10.0.0.2" || cfg.WorkerHosts[2] != "10.0.0.3" {
		t.Errorf("WorkerHosts = %v", cfg.WorkerHosts)
	}
	if cfg.SSHUser != "heph" {
		t.Errorf("SSHUser = %q", cfg.SSHUser)
	}
	if cfg.SSHKeyPath != "/home/heph/.ssh/id_ed25519" {
		t.Errorf("SSHKeyPath = %q", cfg.SSHKeyPath)
	}
	if cfg.SSHPort != 2222 {
		t.Errorf("SSHPort = %d", cfg.SSHPort)
	}
	if cfg.DockerImage != "ghcr.io/heph/worker:latest" {
		t.Errorf("DockerImage = %q", cfg.DockerImage)
	}
}

func TestSelfhostedConfigFromEnv_QueueAndBucket(t *testing.T) {
	t.Setenv("SELFHOSTED_QUEUE_ID", "scan-stream")
	t.Setenv("SELFHOSTED_BUCKET", "results-bucket")
	// Clear other vars.
	for _, k := range []string{"NATS_URL", "NATS_STREAM", "S3_ENDPOINT", "S3_REGION", "S3_ACCESS_KEY", "S3_SECRET_KEY", "S3_PATH_STYLE",
		"SELFHOSTED_WORKER_HOSTS", "SELFHOSTED_SSH_USER", "SELFHOSTED_SSH_KEY_PATH", "SELFHOSTED_SSH_PORT", "SELFHOSTED_DOCKER_IMAGE"} {
		t.Setenv(k, "")
	}

	cfg := SelfhostedConfigFromEnv()
	if cfg.QueueID != "scan-stream" {
		t.Errorf("QueueID = %q, want scan-stream", cfg.QueueID)
	}
	if cfg.Bucket != "results-bucket" {
		t.Errorf("Bucket = %q, want results-bucket", cfg.Bucket)
	}
}

func TestSelfhostedConfigFromEnv_SingleWorkerHost(t *testing.T) {
	t.Setenv("SELFHOSTED_WORKER_HOSTS", "worker.example.com")
	for _, k := range []string{"NATS_URL", "NATS_STREAM", "S3_ENDPOINT", "S3_REGION", "S3_ACCESS_KEY", "S3_SECRET_KEY", "S3_PATH_STYLE",
		"SELFHOSTED_SSH_USER", "SELFHOSTED_SSH_KEY_PATH", "SELFHOSTED_SSH_PORT", "SELFHOSTED_DOCKER_IMAGE"} {
		t.Setenv(k, "")
	}

	cfg := SelfhostedConfigFromEnv()
	if len(cfg.WorkerHosts) != 1 || cfg.WorkerHosts[0] != "worker.example.com" {
		t.Errorf("WorkerHosts = %v, want [worker.example.com]", cfg.WorkerHosts)
	}
}

func TestSelfhostedConfigFromEnv_FullScanRuntime(t *testing.T) {
	// Prove the full selfhosted scan runtime env contract is parsed correctly.
	t.Setenv("NATS_URL", "nats://ctrl:4222")
	t.Setenv("NATS_STREAM", "heph-tasks")
	t.Setenv("S3_ENDPOINT", "http://ctrl:9000")
	t.Setenv("S3_REGION", "us-east-1")
	t.Setenv("S3_ACCESS_KEY", "minioadmin")
	t.Setenv("S3_SECRET_KEY", "minioadmin")
	t.Setenv("S3_PATH_STYLE", "true")
	t.Setenv("SELFHOSTED_QUEUE_ID", "heph-tasks")
	t.Setenv("SELFHOSTED_BUCKET", "heph-results")
	t.Setenv("SELFHOSTED_WORKER_HOSTS", "w1.example.com,w2.example.com")
	t.Setenv("SELFHOSTED_SSH_USER", "heph")
	t.Setenv("SELFHOSTED_SSH_KEY_PATH", "/home/heph/.ssh/id_ed25519")
	t.Setenv("SELFHOSTED_SSH_PORT", "22")
	t.Setenv("SELFHOSTED_DOCKER_IMAGE", "ctrl:5000/heph-nmap-worker:latest")

	cfg := SelfhostedConfigFromEnv()

	// Queue/bucket (scan runtime contract).
	if cfg.QueueID != "heph-tasks" {
		t.Errorf("QueueID = %q", cfg.QueueID)
	}
	if cfg.Bucket != "heph-results" {
		t.Errorf("Bucket = %q", cfg.Bucket)
	}

	// Transport endpoints.
	if cfg.NATSURL != "nats://ctrl:4222" {
		t.Errorf("NATSURL = %q", cfg.NATSURL)
	}
	if cfg.S3Endpoint != "http://ctrl:9000" {
		t.Errorf("S3Endpoint = %q", cfg.S3Endpoint)
	}

	// Compute.
	if len(cfg.WorkerHosts) != 2 {
		t.Fatalf("WorkerHosts = %v", cfg.WorkerHosts)
	}
	if cfg.SSHUser != "heph" {
		t.Errorf("SSHUser = %q", cfg.SSHUser)
	}
	if cfg.SSHPort != 22 {
		t.Errorf("SSHPort = %d", cfg.SSHPort)
	}
	if cfg.DockerImage != "ctrl:5000/heph-nmap-worker:latest" {
		t.Errorf("DockerImage = %q", cfg.DockerImage)
	}
}

func TestSelfhostedConfigFromEnv_InvalidSSHPortIgnored(t *testing.T) {
	t.Setenv("SELFHOSTED_SSH_PORT", "not-a-number")
	for _, k := range []string{"NATS_URL", "S3_ENDPOINT", "S3_REGION", "S3_ACCESS_KEY", "S3_SECRET_KEY", "S3_PATH_STYLE",
		"SELFHOSTED_WORKER_HOSTS", "SELFHOSTED_SSH_USER", "SELFHOSTED_SSH_KEY_PATH", "SELFHOSTED_DOCKER_IMAGE",
		"SELFHOSTED_QUEUE_ID", "SELFHOSTED_BUCKET", "NATS_STREAM"} {
		t.Setenv(k, "")
	}

	cfg := SelfhostedConfigFromEnv()
	if cfg.SSHPort != 0 {
		t.Errorf("expected SSHPort 0 for invalid input, got %d", cfg.SSHPort)
	}
}

func TestBuildSelfhostedWithComputeConfig(t *testing.T) {
	p, err := Build(Config{
		Kind: cloud.KindSelfhosted,
		Selfhosted: &SelfhostedConfig{
			S3Endpoint:  "https://minio.example:9000",
			S3Region:    "us-east-1",
			S3AccessKey: "ak",
			S3Secret:    "sk",
			S3PathStyle: true,
			WorkerHosts: []string{"10.0.0.1", "10.0.0.2"},
			SSHUser:     "heph",
			SSHKeyPath:  "/tmp/key",
			SSHPort:     2222,
			DockerImage: "ghcr.io/heph/worker:latest",
		},
		Logger: logger.NewSimpleLogger(),
	})
	if err != nil {
		t.Fatalf("Build selfhosted with compute: %v", err)
	}
	if p == nil || p.Storage() == nil {
		t.Fatal("expected selfhosted provider with storage")
	}
	// Compute is backed by real DockerCompute when config is present.
	if p.Compute() == nil {
		t.Fatal("expected compute surface")
	}
}

func TestSelfhostedConfigFromOutputs(t *testing.T) {
	cfg := SelfhostedConfigFromOutputs(map[string]string{
		"nats_url":                      "nats://ctrl:4222",
		"nats_stream":                   "heph-tasks",
		"s3_endpoint":                   "http://ctrl:9000",
		"s3_region":                     "us-east-1",
		"s3_access_key":                 "ak",
		"s3_secret_key":                 "sk",
		"s3_path_style":                 "true",
		"sqs_queue_url":                 "heph-tasks",
		"s3_bucket_name":                "heph-results",
		"registry_url":                  "10.0.1.2:5000",
		"docker_image":                  "heph-nmap-worker:latest",
		"worker_hosts":                  "203.0.113.10,203.0.113.11",
		"nats_operator_client_cert_pem": "operator-cert",
		"nats_operator_client_key_pem":  "operator-key",

		"controller_security_mode": "private-auth",
		"nats_tls_enabled":         "false",
		"nats_auth_enabled":        "true",
		"minio_tls_enabled":        "false",
		"minio_auth_enabled":       "true",
		"registry_tls_enabled":     "false",
		"registry_auth_enabled":    "false",
	})

	if cfg.QueueID != "heph-tasks" {
		t.Fatalf("QueueID = %q, want heph-tasks", cfg.QueueID)
	}
	if cfg.DockerImage != "10.0.1.2:5000/heph-nmap-worker:latest" {
		t.Fatalf("DockerImage = %q", cfg.DockerImage)
	}
	if len(cfg.WorkerHosts) != 2 {
		t.Fatalf("WorkerHosts = %v, want 2 entries", cfg.WorkerHosts)
	}
	if cfg.ControllerSecurityMode != "private-auth" {
		t.Fatalf("ControllerSecurityMode = %q, want private-auth", cfg.ControllerSecurityMode)
	}
	if cfg.NATSClientCertPEM != "operator-cert" || cfg.NATSClientKeyPEM != "operator-key" {
		t.Fatalf("NATS client cert material = %q/%q", cfg.NATSClientCertPEM, cfg.NATSClientKeyPEM)
	}
	if !cfg.NATSAuthEnabled || !cfg.MinIOAuthEnabled {
		t.Fatalf("expected NATS and MinIO auth enabled, got nats=%t minio=%t", cfg.NATSAuthEnabled, cfg.MinIOAuthEnabled)
	}
	if cfg.NATSTLSEnabled || cfg.MinIOTLSEnabled || cfg.RegistryTLSEnabled || cfg.RegistryAuthEnabled {
		t.Fatalf("unexpected TLS/auth posture: natsTLS=%t minioTLS=%t registryTLS=%t registryAuth=%t", cfg.NATSTLSEnabled, cfg.MinIOTLSEnabled, cfg.RegistryTLSEnabled, cfg.RegistryAuthEnabled)
	}
}

func TestBuildHetznerDisablesSSHCompute(t *testing.T) {
	p, err := Build(Config{
		Kind: cloud.KindHetzner,
		Selfhosted: &SelfhostedConfig{
			S3Endpoint:  "http://ctrl:9000",
			S3Region:    "us-east-1",
			S3AccessKey: "ak",
			S3Secret:    "sk",
			S3PathStyle: true,
			WorkerHosts: []string{"203.0.113.10"},
			SSHUser:     "root",
			SSHKeyPath:  "/tmp/key",
			DockerImage: "10.0.1.2:5000/heph-nmap-worker:latest",
		},
		Logger: logger.NewSimpleLogger(),
	})
	if err != nil {
		t.Fatalf("Build hetzner: %v", err)
	}
	if _, err := p.Compute().RunContainer(context.Background(), cloud.ContainerOpts{}); err == nil {
		t.Fatal("expected provider-native Hetzner compute to reject RunContainer")
	} else if !strings.Contains(err.Error(), "compute is not configured") {
		t.Fatalf("unexpected RunContainer error: %v", err)
	}
}

func TestBuildLinodeDisablesSSHCompute(t *testing.T) {
	p, err := Build(Config{
		Kind: cloud.KindLinode,
		Selfhosted: &SelfhostedConfig{
			S3Endpoint:  "http://ctrl:9000",
			S3Region:    "us-east-1",
			S3AccessKey: "ak",
			S3Secret:    "sk",
			S3PathStyle: true,
		},
		Logger: logger.NewSimpleLogger(),
	})
	if err != nil {
		t.Fatalf("Build linode: %v", err)
	}
	if _, err := p.Compute().RunContainer(context.Background(), cloud.ContainerOpts{}); err == nil {
		t.Fatal("expected provider-native Linode compute to reject RunContainer")
	} else if !strings.Contains(err.Error(), "compute is not configured") {
		t.Fatalf("unexpected RunContainer error: %v", err)
	}
}

func TestBuildLinodeMissingConfig(t *testing.T) {
	_, err := Build(Config{Kind: cloud.KindLinode, Logger: logger.NewSimpleLogger()})
	if err == nil {
		t.Fatal("expected config-required error for linode")
	}
}
