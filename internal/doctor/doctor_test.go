package doctor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"heph4estus/internal/operator"
)

// --- helpers ---

func found(path string) func(string) (string, error) {
	return func(name string) (string, error) { return path, nil }
}

func notFound() func(string) (string, error) {
	return func(name string) (string, error) { return "", fmt.Errorf("not found") }
}

func cmdOK(output string) func(context.Context, string, ...string) ([]byte, error) {
	return func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte(output), nil
	}
}

func cmdFail(msg string) func(context.Context, string, ...string) ([]byte, error) {
	return func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return nil, fmt.Errorf("%s", msg)
	}
}

func envWith(m map[string]string) func(string) string {
	return func(key string) string { return m[key] }
}

func emptyEnv() func(string) string {
	return func(string) string { return "" }
}

func cfgWith(cfg *operator.OperatorConfig) func() (*operator.OperatorConfig, error) {
	return func() (*operator.OperatorConfig, error) { return cfg, nil }
}

func cfgEmpty() func() (*operator.OperatorConfig, error) {
	return cfgWith(&operator.OperatorConfig{})
}

func baseDeps() Deps {
	return Deps{
		LookPath:   found("/usr/bin/thing"),
		RunCmd:     cmdOK("ok"),
		Getenv:     emptyEnv(),
		LoadConfig: cfgEmpty(),
		ConfigDir:  func() (string, error) { return os.TempDir(), nil },
	}
}

// --- binary checks ---

func TestCheckBinary_Found(t *testing.T) {
	d := baseDeps()
	d.LookPath = found("/usr/bin/terraform")
	r := checkBinary(d, "terraform")
	if r.Status != StatusPass {
		t.Fatalf("expected pass, got %s: %s", r.Status, r.Summary)
	}
	if r.Name != "terraform_binary" {
		t.Fatalf("unexpected name: %s", r.Name)
	}
}

func TestCheckBinary_NotFound(t *testing.T) {
	d := baseDeps()
	d.LookPath = notFound()
	r := checkBinary(d, "docker")
	if r.Status != StatusFail {
		t.Fatalf("expected fail, got %s: %s", r.Status, r.Summary)
	}
	if r.Fix == "" {
		t.Fatal("expected a fix suggestion")
	}
}

// --- docker daemon ---

func TestCheckDockerDaemon_Reachable(t *testing.T) {
	d := baseDeps()
	r := checkDockerDaemon(context.Background(), d)
	if r.Status != StatusPass {
		t.Fatalf("expected pass, got %s: %s", r.Status, r.Summary)
	}
}

func TestCheckDockerDaemon_Unreachable(t *testing.T) {
	d := baseDeps()
	d.RunCmd = cmdFail("connection refused")
	r := checkDockerDaemon(context.Background(), d)
	if r.Status != StatusFail {
		t.Fatalf("expected fail, got %s: %s", r.Status, r.Summary)
	}
}

func TestCheckDockerDaemon_NoBinary(t *testing.T) {
	d := baseDeps()
	d.LookPath = notFound()
	r := checkDockerDaemon(context.Background(), d)
	if r.Status != StatusFail {
		t.Fatalf("expected fail, got %s: %s", r.Status, r.Summary)
	}
}

// --- AWS region ---

func TestCheckAWSRegion_FromEnv(t *testing.T) {
	d := baseDeps()
	d.Getenv = envWith(map[string]string{"AWS_REGION": "us-west-2"})
	r := checkAWSRegion(d)
	if r.Status != StatusPass {
		t.Fatalf("expected pass, got %s: %s", r.Status, r.Summary)
	}
}

func TestCheckAWSRegion_FromDefaultRegionEnv(t *testing.T) {
	d := baseDeps()
	d.Getenv = envWith(map[string]string{"AWS_DEFAULT_REGION": "eu-west-1"})
	r := checkAWSRegion(d)
	if r.Status != StatusPass {
		t.Fatalf("expected pass, got %s: %s", r.Status, r.Summary)
	}
}

func TestCheckAWSRegion_FromConfig(t *testing.T) {
	d := baseDeps()
	d.LoadConfig = cfgWith(&operator.OperatorConfig{Region: "ap-southeast-1"})
	r := checkAWSRegion(d)
	if r.Status != StatusPass {
		t.Fatalf("expected pass, got %s: %s", r.Status, r.Summary)
	}
}

func TestCheckAWSRegion_Missing(t *testing.T) {
	d := baseDeps()
	r := checkAWSRegion(d)
	if r.Status != StatusFail {
		t.Fatalf("expected fail, got %s: %s", r.Status, r.Summary)
	}
	if r.Fix == "" {
		t.Fatal("expected fix")
	}
}

// --- AWS profile ---

func TestCheckAWSProfile_FromEnv(t *testing.T) {
	d := baseDeps()
	d.Getenv = envWith(map[string]string{"AWS_PROFILE": "prod"})
	r := checkAWSProfile(d)
	if r.Status != StatusPass {
		t.Fatalf("expected pass, got %s: %s", r.Status, r.Summary)
	}
}

