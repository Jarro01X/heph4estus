package fleet

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"heph4estus/internal/fleetstate"
	"heph4estus/internal/logger"
	"heph4estus/internal/testutil/natstest"
)

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

func TestApplyAdmissionPolicy_ReputationExcludesWorker(t *testing.T) {
	state := &FleetState{
		DesiredWorkers: 1,
		Cloud:          "hetzner",
		Workers: map[string]*WorkerInfo{
			"heph-worker-0": {
				ID:         "heph-worker-0",
				PublicIPv4: "203.0.113.10",
				Healthy:    true,
				Ready:      true,
			},
		},
		Reputation: []fleetstate.ReputationRecord{{
			Cloud:         "hetzner",
			PublicIPv4:    "203.0.113.10",
			State:         fleetstate.ReputationStateQuarantined,
			Reason:        "burned",
			CooldownUntil: time.Now().Add(time.Hour),
		}},
	}

	applyAdmissionPolicy(state)
	worker := state.Workers["heph-worker-0"]
	if worker.Eligible {
		t.Fatal("expected worker to be excluded")
	}
	if worker.ExcludedReason != "reputation_quarantined" {
		t.Fatalf("ExcludedReason = %q, want reputation_quarantined", worker.ExcludedReason)
	}
	if worker.ReputationReason != "burned" {
		t.Fatalf("ReputationReason = %q, want burned", worker.ReputationReason)
	}
}

func TestApplyAdmissionPolicy_RolloutCanaryRestrictsAdmission(t *testing.T) {
	state := &FleetState{
		DesiredWorkers:  2,
		ExpectedVersion: "registry/heph-httpx:2",
		Rollout: &fleetstate.RolloutRecord{
			Phase:           fleetstate.RolloutPhaseCanary,
			TargetVersion:   "registry/heph-httpx:2",
			CanaryWorkerIDs: []string{"heph-worker-1"},
		},
		Workers: map[string]*WorkerInfo{
			"heph-worker-0": {
				ID:      "heph-worker-0",
				Version: "registry/heph-httpx:2",
				Healthy: true,
				Ready:   true,
			},
			"heph-worker-1": {
				ID:      "heph-worker-1",
				Version: "registry/heph-httpx:2",
				Healthy: true,
				Ready:   true,
			},
		},
	}

	applyAdmissionPolicy(state)
	if !state.Workers["heph-worker-1"].Eligible {
		t.Fatal("expected canary worker to be eligible")
	}
	if state.Workers["heph-worker-0"].Eligible {
		t.Fatal("expected non-canary worker to be excluded")
	}
	if state.Workers["heph-worker-0"].ExcludedReason != "not_canary" {
		t.Fatalf("ExcludedReason = %q, want not_canary", state.Workers["heph-worker-0"].ExcludedReason)
	}
}

func TestNATSFleetManager_Heartbeat(t *testing.T) {
	srv := natstest.Start(t, natstest.Options{})

	nc := natstest.Connect(t, srv)
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
	defer func() { _ = mgr.Close() }()

	// Publish a heartbeat from a separate connection to simulate a worker.
	pub := natstest.Connect(t, srv)
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
	_ = pub.Flush()

	state := awaitWorkers(t, mgr, 1)

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
	srv := natstest.Start(t, natstest.Options{})

	nc := natstest.Connect(t, srv)
	t.Cleanup(nc.Close)

	mgr, err := NewNATSFleetManagerFromConn(nc, NATSFleetManagerConfig{
		DesiredWorkers: 3,
		Cloud:          "hetzner",
		HealthTimeout:  10 * time.Second,
	}, logger.NewSimpleLogger())
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	defer func() { _ = mgr.Close() }()

	pub := natstest.Connect(t, srv)
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
	_ = pub.Flush()

	state := awaitWorkers(t, mgr, 3)
	s := state.Summarize()
	assertInt(t, "ReadyCount", s.ReadyCount, 3)
	assertInt(t, "HealthyCount", s.HealthyCount, 3)
}

