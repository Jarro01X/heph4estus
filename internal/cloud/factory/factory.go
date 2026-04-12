// Package factory builds a cloud.Provider for a given Kind. It is the single
// place new cloud backends should be wired in so call sites do not branch on
// the kind directly.
package factory

import (
	"context"
	"fmt"
	"os"

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
// for the selfhosted provider. PR 6.1 Track 0 only persists the shape; later
// tracks attach the actual NATS and S3-compatible client code.
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
	switch cfg.Kind {
	case cloud.KindAWS, "":
		if cfg.AWS == nil {
			return nil, fmt.Errorf("factory: AWS config is required for cloud %q", cloud.KindAWS)
		}
		return awscloud.NewProvider(cfg.AWS.SDKConfig, cfg.Logger), nil
	case cloud.KindSelfhosted:
		if cfg.Selfhosted == nil {
			return nil, fmt.Errorf("factory: selfhosted config is required for cloud %q", cloud.KindSelfhosted)
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
	return &SelfhostedConfig{
		NATSURL:     os.Getenv("NATS_URL"),
		StreamName:  os.Getenv("NATS_STREAM"),
		S3Endpoint:  os.Getenv("S3_ENDPOINT"),
		S3Region:    envOr("S3_REGION", "us-east-1"),
		S3AccessKey: os.Getenv("S3_ACCESS_KEY"),
		S3Secret:    os.Getenv("S3_SECRET_KEY"),
		S3PathStyle: os.Getenv("S3_PATH_STYLE") == "true",
	}
}

// BuildForKind constructs a provider for the given kind, loading
// configuration from the standard environment. For AWS this uses the
// default SDK credential chain; for selfhosted it reads NATS_URL,
// S3_ENDPOINT, and related env vars via SelfhostedConfigFromEnv.
func BuildForKind(ctx context.Context, kind cloud.Kind, log logger.Logger) (cloud.Provider, error) {
	switch kind {
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
	case cloud.KindSelfhosted:
		return Build(Config{
			Kind:       cloud.KindSelfhosted,
			Selfhosted: SelfhostedConfigFromEnv(),
			Logger:     log,
		})
	default:
		return nil, fmt.Errorf("factory: unsupported cloud %q", kind)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
