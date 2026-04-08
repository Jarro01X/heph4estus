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
}

func (c *mockCompute) RunContainer(context.Context, cloud.ContainerOpts) (string, error) {
	return "task-1", c.runContainerErr
}

func (c *mockCompute) RunSpotInstances(context.Context, cloud.SpotOpts) ([]string, error) {
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
