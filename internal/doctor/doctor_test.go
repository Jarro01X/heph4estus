package doctor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"heph4estus/internal/cloud"
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
	if len(results) != 15 {
		t.Fatalf("expected 15 checks, got %d", len(results))
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
		"hetzner_token",
		"hetzner_ssh_key",
		"linode_token",
		"linode_ssh_key",
		"vultr_api_key",
		"vultr_ssh_key",
	}
	for i, name := range expected {
		if results[i].Name != name {
			t.Errorf("check %d: expected %s, got %s", i, name, results[i].Name)
		}
	}
}

// --- Hetzner token ---

func TestCheckHetznerToken_Set(t *testing.T) {
	d := baseDeps()
	d.Getenv = envWith(map[string]string{"HCLOUD_TOKEN": "test-token-abc123"})
	r := checkHetznerToken(d)
	if r.Status != StatusPass {
		t.Fatalf("expected pass, got %s: %s", r.Status, r.Summary)
	}
	if r.Name != "hetzner_token" {
		t.Fatalf("unexpected name: %s", r.Name)
	}
}

func TestCheckHetznerToken_Unset(t *testing.T) {
	d := baseDeps()
	r := checkHetznerToken(d)
	if r.Status != StatusWarn {
		t.Fatalf("expected warn, got %s: %s", r.Status, r.Summary)
	}
	if r.Fix == "" {
		t.Fatal("expected a fix suggestion")
	}
}

// --- Hetzner SSH key ---

func TestCheckHetznerSSHKey_Found(t *testing.T) {
	tmp := t.TempDir()
	sshDir := filepath.Join(tmp, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sshDir, "id_ed25519.pub"), []byte("ssh-ed25519 AAAA..."), 0o644); err != nil {
		t.Fatal(err)
	}
	d := baseDeps()
	d.Getenv = envWith(map[string]string{"HOME": tmp})
	r := checkHetznerSSHKey(d)
	if r.Status != StatusPass {
		t.Fatalf("expected pass, got %s: %s", r.Status, r.Summary)
	}
	if r.Name != "hetzner_ssh_key" {
		t.Fatalf("unexpected name: %s", r.Name)
	}
}

func TestCheckHetznerSSHKey_Missing(t *testing.T) {
	tmp := t.TempDir()
	d := baseDeps()
	d.Getenv = envWith(map[string]string{"HOME": tmp})
	r := checkHetznerSSHKey(d)
	if r.Status != StatusWarn {
		t.Fatalf("expected warn, got %s: %s", r.Status, r.Summary)
	}
	if r.Fix == "" {
		t.Fatal("expected a fix suggestion")
	}
}

// --- Linode token ---

func TestCheckLinodeToken_Set(t *testing.T) {
	d := baseDeps()
	d.Getenv = envWith(map[string]string{"LINODE_TOKEN": "test-token-abc123"})
	r := checkLinodeToken(d)
	if r.Status != StatusPass {
		t.Fatalf("expected pass, got %s: %s", r.Status, r.Summary)
	}
	if r.Name != "linode_token" {
		t.Fatalf("unexpected name: %s", r.Name)
	}
}

func TestCheckLinodeToken_Unset(t *testing.T) {
	d := baseDeps()
	r := checkLinodeToken(d)
	if r.Status != StatusWarn {
		t.Fatalf("expected warn, got %s: %s", r.Status, r.Summary)
	}
	if r.Fix == "" {
		t.Fatal("expected a fix suggestion")
	}
}

// --- Vultr API key ---

func TestCheckVultrAPIKey_Set(t *testing.T) {
	d := baseDeps()
	d.Getenv = envWith(map[string]string{"VULTR_API_KEY": "test-key-abc123"})
	r := checkVultrAPIKey(d)
	if r.Status != StatusPass {
		t.Fatalf("expected pass, got %s: %s", r.Status, r.Summary)
	}
	if r.Name != "vultr_api_key" {
		t.Fatalf("unexpected name: %s", r.Name)
	}
}

