package scanruntime

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"heph4estus/internal/cloud"
	"heph4estus/internal/fleet"
	"heph4estus/internal/infra"
	"heph4estus/internal/logger"
	"heph4estus/internal/operator"
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
	count int
}

func (s *mockStorage) Upload(context.Context, string, string, []byte) error { return nil }
func (s *mockStorage) Download(context.Context, string, string) ([]byte, error) {
	return []byte("{}"), nil
}
func (s *mockStorage) List(context.Context, string, string) ([]string, error) { return nil, nil }
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

func runtimeOutputs(kind cloud.Kind, values map[string]string) infra.TerraformOutputs {
	return infra.DecodeTerraformOutputs(kind, values)
}

func awsRuntimeOutputs() infra.TerraformOutputs {
	return runtimeOutputs(cloud.KindAWS, map[string]string{
		infra.OutputSQSQueueURL:       "queue-url",
		infra.OutputS3BucketName:      "bucket",
		infra.OutputECRRepoURL:        "123.dkr.ecr.us-east-1.amazonaws.com/repo",
		infra.OutputImageTag:          "image-tag",
		infra.OutputDockerImage:       "repo:image-tag",
		infra.OutputECSClusterName:    "cluster",
		infra.OutputTaskDefinitionARN: "task-def",
		infra.OutputSubnetIDs:         "[subnet-a subnet-b]",
		infra.OutputSecurityGroupID:   "sg-123",
		infra.OutputAMIID:             "ami-123",
		infra.OutputInstanceProfile:   "profile-arn",
	})
}

func TestCreateJobRecordUsesTypedOutputs(t *testing.T) {
	store := operator.NewJobStoreAt(t.TempDir())
	tracker := operator.NewTracker(store)
	outputs := runtimeOutputs(cloud.KindHetzner, map[string]string{
		infra.OutputSQSQueueURL:                    "nats-stream",
		infra.OutputS3BucketName:                   "minio-bucket",
		infra.OutputS3Endpoint:                     "https://minio.example",
		infra.OutputS3Region:                       "us-east-1",
		infra.OutputS3AccessKey:                    "access",
		infra.OutputS3SecretKey:                    "secret",
		infra.OutputS3PathStyle:                    "true",
		infra.OutputDockerImage:                    "registry/tool:tag",
		infra.OutputNATSURL:                        "tls://nats.example:4222",
		infra.OutputControllerIP:                   "10.0.0.2",
		infra.OutputGenerationID:                   "gen-1",
		infra.OutputControllerCAPEM:                "ca",
		infra.OutputControllerHost:                 "controller.local",
		infra.OutputNATSOperatorClientCertPEM:      "cert",
		infra.OutputNATSOperatorClientKeyPEM:       "key",
		infra.OutputNATSOperatorClientCertNotAfter: "2035-01-01T00:00:00Z",
	})

	if err := CreateJobRecord(JobRecordOptions{
		Tracker:       tracker,
		JobID:         "job-1",
		ToolName:      "httpx",
		Workers:       3,
		ComputeMode:   "auto",
		CloudKind:     cloud.KindHetzner,
		CleanupPolicy: "destroy-after",
		Bucket:        "minio-bucket",
		Outputs:       outputs,
		Placement: fleet.PlacementPolicy{
			Mode: fleet.PlacementModeDiversity,
		},
	}); err != nil {
		t.Fatalf("CreateJobRecord: %v", err)
	}

	rec, err := store.Load("job-1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if rec.Bucket != "minio-bucket" || rec.S3Endpoint != "https://minio.example" || !rec.S3PathStyle {
		t.Fatalf("storage metadata not persisted: %#v", rec)
	}
	if rec.ExpectedWorkerVersion != "registry/tool:tag" || rec.NATSUrl != "tls://nats.example:4222" || rec.ControllerHost != "controller.local" {
		t.Fatalf("provider-native metadata not persisted: %#v", rec)
	}
	if rec.CleanupPolicy != "destroy-after" || rec.WorkerCount != 3 || rec.Cloud != "hetzner" {
		t.Fatalf("run metadata not persisted: %#v", rec)
	}
}

