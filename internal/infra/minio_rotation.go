package infra

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
)

type MinIOCredentials struct {
	OperatorAccessKey string
	OperatorSecretKey string
	WorkerAccessKey   string
	WorkerSecretKey   string
	Generation        string
	RotatedAt         string
}

type MinIOAuthUpdateMode string

const (
	MinIOAuthUpdateGrace MinIOAuthUpdateMode = "grace"
	MinIOAuthUpdateFinal MinIOAuthUpdateMode = "final"
)

type MinIOControllerAuthUpdate struct {
	Credentials       MinIOCredentials
	TLSEnabled        bool
	Bucket            string
	OldOperatorKey    string
	PreviousWorkerKey string
	Endpoint          string
	Mode              MinIOAuthUpdateMode
}

func GenerateMinIOCredentials(now time.Time) (MinIOCredentials, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	suffix, err := randomHex(4)
	if err != nil {
		return MinIOCredentials{}, err
	}
	operatorSecret, err := randomAlphaNum(40)
	if err != nil {
		return MinIOCredentials{}, err
	}
	workerSecret, err := randomAlphaNum(40)
	if err != nil {
		return MinIOCredentials{}, err
	}
	return MinIOCredentials{
		OperatorAccessKey: "hephop" + suffix,
		OperatorSecretKey: operatorSecret,
		WorkerAccessKey:   "hephwk" + suffix,
		WorkerSecretKey:   workerSecret,
		Generation:        fmt.Sprintf("minio-%s-%s", now.UTC().Format("20060102t150405z"), suffix),
		RotatedAt:         now.UTC().Format(time.RFC3339),
	}, nil
}

func MinIOTerraformVars(creds MinIOCredentials) map[string]string {
	return map[string]string{
		"minio_operator_access_key_override": creds.OperatorAccessKey,
		"minio_operator_secret_key_override": creds.OperatorSecretKey,
		"minio_worker_access_key_override":   creds.WorkerAccessKey,
		"minio_worker_secret_key_override":   creds.WorkerSecretKey,
		"minio_credential_generation":        creds.Generation,
		"minio_credential_rotated_at":        creds.RotatedAt,
	}
}

func UpdateControllerMinIOAuth(ctx context.Context, runner RemoteCommandRunner, host string, update MinIOControllerAuthUpdate) error {
	if runner == nil {
		return fmt.Errorf("remote runner is required")
	}
	if err := validateMinIOCredentials(update.Credentials); err != nil {
		return err
	}
	if strings.TrimSpace(update.Bucket) == "" {
		return fmt.Errorf("minio bucket is required")
	}
	var cmd string
	switch update.Mode {
	case MinIOAuthUpdateGrace:
		cmd = minioGraceAuthCommand(update)
	case MinIOAuthUpdateFinal:
		cmd = minioFinalAuthCommand(update)
	default:
		return fmt.Errorf("unsupported minio auth update mode %q", update.Mode)
	}
	return runner.Run(ctx, host, cmd)
}

func minioGraceAuthCommand(update MinIOControllerAuthUpdate) string {
	return minioAuthCommand(update, false)
}

func minioFinalAuthCommand(update MinIOControllerAuthUpdate) string {
	return minioAuthCommand(update, true)
}

func minioAuthCommand(update MinIOControllerAuthUpdate, cleanupOld bool) string {
	creds := update.Credentials
	endpoint := minioLocalEndpoint(update)
	mcRun := minioMCRunCommand(update.TLSEnabled)
	lines := []string{
		"set -eu",
		"service=/etc/systemd/system/minio.service",
		`root_user=$(awk -F'MINIO_ROOT_USER=' '/MINIO_ROOT_USER=/{print $2; exit}' "$service" | awk '{print $1}')`,
		`root_password=$(awk -F'MINIO_ROOT_PASSWORD=' '/MINIO_ROOT_PASSWORD=/{print $2; exit}' "$service" | awk '{print $1}')`,
		`test -n "$root_user"`,
		`test -n "$root_password"`,
		`tmpdir=$(mktemp -d)`,
		`trap 'rm -rf "$tmpdir"' EXIT`,
		`cat > "$tmpdir/operator-policy.json" <<'HEPH_MINIO_OPERATOR_POLICY'`,
		minioOperatorPolicy(update.Bucket),
		"HEPH_MINIO_OPERATOR_POLICY",
		`cat > "$tmpdir/worker-policy.json" <<'HEPH_MINIO_WORKER_POLICY'`,
		minioWorkerPolicy(update.Bucket),
		"HEPH_MINIO_WORKER_POLICY",
		mcRun + " " + shellQuote(strings.Join([]string{
			"set -eu",
			"mc alias set local " + shellQuote(endpoint) + ` "$MINIO_ROOT_USER" "$MINIO_ROOT_PASSWORD"`,
			"mc mb --ignore-existing local/" + shellQuote(update.Bucket),
			"mc admin policy create local heph-operator /work/operator-policy.json || mc admin policy info local heph-operator >/dev/null",
			"mc admin policy create local heph-worker /work/worker-policy.json || mc admin policy info local heph-worker >/dev/null",
			"mc admin user info local " + shellQuote(creds.OperatorAccessKey) + " >/dev/null 2>&1 || mc admin user add local " + shellQuote(creds.OperatorAccessKey) + " " + shellQuote(creds.OperatorSecretKey),
			"mc admin user info local " + shellQuote(creds.WorkerAccessKey) + " >/dev/null 2>&1 || mc admin user add local " + shellQuote(creds.WorkerAccessKey) + " " + shellQuote(creds.WorkerSecretKey),
			"mc admin policy attach local heph-operator --user " + shellQuote(creds.OperatorAccessKey),
			"mc admin policy attach local heph-worker --user " + shellQuote(creds.WorkerAccessKey),
		}, "\n")),
		minioVerifyCommand(update, creds.OperatorAccessKey, creds.OperatorSecretKey, "operator", "mc ls operator/"+shellQuote(update.Bucket)+" >/dev/null"),
		minioVerifyCommand(update, creds.WorkerAccessKey, creds.WorkerSecretKey, "worker", "printf heph-rotation-"+shellQuote(creds.Generation)+" > /tmp/heph-minio-probe && mc cp /tmp/heph-minio-probe worker/"+shellQuote(update.Bucket)+"/.heph-rotation/"+shellQuote(creds.Generation)+"/worker-probe >/dev/null"),
	}
	if cleanupOld {
		lines = append(lines, minioCleanupCommand(update))
	}
	return strings.Join(lines, "\n")
}

