package fleet

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"heph4estus/internal/logger"

	"github.com/nats-io/nats.go"
)

// waitPollInterval is how often WaitForWorkers re-checks readiness.
const waitPollInterval = 5 * time.Second

// NATSFleetManagerConfig configures a NATSFleetManager.
type NATSFleetManagerConfig struct {
	NATSURL        string
	DesiredWorkers int
	ControllerIP   string
	GenerationID   string
	Cloud          string
	HealthTimeout  time.Duration // 0 means DefaultHealthTimeout
}

// NATSFleetManager implements Manager by subscribing to NATS heartbeats.
type NATSFleetManager struct {
	natsURL        string
	desiredWorkers int
	controllerIP   string
	generationID   string
	cloud          string
	healthTimeout  time.Duration

	mu      sync.RWMutex
	workers map[string]*WorkerInfo

	conn   *nats.Conn
	sub    *nats.Subscription
	log    logger.Logger
	closed chan struct{}
}

// Compile-time interface check.
var _ Manager = (*NATSFleetManager)(nil)

// NewNATSFleetManager connects to NATS and subscribes to worker heartbeats.
func NewNATSFleetManager(cfg NATSFleetManagerConfig, log logger.Logger) (*NATSFleetManager, error) {
	if log == nil {
		return nil, fmt.Errorf("fleet: logger is required")
	}
	if cfg.NATSURL == "" {
		return nil, fmt.Errorf("fleet: NATS URL is required")
	}

	ht := cfg.HealthTimeout
	if ht == 0 {
		ht = DefaultHealthTimeout
	}

	nc, err := nats.Connect(cfg.NATSURL)
	if err != nil {
		return nil, fmt.Errorf("fleet: nats connect: %w", err)
	}

	m := &NATSFleetManager{
		natsURL:        cfg.NATSURL,
		desiredWorkers: cfg.DesiredWorkers,
		controllerIP:   cfg.ControllerIP,
		generationID:   cfg.GenerationID,
		cloud:          cfg.Cloud,
		healthTimeout:  ht,
		workers:        make(map[string]*WorkerInfo),
		conn:           nc,
		log:            log,
		closed:         make(chan struct{}),
	}

	sub, err := nc.Subscribe(HeartbeatSubject, m.handleHeartbeat)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("fleet: subscribe heartbeat: %w", err)
	}
	m.sub = sub

	log.Info("Fleet manager subscribed to %s on %s", HeartbeatSubject, cfg.NATSURL)
	return m, nil
}

// NewNATSFleetManagerFromConn wraps an existing NATS connection. Used by
// tests that spin up an embedded server and want to share the connection.
func NewNATSFleetManagerFromConn(nc *nats.Conn, cfg NATSFleetManagerConfig, log logger.Logger) (*NATSFleetManager, error) {
	if log == nil {
		return nil, fmt.Errorf("fleet: logger is required")
	}

	ht := cfg.HealthTimeout
	if ht == 0 {
		ht = DefaultHealthTimeout
	}

	m := &NATSFleetManager{
		natsURL:        cfg.NATSURL,
		desiredWorkers: cfg.DesiredWorkers,
		controllerIP:   cfg.ControllerIP,
		generationID:   cfg.GenerationID,
		cloud:          cfg.Cloud,
		healthTimeout:  ht,
		workers:        make(map[string]*WorkerInfo),
		conn:           nc,
		log:            log,
		closed:         make(chan struct{}),
	}

	sub, err := nc.Subscribe(HeartbeatSubject, m.handleHeartbeat)
	if err != nil {
		return nil, fmt.Errorf("fleet: subscribe heartbeat: %w", err)
	}
	m.sub = sub

	log.Info("Fleet manager subscribed to %s (shared conn)", HeartbeatSubject)
	return m, nil
}

