package selfhosted

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"heph4estus/internal/cloud"
	"heph4estus/internal/logger"
)

// errQueueNotConfigured is returned by the stub queue when no NATS URL was
// provided in the provider config. Provide a QueueConfig to enable the real
// JetStream-backed queue.
var errQueueNotConfigured = errors.New("selfhosted: queue is not configured — provide NATS_URL to enable queue support")

// Compile-time interface check.
var _ cloud.Provider = (*Provider)(nil)

// Provider is the selfhosted cloud.Provider backed by S3-compatible storage,
// NATS JetStream queuing, and Docker-over-SSH compute.
type Provider struct {
	storage *Storage
	queue   cloud.Queue
	compute cloud.Compute
}

// ComputeConfig describes the SSH/Docker compute settings for selfhosted
// workers. The DockerCompute implementation in docker.go uses these to
// launch containers on remote hosts over SSH.
type ComputeConfig struct {
	WorkerHosts []string // SSH-reachable worker addresses
	SSHUser     string   // SSH login user
	SSHKeyPath  string   // path to private key
	SSHPort     int      // SSH port (0 means default 22)
	DockerImage string   // Docker image reference for the worker container
}

// ProviderConfig is the input to NewProvider.
type ProviderConfig struct {
	Storage StorageConfig
	Queue   *QueueConfig
	Compute *ComputeConfig
}

// NewProvider builds a selfhosted Provider from explicit configuration.
// When cfg.Queue is non-nil, a real NATS JetStream queue is constructed;
// otherwise the queue surface remains a loud stub.
func NewProvider(cfg ProviderConfig, log logger.Logger) (*Provider, error) {
	storage, err := NewStorage(cfg.Storage, log)
	if err != nil {
		return nil, err
	}
	var q cloud.Queue = stubQueue{}
	if cfg.Queue != nil {
		nq, err := NewQueue(*cfg.Queue, log)
		if err != nil {
			return nil, fmt.Errorf("selfhosted: queue init: %w", err)
		}
		q = nq
	}
	var comp cloud.Compute
	if err := validateComputeConfig(cfg.Compute); err != nil {
		comp = configErrorCompute{err: err}
	} else {
		transportEnv := buildTransportEnv(cfg)
		runner := &sshRunner{
			user:    cfg.Compute.SSHUser,
			keyPath: cfg.Compute.SSHKeyPath,
			port:    cfg.Compute.SSHPort,
		}
		comp = newDockerCompute(*cfg.Compute, transportEnv, runner, log)
	}
	return &Provider{
		storage: storage,
		queue:   q,
		compute: comp,
	}, nil
}

// Storage returns the S3-compatible object store client.
func (p *Provider) Storage() cloud.Storage { return p.storage }

// Queue returns the NATS JetStream queue, or a stub if no NATS URL was configured.
func (p *Provider) Queue() cloud.Queue { return p.queue }

// Compute returns the Docker-over-SSH compute surface when compute config is
// present, or a config-error surface otherwise.
func (p *Provider) Compute() cloud.Compute { return p.compute }

type stubQueue struct{}

func (stubQueue) Send(context.Context, string, string) error        { return errQueueNotConfigured }
func (stubQueue) SendBatch(context.Context, string, []string) error { return errQueueNotConfigured }
func (stubQueue) Receive(context.Context, string) (*cloud.Message, error) {
	return nil, errQueueNotConfigured
}
func (stubQueue) Delete(context.Context, string, string) error { return errQueueNotConfigured }

func buildTransportEnv(cfg ProviderConfig) map[string]string {
	region := cfg.Storage.Region
	if region == "" {
		region = "us-east-1"
	}
	env := map[string]string{
		"S3_ENDPOINT":   cfg.Storage.Endpoint,
		"S3_REGION":     region,
		"S3_ACCESS_KEY": cfg.Storage.AccessKey,
		"S3_SECRET_KEY": cfg.Storage.Secret,
		"S3_PATH_STYLE": strconv.FormatBool(cfg.Storage.PathStyle),
	}
	if cfg.Queue != nil {
		env["NATS_URL"] = cfg.Queue.URL
		if cfg.Queue.StreamName != "" {
			env["NATS_STREAM"] = cfg.Queue.StreamName
		}
	}
	return env
}
