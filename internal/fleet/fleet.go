package fleet

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"heph4estus/internal/logger"

	"github.com/nats-io/nats.go"
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
	ID               string    // unique worker identifier (e.g. "heph-worker-0")
	Host             string    // private IP or hostname
	PublicIPv4       string    // public IPv4 address
	PublicIPv6       string    // public IPv6 address
	IPv6Ready        bool      // true if IPv6 is validated from inside the container
	Version          string    // container image version
	Ready            bool      // true if the worker is ready to accept tasks
	Healthy          bool      // true if heartbeat is recent
	Eligible         bool      // true if admitted by placement/rollout policy
	ExcludedReason   string    // why the worker was excluded from the admitted fleet
	QuarantinedUntil time.Time // cooldown deadline for unhealthy workers
	RegisteredAt     time.Time // when the worker first registered
	LastHeartbeat    time.Time // last heartbeat timestamp
}

// FleetState is a snapshot of the fleet at a point in time.
type FleetState struct {
	DesiredWorkers  int                    // expected from Terraform/config
	Workers         map[string]*WorkerInfo // keyed by WorkerInfo.ID
	ControllerIP    string
	GenerationID    string // ownership/generation marker
	Cloud           string // provider name ("hetzner")
	Placement       PlacementPolicy
	ExpectedVersion string
}

// FleetSummary is a concise view of fleet health for CLI/TUI display.
type FleetSummary struct {
	DesiredWorkers          int
	RegisteredCount         int
	HealthyCount            int
	ReadyCount              int
	EligibleCount           int
	ExcludedCount           int
	QuarantinedCount        int
	IPv6ReadyCount          int
	UniqueIPv4Count         int
	UniqueEligibleIPv4Count int
	MismatchedVersionCount  int
	ExcludedByReason        map[string]int
	VersionCounts           map[string]int
}

