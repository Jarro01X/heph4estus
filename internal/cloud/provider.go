package cloud

import (
	"context"
	"errors"
)

// ErrNotImplemented is returned by stub implementations.
var ErrNotImplemented = errors.New("cloud: operation not implemented")

// Provider gives access to cloud storage, queuing, and compute services.
type Provider interface {
	Storage() Storage
	Queue() Queue
	Compute() Compute
}

// Storage abstracts object-store operations (S3, GCS, etc.).
type Storage interface {
	Upload(ctx context.Context, bucket, key string, data []byte) error
	Download(ctx context.Context, bucket, key string) ([]byte, error)
	List(ctx context.Context, bucket, prefix string) ([]string, error)
}

// Queue abstracts message-queue operations (SQS, Pub/Sub, etc.).
type Queue interface {
	Send(ctx context.Context, queueID, body string) error
	Receive(ctx context.Context, queueID string) (*Message, error)
	Delete(ctx context.Context, queueID, receiptHandle string) error
}

// Message represents a single message received from a queue.
type Message struct {
	ID            string
	Body          string
	ReceiptHandle string
}

// Compute abstracts container and spot-instance operations.
type Compute interface {
	RunContainer(ctx context.Context, opts ContainerOpts) (string, error)
	RunSpotInstances(ctx context.Context, opts SpotOpts) ([]string, error)
	GetSpotStatus(ctx context.Context, instanceIDs []string) ([]SpotStatus, error)
}

// ContainerOpts configures a container task (ECS Fargate, Cloud Run, etc.).
type ContainerOpts struct {
	Image          string
	Cluster        string
	CPU            int
	MemoryMB       int
	Command        []string
	Env            map[string]string
	Subnets        []string
	SecurityGroups []string
}

// SpotOpts configures a spot/preemptible instance request.
type SpotOpts struct {
	AMI            string
	InstanceType   string
	KeyPair        string
	UserData       string
	MaxPrice       string
	Count          int
	SecurityGroups []string
}

// SpotStatus describes the current state of a spot instance.
type SpotStatus struct {
	InstanceID string
	State      string
	PublicIP   string
}
