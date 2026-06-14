package statuscore

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"heph4estus/internal/cloud"
	"heph4estus/internal/tui/core"
)

type launchRecorder struct {
	containerCalls int
	spotCalls      int
	containerOpts  cloud.ContainerOpts
	spotOpts       cloud.SpotOpts
	containerErr   error
	spotErr        error
	spotIDs        []string
}

func (r *launchRecorder) LaunchWorkers(_ context.Context, opts cloud.ContainerOpts) (string, error) {
	r.containerCalls++
	r.containerOpts = opts
	return "task-1", r.containerErr
}

func (r *launchRecorder) LaunchSpotWorkers(_ context.Context, opts cloud.SpotOpts) ([]string, error) {
	r.spotCalls++
	r.spotOpts = opts
	if r.spotIDs == nil {
		r.spotIDs = []string{"i-1", "i-2"}
	}
	return r.spotIDs, r.spotErr
}

type fakeStorage struct{}

func (fakeStorage) Upload(context.Context, string, string, []byte) error { return nil }
func (fakeStorage) Download(context.Context, string, string) ([]byte, error) {
	return nil, nil
}
func (fakeStorage) List(context.Context, string, string) ([]string, error) { return nil, nil }
func (fakeStorage) Count(context.Context, string, string) (int, error)     { return 0, nil }

type progressRecorder struct {
	bucket string
	prefix string
	count  int
	err    error
}

func (r *progressRecorder) CountResults(_ context.Context, bucket, prefix string) (int, error) {
	r.bucket = bucket
	r.prefix = prefix
	return r.count, r.err
}

type destroyRecorder struct {
	called bool
	err    error
}

func (r *destroyRecorder) Destroy(context.Context) error {
	r.called = true
	return r.err
}

func baseInfra() core.InfraOutputs {
	return core.InfraOutputs{
		SQSQueueURL:        "queue-url",
		S3BucketName:       "bucket",
		ECSClusterName:     "cluster",
		TaskDefinitionARN:  "task-def",
		ECRRepoURL:         "123456789012.dkr.ecr.us-west-2.amazonaws.com/nmap-worker",
		ImageTag:           "worker-v1",
		SubnetIDs:          []string{"subnet-1"},
		SecurityGroupID:    "sg-1",
		InstanceProfileARN: "profile",
		AMIID:              "ami-1",
		ToolName:           "nmap",
		JobID:              "job-123",
		WorkerCount:        2,
		JitterMaxSeconds:   7,
	}
}

func TestUseSpotModes(t *testing.T) {
	low := baseInfra()
	low.WorkerCount = SpotThreshold - 1
	if UseSpot(low) {
		t.Fatal("auto below threshold should not use spot")
	}

	high := baseInfra()
	high.WorkerCount = SpotThreshold
	if !UseSpot(high) {
		t.Fatal("auto at threshold should use spot")
	}

	forcedFargate := baseInfra()
	forcedFargate.WorkerCount = SpotThreshold
	forcedFargate.ComputeMode = "fargate"
	if UseSpot(forcedFargate) {
		t.Fatal("fargate mode should not use spot")
	}

	forcedSpot := baseInfra()
	forcedSpot.ComputeMode = "spot"
	if !UseSpot(forcedSpot) {
		t.Fatal("spot mode should use spot")
	}

	selfhosted := baseInfra()
	selfhosted.Cloud = cloud.KindManual
	selfhosted.ComputeMode = "spot"
	if UseSpot(selfhosted) {
		t.Fatal("manual selfhosted should not use spot")
	}
}