// Summarize returns a FleetSummary from the current state.
func (s *FleetState) Summarize() FleetSummary {
	seen := make(map[string]struct{})
	eligibleSeen := make(map[string]struct{})
	summary := FleetSummary{
		DesiredWorkers:   s.DesiredWorkers,
		RegisteredCount:  len(s.Workers),
		ExcludedByReason: make(map[string]int),
		VersionCounts:    make(map[string]int),
	}
	now := time.Now()
	for _, w := range s.Workers {
		version := w.Version
		if version == "" {
			version = "unknown"
		}
		summary.VersionCounts[version]++
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
		if w.Eligible {
			summary.EligibleCount++
			if w.PublicIPv4 != "" {
				if _, dup := eligibleSeen[w.PublicIPv4]; !dup {
					eligibleSeen[w.PublicIPv4] = struct{}{}
					summary.UniqueEligibleIPv4Count++
				}
			}
			continue
		}
		summary.ExcludedCount++
		if !w.QuarantinedUntil.IsZero() && w.QuarantinedUntil.After(now) {
			summary.QuarantinedCount++
		}
		if w.ExcludedReason != "" {
			summary.ExcludedByReason[w.ExcludedReason]++
			if w.ExcludedReason == string(ExclusionReasonVersionMismatch) || w.ExcludedReason == string(ExclusionReasonVersionUnknown) {
				summary.MismatchedVersionCount++
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

// snapshotCollectDuration is how long QueryFleetSnapshot listens for heartbeats
// before returning the collected state.
const snapshotCollectDuration = 3 * time.Second

// QueryFleetSnapshot connects to NATS, collects worker heartbeats for a short
// window, and returns a point-in-time FleetState. Unlike the long-running
// NATSFleetManager, this is designed for one-shot status queries where the
// caller does not need a persistent subscription.
func QueryFleetSnapshot(ctx context.Context, cfg NATSFleetManagerConfig, log logger.Logger) (*FleetState, error) {
	if cfg.NATSURL == "" {
		return nil, fmt.Errorf("fleet: NATS URL is required for snapshot query")
	}
	if log == nil {
		return nil, fmt.Errorf("fleet: logger is required")
	}

	nc, err := nats.Connect(cfg.NATSURL, nats.Timeout(5*time.Second))
	if err != nil {
		return nil, fmt.Errorf("fleet: snapshot connect: %w", err)
	}
	defer nc.Close()

	ht := cfg.HealthTimeout
	if ht == 0 {
		ht = DefaultHealthTimeout
	}

	workers := make(map[string]*WorkerInfo)
	sub, err := nc.Subscribe(HeartbeatSubject, func(msg *nats.Msg) {
		var hb HeartbeatMessage
		if err := json.Unmarshal(msg.Data, &hb); err != nil || hb.WorkerID == "" {
			return
		}
		if cfg.Cloud != "" && hb.Cloud != cfg.Cloud {
			return
		}
		if cfg.GenerationID != "" && hb.GenerationID != cfg.GenerationID {
			return
		}
		now := time.Now()
		w, exists := workers[hb.WorkerID]
		if !exists {
			w = &WorkerInfo{
				ID:           hb.WorkerID,
				RegisteredAt: now,
			}
			workers[hb.WorkerID] = w
		}
		w.Host = hb.Host
		w.PublicIPv4 = hb.PublicIPv4
		w.PublicIPv6 = hb.PublicIPv6
		w.IPv6Ready = hb.IPv6Ready
		w.Version = hb.Version
		w.Ready = hb.Ready
		w.LastHeartbeat = now
		w.Healthy = true
	})
	if err != nil {
		return nil, fmt.Errorf("fleet: snapshot subscribe: %w", err)
	}
	defer func() { _ = sub.Unsubscribe() }()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(snapshotCollectDuration):
	}

	// Mark stale workers as unhealthy.
	now := time.Now()
	for _, w := range workers {
		if now.Sub(w.LastHeartbeat) > ht {
			w.Healthy = false
			w.Ready = false
		}
	}
	state := &FleetState{
		DesiredWorkers:  cfg.DesiredWorkers,
		Workers:         workers,
		ControllerIP:    cfg.ControllerIP,
		GenerationID:    cfg.GenerationID,
		Cloud:           cfg.Cloud,
		Placement:       cfg.Placement.Normalize(cfg.DesiredWorkers),
		ExpectedVersion: cfg.ExpectedVersion,
	}
	applyAdmissionPolicy(state)
	return state, nil
}

func applyAdmissionPolicy(state *FleetState) {
	if state == nil {
		return
	}
	state.Placement = state.Placement.Normalize(state.DesiredWorkers)
	if len(state.Workers) == 0 {
		return
	}

	ids := make([]string, 0, len(state.Workers))
	for id := range state.Workers {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	perIdentity := make(map[string]int)
	now := time.Now()
	for _, id := range ids {
		w := state.Workers[id]
		w.Eligible = false
		w.ExcludedReason = ""

		if !w.Healthy {
			w.ExcludedReason = string(ExclusionReasonUnhealthy)
			continue
		}
		if !w.Ready {
			w.ExcludedReason = string(ExclusionReasonNotReady)
			continue
		}
		if !w.QuarantinedUntil.IsZero() && w.QuarantinedUntil.After(now) {
			w.ExcludedReason = string(ExclusionReasonQuarantinedUnhealthy)
			continue
		}
		if state.ExpectedVersion != "" {
			if w.Version == "" {
				w.ExcludedReason = string(ExclusionReasonVersionUnknown)
				continue
			}
			if w.Version != state.ExpectedVersion {
				w.ExcludedReason = string(ExclusionReasonVersionMismatch)
				continue
			}
		}
		if state.Placement.DualStackRequired {
			if w.PublicIPv4 == "" || w.PublicIPv6 == "" || !w.IPv6Ready {
				w.ExcludedReason = string(ExclusionReasonDualStackRequired)
				continue
			}
		} else if state.Placement.IPv6Required {
			if w.PublicIPv6 == "" || !w.IPv6Ready {
				w.ExcludedReason = string(ExclusionReasonIPv6NotReady)
				continue
			}
		}
		if limit := state.Placement.MaxWorkersPerHost; limit > 0 {
			identity := workerPlacementIdentity(w)
			if identity != "" {
				if perIdentity[identity] >= limit {
					w.ExcludedReason = string(ExclusionReasonPlacementLimit)
					continue
				}
				perIdentity[identity]++
			}
		}

		w.Eligible = true
	}
}

func workerPlacementIdentity(w *WorkerInfo) string {
	switch {
	case w.PublicIPv4 != "":
		return w.PublicIPv4
	case w.Host != "":
		return w.Host
	default:
		return w.ID
	}
}
