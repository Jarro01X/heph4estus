package infra

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type NATSCredentials struct {
	OperatorUser     string
	OperatorPassword string
	WorkerUser       string
	WorkerPassword   string
	Generation       string
	RotatedAt        string
}

type NATSAuthUpdateMode string

const (
	NATSAuthUpdateGrace NATSAuthUpdateMode = "grace"
	NATSAuthUpdateFinal NATSAuthUpdateMode = "final"
)

type NATSControllerAuthUpdate struct {
	Credentials NATSCredentials
	TLSEnabled  bool
	MTLSEnabled bool
	Mode        NATSAuthUpdateMode
}

type RemoteCommandRunner interface {
	Run(ctx context.Context, host string, remoteCmd string) error
}

type SSHRemoteRunner struct {
	User    string
	KeyPath string
	Port    int
	RunCmd  CommandExecutor
}

func (r SSHRemoteRunner) Run(ctx context.Context, host string, remoteCmd string) error {
	if strings.TrimSpace(host) == "" {
		return fmt.Errorf("ssh host is required")
	}
	if strings.TrimSpace(r.User) == "" {
		return fmt.Errorf("ssh user is required")
	}
	if strings.TrimSpace(r.KeyPath) == "" {
		return fmt.Errorf("ssh key path is required")
	}
	port := r.Port
	if port == 0 {
		port = 22
	}
	runCmd := r.RunCmd
	if runCmd == nil {
		runCmd = DefaultExecutor
	}
	args := []string{
		"ssh",
		"-i", r.KeyPath,
		"-p", strconv.Itoa(port),
		"-o", "StrictHostKeyChecking=no",
		"-o", "BatchMode=yes",
		fmt.Sprintf("%s@%s", r.User, host),
		remoteCmd,
	}
	result, err := runCmd(ctx, "", nil, args...)
	if err != nil {
		stderr := ""
		if result != nil {
			stderr = strings.TrimSpace(string(result.Stderr))
		}
		return fmt.Errorf("ssh %s: %w: %s", host, err, stderr)
	}
	return nil
}

func GenerateNATSCredentials(now time.Time) (NATSCredentials, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	suffix, err := randomHex(4)
	if err != nil {
		return NATSCredentials{}, err
	}
	operatorPassword, err := randomAlphaNum(32)
	if err != nil {
		return NATSCredentials{}, err
	}
	workerPassword, err := randomAlphaNum(32)
	if err != nil {
		return NATSCredentials{}, err
	}
	generation := fmt.Sprintf("nats-%s-%s", now.UTC().Format("20060102t150405z"), suffix)
	return NATSCredentials{
		OperatorUser:     "heph-operator-" + suffix,
		OperatorPassword: operatorPassword,
		WorkerUser:       "heph-worker-" + suffix,
		WorkerPassword:   workerPassword,
		Generation:       generation,
		RotatedAt:        now.UTC().Format(time.RFC3339),
	}, nil
}

func NATSTerraformVars(creds NATSCredentials) map[string]string {
	return map[string]string{
		"nats_operator_user_override":     creds.OperatorUser,
		"nats_operator_password_override": creds.OperatorPassword,
		"nats_worker_user_override":       creds.WorkerUser,
		"nats_worker_password_override":   creds.WorkerPassword,
		"nats_credential_generation":      creds.Generation,
		"nats_credential_rotated_at":      creds.RotatedAt,
	}
}

func UpdateControllerNATSAuth(ctx context.Context, runner RemoteCommandRunner, host string, update NATSControllerAuthUpdate) error {
	if runner == nil {
		return fmt.Errorf("remote runner is required")
	}
	if err := validateNATSCredentials(update.Credentials); err != nil {
		return err
	}
	var cmd string
	switch update.Mode {
	case NATSAuthUpdateGrace:
		cmd = natsGraceAuthCommand(update.Credentials)
	case NATSAuthUpdateFinal:
		cmd = natsFinalAuthCommandWithMTLS(update.Credentials, update.TLSEnabled, update.MTLSEnabled)
	default:
		return fmt.Errorf("unsupported NATS auth update mode %q", update.Mode)
	}
	return runner.Run(ctx, host, cmd)
}