// handleHeartbeat processes a single heartbeat message from a worker.
func (m *NATSFleetManager) handleHeartbeat(msg *nats.Msg) {
	var hb HeartbeatMessage
	if err := json.Unmarshal(msg.Data, &hb); err != nil {
		m.log.Error("Fleet: invalid heartbeat JSON: %v", err)
		return
	}
	if hb.WorkerID == "" {
		m.log.Error("Fleet: heartbeat missing worker_id, ignoring")
		return
	}
	if m.cloud != "" && hb.Cloud != m.cloud {
		m.log.Info("Fleet: ignoring worker %s from cloud %q (want %q)", hb.WorkerID, hb.Cloud, m.cloud)
		return
	}
	if m.generationID != "" && hb.GenerationID != m.generationID {
		m.log.Info("Fleet: ignoring worker %s from generation %q (want %q)", hb.WorkerID, hb.GenerationID, m.generationID)
		return
	}

	now := time.Now()

	m.mu.Lock()
	defer m.mu.Unlock()

	w, exists := m.workers[hb.WorkerID]
	if !exists {
		w = &WorkerInfo{
			ID:           hb.WorkerID,
			RegisteredAt: now,
		}
		m.workers[hb.WorkerID] = w
		m.log.Info("Fleet: new worker registered: %s (%s)", hb.WorkerID, hb.PublicIPv4)
	}

	w.Host = hb.Host
	w.PublicIPv4 = hb.PublicIPv4
	w.PublicIPv6 = hb.PublicIPv6
	w.IPv6Ready = hb.IPv6Ready
	w.Version = hb.Version
	w.Ready = hb.Ready
	w.LastHeartbeat = now
	w.Healthy = true
}

// Reconcile snapshots the current fleet state, marking workers whose last
// heartbeat exceeds the health timeout as unhealthy.
func (m *NATSFleetManager) Reconcile(ctx context.Context) (*FleetState, error) {
	select {
	case <-m.closed:
		return nil, fmt.Errorf("fleet: manager is closed")
	default:
	}

	now := time.Now()

	m.mu.Lock()
	defer m.mu.Unlock()

	snapshot := make(map[string]*WorkerInfo, len(m.workers))
	for id, w := range m.workers {
		// Mark unhealthy if heartbeat is stale.
		if now.Sub(w.LastHeartbeat) > m.healthTimeout {
			w.Healthy = false
			w.Ready = false
		}
		cp := *w
		snapshot[id] = &cp
	}

	return &FleetState{
		DesiredWorkers: m.desiredWorkers,
		Workers:        snapshot,
		ControllerIP:   m.controllerIP,
		GenerationID:   m.generationID,
		Cloud:          m.cloud,
	}, nil
}

// WaitForWorkers polls Reconcile until at least minReady workers report
// ready, or until the context is cancelled.
func (m *NATSFleetManager) WaitForWorkers(ctx context.Context, minReady int) (*FleetState, error) {
	for {
		state, err := m.Reconcile(ctx)
		if err != nil {
			return nil, err
		}

		readyCount := 0
		for _, w := range state.Workers {
			if w.Ready && w.Healthy {
				readyCount++
			}
		}

		if readyCount >= minReady {
			m.log.Info("Fleet: %d/%d workers ready (desired %d)", readyCount, len(state.Workers), m.desiredWorkers)
			return state, nil
		}

		m.log.Info("Fleet: waiting for workers — %d/%d ready, need %d", readyCount, len(state.Workers), minReady)

		select {
		case <-ctx.Done():
			return state, ctx.Err()
		case <-m.closed:
			return state, fmt.Errorf("fleet: manager closed while waiting")
		case <-time.After(waitPollInterval):
		}
	}
}

// Close unsubscribes from heartbeats and closes the NATS connection.
func (m *NATSFleetManager) Close() error {
	select {
	case <-m.closed:
		return nil // already closed
	default:
	}
	close(m.closed)

	if m.sub != nil {
		if err := m.sub.Unsubscribe(); err != nil {
			m.log.Error("Fleet: unsubscribe error: %v", err)
		}
	}
	if m.conn != nil {
		m.conn.Close()
	}
	m.log.Info("Fleet manager closed")
	return nil
}