func TestNATSFleetManager_HeartbeatUpdate(t *testing.T) {
	srv := natstest.Start(t, natstest.Options{})

	nc := natstest.Connect(t, srv)
	t.Cleanup(nc.Close)

	mgr, err := NewNATSFleetManagerFromConn(nc, NATSFleetManagerConfig{
		DesiredWorkers: 1,
		Cloud:          "hetzner",
		HealthTimeout:  10 * time.Second,
	}, logger.NewSimpleLogger())
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	defer func() { _ = mgr.Close() }()

	pub := natstest.Connect(t, srv)
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
	_ = pub.Publish(HeartbeatSubject, data)
	_ = pub.Flush()

	state := awaitWorkers(t, mgr, 1)
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
	_ = pub.Publish(HeartbeatSubject, data)
	_ = pub.Flush()

	// Poll until the version update lands (worker already exists so
	// awaitWorkers won't help — poll for the field change instead).
	deadline := time.Now().Add(2 * time.Second)
	for {
		state, _ = mgr.Reconcile(context.Background())
		if state.Workers["heph-worker-0"] != nil && state.Workers["heph-worker-0"].Version == "v0.6.3" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for heartbeat update")
		}
		time.Sleep(10 * time.Millisecond)
	}
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
	srv := natstest.Start(t, natstest.Options{})

	nc := natstest.Connect(t, srv)
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
	defer func() { _ = mgr.Close() }()

	pub := natstest.Connect(t, srv)
	defer pub.Close()

	hb := HeartbeatMessage{
		WorkerID:   "heph-worker-0",
		PublicIPv4: "5.6.7.8",
		Ready:      true,
		Cloud:      "hetzner",
		Timestamp:  time.Now().Unix(),
	}
	data, _ := json.Marshal(hb)
	_ = pub.Publish(HeartbeatSubject, data)
	_ = pub.Flush()

	// Should be healthy immediately after heartbeat.
	state := awaitWorkers(t, mgr, 1)
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
	srv := natstest.Start(t, natstest.Options{})

	nc := natstest.Connect(t, srv)
	t.Cleanup(nc.Close)

	mgr, err := NewNATSFleetManagerFromConn(nc, NATSFleetManagerConfig{
		DesiredWorkers: 2,
		Cloud:          "hetzner",
		HealthTimeout:  10 * time.Second,
	}, logger.NewSimpleLogger())
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	defer func() { _ = mgr.Close() }()

	pub := natstest.Connect(t, srv)
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
		_ = pub.Publish(HeartbeatSubject, data)
	}
	_ = pub.Flush()

	// Ensure heartbeats are processed before calling WaitForWorkers.
	awaitWorkers(t, mgr, 2)

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

func TestNATSFleetManager_WaitForWorkers_DiversityPolicy(t *testing.T) {
	srv := natstest.Start(t, natstest.Options{})

	nc := natstest.Connect(t, srv)
	t.Cleanup(nc.Close)

	mgr, err := NewNATSFleetManagerFromConn(nc, NATSFleetManagerConfig{
		DesiredWorkers: 2,
		Cloud:          "hetzner",
		Placement: PlacementPolicy{
			Mode:              PlacementModeDiversity,
			MaxWorkersPerHost: 1,
		},
		HealthTimeout: 10 * time.Second,
	}, logger.NewSimpleLogger())
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	defer func() { _ = mgr.Close() }()

	pub := natstest.Connect(t, srv)
	defer pub.Close()

	heartbeats := []HeartbeatMessage{
		{WorkerID: "w-0", PublicIPv4: "203.0.113.10", Ready: true, Cloud: "hetzner", Timestamp: time.Now().Unix()},
		{WorkerID: "w-1", PublicIPv4: "203.0.113.10", Ready: true, Cloud: "hetzner", Timestamp: time.Now().Unix()},
	}
	for _, hb := range heartbeats {
		data, _ := json.Marshal(hb)
		_ = pub.Publish(HeartbeatSubject, data)
	}
	_ = pub.Flush()

	state := awaitWorkers(t, mgr, 2)
	summary := state.Summarize()
	if summary.EligibleCount != 1 {
		t.Fatalf("EligibleCount = %d, want 1", summary.EligibleCount)
	}
	if summary.ExcludedByReason[string(ExclusionReasonPlacementLimit)] != 1 {
		t.Fatalf("placement exclusions = %d, want 1", summary.ExcludedByReason[string(ExclusionReasonPlacementLimit)])
	}
}

