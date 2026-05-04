package infra

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
)

type RegistryCredentials struct {
	PublisherUsername string
	PublisherPassword string
	WorkerUsername    string
	WorkerPassword    string
	Generation        string
	RotatedAt         string
}

type RegistryAuthUpdateMode string

const (
	RegistryAuthUpdateGrace RegistryAuthUpdateMode = "grace"
	RegistryAuthUpdateFinal RegistryAuthUpdateMode = "final"
)

type RegistryControllerAuthUpdate struct {
	Credentials RegistryCredentials
	TLSEnabled  bool
	RegistryURL string
	Mode        RegistryAuthUpdateMode
}

func GenerateRegistryCredentials(now time.Time) (RegistryCredentials, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	suffix, err := randomHex(4)
	if err != nil {
		return RegistryCredentials{}, err
	}
	publisherPassword, err := randomAlphaNum(32)
	if err != nil {
		return RegistryCredentials{}, err
	}
	workerPassword, err := randomAlphaNum(32)
	if err != nil {
		return RegistryCredentials{}, err
	}
	return RegistryCredentials{
		PublisherUsername: "heph-registry-publisher-" + suffix,
		PublisherPassword: publisherPassword,
		WorkerUsername:    "heph-registry-worker-" + suffix,
		WorkerPassword:    workerPassword,
		Generation:        fmt.Sprintf("registry-%s-%s", now.UTC().Format("20060102t150405z"), suffix),
		RotatedAt:         now.UTC().Format(time.RFC3339),
	}, nil
}

func RegistryTerraformVars(creds RegistryCredentials) map[string]string {
	return map[string]string{
		"registry_publisher_username_override": creds.PublisherUsername,
		"registry_publisher_password_override": creds.PublisherPassword,
		"registry_worker_username_override":    creds.WorkerUsername,
		"registry_worker_password_override":    creds.WorkerPassword,
		"registry_credential_generation":       creds.Generation,
		"registry_credential_rotated_at":       creds.RotatedAt,
	}
}

func UpdateControllerRegistryAuth(ctx context.Context, runner RemoteCommandRunner, host string, update RegistryControllerAuthUpdate) error {
	if runner == nil {
		return fmt.Errorf("remote runner is required")
	}
	if err := validateRegistryCredentials(update.Credentials); err != nil {
		return err
	}
	var cmd string
	switch update.Mode {
	case RegistryAuthUpdateGrace:
		cmd = registryGraceAuthCommand(update)
	case RegistryAuthUpdateFinal:
		cmd = registryFinalAuthCommand(update)
	default:
		return fmt.Errorf("unsupported registry auth update mode %q", update.Mode)
	}
	return runner.Run(ctx, host, cmd)
}

func registryGraceAuthCommand(update RegistryControllerAuthUpdate) string {
	creds := update.Credentials
	return strings.Join([]string{
		"set -eu",
		"auth=/data/registry-auth/htpasswd",
		"backup=/data/registry-auth/htpasswd.pre-rotate-" + creds.Generation,
		"install -d -m 0700 /data/registry-auth",
		`touch "$auth"`,
		`cp "$auth" "$backup"`,
		registryUpsertUserFunction(),
		"registry_upsert_user " + shellQuote(creds.PublisherUsername) + " " + shellQuote(creds.PublisherPassword),
		"registry_upsert_user " + shellQuote(creds.WorkerUsername) + " " + shellQuote(creds.WorkerPassword),
		"systemctl restart registry",
		registryVerifyCurlCommand(update, creds.PublisherUsername, creds.PublisherPassword),
		registryVerifyCurlCommand(update, creds.WorkerUsername, creds.WorkerPassword),
	}, "\n")
}

func registryFinalAuthCommand(update RegistryControllerAuthUpdate) string {
	creds := update.Credentials
	return strings.Join([]string{
		"set -eu",
		"auth=/data/registry-auth/htpasswd",
		"backup=/data/registry-auth/htpasswd.final-pre-rotate-" + creds.Generation,
		"install -d -m 0700 /data/registry-auth",
		`touch "$auth"`,
		`cp "$auth" "$backup"`,
		`tmp=$(mktemp)`,
		"htpasswd -Bbn " + shellQuote(creds.PublisherUsername) + " " + shellQuote(creds.PublisherPassword) + ` > "$tmp"`,
		"htpasswd -Bbn " + shellQuote(creds.WorkerUsername) + " " + shellQuote(creds.WorkerPassword) + ` >> "$tmp"`,
		`install -m 0600 "$tmp" "$auth"`,
		`rm -f "$tmp"`,
		"systemctl restart registry",
		registryVerifyCurlCommand(update, creds.PublisherUsername, creds.PublisherPassword),
		registryVerifyCurlCommand(update, creds.WorkerUsername, creds.WorkerPassword),
	}, "\n")
}

func registryUpsertUserFunction() string {
	return strings.Join([]string{
		"registry_upsert_user() {",
		"  user=$1",
		"  pass=$2",
		`  line=$(htpasswd -Bbn "$user" "$pass")`,
		`  tmp=$(mktemp)`,
		`  awk -v user="$user" -v line="$line" 'BEGIN { found = 0 } index($0, user ":") == 1 { print line; found = 1; next } { print } END { if (!found) print line }' "$auth" > "$tmp"`,
		`  install -m 0600 "$tmp" "$auth"`,
		`  rm -f "$tmp"`,
		"}",
	}, "\n")
}

func registryVerifyCurlCommand(update RegistryControllerAuthUpdate, username, password string) string {
	args := []string{"curl", "-fsS"}
	if update.TLSEnabled {
		args = append(args, "--cacert", "/data/tls/ca.crt")
	}
	args = append(args,
		"-u", shellQuote(username+":"+password),
		shellQuote(registryLocalEndpoint(update)+"/v2/"),
		">/dev/null",
	)
	return strings.Join(args, " ")
}

func registryLocalEndpoint(update RegistryControllerAuthUpdate) string {
	if registryURL := strings.TrimSpace(update.RegistryURL); registryURL != "" {
		if !strings.Contains(registryURL, "://") {
			registryURL = "http://" + registryURL
		}
		if parsed, err := url.Parse(registryURL); err == nil && parsed.Scheme != "" {
			port := parsed.Port()
			if port == "" {
				port = "5000"
			}
			return parsed.Scheme + "://localhost:" + port
		}
	}
	if update.TLSEnabled {
		return "https://localhost:5000"
	}
	return "http://localhost:5000"
}

func validateRegistryCredentials(creds RegistryCredentials) error {
	for name, value := range map[string]string{
		"publisher username": creds.PublisherUsername,
		"publisher password": creds.PublisherPassword,
		"worker username":    creds.WorkerUsername,
		"worker password":    creds.WorkerPassword,
		"generation":         creds.Generation,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("registry %s is required", name)
		}
		if strings.ContainsAny(value, "\n\r") {
			return fmt.Errorf("registry %s must not contain newlines", name)
		}
	}
	return nil
}
