package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"heph4estus/internal/cloud"
	"heph4estus/internal/operator"
	nmaptool "heph4estus/internal/tools/nmap"
)

type mockQueue struct {
	sendBatchErr error
}

func (q *mockQueue) Send(context.Context, string, string) error { return nil }

func (q *mockQueue) SendBatch(context.Context, string, []string) error {
	return q.sendBatchErr
}

func (q *mockQueue) Receive(context.Context, string) (*cloud.Message, error) { return nil, nil }

func (q *mockQueue) Delete(context.Context, string, string) error { return nil }

type mockStorage struct {
	count    int
	countErr error
	listErr  error
	keys     []string
}

func (s *mockStorage) Upload(context.Context, string, string, []byte) error { return nil }

func (s *mockStorage) Download(context.Context, string, string) ([]byte, error) {
	return []byte("{}"), nil
}

func (s *mockStorage) List(context.Context, string, string) ([]string, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.keys, nil
}

func (s *mockStorage) Count(context.Context, string, string) (int, error) {
	return s.count, s.countErr
}

type mockCompute struct {
	runContainerErr error
	runSpotErr      error
	runContainerN   int
	runSpotN        int
}

func (c *mockCompute) RunContainer(context.Context, cloud.ContainerOpts) (string, error) {
	c.runContainerN++
	return "task-1", c.runContainerErr
}

func (c *mockCompute) RunSpotInstances(context.Context, cloud.SpotOpts) ([]string, error) {
	c.runSpotN++
	if c.runSpotErr != nil {
		return nil, c.runSpotErr
	}
	return []string{"i-1"}, nil
}

func (c *mockCompute) GetSpotStatus(context.Context, []string) ([]cloud.SpotStatus, error) {
	return nil, nil
}

func testOutputs() map[string]string {
	return map[string]string{
		"sqs_queue_url":        "queue-url",
		"s3_bucket_name":       "results-bucket",
		"ecr_repo_url":         "123.dkr.ecr.us-east-1.amazonaws.com/repo",
		"ecs_cluster_name":     "cluster",
		"task_definition_arn":  "task-def",
		"subnet_ids":           "[subnet-a subnet-b]",
		"security_group_id":    "sg-123",
		"ami_id":               "ami-123",
		"instance_profile_arn": "profile-arn",
	}
}

