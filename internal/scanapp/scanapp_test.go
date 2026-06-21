package scanapp

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
	q.batches = append(q.batches, append([]string(nil), bodies...))
	return q.sendBatchErr
}

func (q *mockQueue) Receive(context.Context, string) (*cloud.Message, error) { return nil, nil }
func (q *mockQueue) Delete(context.Context, string, string) error            { return nil }

type mockStorage struct {
	count      int
	listErr    error
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
	return nil, nil
}

func (s *mockStorage) Count(context.Context, string, string) (int, error) {
	return s.count, nil
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
		"image_tag":            "heph-worker-20260608T032422Z-a1b2c3d4",
		"docker_image":         "123.dkr.ecr.us-east-1.amazonaws.com/repo:heph-worker-20260608T032422Z-a1b2c3d4",
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

func TestPreflightWordlistFileAutoSizesOmittedChunks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "words.txt")
	if err := os.WriteFile(path, []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatalf("write wordlist: %v", err)
	}

	meta, err := PreflightWordlistFile("ffuf", path, "https://example.com/FUZZ", "", 0, 2)
	if err != nil {
		t.Fatalf("PreflightWordlistFile: %v", err)
	}
	if meta.TotalWords != 3 || meta.EffectiveChunks != 2 {
		t.Fatalf("wordlist metadata = words %d chunks %d", meta.TotalWords, meta.EffectiveChunks)
	}
}

func TestRunTargetListScanStartedFalseOnLaunchFailure(t *testing.T) {
	path := writeTargetFile(t, "example.com\n")
	meta, err := PreflightTargetListFile(path, 1)
	if err != nil {
		t.Fatalf("preflight target file: %v", err)
	}

	started, err := RunTargetListScan(context.Background(), TargetListOptions{
		ToolName:    "httpx",
		JobID:       "job-1",
		InputFile:   path,
		Preflight:   meta,
		Module:      targetListModule(false),
		Workers:     1,
		ComputeMode: "fargate",
		Format:      "text",
		Queue:       &mockQueue{},
		Storage:     &mockStorage{},
		Compute:     &mockCompute{runContainerErr: errors.New("launch failed")},
		Outputs:     testRuntimeOutputs(cloud.KindAWS, testOutputs()),
		Bucket:      "results-bucket",
		QueueURL:    "queue-url",
		Tracker:     operator.NoopTracker(),
		CloudKind:   cloud.KindAWS,
	})
	if err == nil {
		t.Fatal("expected launch error")
	}
	if started {
		t.Fatal("expected started=false")
	}
}