func TestLaunchWorkersAndProviderNative(t *testing.T) {
	launcher := &launchRecorder{}
	infra := baseInfra()
	infra.ComputeMode = "fargate"

	result := Launch(context.Background(), LaunchOptions{Infra: infra, Launcher: launcher})
	if result.Err != nil {
		t.Fatalf("launch failed: %v", result.Err)
	}
	if launcher.containerCalls != 1 || launcher.spotCalls != 0 {
		t.Fatalf("launch calls = container %d spot %d", launcher.containerCalls, launcher.spotCalls)
	}
	if launcher.containerOpts.ContainerName != "nmap-worker" {
		t.Fatalf("container name = %q", launcher.containerOpts.ContainerName)
	}
	if launcher.containerOpts.Env["TOOL_NAME"] != "nmap" || launcher.containerOpts.Env["JITTER_MAX_SECONDS"] != "7" {
		t.Fatalf("unexpected env: %#v", launcher.containerOpts.Env)
	}
	if result.Launched != 2 || result.Total != 2 {
		t.Fatalf("launch result = %#v", result)
	}

	providerNative := baseInfra()
	providerNative.Cloud = cloud.KindHetzner
	providerNative.WorkerCount = 10
	providerNative.FleetWorkerCount = 3
	launcher = &launchRecorder{}
	result = Launch(context.Background(), LaunchOptions{Infra: providerNative, Launcher: launcher})
	if launcher.containerCalls != 0 || launcher.spotCalls != 0 {
		t.Fatalf("provider-native should not launch workers, got container %d spot %d", launcher.containerCalls, launcher.spotCalls)
	}
	if result.Launched != 3 || result.Total != 3 {
		t.Fatalf("provider-native launch result = %#v", result)
	}
}

func TestLaunchSpot(t *testing.T) {
	infra := baseInfra()
	infra.ComputeMode = "spot"
	launcher := &launchRecorder{}

	result := Launch(context.Background(), LaunchOptions{Infra: infra, Launcher: launcher, ToolName: "nmap"})
	if result.Err != nil {
		t.Fatalf("spot launch failed: %v", result.Err)
	}
	if !result.Spot || result.Launched != 2 || len(result.InstanceIDs) != 2 {
		t.Fatalf("spot launch result = %#v", result)
	}
	if launcher.spotCalls != 1 || launcher.containerCalls != 0 {
		t.Fatalf("launch calls = container %d spot %d", launcher.containerCalls, launcher.spotCalls)
	}
	if launcher.spotOpts.Tags["Tool"] != "nmap" || launcher.spotOpts.UserData == "" {
		t.Fatalf("unexpected spot opts: %#v", launcher.spotOpts)
	}

	missingTag := baseInfra()
	missingTag.ComputeMode = "spot"
	missingTag.ImageTag = " "
	launcher = &launchRecorder{}
	result = Launch(context.Background(), LaunchOptions{Infra: missingTag, Launcher: launcher})
	if result.Err == nil || !result.Spot || launcher.spotCalls != 0 {
		t.Fatalf("missing tag result = %#v, spot calls = %d", result, launcher.spotCalls)
	}
}

func TestCompletionDecisions(t *testing.T) {
	infra := baseInfra()
	infra.CleanupPolicy = "destroy-after"
	infra.OutputDir = "/tmp/results"
	if got := CompleteScan(infra, fakeStorage{}); got.Action != CompletionExport || got.Warning != "" {
		t.Fatalf("complete scan with export = %#v", got)
	}

	infra.OutputDir = ""
	if got := CompleteScan(infra, nil); got.Action != CompletionNavigate || got.Warning != "destroy-after skipped: no output directory configured" {
		t.Fatalf("complete scan without output = %#v", got)
	}

	infra.OutputDir = "/tmp/results"
	infra.Cloud = cloud.KindManual
	if got := CompleteScan(infra, nil); got.Action != CompletionNavigate || got.Warning != "destroy-after skipped: selfhosted does not support auto-destroy" {
		t.Fatalf("complete scan selfhosted = %#v", got)
	}
}

