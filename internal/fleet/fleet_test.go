package fleet

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"heph4estus/internal/logger"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

func startEmbeddedNATS(t *testing.T) *natsserver.Server {
	t.Helper()
	opts := &natsserver.Options{
		Host:     "127.0.0.1",
		Port:     -1,
		HTTPHost: "127.0.0.1",
		HTTPPort: -1,
		NoSigs:   true,
		NoLog:    true,
	}
	srv, err := natsserver.NewServer(opts)
	if err != nil {
		t.Fatalf("embedded nats: %v", err)
	}
	srv.Start()
	if !srv.ReadyForConnections(5 * time.Second) {
		t.Fatal("embedded nats not ready")
	}
	t.Cleanup(srv.Shutdown)
	return srv
}

func TestHeartbeatMessage_Roundtrip(t *testing.T) {
	orig := HeartbeatMessage{
		WorkerID:   "heph-worker-0",
		Host:       "10.0.1.5",
		PublicIPv4: "203.0.113.10",
		PublicIPv6: "2001:db8::1",
		IPv6Ready:  true,
		Version:    "v0.6.3",
		Ready:      true,
		Timestamp:  1713200000,
	}

	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded HeartbeatMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded != orig {
		t.Fatalf("roundtrip mismatch:\n  got:  %+v\n  want: %+v", decoded, orig)
	}
}

func TestHeartbeatMessage_PartialFields(t *testing.T) {
	// Workers may omit optional fields (IPv6, version).
	raw := `{"worker_id":"w-1","host":"10.0.0.1","public_ipv4":"1.2.3.4","ready":true,"timestamp":1}`
	var hb HeartbeatMessage
	if err := json.Unmarshal([]byte(raw), &hb); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if hb.WorkerID != "w-1" {
		t.Fatalf("worker_id = %q, want %q", hb.WorkerID, "w-1")
	}
	if hb.PublicIPv6 != "" {
		t.Fatalf("expected empty PublicIPv6, got %q", hb.PublicIPv6)
	}
	if hb.IPv6Ready {
		t.Fatal("expected IPv6Ready false for missing field")
	}
}

func TestFleetState_Summarize(t *testing.T) {
	state := &FleetState{
		DesiredWorkers: 5,
		Workers: map[string]*WorkerInfo{
			"w-0": {
				ID: "w-0", PublicIPv4: "1.1.1.1",
				Healthy: true, Ready: true, IPv6Ready: true,
			},
			"w-1": {
				ID: "w-1", PublicIPv4: "2.2.2.2",
				Healthy: true, Ready: true, IPv6Ready: false,
			},
			"w-2": {
				ID: "w-2", PublicIPv4: "3.3.3.3",
				Healthy: false, Ready: false, IPv6Ready: false,
			},
			"w-3": {
				ID: "w-3", PublicIPv4: "1.1.1.1", // duplicate IP
				Healthy: true, Ready: false, IPv6Ready: true,
			},
		},
		ControllerIP: "10.0.0.1",
		GenerationID: "gen-abc",
		Cloud:        "hetzner",
	}

	s := state.Summarize()

	assertInt(t, "DesiredWorkers", s.DesiredWorkers, 5)
	assertInt(t, "RegisteredCount", s.RegisteredCount, 4)
	assertInt(t, "HealthyCount", s.HealthyCount, 3)
	assertInt(t, "ReadyCount", s.ReadyCount, 2)
	assertInt(t, "IPv6ReadyCount", s.IPv6ReadyCount, 2)
	assertInt(t, "UniqueIPv4Count", s.UniqueIPv4Count, 3) // 1.1.1.1 counted once
}

func TestFleetState_Summarize_Empty(t *testing.T) {
	state := &FleetState{
		DesiredWorkers: 3,
		Workers:        map[string]*WorkerInfo{},
	}
	s := state.Summarize()

	assertInt(t, "DesiredWorkers", s.DesiredWorkers, 3)
	assertInt(t, "RegisteredCount", s.RegisteredCount, 0)
	assertInt(t, "HealthyCount", s.HealthyCount, 0)
	assertInt(t, "ReadyCount", s.ReadyCount, 0)
	assertInt(t, "UniqueIPv4Count", s.UniqueIPv4Count, 0)
}