func DefaultSSHPrivateKeyPath() string {
	for _, env := range []string{"HEPH_SSH_PRIVATE_KEY_PATH", "SSH_PRIVATE_KEY_PATH", "SELFHOSTED_SSH_KEY_PATH"} {
		if path := strings.TrimSpace(os.Getenv(env)); path != "" {
			return path
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	for _, name := range []string{"id_ed25519", "id_rsa"} {
		path := filepath.Join(home, ".ssh", name)
		if fileExists(path) {
			return path
		}
	}
	return ""
}

func NATSAuthConfig(creds NATSCredentials, tlsEnabled, mtlsEnabled bool) string {
	var b strings.Builder
	if tlsEnabled {
		b.WriteString(`tls {
  cert_file: "/tls/public.crt"
  key_file: "/tls/private.key"
  ca_file: "/tls/ca.crt"
`)
		if mtlsEnabled {
			b.WriteString(`  verify: true
`)
		}
		b.WriteString(`
}

`)
	}
	b.WriteString("authorization {\n")
	b.WriteString("  users = [\n")
	_, _ = fmt.Fprintf(&b, "    { user: %s, password: %s },\n", strconv.Quote(creds.OperatorUser), strconv.Quote(creds.OperatorPassword))
	_, _ = fmt.Fprintf(&b, "    { user: %s, password: %s }\n", strconv.Quote(creds.WorkerUser), strconv.Quote(creds.WorkerPassword))
	b.WriteString("  ]\n")
	b.WriteString("}\n")
	return b.String()
}

func natsGraceAuthCommand(creds NATSCredentials) string {
	operatorLine := fmt.Sprintf("          { user: %s, password: %s },", strconv.Quote(creds.OperatorUser), strconv.Quote(creds.OperatorPassword))
	workerLine := fmt.Sprintf("          { user: %s, password: %s },", strconv.Quote(creds.WorkerUser), strconv.Quote(creds.WorkerPassword))
	return strings.Join([]string{
		"set -eu",
		"auth=/data/nats/auth.conf",
		"backup=/data/nats/auth.conf.pre-rotate-" + creds.Generation,
		`tmp=$(mktemp)`,
		`cp "$auth" "$backup"`,
		fmt.Sprintf("if ! grep -Fq %s \"$auth\"; then", shellQuote(`user: "`+creds.OperatorUser+`"`)),
		fmt.Sprintf("  awk -v operator_line=%s -v worker_line=%s 'BEGIN { inserted=0 } /users[[:space:]]*=[[:space:]]*\\[/ && !inserted { print; print operator_line; print worker_line; inserted=1; next } { print }' \"$auth\" > \"$tmp\"", shellQuote(operatorLine), shellQuote(workerLine)),
		`  install -m 0600 "$tmp" "$auth"`,
		"fi",
		`rm -f "$tmp"`,
		"systemctl restart nats",
	}, "\n")
}

func natsFinalAuthCommand(creds NATSCredentials, tlsEnabled bool) string {
	return natsFinalAuthCommandWithMTLS(creds, tlsEnabled, false)
}

func natsFinalAuthCommandWithMTLS(creds NATSCredentials, tlsEnabled, mtlsEnabled bool) string {
	return strings.Join([]string{
		"set -eu",
		"auth=/data/nats/auth.conf",
		"backup=/data/nats/auth.conf.final-pre-rotate-" + creds.Generation,
		`tmp=$(mktemp)`,
		`cp "$auth" "$backup"`,
		`cat > "$tmp" <<'HEPH_NATS_AUTH'`,
		NATSAuthConfig(creds, tlsEnabled, mtlsEnabled),
		"HEPH_NATS_AUTH",
		`install -m 0600 "$tmp" "$auth"`,
		`rm -f "$tmp"`,
		"systemctl restart nats",
	}, "\n")
}

func validateNATSCredentials(creds NATSCredentials) error {
	for name, value := range map[string]string{
		"operator user":     creds.OperatorUser,
		"operator password": creds.OperatorPassword,
		"worker user":       creds.WorkerUser,
		"worker password":   creds.WorkerPassword,
		"generation":        creds.Generation,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("nats %s is required", name)
		}
		if strings.ContainsAny(value, "\n\r") {
			return fmt.Errorf("nats %s must not contain newlines", name)
		}
	}
	return nil
}

func randomHex(bytes int) (string, error) {
	buf := make([]byte, bytes)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		return "", fmt.Errorf("generating random hex: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

func randomAlphaNum(length int) (string, error) {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	buf := make([]byte, length)
	random := make([]byte, length)
	if _, err := io.ReadFull(rand.Reader, random); err != nil {
		return "", fmt.Errorf("generating random password: %w", err)
	}
	for i, b := range random {
		buf[i] = alphabet[int(b)%len(alphabet)]
	}
	return string(buf), nil
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