func TestCompleteExportAndDestroy(t *testing.T) {
	boom := errors.New("boom")
	infra := baseInfra()
	result := CompleteExport(&infra, &destroyRecorder{}, ExportResult{Err: boom})
	if result.Action != CompletionNavigate || result.Warning == "" || infra.Exported {
		t.Fatalf("export failure result = %#v exported=%v", result, infra.Exported)
	}

	infra = baseInfra()
	destroyer := &destroyRecorder{}
	result = CompleteExport(&infra, destroyer, ExportResult{Dir: "/tmp/exported", Count: 3})
	if result.Action != CompletionDestroy || result.Warning != "" {
		t.Fatalf("export success result = %#v", result)
	}
	if !infra.Exported || infra.ExportDir != "/tmp/exported" {
		t.Fatalf("export state = exported %v dir %q", infra.Exported, infra.ExportDir)
	}

	destroyResult := RunAutoDestroy(context.Background(), destroyer)
	if destroyResult.Err != nil || !destroyer.called {
		t.Fatalf("destroy result = %#v called=%v", destroyResult, destroyer.called)
	}

	infra = baseInfra()
	result = CompleteExport(&infra, nil, ExportResult{Dir: "/tmp/exported"})
	if result.Action != CompletionNavigate || result.Warning != "destroy-after skipped: no terraform directory" {
		t.Fatalf("export without destroyer = %#v", result)
	}

	infra = baseInfra()
	infra.Cloud = cloud.KindManual
	result = CompleteExport(&infra, &destroyRecorder{}, ExportResult{Dir: "/tmp/exported"})
	if result.Action != CompletionNavigate || result.Warning != "destroy-after skipped: selfhosted does not support auto-destroy" {
		t.Fatalf("selfhosted export result = %#v", result)
	}
}

func TestCompleteDestroy(t *testing.T) {
	infra := baseInfra()
	if warning := CompleteDestroy(&infra, DestroyResult{}); warning != "" || !infra.Destroyed {
		t.Fatalf("destroy success warning=%q destroyed=%v", warning, infra.Destroyed)
	}

	infra = baseInfra()
	boom := errors.New("boom")
	if warning := CompleteDestroy(&infra, DestroyResult{Err: boom}); warning != "destroy failed: boom" || infra.DestroyErr != "boom" {
		t.Fatalf("destroy failure warning=%q err=%q", warning, infra.DestroyErr)
	}
}

func TestProgressHelpers(t *testing.T) {
	now := time.Now()
	samples := []RateSample{
		{Time: now.Add(-45 * time.Second), Count: 1},
		{Time: now.Add(-20 * time.Second), Count: 20},
	}
	samples = UpdateRateSamples(samples, 40, now)
	if len(samples) != 2 || samples[0].Count != 20 || samples[1].Count != 40 {
		t.Fatalf("samples after prune = %#v", samples)
	}

	rate, eta := CalcRateETA(samples, 100, 40)
	if math.Abs(rate-60) > 0.001 {
		t.Fatalf("rate = %f", rate)
	}
	if eta != time.Minute {
		t.Fatalf("eta = %s", eta)
	}

	if got := ProgressBar(5, 10, 10); got != "[█████░░░░░]" {
		t.Fatalf("progress bar = %q", got)
	}
	if got := ProgressBar(0, 0, 3); got != "░░░" {
		t.Fatalf("zero progress bar = %q", got)
	}
}

func TestPollProgressAndNavigation(t *testing.T) {
	tracker := &progressRecorder{count: 4}
	infra := baseInfra()
	result := PollProgress(context.Background(), tracker, infra, "nmap")
	if result.Completed != 4 || result.Err != nil {
		t.Fatalf("poll result = %#v", result)
	}
	if tracker.bucket != "bucket" || tracker.prefix != "scans/nmap/job-123/results/" {
		t.Fatalf("poll args = bucket %q prefix %q", tracker.bucket, tracker.prefix)
	}

	msg := NavigateToResults(core.ViewNmapResults, infra)()
	nav, ok := msg.(core.NavigateWithDataMsg)
	if !ok {
		t.Fatalf("navigate msg = %T", msg)
	}
	if nav.Target != core.ViewNmapResults {
		t.Fatalf("target = %v", nav.Target)
	}
}