func minioVerifyCommand(update MinIOControllerAuthUpdate, accessKey, secretKey, alias, verify string) string {
	endpoint := minioLocalEndpoint(update)
	return minioMCRunCommand(update.TLSEnabled) + " " + shellQuote(strings.Join([]string{
		"set -eu",
		"mc alias set " + alias + " " + shellQuote(endpoint) + " " + shellQuote(accessKey) + " " + shellQuote(secretKey),
		verify,
	}, "\n"))
}

func minioCleanupCommand(update MinIOControllerAuthUpdate) string {
	keys := []string{}
	if oldOperator := strings.TrimSpace(update.OldOperatorKey); oldOperator != "" && oldOperator != update.Credentials.OperatorAccessKey {
		keys = append(keys, oldOperator)
	}
	if previousWorker := strings.TrimSpace(update.PreviousWorkerKey); previousWorker != "" && previousWorker != update.Credentials.WorkerAccessKey {
		keys = append(keys, previousWorker)
	}
	if len(keys) == 0 {
		return ":"
	}
	commands := []string{
		"set -eu",
		"mc alias set local " + shellQuote(minioLocalEndpoint(update)) + ` "$MINIO_ROOT_USER" "$MINIO_ROOT_PASSWORD"`,
	}
	for _, key := range keys {
		commands = append(commands, "mc admin user remove local "+shellQuote(key)+" >/dev/null 2>&1 || true")
	}
	return minioMCRunCommand(update.TLSEnabled) + " " + shellQuote(strings.Join(commands, "\n"))
}

func minioMCRunCommand(tlsEnabled bool) string {
	parts := []string{"docker", "run", "--rm", "--network", "host"}
	if tlsEnabled {
		parts = append(parts, "-v", "/data/tls/ca.crt:/root/.mc/certs/CAs/heph-controller-ca.crt:ro")
	}
	parts = append(parts,
		"-v", `"$tmpdir:/work:ro"`,
		"-e", "MINIO_ROOT_USER=\"$root_user\"",
		"-e", "MINIO_ROOT_PASSWORD=\"$root_password\"",
		"--entrypoint", "sh",
		"minio/mc",
		"-c",
	)
	return strings.Join(parts, " ")
}

func minioLocalEndpoint(update MinIOControllerAuthUpdate) string {
	if endpoint := strings.TrimSpace(update.Endpoint); endpoint != "" {
		if parsed, err := url.Parse(endpoint); err == nil && parsed.Scheme != "" {
			port := parsed.Port()
			if port == "" {
				port = "9000"
			}
			return parsed.Scheme + "://localhost:" + port
		}
	}
	if update.TLSEnabled {
		return "https://localhost:9000"
	}
	return "http://localhost:9000"
}

func minioOperatorPolicy(bucket string) string {
	return `{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": ["s3:ListBucket"],
      "Resource": ["arn:aws:s3:::` + bucket + `"]
    },
    {
      "Effect": "Allow",
      "Action": ["s3:GetObject", "s3:PutObject"],
      "Resource": ["arn:aws:s3:::` + bucket + `/*"]
    }
  ]
}`
}

func minioWorkerPolicy(bucket string) string {
	return `{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": ["s3:GetObject", "s3:PutObject"],
      "Resource": ["arn:aws:s3:::` + bucket + `/*"]
    }
  ]
}`
}

func validateMinIOCredentials(creds MinIOCredentials) error {
	for name, value := range map[string]string{
		"operator access key": creds.OperatorAccessKey,
		"operator secret key": creds.OperatorSecretKey,
		"worker access key":   creds.WorkerAccessKey,
		"worker secret key":   creds.WorkerSecretKey,
		"generation":          creds.Generation,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("minio %s is required", name)
		}
		if strings.ContainsAny(value, "\n\r") {
			return fmt.Errorf("minio %s must not contain newlines", name)
		}
	}
	return nil
}
