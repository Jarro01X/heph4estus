package nmap

import (
	"context"
	"strings"
	"testing"

	"heph4estus/internal/cloud"
	"heph4estus/internal/tui/core"
	"heph4estus/internal/worker"
)

type mockSubmitter struct {
	enqueueErr    error
	launchErr     error
	spotLaunchErr error
	spotIDs       []string
}

func (s *mockSubmitter) EnqueueTargets(_ context.Context, _ string, _ []worker.Task) error {
	return s.enqueueErr
}

func (s *mockSubmitter) LaunchWorkers(_ context.Context, _ cloud.ContainerOpts) (string, error) {
	return "arn:task:1", s.launchErr
}

func (s *mockSubmitter) LaunchSpotWorkers(_ context.Context, _ cloud.SpotOpts) ([]string, error) {
	if s.spotIDs == nil {
		s.spotIDs = []string{"i-spot1", "i-spot2"}
	}
	return s.spotIDs, s.spotLaunchErr
}

type mockTracker struct {
	count    int
	countErr error
}

func (t *mockTracker) CountResults(_ context.Context, _, _ string) (int, error) {
	return t.count, t.countErr
}

func testInfra() core.InfraOutputs {
	return core.InfraOutputs{
		SQSQueueURL:       "https://sqs/q",
		S3BucketName:      "bucket",
		ECSClusterName:    "cluster",
		TaskDefinitionARN: "arn:td",
		SubnetIDs:         []string{"subnet-a"},
		SecurityGroupID:   "sg-1",
		JobID:             "job-123",
		TargetsContent:    "1.1.1.1\n2.2.2.2\n",
		NmapOptions:       "-sS",
		WorkerCount:       2,
	}
}

func TestStatusModel_Init(t *testing.T) {
	m := NewStatusWithDeps(testInfra(), &mockSubmitter{}, &mockTracker{})
	cmd := m.Init()

	if m.totalTargets != 2 {
		t.Fatalf("expected 2 targets, got %d", m.totalTargets)
	}
	if cmd == nil {
		t.Fatal("expected init command")
	}
}

func TestStatusModel_EnqueueSuccess(t *testing.T) {
	m := NewStatusWithDeps(testInfra(), &mockSubmitter{}, &mockTracker{})
	cmd := m.Init()

	msg := cmd()
	_, _ = m.Update(msg)
	if m.phase != phaseLaunching {
		t.Fatalf("expected phaseLaunching, got %d", m.phase)
	}
}

func TestStatusModel_EnqueueError(t *testing.T) {
	sub := &mockSubmitter{enqueueErr: context.DeadlineExceeded}
	m := NewStatusWithDeps(testInfra(), sub, &mockTracker{})
	cmd := m.Init()
	msg := cmd()
	_, _ = m.Update(msg)

	if m.errMsg == "" {
		t.Fatal("expected error message")
	}
}

func TestStatusModel_LaunchAndScan(t *testing.T) {
	sub := &mockSubmitter{}
	tracker := &mockTracker{count: 2}
	m := NewStatusWithDeps(testInfra(), sub, tracker)

	// Enqueue
	cmd := m.Init()
	msg := cmd()
	_, cmd = m.Update(msg)

	// Launch
	msg = cmd()
	_, _ = m.Update(msg)
	if m.phase != phaseScanning {
		t.Fatalf("expected phaseScanning, got %d", m.phase)
	}
	if m.workersUp != 2 {
		t.Fatalf("expected 2 workers, got %d", m.workersUp)
	}
}

func TestStatusModel_ScanProgress(t *testing.T) {
	m := NewStatusWithDeps(testInfra(), &mockSubmitter{}, &mockTracker{})
	m.phase = phaseScanning
	m.totalTargets = 2

	m.Update(scanProgressMsg{completed: 1})
	if m.completed != 1 {
		t.Fatalf("expected 1 completed, got %d", m.completed)
	}

	_, cmd := m.Update(scanProgressMsg{completed: 2})
	if m.phase != phaseComplete {
		t.Fatalf("expected phaseComplete, got %d", m.phase)
	}
	if cmd == nil {
		t.Fatal("expected navigate command")
	}
	msg := cmd()
	nav, ok := msg.(core.NavigateWithDataMsg)
	if !ok {
		t.Fatalf("expected NavigateWithDataMsg, got %T", msg)
	}
	if nav.Target != core.ViewNmapResults {
		t.Fatalf("expected ViewNmapResults, got %v", nav.Target)
	}
}

func TestStatusModel_View(t *testing.T) {
	m := NewStatusWithDeps(testInfra(), &mockSubmitter{}, &mockTracker{})
	m.totalTargets = 100
	m.phase = phaseScanning
	m.completed = 50
	m.workersUp = 10

	v := m.View()
	if !strings.Contains(v, "Scanning") {
		t.Fatal("expected Scanning in view")
	}
	if !strings.Contains(v, "50 / 100") {
		t.Fatal("expected progress in view")
	}
}

func TestProgressBar(t *testing.T) {
	bar := progressBar(5, 10, 20)
	if !strings.Contains(bar, "██████████") {
		t.Fatalf("expected half-filled bar, got %s", bar)
	}
}

func TestRealTracker_UsesCounterAboveThreshold(t *testing.T) {
	counterCalled := false
	storageCalled := false

	tracker := &realTracker{
		counter: &mockProgressCounter{
			getFunc: func(_ context.Context, _ string) (int, error) {
				counterCalled = true
				return 42, nil
			},
		},
		storage: &mockCountStorage{
			countFunc: func(_ context.Context, _, _ string) (int, error) {
				storageCalled = true
				return 42, nil
			},
		},
		useCounter: true,
	}

	count, err := tracker.CountResults(context.Background(), "bucket", "scans/nmap/job-123/results/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 42 {
		t.Fatalf("expected 42, got %d", count)
	}
	if !counterCalled {
		t.Fatal("expected counter to be called")
	}
	if storageCalled {
		t.Fatal("expected storage NOT to be called")
	}
}

