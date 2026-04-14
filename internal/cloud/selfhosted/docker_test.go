package selfhosted

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"heph4estus/internal/cloud"
	"heph4estus/internal/logger"
)

// runCall records one invocation of a CommandRunner.
type runCall struct {
	Host string
	Cmd  string
}

// recordingRunner captures every Run call for later assertion.
type recordingRunner struct {
	calls []runCall
	err   error // if non-nil, every call returns this
}

func (r *recordingRunner) Run(_ context.Context, host, cmd string) error {
	r.calls = append(r.calls, runCall{Host: host, Cmd: cmd})
	return r.err
}

func testCompute(hosts []string, image string, transportEnv map[string]string, runner CommandRunner) *DockerCompute {
	return &DockerCompute{
		hosts:        hosts,
		image:        image,
		transportEnv: transportEnv,
		runner:       runner,
		logger:       logger.NewSimpleLogger(),
	}
}

// --- Single-container launch ---

func TestDockerCompute_SingleContainer(t *testing.T) {
	rec := &recordingRunner{}
	dc := testCompute(
		[]string{"10.0.0.1"},
		"ghcr.io/heph/worker:latest",
		map[string]string{
			"S3_ENDPOINT":   "https://minio:9000",
			"S3_REGION":     "us-east-1",
			"S3_ACCESS_KEY": "ak",
			"S3_SECRET_KEY": "sk",
			"S3_PATH_STYLE": "true",
			"NATS_URL":      "nats://nats:4222",
		},
		rec,
	)

	result, err := dc.RunContainer(context.Background(), cloud.ContainerOpts{
		ContainerName: "test-worker",
		Count:         1,
	})
	if err != nil {
		t.Fatalf("RunContainer: %v", err)
	}

	if len(rec.calls) != 2 {
		t.Fatalf("expected 2 calls (pull + run), got %d", len(rec.calls))
	}

	// Both target the single host.
	for i, c := range rec.calls {
		if c.Host != "10.0.0.1" {
			t.Errorf("call[%d] host = %q, want 10.0.0.1", i, c.Host)
		}
	}

	// Pull command.
	if rec.calls[0].Cmd != "docker pull ghcr.io/heph/worker:latest" {
		t.Errorf("pull cmd = %q", rec.calls[0].Cmd)
	}

	// Run command: verify name and required env.
	runCmd := rec.calls[1].Cmd
	if !strings.Contains(runCmd, "--name test-worker") {
		t.Errorf("run cmd missing --name: %q", runCmd)
	}
	for _, want := range []string{
		"-e CLOUD=selfhosted",
		"-e S3_ENDPOINT=https://minio:9000",
		"-e S3_REGION=us-east-1",
		"-e S3_ACCESS_KEY=ak",
		"-e S3_SECRET_KEY=sk",
		"-e S3_PATH_STYLE=true",
		"-e NATS_URL=nats://nats:4222",
	} {
		if !strings.Contains(runCmd, want) {
			t.Errorf("run cmd missing %q: %q", want, runCmd)
		}
	}

	if result != "10.0.0.1:test-worker" {
		t.Errorf("result = %q, want 10.0.0.1:test-worker", result)
	}
}

// --- Multi-container with round-robin ---