func TestCheckVultrAPIKey_Unset(t *testing.T) {
	d := baseDeps()
	r := checkVultrAPIKey(d)
	if r.Status != StatusWarn {
		t.Fatalf("expected warn, got %s: %s", r.Status, r.Summary)
	}
	if r.Fix == "" {
		t.Fatal("expected a fix suggestion")
	}
}

// --- RunForCloud filtering ---

func TestRunForCloud_AWS(t *testing.T) {
	d := baseDeps()
	d.Getenv = envWith(map[string]string{"AWS_REGION": "us-east-1"})
	results := RunForCloud(context.Background(), d, cloud.KindAWS)

	// AWS should include aws_binary, aws_region, aws_profile, sts_identity
	// but NOT hetzner_token, linode_token, vultr_api_key
	names := checkNames(results)
	assertContains(t, names, "aws_binary")
	assertContains(t, names, "aws_region")
	assertNotContains(t, names, "hetzner_token")
	assertNotContains(t, names, "linode_token")
	assertNotContains(t, names, "vultr_api_key")
}

func TestRunForCloud_Hetzner(t *testing.T) {
	d := baseDeps()
	d.Getenv = envWith(map[string]string{"HCLOUD_TOKEN": "tok", "HOME": t.TempDir()})
	results := RunForCloud(context.Background(), d, cloud.KindHetzner)

	names := checkNames(results)
	assertContains(t, names, "hetzner_token")
	assertContains(t, names, "hetzner_ssh_key")
	assertContains(t, names, "controller_reachable")
	assertContains(t, names, "nats_auth")
	assertContains(t, names, "registry_exposure")
	// AWS-specific checks should not be present.
	assertNotContains(t, names, "aws_binary")
	assertNotContains(t, names, "aws_region")
	assertNotContains(t, names, "linode_token")
	assertNotContains(t, names, "vultr_api_key")
}

func TestRunForCloud_Linode(t *testing.T) {
	d := baseDeps()
	results := RunForCloud(context.Background(), d, cloud.KindLinode)

	names := checkNames(results)
	assertContains(t, names, "linode_token")
	assertContains(t, names, "linode_ssh_key")
	assertContains(t, names, "controller_reachable")
	assertNotContains(t, names, "aws_binary")
	assertNotContains(t, names, "hetzner_token")
	assertNotContains(t, names, "vultr_api_key")
}

func TestRunForCloud_Vultr(t *testing.T) {
	d := baseDeps()
	results := RunForCloud(context.Background(), d, cloud.KindVultr)

	names := checkNames(results)
	assertContains(t, names, "vultr_api_key")
	assertContains(t, names, "vultr_ssh_key")
	assertContains(t, names, "controller_reachable")
	assertNotContains(t, names, "aws_binary")
	assertNotContains(t, names, "hetzner_token")
	assertNotContains(t, names, "linode_token")
}

func TestRunForCloud_Manual(t *testing.T) {
	d := baseDeps()
	results := RunForCloud(context.Background(), d, cloud.KindManual)

	names := checkNames(results)
	assertContains(t, names, "controller_reachable")
	assertContains(t, names, "nats_auth")
	assertNotContains(t, names, "aws_binary")
	assertNotContains(t, names, "hetzner_token")
}

// --- Security checks ---

func TestCheckNATSAuth_Configured(t *testing.T) {
	d := baseDeps()
	d.Getenv = envWith(map[string]string{"NATS_USER": "heph", "NATS_PASSWORD": "secret"})
	r := checkNATSAuth(d)
	if r.Status != StatusPass {
		t.Fatalf("expected pass, got %s: %s", r.Status, r.Summary)
	}
}

func TestCheckNATSAuth_Missing(t *testing.T) {
	d := baseDeps()
	r := checkNATSAuth(d)
	if r.Status != StatusWarn {
		t.Fatalf("expected warn, got %s: %s", r.Status, r.Summary)
	}
}

