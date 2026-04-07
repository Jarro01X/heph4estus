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
	Count(ctx context.Context, bucket, prefix string) (int, error)
}

// Queue abstracts message-queue operations (SQS, Pub/Sub, etc.).
type Queue interface {
	Send(ctx context.Context, queueID, body string) error
	SendBatch(ctx context.Context, queueID string, bodies []string) error
	Receive(ctx context.Context, queueID string) (*Message, error)
	Delete(ctx context.Context, queueID, receiptHandle string) error
}

// Message represents a single message received from a queue.
type Message struct {
	ID            string
	Body          string
	ReceiptHandle string
	ReceiveCount  int // ApproximateReceiveCount from SQS; 0 if unavailable
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
	TaskDefinition string // ECS task definition ARN
	ContainerName  string // Container name for env overrides (e.g. "nmap-worker")
	Count          int    // Number of tasks to launch (default 1)
}

// SpotOpts configures a spot/preemptible instance request.
type SpotOpts struct {
	AMI             string
	InstanceTypes   []string          // Multiple types for availability
	KeyPair         string
	UserData        string            // base64-encoded
	MaxPrice        string            // Per-instance-hour bid
	Count           int
	SecurityGroups  []string
	SubnetIDs       []string
	InstanceProfile string            // IAM instance profile ARN
	Tags            map[string]string
}

// SpotStatus describes the current state of a spot instance.
type SpotStatus struct {
	InstanceID string
	State      string
	PublicIP   string
}

// ProgressCounter provides O(1) progress tracking for large-scale jobs.
// Workers call Increment after each successful result upload; the TUI calls
// Get to read the current count. At 1M+ targets this is far cheaper than
// listing S3 objects (which is O(n/1000) per poll).
//
// Implementations: DynamoDB atomic counter (AWS), Redis INCR, NATS KV.
// Falls back to Storage.Count() when no counter backend is available.
type ProgressCounter interface {
	Increment(ctx context.Context, counterID string) error
	Get(ctx context.Context, counterID string) (int, error)
}