func TestRunTargetListScanUploadsChunksForInputModules(t *testing.T) {
	path := writeTargetFile(t, "example.com\n10.0.0.1\n# skipped\n10.0.0.2\n")
	meta, err := PreflightTargetListFile(path, 2)
	if err != nil {
		t.Fatalf("preflight target file: %v", err)
	}
	queue := &mockQueue{}
	storage := &mockStorage{count: 2}

	started, err := RunTargetListScan(context.Background(), TargetListOptions{
		ToolName:    "httpx",
		JobID:       "job-1",
		InputFile:   path,
		Preflight:   meta,
		Module:      targetListModule(true),
		Workers:     2,
		ComputeMode: "fargate",
		Format:      "text",
		Queue:       queue,
		Storage:     storage,
		Compute:     &mockCompute{},
		Outputs:     testRuntimeOutputs(cloud.KindAWS, testOutputs()),
		Bucket:      "results-bucket",
		QueueURL:    "queue-url",
		Tracker:     operator.NoopTracker(),
		CloudKind:   cloud.KindAWS,
	})
	if err != nil {
		t.Fatalf("RunTargetListScan: %v", err)
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
	if task.InputKey == "" || task.TotalChunks != 2 {
		t.Fatalf("chunk task = %#v", task)
	}
}

func TestRunNmapScanBatchesLargeEnqueue(t *testing.T) {
	tasks := make([]nmaptool.ScanTask, 25)
	for i := range tasks {
		tasks[i] = nmaptool.ScanTask{
			JobID:   "job-1",
			Target:  fmt.Sprintf("192.0.2.%d", i),
			Options: "-sS",
		}
	}
	queue := &mockQueue{}

	started, err := RunNmapScan(context.Background(), NmapScanOptions{
		Tasks:       tasks,
		Workers:     1,
		ComputeMode: "fargate",
		Format:      "text",
		Outputs:     testRuntimeOutputs(cloud.KindAWS, testOutputs()),
		Queue:       queue,
		Storage:     &mockStorage{count: len(tasks)},
		Compute:     &mockCompute{},
		Tracker:     operator.NoopTracker(),
		JobID:       "job-1",
	})
	if err != nil {
		t.Fatalf("RunNmapScan: %v", err)
	}
	if !started {
		t.Fatal("expected started=true")
	}

	gotSizes := make([]int, len(queue.batches))
	for i, batch := range queue.batches {
		gotSizes[i] = len(batch)
	}
	sort.Ints(gotSizes)
	if wantSizes := []int{5, 10, 10}; fmt.Sprint(gotSizes) != fmt.Sprint(wantSizes) {
		t.Fatalf("queued batch sizes = %v, want %v", gotSizes, wantSizes)
	}
}

func TestRunNmapScanProviderNativeSkipsRunContainer(t *testing.T) {
	tasks := []nmaptool.ScanTask{{
		JobID:   "job-sh",
		Target:  "10.0.0.1",
		Options: "-sS",
	}}
	comp := &mockCompute{}

	started, err := RunNmapScan(context.Background(), NmapScanOptions{
		Tasks:       tasks,
		Workers:     1,
		ComputeMode: "auto",
		Format:      "text",
		Outputs: testRuntimeOutputs(cloud.KindHetzner, map[string]string{
			"sqs_queue_url":  "nats-stream",
			"s3_bucket_name": "minio-bucket",
		}),
		Queue:     &mockQueue{},
		Storage:   &mockStorage{count: 1},
		Compute:   comp,
		Tracker:   operator.NoopTracker(),
		JobID:     "job-sh",
		CloudKind: cloud.KindHetzner,
		FleetWaiter: func(context.Context, cloud.Kind, map[string]string, fleet.PlacementPolicy) (int, error) {
			return 1, nil
		},
	})
	if err != nil {
		t.Fatalf("RunNmapScan: %v", err)
	}
	if !started {
		t.Fatal("expected started=true")
	}
	if comp.runContainerN != 0 || comp.runSpotN != 0 {
		t.Fatalf("provider-native path used compute: container=%d spot=%d", comp.runContainerN, comp.runSpotN)
	}
}

func TestRunNmapScanSelfhostedNeverCallsSpot(t *testing.T) {
	tasks := []nmaptool.ScanTask{{
		JobID:   "job-sh",
		Target:  "10.0.0.1",
		Options: "-sS",
	}}
	comp := &mockCompute{runSpotErr: errors.New("spot should not be called")}

	started, err := RunNmapScan(context.Background(), NmapScanOptions{
		Tasks:       tasks,
		Workers:     200,
		ComputeMode: "auto",
		Format:      "text",
		Outputs: testRuntimeOutputs(cloud.KindManual, map[string]string{
			"sqs_queue_url":  "nats-stream",
			"s3_bucket_name": "minio-bucket",
		}),
		Queue:     &mockQueue{},
		Storage:   &mockStorage{count: 1},
		Compute:   comp,
		Tracker:   operator.NoopTracker(),
		JobID:     "job-sh",
		CloudKind: cloud.KindManual,
	})
	if err != nil {
		t.Fatalf("RunNmapScan: %v", err)
	}
	if !started {
		t.Fatal("expected started=true")
	}
	if comp.runSpotN != 0 {
		t.Fatalf("manual selfhosted path used spot %d times", comp.runSpotN)
	}
}

func TestRunTargetListScanReturnsStartedTrueOnOutputFailure(t *testing.T) {
	path := writeTargetFile(t, "example.com\n")
	meta, err := PreflightTargetListFile(path, 1)
	if err != nil {
		t.Fatalf("preflight target file: %v", err)
	}

	started, err := RunTargetListScan(context.Background(), TargetListOptions{
		ToolName:    "httpx",
		JobID:       "job-1",
		InputFile:   path,
		Preflight:   meta,
		Module:      targetListModule(false),
		Workers:     1,
		ComputeMode: "fargate",
		Format:      "text",
		Queue:       &mockQueue{},
		Storage:     &mockStorage{count: 1, listErr: errors.New("list failed")},
		Compute:     &mockCompute{},
		Outputs:     testRuntimeOutputs(cloud.KindAWS, testOutputs()),
		Bucket:      "results-bucket",
		QueueURL:    "queue-url",
		Tracker:     operator.NoopTracker(),
		CloudKind:   cloud.KindAWS,
	})
	if err == nil || !strings.Contains(err.Error(), "list failed") {
		t.Fatalf("expected output failure, got %v", err)
	}
	if !started {
		t.Fatal("expected started=true")
	}
}
