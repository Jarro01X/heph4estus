// Package factory builds a cloud.Provider for a given Kind. It is the single
// place new cloud backends should be wired in so call sites do not branch on
// the kind directly.
package factory

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"heph4estus/internal/cloud"
	awscloud "heph4estus/internal/cloud/aws"
	"heph4estus/internal/cloud/selfhosted"
	"heph4estus/internal/logger"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
)

// AWSConfig carries everything needed to construct the AWS provider.
// It is intentionally a thin wrapper around the SDK config so the factory
// signature stays stable when later tracks add fields.
type AWSConfig struct {
	SDKConfig awssdk.Config
}

// SelfhostedConfig describes the explicit endpoints and credentials needed
// for the selfhosted provider (NATS JetStream, S3-compatible storage,
// Docker-over-SSH compute).
type SelfhostedConfig struct {
	// NATS JetStream queue settings.
	NATSURL        string
	StreamName     string
	DurablePrefix  string
	AckWaitSeconds int
	MaxDeliver     int

	// S3-compatible object store settings.
	S3Endpoint  string
	S3Region    string
	S3AccessKey string
	S3Secret    string
	S3PathStyle bool

	// Scan runtime settings — queue identifier and bucket for scan execution.
	QueueID string // Queue identifier (SQS URL or NATS subject)
	Bucket  string // Storage bucket name

	// Compute settings for SSH/Docker workers.
	WorkerHosts []string // SSH-reachable worker addresses
	SSHUser     string   // SSH login user
	SSHKeyPath  string   // path to private key
	SSHPort     int      // SSH port (0 means default 22)
	DockerImage string   // Docker image reference for the worker container
}

// Config is the input to Build. Exactly one of AWS or Selfhosted should be
// populated, matching Kind.
type Config struct {
	Kind       cloud.Kind
	AWS        *AWSConfig
	Selfhosted *SelfhostedConfig
	Logger     logger.Logger
}

// Build returns a cloud.Provider for the requested kind. It validates that
// the matching config block is populated and returns an actionable error
// otherwise.
func Build(cfg Config) (cloud.Provider, error) {
	if cfg.Logger == nil {
		return nil, fmt.Errorf("factory: logger is required")
	}
	switch cfg.Kind.RuntimeFamily() {
	case cloud.KindAWS, "":
		if cfg.AWS == nil {
			return nil, fmt.Errorf("factory: AWS config is required for cloud %q", cloud.KindAWS)
		}
		return awscloud.NewProvider(cfg.AWS.SDKConfig, cfg.Logger), nil
	case cloud.KindManual:
		// Manual mode uses the selfhosted runtime end-to-end, including
		// Docker-over-SSH compute.
		if cfg.Selfhosted == nil {
			return nil, fmt.Errorf("factory: selfhosted config is required for cloud %q", cfg.Kind.Canonical())
		}
		pcfg := selfhosted.ProviderConfig{
			Storage: selfhosted.StorageConfig{
				Endpoint:  cfg.Selfhosted.S3Endpoint,
				Region:    cfg.Selfhosted.S3Region,
				AccessKey: cfg.Selfhosted.S3AccessKey,
				Secret:    cfg.Selfhosted.S3Secret,
				PathStyle: cfg.Selfhosted.S3PathStyle,
			},
		}
		if cfg.Selfhosted.NATSURL != "" {
			pcfg.Queue = &selfhosted.QueueConfig{
				URL:            cfg.Selfhosted.NATSURL,
				StreamName:     cfg.Selfhosted.StreamName,
				DurablePrefix:  cfg.Selfhosted.DurablePrefix,
				AckWaitSeconds: cfg.Selfhosted.AckWaitSeconds,
				MaxDeliver:     cfg.Selfhosted.MaxDeliver,
			}
		}
		if len(cfg.Selfhosted.WorkerHosts) > 0 {
			pcfg.Compute = &selfhosted.ComputeConfig{
				WorkerHosts: cfg.Selfhosted.WorkerHosts,
				SSHUser:     cfg.Selfhosted.SSHUser,
				SSHKeyPath:  cfg.Selfhosted.SSHKeyPath,
				SSHPort:     cfg.Selfhosted.SSHPort,
				DockerImage: cfg.Selfhosted.DockerImage,
			}
		}
		return selfhosted.NewProvider(pcfg, cfg.Logger)
	case cloud.KindHetzner, cloud.KindLinode:
		// Provider-native VPS paths reuse the selfhosted queue/storage runtime,
		// but normal operator flows must not fall back to ad hoc SSH launches.
		if cfg.Selfhosted == nil {
			return nil, fmt.Errorf("factory: selfhosted config is required for cloud %q", cfg.Kind.Canonical())
		}
		pcfg := selfhosted.ProviderConfig{
			Storage: selfhosted.StorageConfig{
				Endpoint:  cfg.Selfhosted.S3Endpoint,
				Region:    cfg.Selfhosted.S3Region,
				AccessKey: cfg.Selfhosted.S3AccessKey,
				Secret:    cfg.Selfhosted.S3Secret,
				PathStyle: cfg.Selfhosted.S3PathStyle,
			},
		}
		if cfg.Selfhosted.NATSURL != "" {
			pcfg.Queue = &selfhosted.QueueConfig{
				URL:            cfg.Selfhosted.NATSURL,
				StreamName:     cfg.Selfhosted.StreamName,
				DurablePrefix:  cfg.Selfhosted.DurablePrefix,
				AckWaitSeconds: cfg.Selfhosted.AckWaitSeconds,
				MaxDeliver:     cfg.Selfhosted.MaxDeliver,
			}
		}
		return selfhosted.NewProvider(pcfg, cfg.Logger)
	default:
		return nil, fmt.Errorf("factory: unsupported cloud %q", cfg.Kind)
	}
}