func TestNATSFleetManager_Heartbeat(t *testing.T) {
	srv := startEmbeddedNATS(t)

	nc, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("nats connect: %v", err)
	}
	t.Cleanup(nc.Close)

	mgr, err := NewNATSFleetManagerFromConn(nc, NATSFleetManagerConfig{
		DesiredWorkers: 2,
		ControllerIP:   "10.0.0.1",
		GenerationID:   "gen-1",
		Cloud:          "hetzner",
		HealthTimeout:  10 * time.Second,
	}, logger.NewSimpleLogger())
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	defer mgr.Close()

	// Publish a heartbeat from a separate connection to simulate a worker.
	pub, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("pub connect: %v", err)
	}
	defer pub.Close()

	hb := HeartbeatMessage{
		WorkerID:     "heph-worker-0",
		Host:         "10.0.1.5",
		PublicIPv4:   "203.0.113.10",
		PublicIPv6:   "2001:db8::1",
		IPv6Ready:    true,
		Version:      "v0.6.3",
		Ready:        true,
		Cloud:        "hetzner",
		GenerationID: "gen-1",
		Timestamp:    time.Now().Unix(),
	}
	data, _ := json.Marshal(hb)
	if err := pub.Publish(HeartbeatSubject, data); err != nil {
		t.Fatalf("publish heartbeat: %v", err)
	}
	pub.Flush()

	// Give the subscription handler time to fire.
	time.Sleep(200 * time.Millisecond)

	state, err := mgr.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	if len(state.Workers) != 1 {
		t.Fatalf("expected 1 worker, got %d", len(state.Workers))
	}
	w, ok := state.Workers["heph-worker-0"]
	if !ok {
		t.Fatal("worker heph-worker-0 not found")
	}
	if w.PublicIPv4 != "203.0.113.10" {
		t.Fatalf("PublicIPv4 = %q, want %q", w.PublicIPv4, "203.0.113.10")
	}
	if !w.Healthy {
		t.Fatal("expected worker to be healthy")
	}
	if !w.Ready {
		t.Fatal("expected worker to be ready")
	}
	if !w.IPv6Ready {
		t.Fatal("expected worker to have IPv6Ready=true")
	}
	if state.DesiredWorkers != 2 {
		t.Fatalf("DesiredWorkers = %d, want 2", state.DesiredWorkers)
	}
	if state.Cloud != "hetzner" {
		t.Fatalf("Cloud = %q, want %q", state.Cloud, "hetzner")
	}
}

func TestNATSFleetManager_MultipleWorkers(t *testing.T) {
	srv := startEmbeddedNATS(t)

	nc, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("nats connect: %v", err)
	}
	t.Cleanup(nc.Close)

	mgr, err := NewNATSFleetManagerFromConn(nc, NATSFleetManagerConfig{
		DesiredWorkers: 3,
		Cloud:          "hetzner",
		HealthTimeout:  10 * time.Second,
	}, logger.NewSimpleLogger())
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	defer mgr.Close()

	pub, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("pub connect: %v", err)
	}
	defer pub.Close()

	for i := range 3 {
		hb := HeartbeatMessage{
			WorkerID:   fmt.Sprintf("heph-worker-%d", i),
			PublicIPv4: fmt.Sprintf("10.0.0.%d", i+1),
			Ready:      true,
			Cloud:      "hetzner",
			Timestamp:  time.Now().Unix(),
		}
		data, _ := json.Marshal(hb)
		if err := pub.Publish(HeartbeatSubject, data); err != nil {
			t.Fatalf("publish worker %d: %v", i, err)
		}
	}
	pub.Flush()
	time.Sleep(200 * time.Millisecond)

	state, err := mgr.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(state.Workers) != 3 {
		t.Fatalf("expected 3 workers, got %d", len(state.Workers))
	}
	s := state.Summarize()
	assertInt(t, "ReadyCount", s.ReadyCount, 3)
	assertInt(t, "HealthyCount", s.HealthyCount, 3)
}