func TestDockerCompute_MultiContainer_RoundRobin(t *testing.T) {
	rec := &recordingRunner{}
	dc := testCompute([]string{"h1", "h2"}, "worker:latest", map[string]string{}, rec)

	result, err := dc.RunContainer(context.Background(), cloud.ContainerOpts{
		ContainerName: "scan",
		Count:         5,
	})
	if err != nil {
		t.Fatalf("RunContainer: %v", err)
	}

	// 5 containers x 2 calls each = 10 calls.
	if len(rec.calls) != 10 {
		t.Fatalf("expected 10 calls, got %d", len(rec.calls))
	}

	// Round-robin: h1, h2, h1, h2, h1.
	wantHosts := []string{"h1", "h2", "h1", "h2", "h1"}
	for i, want := range wantHosts {
		pullHost := rec.calls[i*2].Host
		runHost := rec.calls[i*2+1].Host
		if pullHost != want {
			t.Errorf("container %d pull host = %q, want %q", i, pullHost, want)
		}
		if runHost != want {
			t.Errorf("container %d run host = %q, want %q", i, runHost, want)
		}
	}

	// Container names: scan-0 through scan-4.
	for i := 0; i < 5; i++ {
		wantName := fmt.Sprintf("--name scan-%d", i)
		if !strings.Contains(rec.calls[i*2+1].Cmd, wantName) {
			t.Errorf("container %d: cmd missing %q: %q", i, wantName, rec.calls[i*2+1].Cmd)
		}
	}

	// Deterministic result.
	parts := strings.Split(result, ",")
	want := []string{"h1:scan-0", "h2:scan-1", "h1:scan-2", "h2:scan-3", "h1:scan-4"}
	if len(parts) != len(want) {
		t.Fatalf("result parts = %d, want %d", len(parts), len(want))
	}
	for i := range want {
		if parts[i] != want[i] {
			t.Errorf("result[%d] = %q, want %q", i, parts[i], want[i])
		}
	}
}

// --- Zero count treated as 1 ---

func TestDockerCompute_ZeroCount(t *testing.T) {
	rec := &recordingRunner{}
	dc := testCompute([]string{"h1"}, "img", map[string]string{}, rec)

	result, err := dc.RunContainer(context.Background(), cloud.ContainerOpts{Count: 0})
	if err != nil {
		t.Fatalf("RunContainer: %v", err)
	}
	if len(rec.calls) != 2 {
		t.Fatalf("expected 2 calls (1 container), got %d", len(rec.calls))
	}
	if result != "h1:heph-worker" {
		t.Errorf("result = %q, want h1:heph-worker", result)
	}
}

// --- Image override ---

func TestDockerCompute_ImageOverride(t *testing.T) {
	rec := &recordingRunner{}
	dc := testCompute([]string{"h1"}, "default:latest", map[string]string{}, rec)

	_, err := dc.RunContainer(context.Background(), cloud.ContainerOpts{
		Image: "override:v2",
		Count: 1,
	})
	if err != nil {
		t.Fatalf("RunContainer: %v", err)
	}
	if !strings.Contains(rec.calls[0].Cmd, "override:v2") {
		t.Errorf("pull should use override image: %q", rec.calls[0].Cmd)
	}
	if !strings.Contains(rec.calls[1].Cmd, "override:v2") {
		t.Errorf("run should use override image: %q", rec.calls[1].Cmd)
	}
}

// --- Runner error propagates ---

func TestDockerCompute_RunnerError(t *testing.T) {
	rec := &recordingRunner{err: fmt.Errorf("connection refused")}
	dc := testCompute([]string{"h1"}, "img", map[string]string{}, rec)

	_, err := dc.RunContainer(context.Background(), cloud.ContainerOpts{Count: 1})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "docker pull on h1") {
		t.Errorf("error = %q, want docker pull context", err.Error())
	}
}

// --- Env precedence ---

func TestBuildEnv(t *testing.T) {
	dc := &DockerCompute{
		transportEnv: map[string]string{
			"S3_ENDPOINT": "https://minio:9000",
			"NATS_URL":    "nats://nats:4222",
		},
	}

	env := dc.buildEnv(map[string]string{
		"CUSTOM":   "val",
		"CLOUD":    "aws",
		"NATS_URL": "nats://should-be-overridden",
	})

	if env["CLOUD"] != "selfhosted" {
		t.Errorf("CLOUD = %q, want selfhosted (must be forced)", env["CLOUD"])
	}
	if env["NATS_URL"] != "nats://nats:4222" {
		t.Errorf("NATS_URL = %q, transport should override caller", env["NATS_URL"])
	}
	if env["S3_ENDPOINT"] != "https://minio:9000" {
		t.Errorf("S3_ENDPOINT = %q", env["S3_ENDPOINT"])
	}
	if env["CUSTOM"] != "val" {
		t.Errorf("CUSTOM = %q, caller env should pass through", env["CUSTOM"])
	}
}