func TestCheckAWSProfile_FromConfig(t *testing.T) {
	d := baseDeps()
	d.LoadConfig = cfgWith(&operator.OperatorConfig{Profile: "dev"})
	r := checkAWSProfile(d)
	if r.Status != StatusPass {
		t.Fatalf("expected pass, got %s: %s", r.Status, r.Summary)
	}
}

func TestCheckAWSProfile_Missing_Warns(t *testing.T) {
	d := baseDeps()
	r := checkAWSProfile(d)
	if r.Status != StatusWarn {
		t.Fatalf("expected warn, got %s: %s", r.Status, r.Summary)
	}
}

// --- STS identity ---

func TestCheckSTSIdentity_OK(t *testing.T) {
	d := baseDeps()
	r := checkSTSIdentity(context.Background(), d)
	if r.Status != StatusPass {
		t.Fatalf("expected pass, got %s: %s", r.Status, r.Summary)
	}
}

func TestCheckSTSIdentity_Fail(t *testing.T) {
	d := baseDeps()
	d.RunCmd = cmdFail("expired token")
	r := checkSTSIdentity(context.Background(), d)
	if r.Status != StatusFail {
		t.Fatalf("expected fail, got %s: %s", r.Status, r.Summary)
	}
}

func TestCheckSTSIdentity_NoAWSCLI(t *testing.T) {
	d := baseDeps()
	d.LookPath = notFound()
	r := checkSTSIdentity(context.Background(), d)
	if r.Status != StatusFail {
		t.Fatalf("expected fail, got %s: %s", r.Status, r.Summary)
	}
}

// --- config dir writable ---

func TestCheckConfigDirWritable_OK(t *testing.T) {
	tmp := t.TempDir()
	d := baseDeps()
	d.ConfigDir = func() (string, error) { return tmp, nil }
	r := checkConfigDirWritable(d)
	if r.Status != StatusPass {
		t.Fatalf("expected pass, got %s: %s", r.Status, r.Summary)
	}
}

func TestCheckConfigDirWritable_DirError(t *testing.T) {
	d := baseDeps()
	d.ConfigDir = func() (string, error) { return "", fmt.Errorf("no home") }
	r := checkConfigDirWritable(d)
	if r.Status != StatusFail {
		t.Fatalf("expected fail, got %s: %s", r.Status, r.Summary)
	}
}

// --- output dir ---

func TestCheckOutputDir_NotConfigured(t *testing.T) {
	d := baseDeps()
	r := checkOutputDir(d)
	if r.Status != StatusPass {
		t.Fatalf("expected pass, got %s: %s", r.Status, r.Summary)
	}
}

func TestCheckOutputDir_Creatable(t *testing.T) {
	tmp := t.TempDir()
	outDir := filepath.Join(tmp, "results")
	d := baseDeps()
	d.LoadConfig = cfgWith(&operator.OperatorConfig{OutputDir: outDir})
	r := checkOutputDir(d)
	if r.Status != StatusPass {
		t.Fatalf("expected pass, got %s: %s", r.Status, r.Summary)
	}
}

func TestCheckOutputDir_NotCreatable(t *testing.T) {
	d := baseDeps()
	d.LoadConfig = cfgWith(&operator.OperatorConfig{OutputDir: "/proc/nonexistent/deeply/nested"})
	r := checkOutputDir(d)
	if r.Status != StatusFail {
		t.Fatalf("expected fail, got %s: %s", r.Status, r.Summary)
	}
}

// --- RunAll and HasFailures ---

func TestRunAll_ReturnsAllChecks(t *testing.T) {
	d := baseDeps()
	d.Getenv = envWith(map[string]string{"AWS_REGION": "us-east-1"})
	results := RunAll(context.Background(), d)
	if len(results) != 9 {
		t.Fatalf("expected 9 checks, got %d", len(results))
	}
}

func TestHasFailures_NoFailures(t *testing.T) {
	results := []CheckResult{
		{Status: StatusPass},
		{Status: StatusWarn},
		{Status: StatusPass},
	}
	if HasFailures(results) {
		t.Fatal("expected no failures")
	}
}

func TestHasFailures_WithFailure(t *testing.T) {
	results := []CheckResult{
		{Status: StatusPass},
		{Status: StatusFail},
	}
	if !HasFailures(results) {
		t.Fatal("expected failures")
	}
}

func TestRunAll_OrderIsStable(t *testing.T) {
	d := baseDeps()
	d.Getenv = envWith(map[string]string{"AWS_REGION": "us-east-1"})
	results := RunAll(context.Background(), d)
	expected := []string{
		"terraform_binary",
		"docker_binary",
		"docker_daemon",
		"aws_binary",
		"aws_region",
		"aws_profile",
		"sts_identity",
		"config_dir",
		"output_dir",
	}
	for i, name := range expected {
		if results[i].Name != name {
			t.Errorf("check %d: expected %s, got %s", i, name, results[i].Name)
		}
	}
}
