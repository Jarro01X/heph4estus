package selfhosted

import (
	"context"
	"errors"
	"fmt"

	"heph4estus/internal/cloud"
	"heph4estus/internal/logger"
)

// errQueueNotConfigured is returned by the stub queue when no NATS URL was
// provided in the provider config. Provide a QueueConfig to enable the real
// JetStream-backed queue.
var errQueueNotConfigured = errors.New("selfhosted: queue is not configured — provide NATS_URL to enable queue support")

// errComputeNotImplemented is returned by the stub compute surface.
// Selfhosted compute (docker run over SSH) lands in PR 6.2.
var errComputeNotImplemented = errors.New("selfhosted: compute is not implemented (PR 6.2)")

// Compile-time interface check.
var _ cloud.Provider = (*Provider)(nil)

// Provider is the selfhosted cloud.Provider backed by S3-compatible storage
// and NATS JetStream queuing. Compute remains a stub until PR 6.2.
type Provider struct {
	storage *Storage
	queue   cloud.Queue
	compute cloud.Compute
}

// ProviderConfig is the input to NewProvider.
type ProviderConfig struct {
	Storage StorageConfig
	Queue   *QueueConfig
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
	return &Provider{
		storage: storage,
		queue:   q,
		compute: stubCompute{},
	}, nil
}

// Storage returns the S3-compatible object store client.
func (p *Provider) Storage() cloud.Storage { return p.storage }

// Queue returns the NATS JetStream queue, or a stub if no NATS URL was configured.
func (p *Provider) Queue() cloud.Queue { return p.queue }

// Compute returns a placeholder compute surface until PR 6.2 lands.
func (p *Provider) Compute() cloud.Compute { return p.compute }

type stubQueue struct{}

func (stubQueue) Send(context.Context, string, string) error        { return errQueueNotConfigured }
func (stubQueue) SendBatch(context.Context, string, []string) error { return errQueueNotConfigured }
func (stubQueue) Receive(context.Context, string) (*cloud.Message, error) {
	return nil, errQueueNotConfigured
}
func (stubQueue) Delete(context.Context, string, string) error { return errQueueNotConfigured }

type stubCompute struct{}

func (stubCompute) RunContainer(context.Context, cloud.ContainerOpts) (string, error) {
	return "", errComputeNotImplemented
}
func (stubCompute) RunSpotInstances(context.Context, cloud.SpotOpts) ([]string, error) {
	return nil, errComputeNotImplemented
}
func (stubCompute) GetSpotStatus(context.Context, []string) ([]cloud.SpotStatus, error) {
	return nil, errComputeNotImplemented
}
