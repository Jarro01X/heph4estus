package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"heph4estus/internal/cloud"
	"heph4estus/internal/fleet"
	"heph4estus/internal/infra"
	"heph4estus/internal/modules"
	"heph4estus/internal/operator"
	nmaptool "heph4estus/internal/tools/nmap"
	"heph4estus/internal/worker"
)

type mockQueue struct {
	sendBatchErr error
	batches      [][]string
}

func (q *mockQueue) Send(context.Context, string, string) error { return nil }

func (q *mockQueue) SendBatch(_ context.Context, _ string, bodies []string) error {
	copied := append([]string(nil), bodies...)
	q.batches = append(q.batches, copied)
	return q.sendBatchErr
}

func (q *mockQueue) Receive(context.Context, string) (*cloud.Message, error) { return nil, nil }

func (q *mockQueue) Delete(context.Context, string, string) error { return nil }

type mockStorage struct {
	count      int
	countErr   error
	listErr    error
	keys       []string
	uploadKeys []string
}

func (s *mockStorage) Upload(_ context.Context, _, key string, _ []byte) error {
	s.uploadKeys = append(s.uploadKeys, key)
	return nil
}

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
		"image_tag":            "heph-nmap-worker-20260608T032422Z-a1b2c3d4",
		"docker_image":         "123.dkr.ecr.us-east-1.amazonaws.com/repo:heph-nmap-worker-20260608T032422Z-a1b2c3d4",
		"ecs_cluster_name":     "cluster",
		"task_definition_arn":  "task-def",
		"subnet_ids":           "[subnet-a subnet-b]",
		"security_group_id":    "sg-123",
		"ami_id":               "ami-123",
		"instance_profile_arn": "profile-arn",
	}
}

func testRuntimeOutputs(kind cloud.Kind, outputs map[string]string) infra.TerraformOutputs {
	return infra.DecodeTerraformOutputs(kind, outputs)
}

func writeTargetFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "targets.txt")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write target file: %v", err)
	}
	return path
}

func targetListModule(fileInput bool) *modules.ModuleDefinition {
	exec := []string{"tool", "{{target}}"}
	if fileInput {
		exec = []string{"tool", "-l", "{{input}}"}
	}
	return &modules.ModuleDefinition{
		Name:          "httpx",
		Exec:          exec,
		InputType:     modules.InputTypeTargetList,
		OutputExt:     "jsonl",
		InstallCmd:    "true",
		DefaultCPU:    256,
		DefaultMemory: 512,
		Timeout:       "1m",
	}
}