func TestRealTracker_UsesStorageBelowThreshold(t *testing.T) {
	storageCalled := false

	tracker := &realTracker{
		counter: &mockProgressCounter{
			getFunc: func(_ context.Context, _ string) (int, error) {
				t.Fatal("counter should not be called")
				return 0, nil
			},
		},
		storage: &mockCountStorage{
			countFunc: func(_ context.Context, _, _ string) (int, error) {
				storageCalled = true
				return 5, nil
			},
		},
		useCounter: false, // below threshold
	}

	count, err := tracker.CountResults(context.Background(), "bucket", "scans/nmap/job-123/results/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 5 {
		t.Fatalf("expected 5, got %d", count)
	}
	if !storageCalled {
		t.Fatal("expected storage to be called")
	}
}

func TestRealTracker_NilCounterAlwaysUsesStorage(t *testing.T) {
	storageCalled := false

	tracker := &realTracker{
		counter: nil,
		storage: &mockCountStorage{
			countFunc: func(_ context.Context, _, _ string) (int, error) {
				storageCalled = true
				return 10, nil
			},
		},
		useCounter: false, // nil counter means this is always false
	}

	count, err := tracker.CountResults(context.Background(), "bucket", "scans/nmap/job-123/results/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 10 {
		t.Fatalf("expected 10, got %d", count)
	}
	if !storageCalled {
		t.Fatal("expected storage to be called")
	}
}

// mockProgressCounter implements cloud.ProgressCounter for tests.
type mockProgressCounter struct {
	incrementFunc func(ctx context.Context, counterID string) error
	getFunc       func(ctx context.Context, counterID string) (int, error)
}

func (m *mockProgressCounter) Increment(ctx context.Context, counterID string) error {
	return m.incrementFunc(ctx, counterID)
}

func (m *mockProgressCounter) Get(ctx context.Context, counterID string) (int, error) {
	return m.getFunc(ctx, counterID)
}

// mockCountStorage implements just the Count method for tracker tests.
type mockCountStorage struct {
	countFunc func(ctx context.Context, bucket, prefix string) (int, error)
}

func (m *mockCountStorage) Upload(context.Context, string, string, []byte) error { return nil }
func (m *mockCountStorage) Download(context.Context, string, string) ([]byte, error) {
	return nil, nil
}
func (m *mockCountStorage) List(context.Context, string, string) ([]string, error) { return nil, nil }
func (m *mockCountStorage) Count(ctx context.Context, bucket, prefix string) (int, error) {
	return m.countFunc(ctx, bucket, prefix)
}

func TestUseSpot_Auto(t *testing.T) {
	low := core.InfraOutputs{WorkerCount: 10, ComputeMode: "auto"}
	if useSpot(low) {
		t.Fatal("expected Fargate for 10 workers in auto mode")
	}
	high := core.InfraOutputs{WorkerCount: 100, ComputeMode: "auto"}
	if !useSpot(high) {
		t.Fatal("expected Spot for 100 workers in auto mode")
	}
}

func TestUseSpot_Forced(t *testing.T) {
	fargate := core.InfraOutputs{WorkerCount: 200, ComputeMode: "fargate"}
	if useSpot(fargate) {
		t.Fatal("expected Fargate when mode is fargate")
	}
	spot := core.InfraOutputs{WorkerCount: 5, ComputeMode: "spot"}
	if !useSpot(spot) {
		t.Fatal("expected Spot when mode is spot")
	}
}

func TestUseSpot_EmptyDefaultsToAuto(t *testing.T) {
	empty := core.InfraOutputs{WorkerCount: 10, ComputeMode: ""}
	if useSpot(empty) {
		t.Fatal("expected Fargate for empty mode with 10 workers")
	}
	emptyHigh := core.InfraOutputs{WorkerCount: 60, ComputeMode: ""}
	if !useSpot(emptyHigh) {
		t.Fatal("expected Spot for empty mode with 60 workers")
	}
}

func TestStatusModel_SpotLaunch(t *testing.T) {
	infra := testInfra()
	infra.ComputeMode = "spot"
	infra.ECRRepoURL = "123.dkr.ecr.us-east-1.amazonaws.com/nmap-scanner"
	infra.AMIID = "ami-test"
	infra.InstanceProfileARN = "arn:profile"

	sub := &mockSubmitter{}
	m := NewStatusWithDeps(infra, sub, &mockTracker{})

	// Enqueue
	cmd := m.Init()
	msg := cmd()
	_, cmd = m.Update(msg)

	// Launch — should use spot path
	msg = cmd()
	spotMsg, ok := msg.(spotLaunchMsg)
	if !ok {
		t.Fatalf("expected spotLaunchMsg, got %T", msg)
	}
	_, _ = m.Update(spotMsg)
	if m.phase != phaseScanning {
		t.Fatalf("expected phaseScanning, got %d", m.phase)
	}
	if len(m.spotInstanceIDs) != 2 {
		t.Fatalf("expected 2 spot instance IDs, got %d", len(m.spotInstanceIDs))
	}
}

func TestStatusModel_NoTargets(t *testing.T) {
	infra := testInfra()
	infra.TargetsContent = ""
	m := NewStatusWithDeps(infra, &mockSubmitter{}, &mockTracker{})
	cmd := m.Init()

	if cmd != nil {
		t.Fatal("expected no command for zero targets")
	}
	if m.errMsg == "" {
		t.Fatal("expected error for no targets")
	}
}