// --- Container naming ---

func TestContainerName(t *testing.T) {
	tests := []struct {
		base  string
		index int
		total int
		want  string
	}{
		{"worker", 0, 1, "worker"},
		{"worker", 0, 3, "worker-0"},
		{"worker", 2, 3, "worker-2"},
		{"", 0, 1, "heph-worker"},
		{"", 1, 2, "heph-worker-1"},
		{"my/scan@host", 0, 1, "my-scan-host"},
		{"valid.name_ok", 0, 1, "valid.name_ok"},
	}
	for _, tt := range tests {
		got := containerName(tt.base, tt.index, tt.total)
		if got != tt.want {
			t.Errorf("containerName(%q, %d, %d) = %q, want %q", tt.base, tt.index, tt.total, got, tt.want)
		}
	}
}

// --- Spot methods ---

func TestDockerCompute_SpotUnsupported(t *testing.T) {
	dc := testCompute([]string{"h1"}, "img", map[string]string{}, &recordingRunner{})
	ctx := context.Background()

	_, err := dc.RunSpotInstances(ctx, cloud.SpotOpts{})
	if !errors.Is(err, errSpotUnsupported) {
		t.Errorf("RunSpotInstances = %v, want errSpotUnsupported", err)
	}
	if !errors.Is(err, cloud.ErrNotImplemented) {
		t.Error("RunSpotInstances should wrap cloud.ErrNotImplemented")
	}

	_, err = dc.GetSpotStatus(ctx, nil)
	if !errors.Is(err, errSpotUnsupported) {
		t.Errorf("GetSpotStatus = %v, want errSpotUnsupported", err)
	}
	if !errors.Is(err, cloud.ErrNotImplemented) {
		t.Error("GetSpotStatus should wrap cloud.ErrNotImplemented")
	}
}

// --- Config validation ---

func TestValidateComputeConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *ComputeConfig
		wantSub string
	}{
		{"nil config", nil, "compute is not configured"},
		{"no hosts", &ComputeConfig{SSHUser: "u", SSHKeyPath: "/k", DockerImage: "img"}, "no worker hosts"},
		{"no user", &ComputeConfig{WorkerHosts: []string{"h"}, SSHKeyPath: "/k", DockerImage: "img"}, "no SSH user"},
		{"no key", &ComputeConfig{WorkerHosts: []string{"h"}, SSHUser: "u", DockerImage: "img"}, "no SSH key path"},
		{"no image", &ComputeConfig{WorkerHosts: []string{"h"}, SSHUser: "u", SSHKeyPath: "/k"}, "no Docker image"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateComputeConfig(tt.cfg)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantSub) {
				t.Errorf("error = %q, want substring %q", err, tt.wantSub)
			}
			if !errors.Is(err, errComputeNotConfigured) {
				t.Error("error should wrap errComputeNotConfigured")
			}
		})
	}
}

func TestValidateComputeConfig_Valid(t *testing.T) {
	err := validateComputeConfig(&ComputeConfig{
		WorkerHosts: []string{"h"},
		SSHUser:     "u",
		SSHKeyPath:  "/k",
		DockerImage: "img",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- configErrorCompute ---

func TestConfigErrorCompute(t *testing.T) {
	c := configErrorCompute{err: errComputeNotConfigured}
	ctx := context.Background()

	if _, err := c.RunContainer(ctx, cloud.ContainerOpts{}); !errors.Is(err, errComputeNotConfigured) {
		t.Errorf("RunContainer = %v", err)
	}
	if _, err := c.RunSpotInstances(ctx, cloud.SpotOpts{}); !errors.Is(err, errComputeNotConfigured) {
		t.Errorf("RunSpotInstances = %v", err)
	}
	if _, err := c.GetSpotStatus(ctx, nil); !errors.Is(err, errComputeNotConfigured) {
		t.Errorf("GetSpotStatus = %v", err)
	}
}