// SelfhostedConfigFromEnv reads selfhosted provider configuration from
// environment variables. CLI, TUI, and worker paths use this so endpoint
// configuration is centralised in one place.
func SelfhostedConfigFromEnv() *SelfhostedConfig {
	cfg := &SelfhostedConfig{
		NATSURL:     os.Getenv("NATS_URL"),
		StreamName:  os.Getenv("NATS_STREAM"),
		S3Endpoint:  os.Getenv("S3_ENDPOINT"),
		S3Region:    envOr("S3_REGION", "us-east-1"),
		S3AccessKey: os.Getenv("S3_ACCESS_KEY"),
		S3Secret:    os.Getenv("S3_SECRET_KEY"),
		S3PathStyle: os.Getenv("S3_PATH_STYLE") == "true",

		// Scan runtime settings.
		QueueID: os.Getenv("SELFHOSTED_QUEUE_ID"),
		Bucket:  os.Getenv("SELFHOSTED_BUCKET"),

		// Compute settings.
		SSHUser:     os.Getenv("SELFHOSTED_SSH_USER"),
		SSHKeyPath:  os.Getenv("SELFHOSTED_SSH_KEY_PATH"),
		DockerImage: os.Getenv("SELFHOSTED_DOCKER_IMAGE"),
	}
	if hosts := os.Getenv("SELFHOSTED_WORKER_HOSTS"); hosts != "" {
		cfg.WorkerHosts = splitCommaList(hosts)
	}
	if port := os.Getenv("SELFHOSTED_SSH_PORT"); port != "" {
		if p, err := strconv.Atoi(port); err == nil && p > 0 {
			cfg.SSHPort = p
		}
	}
	return cfg
}

// BuildForKind constructs a provider for the given kind, loading
// configuration from the standard environment. For AWS this uses the
// default SDK credential chain; for selfhosted it reads NATS_URL,
// S3_ENDPOINT, and related env vars via SelfhostedConfigFromEnv.
func BuildForKind(ctx context.Context, kind cloud.Kind, log logger.Logger) (cloud.Provider, error) {
	switch kind.RuntimeFamily() {
	case cloud.KindAWS, "":
		sdkCfg, err := awscfg.LoadDefaultConfig(ctx)
		if err != nil {
			return nil, fmt.Errorf("loading AWS config: %w", err)
		}
		return Build(Config{
			Kind:   cloud.KindAWS,
			AWS:    &AWSConfig{SDKConfig: sdkCfg},
			Logger: log,
		})
	case cloud.KindManual, cloud.KindHetzner, cloud.KindLinode:
		return Build(Config{
			Kind:       kind.Canonical(),
			Selfhosted: SelfhostedConfigFromEnv(),
			Logger:     log,
		})
	default:
		return nil, fmt.Errorf("factory: unsupported cloud %q", kind)
	}
}

// SelfhostedConfigFromOutputs constructs a SelfhostedConfig from Terraform
// outputs. This is used by provider-native paths (Hetzner) where the
// controller endpoints and credentials come from deploy outputs rather than
// operator environment variables.
func SelfhostedConfigFromOutputs(outputs map[string]string) *SelfhostedConfig {
	cfg := &SelfhostedConfig{
		NATSURL:     outputs["nats_url"],
		StreamName:  outputs["nats_stream"],
		S3Endpoint:  outputs["s3_endpoint"],
		S3Region:    outputs["s3_region"],
		S3AccessKey: outputs["s3_access_key"],
		S3Secret:    outputs["s3_secret_key"],
		S3PathStyle: outputs["s3_path_style"] == "true",
		QueueID:     outputs["sqs_queue_url"],
		Bucket:      outputs["s3_bucket_name"],
		DockerImage: outputs["registry_url"] + "/" + outputs["docker_image"],
	}
	if hosts := outputs["worker_hosts"]; hosts != "" {
		cfg.WorkerHosts = splitCommaList(hosts)
	}
	return cfg
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// splitCommaList splits a comma-separated string into trimmed, non-empty parts.
func splitCommaList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
