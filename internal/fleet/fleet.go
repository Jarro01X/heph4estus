package fleet

import (
	"context"
	"time"
)

// HeartbeatSubject is the NATS subject workers publish heartbeats on.
const HeartbeatSubject = "heph.fleet.heartbeat"

// DefaultHeartbeatInterval is how often workers should publish heartbeats.
const DefaultHeartbeatInterval = 30 * time.Second

// DefaultHealthTimeout is how long since the last heartbeat before a worker
// is considered unhealthy. Set to 3x the heartbeat interval so transient
// network hiccups don't trigger false positives.
const DefaultHealthTimeout = 3 * DefaultHeartbeatInterval

// HeartbeatMessage is the JSON payload workers publish on HeartbeatSubject.
type HeartbeatMessage struct {
	WorkerID     string `json:"worker_id"`
	Host         string `json:"host"`
	PublicIPv4   string `json:"public_ipv4"`
	PublicIPv6   string `json:"public_ipv6"`
	IPv6Ready    bool   `json:"ipv6_ready"`
	Version      string `json:"version"`
	Ready        bool   `json:"ready"`
	Cloud        string `json:"cloud"`
	GenerationID string `json:"generation_id"`
	Timestamp    int64  `json:"timestamp"`
}

// WorkerInfo holds metadata about a single worker VM.
type WorkerInfo struct {
	ID            string    // unique worker identifier (e.g. "heph-worker-0")
	Host          string    // private IP or hostname
	PublicIPv4    string    // public IPv4 address
	PublicIPv6    string    // public IPv6 address
	IPv6Ready     bool      // true if IPv6 is validated from inside the container
	Version       string    // container image version
	Ready         bool      // true if the worker is ready to accept tasks
	Healthy       bool      // true if heartbeat is recent
	RegisteredAt  time.Time // when the worker first registered
	LastHeartbeat time.Time // last heartbeat timestamp
}

// FleetState is a snapshot of the fleet at a point in time.
type FleetState struct {
	DesiredWorkers int                    // expected from Terraform/config
	Workers        map[string]*WorkerInfo // keyed by WorkerInfo.ID
	ControllerIP   string
	GenerationID   string // ownership/generation marker
	Cloud          string // provider name ("hetzner")
}

// FleetSummary is a concise view of fleet health for CLI/TUI display.
type FleetSummary struct {
	DesiredWorkers  int
	RegisteredCount int
	HealthyCount    int
	ReadyCount      int
	IPv6ReadyCount  int
	UniqueIPv4Count int
}

// Summarize returns a FleetSummary from the current state.
func (s *FleetState) Summarize() FleetSummary {
	seen := make(map[string]struct{})
	summary := FleetSummary{
		DesiredWorkers:  s.DesiredWorkers,
		RegisteredCount: len(s.Workers),
	}
	for _, w := range s.Workers {
		if w.Healthy {
			summary.HealthyCount++
		}
		if w.Ready {
			summary.ReadyCount++
		}
		if w.IPv6Ready {
			summary.IPv6ReadyCount++
		}
		if w.PublicIPv4 != "" {
			if _, dup := seen[w.PublicIPv4]; !dup {
				seen[w.PublicIPv4] = struct{}{}
				summary.UniqueIPv4Count++
			}
		}
	}
	return summary
}

// Manager is the interface for fleet lifecycle operations.
type Manager interface {
	// Reconcile compares desired state against actual registered workers.
	// Returns the current fleet state.
	Reconcile(ctx context.Context) (*FleetState, error)

	// WaitForWorkers blocks until at least minReady workers are ready,
	// or the context is cancelled. Returns the fleet state at that point.
	WaitForWorkers(ctx context.Context, minReady int) (*FleetState, error)

	// Close stops the manager and releases resources.
	Close() error
}
