package infra

import (
	"context"
	"errors"
	"io"
	"testing"
)

// --- test helpers ---

type nopLogger struct{}

func (nopLogger) Info(string, ...interface{})  {}
func (nopLogger) Error(string, ...interface{}) {}
func (nopLogger) Fatal(string, ...interface{}) {}

func newMockExecutor(stdout, stderr string, exitCode int, err error) CommandExecutor {
	return func(_ context.Context, _ string, _ io.Writer, _ ...string) (*CommandResult, error) {
		return &CommandResult{
			Stdout:   []byte(stdout),
			Stderr:   []byte(stderr),
			ExitCode: exitCode,
		}, err
	}
}

type mockCall struct {
	stdout   string
	stderr   string
	exitCode int
	err      error
}

func newSequentialMockExecutor(calls []mockCall) CommandExecutor {
	idx := 0
	return func(_ context.Context, _ string, _ io.Writer, _ ...string) (*CommandResult, error) {
		if idx >= len(calls) {
			return &CommandResult{}, errors.New("unexpected call to executor")
		}
		c := calls[idx]
		idx++
		return &CommandResult{
			Stdout:   []byte(c.stdout),
			Stderr:   []byte(c.stderr),
			ExitCode: c.exitCode,
		}, c.err
	}
}

// capturedArgs records the args of every invocation for assertion.
type capturedArgs struct {
	calls [][]string
}

func newCapturingExecutor(stdout string) (*capturedArgs, CommandExecutor) {
	ca := &capturedArgs{}
	return ca, func(_ context.Context, _ string, _ io.Writer, args ...string) (*CommandResult, error) {
		ca.calls = append(ca.calls, args)
		return &CommandResult{Stdout: []byte(stdout)}, nil
	}
}

func newCapturingSequentialExecutor(calls []mockCall) (*capturedArgs, CommandExecutor) {
	ca := &capturedArgs{}
	idx := 0
	return ca, func(_ context.Context, _ string, _ io.Writer, args ...string) (*CommandResult, error) {
		ca.calls = append(ca.calls, args)
		if idx >= len(calls) {
			return &CommandResult{}, errors.New("unexpected call to executor")
		}
		c := calls[idx]
		idx++
		return &CommandResult{
			Stdout:   []byte(c.stdout),
			Stderr:   []byte(c.stderr),
			ExitCode: c.exitCode,
		}, c.err
	}
}

// --- Terraform tests ---