func TestExecuteQueuedScanStartedFalseOnLaunchFailure(t *testing.T) {
	started, err := ExecuteQueuedScan(context.Background(), ExecuteOptions{
		ToolName:     "httpx",
		JobID:        "job-1",
		Tasks:        []worker.Task{{ToolName: "httpx", JobID: "job-1", Target: "example.com"}},
		EnqueueLabel: "target tasks",
		Workers:      1,
		ComputeMode:  "fargate",
		Queue:        &mockQueue{},
		Storage:      &mockStorage{},
		Compute:      &mockCompute{runContainerErr: errors.New("launch failed")},
		Outputs:      awsRuntimeOutputs(),
		QueueURL:     "queue-url",
		Bucket:       "bucket",
		CloudKind:    cloud.KindAWS,
	})
	if err == nil || !strings.Contains(err.Error(), "launching workers") {
		t.Fatalf("expected launch error, got %v", err)
	}
	if started {
		t.Fatal("expected started=false")
	}
}

func TestExecuteQueuedScanProviderNativeSkipsCompute(t *testing.T) {
	comp := &mockCompute{}
	var waited bool
	started, err := ExecuteQueuedScan(context.Background(), ExecuteOptions{
		ToolName:     "httpx",
		JobID:        "job-1",
		Tasks:        []worker.Task{{ToolName: "httpx", JobID: "job-1", Target: "example.com"}},
		EnqueueLabel: "target tasks",
		Workers:      3,
		ComputeMode:  "auto",
		Queue:        &mockQueue{},
		Storage:      &mockStorage{count: 1},
		Compute:      comp,
		Outputs: runtimeOutputs(cloud.KindHetzner, map[string]string{
			infra.OutputSQSQueueURL:  "nats-stream",
			infra.OutputS3BucketName: "minio-bucket",
		}),
		QueueURL:  "nats-stream",
		Bucket:    "minio-bucket",
		CloudKind: cloud.KindHetzner,
		FleetWaiter: func(context.Context, cloud.Kind, map[string]string, fleet.PlacementPolicy) (int, error) {
			waited = true
			return 3, nil
		},
	})
	if err != nil {
		t.Fatalf("ExecuteQueuedScan: %v", err)
	}
	if !started || !waited {
		t.Fatalf("started=%v waited=%v, want true/true", started, waited)
	}
	if comp.runContainerN != 0 || comp.runSpotN != 0 {
		t.Fatalf("provider-native launch used compute: container=%d spot=%d", comp.runContainerN, comp.runSpotN)
	}
}

func TestFinalizeExportsBeforeDestroyAndRecordsOutput(t *testing.T) {
	store := operator.NewJobStoreAt(t.TempDir())
	tracker := operator.NewTracker(store)
	if err := tracker.Create(&operator.JobRecord{JobID: "job-1", ToolName: "httpx"}); err != nil {
		t.Fatalf("create record: %v", err)
	}

	var order []string
	result, err := Finalize(context.Background(), FinalizeOptions{
		JobID:        "job-1",
		ToolName:     "httpx",
		Tracker:      tracker,
		Started:      true,
		OutDir:       t.TempDir(),
		Storage:      &mockStorage{},
		Bucket:       "bucket",
		DestroyAfter: true,
		CloudKind:    cloud.KindAWS,
		ToolConfig:   &infra.ToolConfig{ToolName: "httpx"},
		ExportJob: func(context.Context, cloud.Storage, string, string, string, string) (*operator.ExportResult, error) {
			order = append(order, "export")
			return &operator.ExportResult{Dir: filepath.Join("out", "httpx", "job-1"), ResultCount: 2, ArtifactCount: 1}, nil
		},
		DestroyInfra: func(context.Context, *infra.ToolConfig, io.Writer, logger.Logger) error {
			order = append(order, "destroy")
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if result.ExportDir != filepath.Join("out", "httpx", "job-1") {
		t.Fatalf("ExportDir = %q", result.ExportDir)
	}
	if !reflect.DeepEqual(order, []string{"export", "destroy"}) {
		t.Fatalf("order = %v, want export then destroy", order)
	}
	rec, err := store.Load("job-1")
	if err != nil {
		t.Fatalf("load record: %v", err)
	}
	if rec.LocalOutputDir != filepath.Join("out", "httpx", "job-1") {
		t.Fatalf("LocalOutputDir = %q", rec.LocalOutputDir)
	}
}