func TestNATSFleetManager_WaitForWorkers_ExpectedVersion(t *testing.T) {
	srv := natstest.Start(t, natstest.Options{})

	nc := natstest.Connect(t, srv)
	t.Cleanup(nc.Close)

	mgr, err := NewNATSFleetManagerFromConn(nc, NATSFleetManagerConfig{
		DesiredWorkers:  2,
		Cloud:           "hetzner",
		ExpectedVersion: "heph-httpx-worker:latest",
		HealthTimeout:   10 * time.Second,
	}, logger.NewSimpleLogger())
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	defer func() { _ = mgr.Close() }()

	pub := natstest.Connect(t, srv)
	defer pub.Close()

	heartbeats := []HeartbeatMessage{
		{WorkerID: "w-0", PublicIPv4: "203.0.113.10", Ready: true, Version: "heph-httpx-worker:latest", Cloud: "hetzner", Timestamp: time.Now().Unix()},
		{WorkerID: "w-1", PublicIPv4: "203.0.113.11", Ready: true, Version: "heph-httpx-worker:previous", Cloud: "hetzner", Timestamp: time.Now().Unix()},
	}
	for _, hb := range heartbeats {
		data, _ := json.Marshal(hb)
		_ = pub.Publish(HeartbeatSubject, data)
	}
	_ = pub.Flush()

	state := awaitWorkers(t, mgr, 2)
	summary := state.Summarize()
	if summary.EligibleCount != 1 {
		t.Fatalf("EligibleCount = %d, want 1", summary.EligibleCount)
	}
	if summary.ExcludedByReason[string(ExclusionReasonVersionMismatch)] != 1 {
		t.Fatalf("version mismatch exclusions = %d, want 1", summary.ExcludedByReason[string(ExclusionReasonVersionMismatch)])
	}
}

func TestFleetState_Summarize_IPv6Policy(t *testing.T) {
	state := &FleetState{
		DesiredWorkers: 2,
		Placement: PlacementPolicy{
			Mode:         PlacementModeDiversity,
			IPv6Required: true,
		},
		Workers: map[string]*WorkerInfo{
			"w-0": {ID: "w-0", PublicIPv4: "1.1.1.1", PublicIPv6: "2001:db8::1", IPv6Ready: true, Ready: true, Healthy: true},
			"w-1": {ID: "w-1", PublicIPv4: "2.2.2.2", Ready: true, Healthy: true},
		},
	}
	applyAdmissionPolicy(state)
	summary := state.Summarize()
	if summary.EligibleCount != 1 {
		t.Fatalf("EligibleCount = %d, want 1", summary.EligibleCount)
	}
	if summary.ExcludedByReason[string(ExclusionReasonIPv6NotReady)] != 1 {
		t.Fatalf("ipv6 exclusions = %d, want 1", summary.ExcludedByReason[string(ExclusionReasonIPv6NotReady)])
	}
}

func TestNATSFleetManager_WaitForWorkers_ContextCancel(t *testing.T) {
	srv := natstest.Start(t, natstest.Options{})

	nc := natstest.Connect(t, srv)
	t.Cleanup(nc.Close)

	mgr, err := NewNATSFleetManagerFromConn(nc, NATSFleetManagerConfig{
		DesiredWorkers: 5,
		Cloud:          "hetzner",
		HealthTimeout:  10 * time.Second,
	}, logger.NewSimpleLogger())
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	defer func() { _ = mgr.Close() }()

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
	srv := natstest.Start(t, natstest.Options{})

	nc := natstest.Connect(t, srv)
	t.Cleanup(nc.Close)

	mgr, err := NewNATSFleetManagerFromConn(nc, NATSFleetManagerConfig{
		DesiredWorkers: 1,
		Cloud:          "hetzner",
		HealthTimeout:  10 * time.Second,
	}, logger.NewSimpleLogger())
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	defer func() { _ = mgr.Close() }()

	pub := natstest.Connect(t, srv)
	defer pub.Close()

	// Publish garbage data.
	_ = pub.Publish(HeartbeatSubject, []byte("not json"))
	// Publish valid JSON but missing worker_id.
	_ = pub.Publish(HeartbeatSubject, []byte(`{"ready":true}`))
	_ = pub.Flush()
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
	srv := natstest.Start(t, natstest.Options{})

	nc := natstest.Connect(t, srv)
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
	defer func() { _ = mgr.Close() }()

	pub := natstest.Connect(t, srv)
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
	_ = pub.Flush()
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
	srv := natstest.Start(t, natstest.Options{})

	nc := natstest.Connect(t, srv)

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

// awaitWorkers polls Reconcile until at least wantCount workers are
// registered, or fails the test after a timeout.  This replaces fragile
// time.Sleep calls that race with NATS async message delivery.
func awaitWorkers(t *testing.T, mgr *NATSFleetManager, wantCount int) *FleetState {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		state, err := mgr.Reconcile(context.Background())
		if err != nil {
			t.Fatalf("reconcile: %v", err)
		}
		if len(state.Workers) >= wantCount {
			return state
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d workers, got %d", wantCount, len(state.Workers))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// assertInt is a small test helper.
func assertInt(t *testing.T, name string, got, want int) {
	t.Helper()
	if got != want {
		t.Fatalf("%s = %d, want %d", name, got, want)
	}
}