func TestNATSFleetManager_HeartbeatUpdate(t *testing.T) {
	srv := startEmbeddedNATS(t)

	nc, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("nats connect: %v", err)
	}
	t.Cleanup(nc.Close)

	mgr, err := NewNATSFleetManagerFromConn(nc, NATSFleetManagerConfig{
		DesiredWorkers: 1,
		Cloud:          "hetzner",
		HealthTimeout:  10 * time.Second,
	}, logger.NewSimpleLogger())
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	defer mgr.Close()

	pub, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("pub connect: %v", err)
	}
	defer pub.Close()

	// First heartbeat: not ready yet.
	hb1 := HeartbeatMessage{
		WorkerID:   "heph-worker-0",
		PublicIPv4: "1.2.3.4",
		Ready:      false,
		Version:    "v0.6.2",
		Cloud:      "hetzner",
		Timestamp:  time.Now().Unix(),
	}
	data, _ := json.Marshal(hb1)
	pub.Publish(HeartbeatSubject, data)
	pub.Flush()
	time.Sleep(200 * time.Millisecond)

	state, _ := mgr.Reconcile(context.Background())
	w := state.Workers["heph-worker-0"]
	if w.Ready {
		t.Fatal("expected worker not ready on first heartbeat")
	}
	if w.Version != "v0.6.2" {
		t.Fatalf("Version = %q, want %q", w.Version, "v0.6.2")
	}
	regTime := w.RegisteredAt

	// Second heartbeat: now ready, new version.
	hb2 := HeartbeatMessage{
		WorkerID:   "heph-worker-0",
		PublicIPv4: "1.2.3.4",
		Ready:      true,
		Version:    "v0.6.3",
		Cloud:      "hetzner",
		Timestamp:  time.Now().Unix(),
	}
	data, _ = json.Marshal(hb2)
	pub.Publish(HeartbeatSubject, data)
	pub.Flush()
	time.Sleep(200 * time.Millisecond)

	state, _ = mgr.Reconcile(context.Background())
	w = state.Workers["heph-worker-0"]
	if !w.Ready {
		t.Fatal("expected worker ready after second heartbeat")
	}
	if w.Version != "v0.6.3" {
		t.Fatalf("Version = %q, want %q", w.Version, "v0.6.3")
	}
	// RegisteredAt should NOT change on update.
	if !w.RegisteredAt.Equal(regTime) {
		t.Fatalf("RegisteredAt changed: was %v, now %v", regTime, w.RegisteredAt)
	}
	// Worker count should still be 1 (update, not new registration).
	if len(state.Workers) != 1 {
		t.Fatalf("expected 1 worker after update, got %d", len(state.Workers))
	}
}

func TestWorkerHealthTimeout(t *testing.T) {
	srv := startEmbeddedNATS(t)

	nc, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("nats connect: %v", err)
	}
	t.Cleanup(nc.Close)

	// Use a very short health timeout for testing.
	mgr, err := NewNATSFleetManagerFromConn(nc, NATSFleetManagerConfig{
		DesiredWorkers: 1,
		Cloud:          "hetzner",
		HealthTimeout:  100 * time.Millisecond,
	}, logger.NewSimpleLogger())
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	defer mgr.Close()

	pub, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("pub connect: %v", err)
	}
	defer pub.Close()

	hb := HeartbeatMessage{
		WorkerID:   "heph-worker-0",
		PublicIPv4: "5.6.7.8",
		Ready:      true,
		Cloud:      "hetzner",
		Timestamp:  time.Now().Unix(),
	}
	data, _ := json.Marshal(hb)
	pub.Publish(HeartbeatSubject, data)
	pub.Flush()
	time.Sleep(50 * time.Millisecond)

	// Should be healthy immediately after heartbeat.
	state, err := mgr.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	w := state.Workers["heph-worker-0"]
	if !w.Healthy {
		t.Fatal("expected healthy right after heartbeat")
	}
	if !w.Ready {
		t.Fatal("expected ready right after heartbeat")
	}

	// Wait for the health timeout to expire.
	time.Sleep(200 * time.Millisecond)

	state, err = mgr.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("reconcile after timeout: %v", err)
	}
	w = state.Workers["heph-worker-0"]
	if w.Healthy {
		t.Fatal("expected unhealthy after timeout")
	}
	if w.Ready {
		t.Fatal("expected not ready after health timeout")
	}
}

func TestNATSFleetManager_WaitForWorkers(t *testing.T) {
	srv := startEmbeddedNATS(t)

	nc, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("nats connect: %v", err)
	}
	t.Cleanup(nc.Close)

	mgr, err := NewNATSFleetManagerFromConn(nc, NATSFleetManagerConfig{
		DesiredWorkers: 2,
		Cloud:          "hetzner",
		HealthTimeout:  10 * time.Second,
	}, logger.NewSimpleLogger())
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	defer mgr.Close()

	pub, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("pub connect: %v", err)
	}
	defer pub.Close()

	// Publish 2 ready workers before calling WaitForWorkers.
	for i := range 2 {
		hb := HeartbeatMessage{
			WorkerID:   fmt.Sprintf("heph-worker-%d", i),
			PublicIPv4: fmt.Sprintf("10.0.0.%d", i+1),
			Ready:      true,
			Cloud:      "hetzner",
			Timestamp:  time.Now().Unix(),
		}
		data, _ := json.Marshal(hb)
		pub.Publish(HeartbeatSubject, data)
	}
	pub.Flush()
	time.Sleep(200 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	state, err := mgr.WaitForWorkers(ctx, 2)
	if err != nil {
		t.Fatalf("WaitForWorkers: %v", err)
	}
	s := state.Summarize()
	if s.ReadyCount < 2 {
		t.Fatalf("expected at least 2 ready, got %d", s.ReadyCount)
	}
}