func TestCheckRegistryExposure_HTTPS(t *testing.T) {
	d := baseDeps()
	d.Getenv = envWith(map[string]string{"REGISTRY_URL": "https://registry.example.com:5000"})
	r := checkRegistryExposure(d)
	if r.Status != StatusPass {
		t.Fatalf("expected pass, got %s: %s", r.Status, r.Summary)
	}
}

func TestCheckRegistryExposure_HTTP(t *testing.T) {
	d := baseDeps()
	d.Getenv = envWith(map[string]string{"REGISTRY_URL": "http://1.2.3.4:5000"})
	r := checkRegistryExposure(d)
	if r.Status != StatusWarn {
		t.Fatalf("expected warn, got %s: %s", r.Status, r.Summary)
	}
}

func TestCheckRegistryExposure_Unset(t *testing.T) {
	d := baseDeps()
	r := checkRegistryExposure(d)
	if r.Status != StatusWarn {
		t.Fatalf("expected warn, got %s: %s", r.Status, r.Summary)
	}
}

func TestRunProviderNativeOutputChecks_PrivateAuth(t *testing.T) {
	results := RunProviderNativeOutputChecks(cloud.KindHetzner, map[string]string{
		"controller_security_mode": "private-auth",
		"nats_url":                 "nats://heph:secret@10.0.1.2:4222",
		"nats_user":                "heph",
		"nats_password":            "secret",
		"nats_tls_enabled":         "false",
		"nats_auth_enabled":        "true",
		"s3_endpoint":              "http://10.0.1.2:9000",
		"minio_tls_enabled":        "false",
		"registry_url":             "10.0.1.2:5000",
		"registry_tls_enabled":     "false",
		"registry_auth_enabled":    "false",
	})

	names := checkNames(results)
	assertContains(t, names, "controller_security_mode")
	assertContains(t, names, "nats_auth_posture")
	assertContains(t, names, "nats_tls_posture")
	assertContains(t, names, "minio_tls_posture")
	assertContains(t, names, "registry_tls_posture")
	assertContains(t, names, "registry_auth_posture")

	if results[0].Status != StatusWarn {
		t.Fatalf("private-auth mode should warn, got %s", results[0].Status)
	}
	if HasFailures(results) {
		t.Fatalf("private-auth compatibility outputs should warn but not fail: %+v", results)
	}
}

func TestRunProviderNativeOutputChecks_TLSModeRequiresTLSEndpoints(t *testing.T) {
	results := RunProviderNativeOutputChecks(cloud.KindHetzner, map[string]string{
		"controller_security_mode": "tls",
		"nats_url":                 "nats://heph:secret@10.0.1.2:4222",
		"nats_user":                "heph",
		"nats_password":            "secret",
		"nats_auth_enabled":        "true",
		"s3_endpoint":              "http://10.0.1.2:9000",
		"registry_url":             "10.0.1.2:5000",
	})

	if !HasFailures(results) {
		t.Fatalf("tls mode without TLS endpoints should fail: %+v", results)
	}
}

func TestCheckControllerReachable_Unset(t *testing.T) {
	d := baseDeps()
	r := checkControllerReachable(d)
	if r.Status != StatusWarn {
		t.Fatalf("expected warn, got %s: %s", r.Status, r.Summary)
	}
}

// --- test helpers ---

func checkNames(results []CheckResult) []string {
	names := make([]string, len(results))
	for i, r := range results {
		names[i] = r.Name
	}
	return names
}

func assertContains(t *testing.T, names []string, want string) {
	t.Helper()
	for _, n := range names {
		if n == want {
			return
		}
	}
	t.Errorf("expected %q in check names %v", want, names)
}

func assertNotContains(t *testing.T, names []string, want string) {
	t.Helper()
	for _, n := range names {
		if n == want {
			t.Errorf("did not expect %q in check names", want)
			return
		}
	}
}
