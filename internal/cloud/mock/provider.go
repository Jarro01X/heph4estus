package mock

import (
	"context"
	"heph4estus/internal/cloud"
)

// Compile-time interface checks.
var (
	_ cloud.Provider = (*Provider)(nil)
	_ cloud.Storage  = (*Storage)(nil)
	_ cloud.Queue    = (*Queue)(nil)
	_ cloud.Compute  = (*Compute)(nil)
)

// Provider is a test double for cloud.Provider.
type Provider struct {
	StorageImpl func() cloud.Storage
	QueueImpl   func() cloud.Queue
	ComputeImpl func() cloud.Compute
}

func (p *Provider) Storage() cloud.Storage { return p.StorageImpl() }
func (p *Provider) Queue() cloud.Queue     { return p.QueueImpl() }
func (p *Provider) Compute() cloud.Compute { return p.ComputeImpl() }

// Storage is a test double for cloud.Storage.
type Storage struct {
	UploadFunc   func(ctx context.Context, bucket, key string, data []byte) error
	DownloadFunc func(ctx context.Context, bucket, key string) ([]byte, error)
	ListFunc     func(ctx context.Context, bucket, prefix string) ([]string, error)
}

func (s *Storage) Upload(ctx context.Context, bucket, key string, data []byte) error {
	return s.UploadFunc(ctx, bucket, key, data)
}

func (s *Storage) Download(ctx context.Context, bucket, key string) ([]byte, error) {
	return s.DownloadFunc(ctx, bucket, key)
}

func (s *Storage) List(ctx context.Context, bucket, prefix string) ([]string, error) {
	return s.ListFunc(ctx, bucket, prefix)
}

// Queue is a test double for cloud.Queue.
type Queue struct {
	SendFunc    func(ctx context.Context, queueID, body string) error
	ReceiveFunc func(ctx context.Context, queueID string) (*cloud.Message, error)
	DeleteFunc  func(ctx context.Context, queueID, receiptHandle string) error
}

func (q *Queue) Send(ctx context.Context, queueID, body string) error {
	return q.SendFunc(ctx, queueID, body)
}

func (q *Queue) Receive(ctx context.Context, queueID string) (*cloud.Message, error) {
	return q.ReceiveFunc(ctx, queueID)
}

func (q *Queue) Delete(ctx context.Context, queueID, receiptHandle string) error {
	return q.DeleteFunc(ctx, queueID, receiptHandle)
}

// Compute is a test double for cloud.Compute.
type Compute struct {
	RunContainerFunc    func(ctx context.Context, opts cloud.ContainerOpts) (string, error)
	RunSpotInstancesFunc func(ctx context.Context, opts cloud.SpotOpts) ([]string, error)
	GetSpotStatusFunc   func(ctx context.Context, instanceIDs []string) ([]cloud.SpotStatus, error)
}

func (c *Compute) RunContainer(ctx context.Context, opts cloud.ContainerOpts) (string, error) {
	return c.RunContainerFunc(ctx, opts)
}

func (c *Compute) RunSpotInstances(ctx context.Context, opts cloud.SpotOpts) ([]string, error) {
	return c.RunSpotInstancesFunc(ctx, opts)
}

func (c *Compute) GetSpotStatus(ctx context.Context, instanceIDs []string) ([]cloud.SpotStatus, error) {
	return c.GetSpotStatusFunc(ctx, instanceIDs)
}