func TestNATSFleetManager_WaitForWorkers_ContextCancel(t *testing.T) {
	srv := startEmbeddedNATS(t)

	nc, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("nats connect: %v", err)
	}
	t.Cleanup(nc.Close)

	mgr, err := NewNATSFleetManagerFromConn(nc, NATSFleetManagerConfig{
		DesiredWorkers: 5,
		Cloud:          "hetzner",
		HealthTimeout:  10 * time.Second,
	}, logger.NewSimpleLogger())
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	defer mgr.Close()

	// No workers published, so WaitForWorkers should block until context cancels.
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	state, err := mgr.WaitForWorkers(ctx, 5)
	if err == nil {
		t.Fatal("expected error from context timeout")
	}
	if state == nil {
		t.Fatal("expected partial state even on timeout")
	}
}

func TestNATSFleetManager_InvalidHeartbeat(t *testing.T) {
	srv := startEmbeddedNATS(t)

	nc, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("nats connect: %v", err)
	}
	t.Cleanup(nc.Close)

	mgr, err := NewNATSFleetManagerFromConn(nc, NATSFleetManagerConfig{
		DesiredWorkers: 1,
		Cloud:          "hetzner",
		HealthTimeout:  10 * time.Second,
	}, logger.NewSimpleLogger())
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	defer mgr.Close()

	pub, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("pub connect: %v", err)
	}
	defer pub.Close()

	// Publish garbage data.
	pub.Publish(HeartbeatSubject, []byte("not json"))
	// Publish valid JSON but missing worker_id.
	pub.Publish(HeartbeatSubject, []byte(`{"ready":true}`))
	pub.Flush()
	time.Sleep(200 * time.Millisecond)

	state, err := mgr.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	// Neither message should register a worker.
	if len(state.Workers) != 0 {
		t.Fatalf("expected 0 workers after invalid heartbeats, got %d", len(state.Workers))
	}
}

func TestNATSFleetManager_IgnoresMismatchedGeneration(t *testing.T) {
	srv := startEmbeddedNATS(t)

	nc, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("nats connect: %v", err)
	}
	t.Cleanup(nc.Close)

	mgr, err := NewNATSFleetManagerFromConn(nc, NATSFleetManagerConfig{
		DesiredWorkers: 1,
		Cloud:          "hetzner",
		GenerationID:   "gen-expected",
		HealthTimeout:  10 * time.Second,
	}, logger.NewSimpleLogger())
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	defer mgr.Close()

	pub, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("pub connect: %v", err)
	}
	defer pub.Close()

	hb := HeartbeatMessage{
		WorkerID:     "heph-worker-0",
		PublicIPv4:   "203.0.113.10",
		Ready:        true,
		Cloud:        "hetzner",
		GenerationID: "gen-other",
		Timestamp:    time.Now().Unix(),
	}
	data, _ := json.Marshal(hb)
	if err := pub.Publish(HeartbeatSubject, data); err != nil {
		t.Fatalf("publish heartbeat: %v", err)
	}
	pub.Flush()
	time.Sleep(200 * time.Millisecond)

	state, err := mgr.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(state.Workers) != 0 {
		t.Fatalf("expected 0 workers after mismatched generation, got %d", len(state.Workers))
	}
}

func TestNATSFleetManager_Close(t *testing.T) {
	srv := startEmbeddedNATS(t)

	nc, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("nats connect: %v", err)
	}

	mgr, err := NewNATSFleetManagerFromConn(nc, NATSFleetManagerConfig{
		DesiredWorkers: 1,
		Cloud:          "hetzner",
	}, logger.NewSimpleLogger())
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	if err := mgr.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Reconcile after close should error.
	_, err = mgr.Reconcile(context.Background())
	if err == nil {
		t.Fatal("expected error from Reconcile after Close")
	}

	// Double close should not panic.
	if err := mgr.Close(); err != nil {
		t.Fatalf("double close: %v", err)
	}
}

func TestNewNATSFleetManager_Validation(t *testing.T) {
	// Missing logger.
	_, err := NewNATSFleetManager(NATSFleetManagerConfig{NATSURL: "nats://localhost:4222"}, nil)
	if err == nil {
		t.Fatal("expected error for nil logger")
	}

	// Missing NATS URL.
	_, err = NewNATSFleetManager(NATSFleetManagerConfig{}, logger.NewSimpleLogger())
	if err == nil {
		t.Fatal("expected error for empty NATS URL")
	}
}

// assertInt is a small test helper.
func assertInt(t *testing.T, name string, got, want int) {
	t.Helper()
	if got != want {
		t.Fatalf("%s = %d, want %d", name, got, want)
	}
}
