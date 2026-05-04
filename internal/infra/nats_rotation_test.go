package infra

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

type recordingRemoteRunner struct {
	host string
	cmd  string
	err  error
}

func (r *recordingRemoteRunner) Run(_ context.Context, host string, remoteCmd string) error {
	r.host = host
	r.cmd = remoteCmd
	return r.err
}

func TestGenerateNATSCredentials(t *testing.T) {
	creds, err := GenerateNATSCredentials(time.Date(2026, 5, 3, 22, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("GenerateNATSCredentials: %v", err)
	}
	if !strings.HasPrefix(creds.OperatorUser, "heph-operator-") {
		t.Fatalf("OperatorUser = %q", creds.OperatorUser)
	}
	if !strings.HasPrefix(creds.WorkerUser, "heph-worker-") {
		t.Fatalf("WorkerUser = %q", creds.WorkerUser)
	}
	if !strings.HasPrefix(creds.Generation, "nats-20260503t220000z-") {
		t.Fatalf("Generation = %q", creds.Generation)
	}
	if creds.RotatedAt != "2026-05-03T22:00:00Z" {
		t.Fatalf("RotatedAt = %q", creds.RotatedAt)
	}
	if len(creds.OperatorPassword) != 32 || len(creds.WorkerPassword) != 32 {
		t.Fatalf("password lengths = %d/%d, want 32/32", len(creds.OperatorPassword), len(creds.WorkerPassword))
	}
}

func TestNATSTerraformVars(t *testing.T) {
	creds := testNATSCredentials()
	vars := NATSTerraformVars(creds)
	want := map[string]string{
		"nats_operator_user_override":     creds.OperatorUser,
		"nats_operator_password_override": creds.OperatorPassword,
		"nats_worker_user_override":       creds.WorkerUser,
		"nats_worker_password_override":   creds.WorkerPassword,
		"nats_credential_generation":      creds.Generation,
		"nats_credential_rotated_at":      creds.RotatedAt,
	}
	for key, value := range want {
		if vars[key] != value {
			t.Fatalf("vars[%s] = %q, want %q", key, vars[key], value)
		}
	}
}

func TestNATSAuthConfigTLS(t *testing.T) {
	cfg := NATSAuthConfig(testNATSCredentials(), true)
	for _, want := range []string{
		"tls {",
		`cert_file: "/tls/public.crt"`,
		`{ user: "heph-operator-new", password: "operator-secret" },`,
		`{ user: "heph-worker-new", password: "worker-secret" }`,
	} {
		if !strings.Contains(cfg, want) {
			t.Fatalf("config missing %q:\n%s", want, cfg)
		}
	}
}

func TestUpdateControllerNATSAuthGrace(t *testing.T) {
	runner := &recordingRemoteRunner{}
	err := UpdateControllerNATSAuth(context.Background(), runner, "1.2.3.4", NATSControllerAuthUpdate{
		Credentials: testNATSCredentials(),
		Mode:        NATSAuthUpdateGrace,
	})
	if err != nil {
		t.Fatalf("UpdateControllerNATSAuth: %v", err)
	}
	if runner.host != "1.2.3.4" {
		t.Fatalf("host = %q", runner.host)
	}
	for _, want := range []string{
		"awk -v operator_line=",
		`user: "heph-operator-new"`,
		"systemctl restart nats",
	} {
		if !strings.Contains(runner.cmd, want) {
			t.Fatalf("command missing %q:\n%s", want, runner.cmd)
		}
	}
}

func TestUpdateControllerNATSAuthFinal(t *testing.T) {
	runner := &recordingRemoteRunner{}
	err := UpdateControllerNATSAuth(context.Background(), runner, "1.2.3.4", NATSControllerAuthUpdate{
		Credentials: testNATSCredentials(),
		TLSEnabled:  true,
		Mode:        NATSAuthUpdateFinal,
	})
	if err != nil {
		t.Fatalf("UpdateControllerNATSAuth: %v", err)
	}
	for _, want := range []string{
		"<<'HEPH_NATS_AUTH'",
		"tls {",
		`{ user: "heph-worker-new", password: "worker-secret" }`,
		"systemctl restart nats",
	} {
		if !strings.Contains(runner.cmd, want) {
			t.Fatalf("command missing %q:\n%s", want, runner.cmd)
		}
	}
}

func TestUpdateControllerNATSAuthReturnsRunnerError(t *testing.T) {
	runner := &recordingRemoteRunner{err: errors.New("boom")}
	err := UpdateControllerNATSAuth(context.Background(), runner, "1.2.3.4", NATSControllerAuthUpdate{
		Credentials: testNATSCredentials(),
		Mode:        NATSAuthUpdateFinal,
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestSSHRemoteRunnerBuildsSSHCommand(t *testing.T) {
	var gotArgs []string
	runner := SSHRemoteRunner{
		User:    "root",
		KeyPath: "/tmp/key",
		Port:    2222,
		RunCmd: func(_ context.Context, _ string, _ io.Writer, args ...string) (*CommandResult, error) {
			gotArgs = append([]string(nil), args...)
			return &CommandResult{}, nil
		},
	}
	if err := runner.Run(context.Background(), "1.2.3.4", "echo ok"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, want := range []string{"ssh", "-i", "/tmp/key", "-p", "2222", "root@1.2.3.4", "echo ok"} {
		if !containsString(gotArgs, want) {
			t.Fatalf("args missing %q: %v", want, gotArgs)
		}
	}
}

func testNATSCredentials() NATSCredentials {
	return NATSCredentials{
		OperatorUser:     "heph-operator-new",
		OperatorPassword: "operator-secret",
		WorkerUser:       "heph-worker-new",
		WorkerPassword:   "worker-secret",
		Generation:       "nats-test-generation",
		RotatedAt:        "2026-05-03T22:00:00Z",
	}
}
