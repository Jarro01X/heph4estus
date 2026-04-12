package selfhosted

import (
	"context"
	"errors"
	"testing"

	"heph4estus/internal/cloud"
	"heph4estus/internal/logger"
)

func TestProviderStorageOnlyStubsQueue(t *testing.T) {
	p, err := NewProvider(ProviderConfig{
		Storage: StorageConfig{
			Endpoint:  "https://minio.local:9000",
			Region:    "us-east-1",
			AccessKey: "ak",
			Secret:    "sk",
			PathStyle: true,
		},
	}, logger.NewSimpleLogger())
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	if p.Storage() == nil {
		t.Fatal("expected non-nil storage")
	}
	if p.Queue() == nil {
		t.Fatal("expected non-nil stub queue")
	}
	if p.Compute() == nil {
		t.Fatal("expected non-nil stub compute")
	}
}

func TestStubQueueReturnsError(t *testing.T) {
	p, err := NewProvider(ProviderConfig{
		Storage: StorageConfig{
			Endpoint:  "https://minio.local:9000",
			AccessKey: "ak",
			Secret:    "sk",
		},
	}, logger.NewSimpleLogger())
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}

	ctx := context.Background()
	q := p.Queue()

	if err := q.Send(ctx, "q", "body"); !errors.Is(err, errQueueNotConfigured) {
		t.Errorf("Send = %v, want errQueueNotConfigured", err)
	}
	if err := q.SendBatch(ctx, "q", []string{"a"}); !errors.Is(err, errQueueNotConfigured) {
		t.Errorf("SendBatch = %v, want errQueueNotConfigured", err)
	}
	if _, err := q.Receive(ctx, "q"); !errors.Is(err, errQueueNotConfigured) {
		t.Errorf("Receive = %v, want errQueueNotConfigured", err)
	}
	if err := q.Delete(ctx, "q", "h"); !errors.Is(err, errQueueNotConfigured) {
		t.Errorf("Delete = %v, want errQueueNotConfigured", err)
	}
}

func TestStubComputeReturnsError(t *testing.T) {
	p, err := NewProvider(ProviderConfig{
		Storage: StorageConfig{
			Endpoint:  "https://minio.local:9000",
			AccessKey: "ak",
			Secret:    "sk",
		},
	}, logger.NewSimpleLogger())
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}

	ctx := context.Background()
	c := p.Compute()

	if _, err := c.RunContainer(ctx, cloud.ContainerOpts{}); !errors.Is(err, errComputeNotImplemented) {
		t.Errorf("RunContainer = %v, want errComputeNotImplemented", err)
	}
	if _, err := c.RunSpotInstances(ctx, cloud.SpotOpts{}); !errors.Is(err, errComputeNotImplemented) {
		t.Errorf("RunSpotInstances = %v, want errComputeNotImplemented", err)
	}
	if _, err := c.GetSpotStatus(ctx, nil); !errors.Is(err, errComputeNotImplemented) {
		t.Errorf("GetSpotStatus = %v, want errComputeNotImplemented", err)
	}
}

func TestProviderInvalidStorageConfigFails(t *testing.T) {
	_, err := NewProvider(ProviderConfig{
		Storage: StorageConfig{},
	}, logger.NewSimpleLogger())
	if err == nil {
		t.Fatal("expected error for empty storage config")
	}
}

func TestProviderNilLoggerFails(t *testing.T) {
	_, err := NewProvider(ProviderConfig{
		Storage: StorageConfig{
			Endpoint:  "https://minio.local:9000",
			AccessKey: "ak",
			Secret:    "sk",
		},
	}, nil)
	if err == nil {
		t.Fatal("expected error for nil logger")
	}
}

func TestProviderImplementsInterface(t *testing.T) {
	var _ cloud.Provider = (*Provider)(nil)
}