func TestPreflightTargetListFileRejectsEmptyTargets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "targets.txt")
	if err := os.WriteFile(path, []byte("\n# comment only\n\n"), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	_, err := preflightTargetListFile(path, 1)
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
	if !strings.Contains(err.Error(), "no entries found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPreflightWordlistFileAutoSizesOmittedChunks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "words.txt")
	if err := os.WriteFile(path, []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	meta, err := preflightWordlistFile("ffuf", path, "https://example.com/FUZZ", "", 0, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if meta.TotalWords != 3 {
		t.Fatalf("TotalWords = %d, want 3", meta.TotalWords)
	}
	if meta.EffectiveChunks != 2 {
		t.Fatalf("EffectiveChunks = %d, want 2", meta.EffectiveChunks)
	}
}

func TestPreflightWordlistFileHonorsExplicitChunks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "words.txt")
	if err := os.WriteFile(path, []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	meta, err := preflightWordlistFile("ffuf", path, "https://example.com/FUZZ", "", 3, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if meta.RequestedChunks != 3 || meta.EffectiveChunks != 3 {
		t.Fatalf("requested/effective chunks = %d/%d, want 3/3", meta.RequestedChunks, meta.EffectiveChunks)
	}
}

func TestPreflightWordlistFileRejectsDirectory(t *testing.T) {
	_, err := preflightWordlistFile("ffuf", t.TempDir(), "https://example.com/FUZZ", "", 0, 2)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "directory") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPreflightWordlistFileRejectsUnsafeExplicitChunkSizing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "huge-words.txt")
	if err := os.WriteFile(path, []byte("a\n"), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	if err := os.Truncate(path, 65*1024*1024); err != nil {
		t.Fatalf("truncate temp file: %v", err)
	}

	_, err := preflightWordlistFile("ffuf", path, "https://example.com/FUZZ", "", 1, 1)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "increase --chunks") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunTargetListScanStartedFalseOnLaunchFailure(t *testing.T) {
	path := writeTargetFile(t, "example.com\n")
	meta, err := preflightTargetListFile(path, 1)
	if err != nil {
		t.Fatalf("preflight target file: %v", err)
	}
	started, err := runTargetListScan(
		context.Background(),
		"httpx",
		"job-1",
		path,
		meta,
		targetListModule(false),
		"",
		1,
		"fargate",
		"text",
		&mockQueue{},
		&mockStorage{},
		&mockCompute{runContainerErr: errors.New("launch failed")},
		testRuntimeOutputs(cloud.KindAWS, testOutputs()),
		"results-bucket",
		"queue-url",
		operator.NoopTracker(),
		cloud.KindAWS,
		fleet.PlacementPolicy{},
	)
	if err == nil {
		t.Fatal("expected error")
	}
	if started {
		t.Fatal("expected started=false on launch failure")
	}
}

func TestRunTargetListScanStartedTrueOnOutputFailure(t *testing.T) {
	path := writeTargetFile(t, "example.com\n")
	meta, err := preflightTargetListFile(path, 1)
	if err != nil {
		t.Fatalf("preflight target file: %v", err)
	}
	started, err := runTargetListScan(
		context.Background(),
		"httpx",
		"job-1",
		path,
		meta,
		targetListModule(false),
		"",
		1,
		"fargate",
		"text",
		&mockQueue{},
		&mockStorage{count: 1, listErr: errors.New("list failed")},
		&mockCompute{},
		testRuntimeOutputs(cloud.KindAWS, testOutputs()),
		"results-bucket",
		"queue-url",
		operator.NoopTracker(),
		cloud.KindAWS,
		fleet.PlacementPolicy{},
	)
	if err == nil {
		t.Fatal("expected error")
	}
	if !started {
		t.Fatal("expected started=true after successful worker launch")
	}
}

func TestRunTargetListScanUploadsChunksForInputModules(t *testing.T) {
	path := writeTargetFile(t, "example.com\n10.0.0.1\n# skipped\n10.0.0.2\n")
	meta, err := preflightTargetListFile(path, 2)
	if err != nil {
		t.Fatalf("preflight target file: %v", err)
	}
	queue := &mockQueue{}
	storage := &mockStorage{count: 2}

	started, err := runTargetListScan(
		context.Background(),
		"httpx",
		"job-1",
		path,
		meta,
		targetListModule(true),
		"",
		2,
		"fargate",
		"text",
		queue,
		storage,
		&mockCompute{},
		testRuntimeOutputs(cloud.KindAWS, testOutputs()),
		"results-bucket",
		"queue-url",
		operator.NoopTracker(),
		cloud.KindAWS,
		fleet.PlacementPolicy{},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !started {
		t.Fatal("expected started=true")
	}
	if len(storage.uploadKeys) != 2 {
		t.Fatalf("uploaded chunks = %d, want 2", len(storage.uploadKeys))
	}
	if len(queue.batches) != 1 || len(queue.batches[0]) != 2 {
		t.Fatalf("queued batches = %#v, want one batch with two tasks", queue.batches)
	}
	var task worker.Task
	if err := json.Unmarshal([]byte(queue.batches[0][0]), &task); err != nil {
		t.Fatalf("unmarshal task: %v", err)
	}
	if task.InputKey == "" {
		t.Fatal("expected chunk task InputKey")
	}
	if task.TotalChunks != 2 {
		t.Fatalf("TotalChunks = %d, want 2", task.TotalChunks)
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
		testRuntimeOutputs(cloud.KindAWS, testOutputs()),
		&mockQueue{},
		&mockStorage{},
		&mockCompute{runContainerErr: errors.New("launch failed")},
		operator.NoopTracker(),
		"job-1",
		fleet.PlacementPolicy{},
	)
	if err == nil {
		t.Fatal("expected error")
	}
	if started {
		t.Fatal("expected started=false on launch failure")
	}
}

func TestRunNmapScanWithDepsBatchesLargeEnqueue(t *testing.T) {
	tasks := make([]nmaptool.ScanTask, 25)
	for i := range tasks {
		tasks[i] = nmaptool.ScanTask{
			JobID:   "job-1",
			Target:  fmt.Sprintf("192.0.2.%d", i),
			Options: "-sS",
		}
	}
	queue := &mockQueue{}

	started, err := runNmapScanWithDeps(
		context.Background(),
		tasks,
		1,
		"fargate",
		0,
		"text",
		testRuntimeOutputs(cloud.KindAWS, testOutputs()),
		queue,
		&mockStorage{count: len(tasks)},
		&mockCompute{},
		operator.NoopTracker(),
		"job-1",
		fleet.PlacementPolicy{},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !started {
		t.Fatal("expected started=true")
	}

	gotSizes := make([]int, len(queue.batches))
	for i, batch := range queue.batches {
		gotSizes[i] = len(batch)
	}
	sort.Ints(gotSizes)
	wantSizes := []int{5, 10, 10}
	for i, want := range wantSizes {
		if gotSizes[i] != want {
			t.Fatalf("queued batch sizes = %v, want %v", gotSizes, wantSizes)
		}
	}

	sawLast := false
	for _, batch := range queue.batches {
		for _, body := range batch {
			var task worker.Task
			if err := json.Unmarshal([]byte(body), &task); err != nil {
				t.Fatalf("unmarshal task: %v", err)
			}
			if task.ToolName == "nmap" && task.Target == "192.0.2.24" {
				sawLast = true
			}
		}
	}
	if !sawLast {
		t.Fatal("expected queued payloads to include the final nmap target")
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
		testRuntimeOutputs(cloud.KindAWS, testOutputs()),
		&mockQueue{},
		&mockStorage{count: 1, listErr: errors.New("list failed")},
		&mockCompute{},
		operator.NoopTracker(),
		"job-1",
		fleet.PlacementPolicy{},
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
	waitForProviderNativeFleetFunc = func(context.Context, cloud.Kind, map[string]string, fleet.PlacementPolicy) (int, error) {
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
		testRuntimeOutputs(cloud.KindHetzner, map[string]string{
			"sqs_queue_url":  "nats-stream",
			"s3_bucket_name": "minio-bucket",
		}),
		&mockQueue{},
		&mockStorage{count: 1},
		comp,
		operator.NoopTracker(),
		"job-sh",
		fleet.PlacementPolicy{},
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
		testRuntimeOutputs(cloud.KindManual, map[string]string{
			"sqs_queue_url":  "nats-stream",
			"s3_bucket_name": "minio-bucket",
		}),
		&mockQueue{},
		&mockStorage{count: 1},
		comp,
		operator.NoopTracker(),
		"job-sh",
		fleet.PlacementPolicy{},
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
	waitForProviderNativeFleetFunc = func(context.Context, cloud.Kind, map[string]string, fleet.PlacementPolicy) (int, error) {
		return 3, nil
	}
	t.Cleanup(func() { waitForProviderNativeFleetFunc = oldWait })

	comp := &mockCompute{}
	path := writeTargetFile(t, "example.com\n")
	meta, err := preflightTargetListFile(path, 10)
	if err != nil {
		t.Fatalf("preflight target file: %v", err)
	}
	started, err := runTargetListScan(
		context.Background(),
		"httpx",
		"job-hetzner",
		path,
		meta,
		targetListModule(false),
		"",
		10,
		"auto",
		"text",
		&mockQueue{},
		&mockStorage{count: 1},
		comp,
		testRuntimeOutputs(cloud.KindHetzner, map[string]string{
			"sqs_queue_url":  "heph-tasks",
			"s3_bucket_name": "heph-results",
			"nats_url":       "nats://10.0.1.2:4222",
			"worker_count":   "3",
		}),
		"heph-results",
		"heph-tasks",
		operator.NoopTracker(),
		cloud.KindHetzner,
		fleet.PlacementPolicy{},
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