func TestPreflightTargetListFileRejectsEmptyTargets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "targets.txt")
	if err := os.WriteFile(path, []byte("\n# comment only\n\n"), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	_, err := preflightTargetListFile(path)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "no targets found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPreflightWordlistFileRejectsEmptyWordlist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "words.txt")
	if err := os.WriteFile(path, []byte("\n\n"), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	_, err := preflightWordlistFile("ffuf", path, "https://example.com/FUZZ", "", 0, 5)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "planning wordlist job") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunTargetListScanStartedFalseOnLaunchFailure(t *testing.T) {
	started, err := runTargetListScan(
		context.Background(),
		"httpx",
		"job-1",
		"targets.txt",
		"example.com\n",
		"",
		1,
		"fargate",
		"text",
		&mockQueue{},
		&mockStorage{},
		&mockCompute{runContainerErr: errors.New("launch failed")},
		testOutputs(),
		"results-bucket",
		"queue-url",
		operator.NoopTracker(),
		cloud.KindAWS,
	)
	if err == nil {
		t.Fatal("expected error")
	}
	if started {
		t.Fatal("expected started=false on launch failure")
	}
}

func TestRunTargetListScanStartedTrueOnOutputFailure(t *testing.T) {
	started, err := runTargetListScan(
		context.Background(),
		"httpx",
		"job-1",
		"targets.txt",
		"example.com\n",
		"",
		1,
		"fargate",
		"text",
		&mockQueue{},
		&mockStorage{count: 1, listErr: errors.New("list failed")},
		&mockCompute{},
		testOutputs(),
		"results-bucket",
		"queue-url",
		operator.NoopTracker(),
		cloud.KindAWS,
	)
	if err == nil {
		t.Fatal("expected error")
	}
	if !started {
		t.Fatal("expected started=true after successful worker launch")
	}
}

func TestRunNmapScanWithDepsStartedFalseOnLaunchFailure(t *testing.T) {
	tasks := []nmaptool.ScanTask{{
		JobID:   "job-1",
		Target:  "example.com",
		Options: "-sS",
	}}

	started, err := runNmapScanWithDeps(
		context.Background(),
		tasks,
		1,
		"fargate",
		0,
		"text",
		testOutputs(),
		&mockQueue{},
		&mockStorage{},
		&mockCompute{runContainerErr: errors.New("launch failed")},
		operator.NoopTracker(),
		"job-1",
	)
	if err == nil {
		t.Fatal("expected error")
	}
	if started {
		t.Fatal("expected started=false on launch failure")
	}
}

func TestRunNmapScanWithDepsStartedTrueOnOutputFailure(t *testing.T) {
	tasks := []nmaptool.ScanTask{{
		JobID:   "job-1",
		Target:  "example.com",
		Options: "-sS",
	}}

	started, err := runNmapScanWithDeps(
		context.Background(),
		tasks,
		1,
		"fargate",
		0,
		"text",
		testOutputs(),
		&mockQueue{},
		&mockStorage{count: 1, listErr: errors.New("list failed")},
		&mockCompute{},
		operator.NoopTracker(),
		"job-1",
	)
	if err == nil {
		t.Fatal("expected error")
	}
	if !started {
		t.Fatal("expected started=true after successful worker launch")
	}
}

func TestRunNmapScanWithDeps_ProviderNativeSkipsRunContainer(t *testing.T) {
	tasks := []nmaptool.ScanTask{{
		JobID:   "job-sh",
		Target:  "10.0.0.1",
		Options: "-sS",
	}}

	oldWait := waitForProviderNativeFleetFunc
	waitForProviderNativeFleetFunc = func(context.Context, cloud.Kind, map[string]string) (int, error) {
		return 1, nil
	}
	t.Cleanup(func() { waitForProviderNativeFleetFunc = oldWait })

	comp := &mockCompute{}
	started, err := runNmapScanWithDeps(
		context.Background(),
		tasks,
		1,
		"auto", // auto on VPS providers should NOT use spot
		0,
		"text",
		map[string]string{
			"sqs_queue_url":  "nats-stream",
			"s3_bucket_name": "minio-bucket",
		},
		&mockQueue{},
		&mockStorage{count: 1},
		comp,
		operator.NoopTracker(),
		"job-sh",
		cloud.KindHetzner,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !started {
		t.Fatal("expected started=true")
	}
	if comp.runContainerN != 0 {
		t.Fatalf("expected provider-native Hetzner path to skip RunContainer, got %d calls", comp.runContainerN)
	}
	if comp.runSpotN != 0 {
		t.Fatalf("expected provider-native Hetzner path to skip RunSpotInstances, got %d calls", comp.runSpotN)
	}
}

func TestRunNmapScanWithDeps_SelfhostedNeverCallsSpot(t *testing.T) {
	// Even with 200 workers (above spot threshold), VPS providers should use RunContainer.
	tasks := []nmaptool.ScanTask{{
		JobID:   "job-sh",
		Target:  "10.0.0.1",
		Options: "-sS",
	}}

	comp := &mockCompute{runSpotErr: errors.New("spot should not be called")}
	started, err := runNmapScanWithDeps(
		context.Background(),
		tasks,
		200, // above spot threshold
		"auto",
		0,
		"text",
		map[string]string{
			"sqs_queue_url":  "nats-stream",
			"s3_bucket_name": "minio-bucket",
		},
		&mockQueue{},
		&mockStorage{count: 1},
		comp,
		operator.NoopTracker(),
		"job-sh",
		cloud.KindManual,
	)
	if err != nil {
		t.Fatalf("unexpected error (spot should not have been called): %v", err)
	}
	if !started {
		t.Fatal("expected started=true")
	}
	if comp.runSpotN != 0 {
		t.Fatalf("expected manual selfhosted path to avoid spot, got %d calls", comp.runSpotN)
	}
}

func TestRunTargetListScan_ProviderNativeSkipsRunContainer(t *testing.T) {
	oldWait := waitForProviderNativeFleetFunc
	waitForProviderNativeFleetFunc = func(context.Context, cloud.Kind, map[string]string) (int, error) {
		return 3, nil
	}
	t.Cleanup(func() { waitForProviderNativeFleetFunc = oldWait })

	comp := &mockCompute{}
	started, err := runTargetListScan(
		context.Background(),
		"httpx",
		"job-hetzner",
		"targets.txt",
		"example.com\n",
		"",
		10,
		"auto",
		"text",
		&mockQueue{},
		&mockStorage{count: 1},
		comp,
		map[string]string{
			"sqs_queue_url":  "heph-tasks",
			"s3_bucket_name": "heph-results",
			"nats_url":       "nats://10.0.1.2:4222",
			"worker_count":   "3",
		},
		"heph-results",
		"heph-tasks",
		operator.NoopTracker(),
		cloud.KindHetzner,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !started {
		t.Fatal("expected started=true")
	}
	if comp.runContainerN != 0 {
		t.Fatalf("expected provider-native target-list path to skip RunContainer, got %d calls", comp.runContainerN)
	}
}

func TestPrintRunSummaryReused(t *testing.T) {
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	printRunSummary("nmap-20260408-abc", "nmap", true, "reuse", "")

	_ = w.Close()
	os.Stderr = old

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	output := string(buf[:n])

	checks := []string{
		"Run Summary",
		"Job:      nmap-20260408-abc",
		"Tool:     nmap",
		"Infra:    reused existing",
		"Cleanup:  reuse",
	}
	for _, check := range checks {
		if !strings.Contains(output, check) {
			t.Errorf("summary missing %q\ngot:\n%s", check, output)
		}
	}
	if strings.Contains(output, "Output:") {
		t.Error("summary should not show Output when empty")
	}
}

func TestPrintRunSummaryDeployedWithOutput(t *testing.T) {
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	printRunSummary("httpx-20260408-def", "httpx", false, "destroy-after", "/tmp/results/httpx/httpx-20260408-def")

	_ = w.Close()
	os.Stderr = old

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	output := string(buf[:n])

	checks := []string{
		"Job:      httpx-20260408-def",
		"Tool:     httpx",
		"Infra:    freshly deployed",
		"Cleanup:  destroy-after",
		"Output:   /tmp/results/httpx/httpx-20260408-def",
	}
	for _, check := range checks {
		if !strings.Contains(output, check) {
			t.Errorf("summary missing %q\ngot:\n%s", check, output)
		}
	}
}