func TestTerraformInit(t *testing.T) {
	cap, exec := newCapturingExecutor("")
	tc := &TerraformClient{runCmd: exec, logger: nopLogger{}}

	if err := tc.Init(context.Background(), "/work"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertArgs(t, cap.calls[0], "terraform", "init", "-input=false")
}

func TestTerraformInit_Error(t *testing.T) {
	tc := &TerraformClient{
		runCmd: newMockExecutor("", "error msg", 1, errors.New("exit 1")),
		logger: nopLogger{},
	}
	if err := tc.Init(context.Background(), "/work"); err == nil {
		t.Fatal("expected error")
	}
}

func TestTerraformPlan_WithVars(t *testing.T) {
	planOutput := `Terraform will perform the following actions:
Plan: 2 to add, 1 to change, 0 to destroy.`

	cap, exec := newCapturingExecutor(planOutput)
	tc := &TerraformClient{runCmd: exec, logger: nopLogger{}}

	summary, err := tc.Plan(context.Background(), "/work", map[string]string{"region": "us-east-1", "env": "dev"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if summary != "Plan: 2 to add, 1 to change, 0 to destroy." {
		t.Fatalf("unexpected summary: %s", summary)
	}

	args := cap.calls[0]
	assertContains(t, args, "terraform")
	assertContains(t, args, "plan")
	assertContains(t, args, "-input=false")
	assertContains(t, args, "-no-color")
	assertVarFlag(t, args, "region", "us-east-1")
	assertVarFlag(t, args, "env", "dev")
}

func TestTerraformPlan_NoChanges(t *testing.T) {
	tc := &TerraformClient{
		runCmd: newMockExecutor("No changes. Infrastructure is up-to-date.", "", 0, nil),
		logger: nopLogger{},
	}
	summary, err := tc.Plan(context.Background(), "/work", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if summary != "No changes." {
		t.Fatalf("expected 'No changes.', got: %s", summary)
	}
}

func TestTerraformPlan_Fallback(t *testing.T) {
	tc := &TerraformClient{
		runCmd: newMockExecutor("some unrecognized output", "", 0, nil),
		logger: nopLogger{},
	}
	summary, err := tc.Plan(context.Background(), "/work", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if summary != "Plan completed." {
		t.Fatalf("expected fallback summary, got: %s", summary)
	}
}

func TestTerraformPlan_Error(t *testing.T) {
	tc := &TerraformClient{
		runCmd: newMockExecutor("", "err", 1, errors.New("exit 1")),
		logger: nopLogger{},
	}
	if _, err := tc.Plan(context.Background(), "/work", nil); err == nil {
		t.Fatal("expected error")
	}
}

func TestTerraformApply(t *testing.T) {
	cap, exec := newCapturingExecutor("")
	tc := &TerraformClient{runCmd: exec, logger: nopLogger{}}

	if err := tc.Apply(context.Background(), "/work", map[string]string{"env": "dev"}, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	args := cap.calls[0]
	assertContains(t, args, "apply")
	assertContains(t, args, "-auto-approve")
	assertVarFlag(t, args, "env", "dev")
}

func TestTerraformApply_Error(t *testing.T) {
	tc := &TerraformClient{
		runCmd: newMockExecutor("", "err", 1, errors.New("exit 1")),
		logger: nopLogger{},
	}
	if err := tc.Apply(context.Background(), "/work", nil, nil); err == nil {
		t.Fatal("expected error")
	}
}

func TestTerraformDestroy(t *testing.T) {
	cap, exec := newCapturingExecutor("")
	tc := &TerraformClient{runCmd: exec, logger: nopLogger{}}

	if err := tc.Destroy(context.Background(), "/work", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertArgs(t, cap.calls[0], "terraform", "destroy", "-auto-approve", "-input=false", "-no-color")
}

func TestTerraformDestroy_Error(t *testing.T) {
	tc := &TerraformClient{
		runCmd: newMockExecutor("", "err", 1, errors.New("exit 1")),
		logger: nopLogger{},
	}
	if err := tc.Destroy(context.Background(), "/work", nil); err == nil {
		t.Fatal("expected error")
	}
}

func TestTerraformReadOutputs(t *testing.T) {
	jsonOut := `{"vpc_id":{"value":"vpc-123","type":"string"},"subnet_id":{"value":"subnet-456","type":"string"}}`
	tc := &TerraformClient{
		runCmd: newMockExecutor(jsonOut, "", 0, nil),
		logger: nopLogger{},
	}

	outputs, err := tc.ReadOutputs(context.Background(), "/work")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outputs["vpc_id"] != "vpc-123" {
		t.Fatalf("expected vpc-123, got %s", outputs["vpc_id"])
	}
	if outputs["subnet_id"] != "subnet-456" {
		t.Fatalf("expected subnet-456, got %s", outputs["subnet_id"])
	}
}

func TestTerraformReadOutputs_Empty(t *testing.T) {
	tc := &TerraformClient{
		runCmd: newMockExecutor("{}", "", 0, nil),
		logger: nopLogger{},
	}
	outputs, err := tc.ReadOutputs(context.Background(), "/work")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(outputs) != 0 {
		t.Fatalf("expected empty map, got %v", outputs)
	}
}

func TestTerraformReadOutputs_InvalidJSON(t *testing.T) {
	tc := &TerraformClient{
		runCmd: newMockExecutor("not json", "", 0, nil),
		logger: nopLogger{},
	}
	if _, err := tc.ReadOutputs(context.Background(), "/work"); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestTerraformReadOutputs_Error(t *testing.T) {
	tc := &TerraformClient{
		runCmd: newMockExecutor("", "err", 1, errors.New("exit 1")),
		logger: nopLogger{},
	}
	if _, err := tc.ReadOutputs(context.Background(), "/work"); err == nil {
		t.Fatal("expected error")
	}
}

// --- Docker tests ---

func TestDockerBuild(t *testing.T) {
	cap, exec := newCapturingExecutor("")
	dc := &DockerClient{runCmd: exec, logger: nopLogger{}}

	if err := dc.Build(context.Background(), "Dockerfile", ".", "myimg:latest", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertArgs(t, cap.calls[0], "docker", "build", "-f", "Dockerfile", "-t", "myimg:latest", ".")
}

func TestDockerBuildWithArgs(t *testing.T) {
	cap, exec := newCapturingExecutor("")
	dc := &DockerClient{runCmd: exec, logger: nopLogger{}}

	err := dc.BuildWithArgs(context.Background(), "Dockerfile", ".", "img:latest", map[string]string{
		"RUNTIME_INSTALL_CMD": "apk add --no-cache nmap",
		"GO_INSTALL_CMD":      "go install github.com/example/tool@v1.0.0",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertArgs(t, cap.calls[0],
		"docker", "build", "-f", "Dockerfile", "-t", "img:latest",
		"--build-arg", "GO_INSTALL_CMD=go install github.com/example/tool@v1.0.0",
		"--build-arg", "RUNTIME_INSTALL_CMD=apk add --no-cache nmap",
		".",
	)
}

func TestDockerBuild_Error(t *testing.T) {
	dc := &DockerClient{
		runCmd: newMockExecutor("", "err", 1, errors.New("exit 1")),
		logger: nopLogger{},
	}
	if err := dc.Build(context.Background(), "Dockerfile", ".", "img", nil); err == nil {
		t.Fatal("expected error")
	}
}

func TestDockerTag(t *testing.T) {
	cap, exec := newCapturingExecutor("")
	dc := &DockerClient{runCmd: exec, logger: nopLogger{}}

	if err := dc.Tag(context.Background(), "src:v1", "dst:v1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertArgs(t, cap.calls[0], "docker", "tag", "src:v1", "dst:v1")
}

func TestDockerTag_Error(t *testing.T) {
	dc := &DockerClient{
		runCmd: newMockExecutor("", "err", 1, errors.New("exit 1")),
		logger: nopLogger{},
	}
	if err := dc.Tag(context.Background(), "a", "b"); err == nil {
		t.Fatal("expected error")
	}
}

func TestDockerPush(t *testing.T) {
	cap, exec := newCapturingExecutor("")
	dc := &DockerClient{runCmd: exec, logger: nopLogger{}}

	if err := dc.Push(context.Background(), "img:latest", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertArgs(t, cap.calls[0], "docker", "push", "img:latest")
}

func TestDockerPush_Error(t *testing.T) {
	dc := &DockerClient{
		runCmd: newMockExecutor("", "err", 1, errors.New("exit 1")),
		logger: nopLogger{},
	}
	if err := dc.Push(context.Background(), "img", nil); err == nil {
		t.Fatal("expected error")
	}
}

// --- ECR tests ---

func TestECRAuthenticate(t *testing.T) {
	cap, exec := newCapturingSequentialExecutor([]mockCall{
		{stdout: "mytoken\n"},
		{stdout: "123456789012\n"},
		{stdout: "Login Succeeded\n"},
	})
	ec := &ECRClient{runCmd: exec, logger: nopLogger{}}

	if err := ec.Authenticate(context.Background(), "us-east-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cap.calls) != 3 {
		t.Fatalf("expected 3 calls, got %d", len(cap.calls))
	}

	// Step 1: get-login-password
	assertContains(t, cap.calls[0], "ecr")
	assertContains(t, cap.calls[0], "get-login-password")
	assertContains(t, cap.calls[0], "--region")
	assertContains(t, cap.calls[0], "us-east-1")

	// Step 2: get-caller-identity
	assertContains(t, cap.calls[1], "sts")
	assertContains(t, cap.calls[1], "get-caller-identity")

	// Step 3: docker login
	assertContains(t, cap.calls[2], "docker")
	assertContains(t, cap.calls[2], "login")
	assertContains(t, cap.calls[2], "--password")
	assertContains(t, cap.calls[2], "mytoken")
	assertContains(t, cap.calls[2], "123456789012.dkr.ecr.us-east-1.amazonaws.com")
}

func TestECRAuthenticate_GetLoginPasswordError(t *testing.T) {
	ec := &ECRClient{
		runCmd: newSequentialMockExecutor([]mockCall{
			{stderr: "err", exitCode: 1, err: errors.New("exit 1")},
		}),
		logger: nopLogger{},
	}
	if err := ec.Authenticate(context.Background(), "us-east-1"); err == nil {
		t.Fatal("expected error")
	}
}

func TestECRAuthenticate_GetCallerIdentityError(t *testing.T) {
	ec := &ECRClient{
		runCmd: newSequentialMockExecutor([]mockCall{
			{stdout: "token"},
			{stderr: "err", exitCode: 1, err: errors.New("exit 1")},
		}),
		logger: nopLogger{},
	}
	if err := ec.Authenticate(context.Background(), "us-east-1"); err == nil {
		t.Fatal("expected error")
	}
}

func TestECRAuthenticate_DockerLoginError(t *testing.T) {
	ec := &ECRClient{
		runCmd: newSequentialMockExecutor([]mockCall{
			{stdout: "token"},
			{stdout: "123456789012"},
			{stderr: "err", exitCode: 1, err: errors.New("exit 1")},
		}),
		logger: nopLogger{},
	}
	if err := ec.Authenticate(context.Background(), "us-east-1"); err == nil {
		t.Fatal("expected error")
	}
}

func TestECRGetRepoURI(t *testing.T) {
	cap, exec := newCapturingExecutor("123456789012.dkr.ecr.us-east-1.amazonaws.com/myrepo\n")
	ec := &ECRClient{runCmd: exec, logger: nopLogger{}}

	uri, err := ec.GetRepoURI(context.Background(), "us-east-1", "myrepo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if uri != "123456789012.dkr.ecr.us-east-1.amazonaws.com/myrepo" {
		t.Fatalf("unexpected URI: %s", uri)
	}
	assertContains(t, cap.calls[0], "describe-repositories")
	assertContains(t, cap.calls[0], "--repository-names")
	assertContains(t, cap.calls[0], "myrepo")
	assertContains(t, cap.calls[0], "--region")
	assertContains(t, cap.calls[0], "us-east-1")
}

func TestECRGetRepoURI_Error(t *testing.T) {
	ec := &ECRClient{
		runCmd: newMockExecutor("", "err", 1, errors.New("exit 1")),
		logger: nopLogger{},
	}
	if _, err := ec.GetRepoURI(context.Background(), "us-east-1", "myrepo"); err == nil {
		t.Fatal("expected error")
	}
}

// --- parsePlanSummary tests ---

func TestParsePlanSummary(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{"changes", "blah\nPlan: 3 to add, 1 to change, 0 to destroy.\nblah", "Plan: 3 to add, 1 to change, 0 to destroy."},
		{"no changes", "No changes. Infrastructure is up-to-date.", "No changes."},
		{"fallback", "some unexpected output", "Plan completed."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parsePlanSummary(tt.input)
			if got != tt.expect {
				t.Fatalf("expected %q, got %q", tt.expect, got)
			}
		})
	}
}

// --- assertion helpers ---

func assertArgs(t *testing.T, got []string, expected ...string) {
	t.Helper()
	if len(got) != len(expected) {
		t.Fatalf("expected args %v, got %v", expected, got)
	}
	for i := range expected {
		if got[i] != expected[i] {
			t.Fatalf("arg[%d]: expected %q, got %q", i, expected[i], got[i])
		}
	}
}

func assertContains(t *testing.T, args []string, want string) {
	t.Helper()
	for _, a := range args {
		if a == want {
			return
		}
	}
	t.Fatalf("expected args to contain %q, got %v", want, args)
}

// assertVarFlag checks that a -var flag with the given key=value is present (order-independent).
func assertVarFlag(t *testing.T, args []string, key, value string) {
	t.Helper()
	expected := key + "=" + value
	for i, a := range args {
		if a == "-var" && i+1 < len(args) && args[i+1] == expected {
			return
		}
	}
	t.Fatalf("expected -var %s in args %v", expected, args)
}
