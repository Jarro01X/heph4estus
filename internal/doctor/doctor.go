package doctor

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"heph4estus/internal/cloud"
	"heph4estus/internal/operator"
)

// Status represents the outcome of a single diagnostic check.
type Status string

const (
	StatusPass Status = "pass"
	StatusWarn Status = "warn"
	StatusFail Status = "fail"
)

// CheckResult is the structured output of one diagnostic check.
// It is designed to be consumed by both CLI text output and the TUI
// settings/diagnostics view (Track 5).
type CheckResult struct {
	Name    string `json:"name"`
	Status  Status `json:"status"`
	Summary string `json:"summary"`
	Fix     string `json:"fix,omitempty"`
}

// Deps abstracts external side effects so the diagnostics engine is
// fully testable without real binaries, daemons, or AWS credentials.
type Deps struct {
	// LookPath resolves a binary name to a path. Default: exec.LookPath.
	LookPath func(name string) (string, error)

	// RunCmd executes a command and returns its combined output.
	// Default: exec.CommandContext(...).CombinedOutput().
	RunCmd func(ctx context.Context, name string, args ...string) ([]byte, error)

	// Getenv reads an environment variable. Default: os.Getenv.
	Getenv func(key string) string

	// LoadConfig loads the operator config. Default: operator.LoadConfig.
	LoadConfig func() (*operator.OperatorConfig, error)

	// ConfigDir returns the operator config directory. Default: operator.ConfigDir.
	ConfigDir func() (string, error)
}

// DefaultDeps returns production dependencies.
func DefaultDeps() Deps {
	return Deps{
		LookPath: exec.LookPath,
		RunCmd: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, name, args...).CombinedOutput()
		},
		Getenv:     os.Getenv,
		LoadConfig: operator.LoadConfig,
		ConfigDir:  operator.ConfigDir,
	}
}

// RunAll executes every diagnostic check and returns the results.
// The order is stable and matches the recommended check set from the plan.
func RunAll(ctx context.Context, deps Deps) []CheckResult {
	return []CheckResult{
		checkBinary(deps, "terraform"),
		checkBinary(deps, "docker"),
		checkDockerDaemon(ctx, deps),
		checkBinary(deps, "aws"),
		checkAWSRegion(deps),
		checkAWSProfile(deps),
		checkSTSIdentity(ctx, deps),
		checkConfigDirWritable(deps),
		checkOutputDir(deps),
		// Hetzner checks.
		checkHetznerToken(deps),
		checkHetznerSSHKey(deps),
		// Linode checks.
		checkLinodeToken(deps),
		checkLinodeSSHKey(deps),
		// Vultr checks.
		checkVultrAPIKey(deps),
		checkVultrSSHKey(deps),
	}
}

// RunForCloud executes only the checks relevant to the given cloud provider.
// When kind is empty or aws, the full set is returned (minus VPS-specific
// checks that aren't relevant). When kind is a VPS provider, AWS-specific
// checks are excluded and provider-specific checks are included.
func RunForCloud(ctx context.Context, deps Deps, kind cloud.Kind) []CheckResult {
	k := kind.Canonical()

	// Common checks for all providers.
	common := []CheckResult{
		checkBinary(deps, "terraform"),
		checkBinary(deps, "docker"),
		checkDockerDaemon(ctx, deps),
		checkConfigDirWritable(deps),
		checkOutputDir(deps),
	}

	switch k {
	case cloud.KindAWS, "":
		return append(common,
			checkBinary(deps, "aws"),
			checkAWSRegion(deps),
			checkAWSProfile(deps),
			checkSTSIdentity(ctx, deps),
		)
	case cloud.KindHetzner:
		return append(common,
			checkHetznerToken(deps),
			checkHetznerSSHKey(deps),
			checkControllerReachable(deps),
			checkNATSAuth(deps),
			checkRegistryExposure(deps),
		)
	case cloud.KindLinode:
		return append(common,
			checkLinodeToken(deps),
			checkLinodeSSHKey(deps),
			checkControllerReachable(deps),
			checkNATSAuth(deps),
			checkRegistryExposure(deps),
		)
	case cloud.KindVultr:
		return append(common,
			checkVultrAPIKey(deps),
			checkVultrSSHKey(deps),
			checkControllerReachable(deps),
			checkNATSAuth(deps),
			checkRegistryExposure(deps),
		)
	case cloud.KindManual:
		return append(common,
			checkControllerReachable(deps),
			checkNATSAuth(deps),
			checkRegistryExposure(deps),
		)
	default:
		// Unknown provider — return the full set.
		return RunAll(ctx, deps)
	}
}

// RunProviderNativeOutputChecks validates the security posture surfaced by
// Terraform outputs for provider-native controller fleets.
func RunProviderNativeOutputChecks(kind cloud.Kind, outputs map[string]string) []CheckResult {
	return runProviderNativeOutputChecksAt(kind, outputs, time.Now().UTC())
}

func runProviderNativeOutputChecksAt(kind cloud.Kind, outputs map[string]string, now time.Time) []CheckResult {
	if !kind.IsProviderNative() {
		return nil
	}
	results := []CheckResult{
		checkControllerSecurityMode(outputs),
		checkOutputNATSAuth(outputs),
		checkOutputNATSTLS(outputs),
		checkOutputMinIOTLS(outputs),
		checkOutputRegistryTLS(outputs),
		checkOutputControllerCA(outputs),
		checkOutputControllerCertExpiry(outputs),
		checkOutputRegistryAuth(outputs),
	}
	for _, meta := range providerCredentialRotationMetadata {
		results = append(results, checkOutputCredentialRotationAge(outputs, meta, now))
	}
	return results
}

// HasFailures returns true if any check has StatusFail.
func HasFailures(results []CheckResult) bool {
	for _, r := range results {
		if r.Status == StatusFail {
			return true
		}
	}
	return false
}

func checkBinary(deps Deps, name string) CheckResult {
	path, err := deps.LookPath(name)
	if err != nil {
		return CheckResult{
			Name:    name + "_binary",
			Status:  StatusFail,
			Summary: fmt.Sprintf("%s not found in PATH", name),
			Fix:     fmt.Sprintf("Install %s and ensure it is on your PATH.", name),
		}
	}
	return CheckResult{
		Name:    name + "_binary",
		Status:  StatusPass,
		Summary: fmt.Sprintf("%s found at %s", name, path),
	}
}

func checkDockerDaemon(ctx context.Context, deps Deps) CheckResult {
	// First check that docker binary exists.
	if _, err := deps.LookPath("docker"); err != nil {
		return CheckResult{
			Name:    "docker_daemon",
			Status:  StatusFail,
			Summary: "docker not found; cannot check daemon",
			Fix:     "Install Docker and ensure it is on your PATH.",
		}
	}

	out, err := deps.RunCmd(ctx, "docker", "info")
	if err != nil {
		return CheckResult{
			Name:    "docker_daemon",
			Status:  StatusFail,
			Summary: "Docker daemon is not reachable",
			Fix:     "Start the Docker daemon (e.g. 'sudo systemctl start docker' or open Docker Desktop).",
		}
	}
	_ = out
	return CheckResult{
		Name:    "docker_daemon",
		Status:  StatusPass,
		Summary: "Docker daemon is reachable",
	}
}

func checkAWSRegion(deps Deps) CheckResult {
	// Check multiple region sources in precedence order.
	for _, key := range []string{"AWS_REGION", "AWS_DEFAULT_REGION"} {
		if v := deps.Getenv(key); v != "" {
			return CheckResult{
				Name:    "aws_region",
				Status:  StatusPass,
				Summary: fmt.Sprintf("AWS region resolved to %s (from %s)", v, key),
			}
		}
	}

	// Check operator config.
	if cfg, err := deps.LoadConfig(); err == nil && cfg.Region != "" {
		return CheckResult{
			Name:    "aws_region",
			Status:  StatusPass,
			Summary: fmt.Sprintf("AWS region resolved to %s (from operator config)", cfg.Region),
		}
	}

	return CheckResult{
		Name:    "aws_region",
		Status:  StatusFail,
		Summary: "No AWS region configured",
		Fix:     "Set AWS_REGION or AWS_DEFAULT_REGION, or run 'heph init' to save a default region.",
	}
}

func checkAWSProfile(deps Deps) CheckResult {
	if v := deps.Getenv("AWS_PROFILE"); v != "" {
		return CheckResult{
			Name:    "aws_profile",
			Status:  StatusPass,
			Summary: fmt.Sprintf("AWS profile set to %s (from AWS_PROFILE)", v),
		}
	}

	if cfg, err := deps.LoadConfig(); err == nil && cfg.Profile != "" {
		return CheckResult{
			Name:    "aws_profile",
			Status:  StatusPass,
			Summary: fmt.Sprintf("AWS profile set to %s (from operator config)", cfg.Profile),
		}
	}

	// No explicit profile is not necessarily an error — default creds may work.
	return CheckResult{
		Name:    "aws_profile",
		Status:  StatusWarn,
		Summary: "No explicit AWS profile configured; using default credential chain",
		Fix:     "Set AWS_PROFILE or run 'heph init' to save a default profile if needed.",
	}
}

func checkSTSIdentity(ctx context.Context, deps Deps) CheckResult {
	if _, err := deps.LookPath("aws"); err != nil {
		return CheckResult{
			Name:    "sts_identity",
			Status:  StatusFail,
			Summary: "aws CLI not found; cannot verify credentials",
			Fix:     "Install the AWS CLI and ensure it is on your PATH.",
		}
	}

	out, err := deps.RunCmd(ctx, "aws", "sts", "get-caller-identity")
	if err != nil {
		return CheckResult{
			Name:    "sts_identity",
			Status:  StatusFail,
			Summary: "STS get-caller-identity failed: credentials may be missing or expired",
			Fix:     "Run 'aws configure' or refresh your SSO session with 'aws sso login'.",
		}
	}
	_ = out
	return CheckResult{
		Name:    "sts_identity",
		Status:  StatusPass,
		Summary: "AWS credentials are valid (STS get-caller-identity succeeded)",
	}
}

func checkConfigDirWritable(deps Deps) CheckResult {
	dir, err := deps.ConfigDir()
	if err != nil {
		return CheckResult{
			Name:    "config_dir",
			Status:  StatusFail,
			Summary: fmt.Sprintf("Cannot resolve config directory: %v", err),
			Fix:     "Ensure your OS user config directory is available.",
		}
	}

	// Try to create the directory if it doesn't exist, then test writability.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return CheckResult{
			Name:    "config_dir",
			Status:  StatusFail,
			Summary: fmt.Sprintf("Config directory %s is not writable: %v", dir, err),
			Fix:     fmt.Sprintf("Fix permissions on %s or its parent directory.", dir),
		}
	}

	probe := filepath.Join(dir, ".doctor_probe")
	if err := os.WriteFile(probe, []byte("ok"), 0o600); err != nil {
		return CheckResult{
			Name:    "config_dir",
			Status:  StatusFail,
			Summary: fmt.Sprintf("Config directory %s is not writable: %v", dir, err),
			Fix:     fmt.Sprintf("Fix permissions on %s.", dir),
		}
	}
	_ = os.Remove(probe)

	return CheckResult{
		Name:    "config_dir",
		Status:  StatusPass,
		Summary: fmt.Sprintf("Config directory %s is writable", dir),
	}
}

// checkHetznerToken checks for HCLOUD_TOKEN environment variable.
func checkHetznerToken(deps Deps) CheckResult {
	if v := deps.Getenv("HCLOUD_TOKEN"); v != "" {
		return CheckResult{
			Name:    "hetzner_token",
			Status:  StatusPass,
			Summary: "HCLOUD_TOKEN is set",
		}
	}
	return CheckResult{
		Name:    "hetzner_token",
		Status:  StatusWarn,
		Summary: "HCLOUD_TOKEN is not set (required for --cloud hetzner)",
		Fix:     "Set HCLOUD_TOKEN with your Hetzner Cloud API token, or skip if not using Hetzner.",
	}
}

// checkHetznerSSHKey checks for an SSH key that can be used for Hetzner VMs.
func checkHetznerSSHKey(deps Deps) CheckResult {
	return checkProviderSSHKey(deps, "hetzner", "Hetzner")
}

func checkLinodeSSHKey(deps Deps) CheckResult {
	return checkProviderSSHKey(deps, "linode", "Linode")
}

func checkVultrSSHKey(deps Deps) CheckResult {
	return checkProviderSSHKey(deps, "vultr", "Vultr")
}

func checkProviderSSHKey(deps Deps, name, label string) CheckResult {
	for _, env := range []string{"HEPH_SSH_PUBLIC_KEY", "SSH_PUBLIC_KEY"} {
		if deps.Getenv(env) != "" {
			return CheckResult{
				Name:    name + "_ssh_key",
				Status:  StatusPass,
				Summary: fmt.Sprintf("SSH public key found in %s", env),
			}
		}
	}
	for _, env := range []string{"HEPH_SSH_PUBLIC_KEY_PATH", "SSH_PUBLIC_KEY_PATH"} {
		path := deps.Getenv(env)
		if path == "" {
			continue
		}
		if _, err := os.Stat(path); err == nil {
			return CheckResult{
				Name:    name + "_ssh_key",
				Status:  StatusPass,
				Summary: fmt.Sprintf("SSH public key found at %s", path),
			}
		}
	}

	// Check common SSH key paths.
	home := deps.Getenv("HOME")
	if home == "" {
		return CheckResult{
			Name:    name + "_ssh_key",
			Status:  StatusWarn,
			Summary: "Cannot determine HOME directory for SSH key check",
		}
	}
	for _, keyName := range []string{"id_ed25519", "id_rsa"} {
		path := filepath.Join(home, ".ssh", keyName+".pub")
		if _, err := os.Stat(path); err == nil {
			return CheckResult{
				Name:    name + "_ssh_key",
				Status:  StatusPass,
				Summary: fmt.Sprintf("SSH public key found at %s", path),
			}
		}
	}
	return CheckResult{
		Name:    name + "_ssh_key",
		Status:  StatusWarn,
		Summary: fmt.Sprintf("No SSH public key found for %s VM access", label),
		Fix:     "Set HEPH_SSH_PUBLIC_KEY_PATH, set HEPH_SSH_PUBLIC_KEY, or generate an SSH key with 'ssh-keygen -t ed25519'.",
	}
}

// checkLinodeToken checks for LINODE_TOKEN environment variable.
func checkLinodeToken(deps Deps) CheckResult {
	if v := deps.Getenv("LINODE_TOKEN"); v != "" {
		return CheckResult{
			Name:    "linode_token",
			Status:  StatusPass,
			Summary: "LINODE_TOKEN is set",
		}
	}
	return CheckResult{
		Name:    "linode_token",
		Status:  StatusWarn,
		Summary: "LINODE_TOKEN is not set (required for --cloud linode)",
		Fix:     "Set LINODE_TOKEN with your Linode API token, or skip if not using Linode.",
	}
}

// checkVultrAPIKey checks for VULTR_API_KEY environment variable.
func checkVultrAPIKey(deps Deps) CheckResult {
	if v := deps.Getenv("VULTR_API_KEY"); v != "" {
		return CheckResult{
			Name:    "vultr_api_key",
			Status:  StatusPass,
			Summary: "VULTR_API_KEY is set",
		}
	}
	return CheckResult{
		Name:    "vultr_api_key",
		Status:  StatusWarn,
		Summary: "VULTR_API_KEY is not set (required for --cloud vultr)",
		Fix:     "Set VULTR_API_KEY with your Vultr API key, or skip if not using Vultr.",
	}
}

// checkControllerReachable verifies the operator can reach the controller VM.
func checkControllerReachable(deps Deps) CheckResult {
	natsURL := deps.Getenv("NATS_URL")
	if natsURL == "" {
		return CheckResult{
			Name:    "controller_reachable",
			Status:  StatusWarn,
			Summary: "NATS_URL not set; cannot check controller reachability",
			Fix:     "Set NATS_URL to your controller's NATS endpoint, or deploy infrastructure first.",
		}
	}

	// Extract host:port from nats:// URL.
	host := natsURL
	for _, prefix := range []string{"nats://", "tls://"} {
		if len(host) > len(prefix) && host[:len(prefix)] == prefix {
			host = host[len(prefix):]
		}
	}

	conn, err := net.DialTimeout("tcp", host, 5*time.Second)
	if err != nil {
		return CheckResult{
			Name:    "controller_reachable",
			Status:  StatusFail,
			Summary: fmt.Sprintf("Controller NATS endpoint unreachable: %s", host),
			Fix:     "Verify the controller VM is running and NATS port is open in the firewall.",
		}
	}
	_ = conn.Close()

	return CheckResult{
		Name:    "controller_reachable",
		Status:  StatusPass,
		Summary: fmt.Sprintf("Controller NATS endpoint reachable at %s", host),
	}
}

// checkNATSAuth warns when NATS credentials are not configured.
func checkNATSAuth(deps Deps) CheckResult {
	user := deps.Getenv("NATS_USER")
	pass := deps.Getenv("NATS_PASSWORD")

	if user != "" && pass != "" {
		return CheckResult{
			Name:    "nats_auth",
			Status:  StatusPass,
			Summary: "NATS authentication credentials are configured",
		}
	}

	return CheckResult{
		Name:    "nats_auth",
		Status:  StatusWarn,
		Summary: "NATS authentication not configured; controller queue may be publicly accessible",
		Fix:     "Deploy with authenticated NATS (PR 6.6+) or set NATS_USER and NATS_PASSWORD.",
	}
}

// checkRegistryExposure warns when the container registry lacks authentication.
func checkRegistryExposure(deps Deps) CheckResult {
	registryURL := deps.Getenv("REGISTRY_URL")
	if registryURL == "" {
		return CheckResult{
			Name:    "registry_exposure",
			Status:  StatusWarn,
			Summary: "REGISTRY_URL not set; cannot check registry exposure",
			Fix:     "Set REGISTRY_URL to your controller's registry endpoint, or deploy infrastructure first.",
		}
	}

	// Check if the registry is using TLS.
	if len(registryURL) > 8 && registryURL[:8] == "https://" {
		return CheckResult{
			Name:    "registry_exposure",
			Status:  StatusPass,
			Summary: "Container registry is using TLS",
		}
	}

	return CheckResult{
		Name:    "registry_exposure",
		Status:  StatusWarn,
		Summary: "Container registry is using HTTP (insecure); images are pulled without TLS",
		Fix:     "Configure TLS for the registry or restrict access to the private network.",
	}
}

func checkControllerSecurityMode(outputs map[string]string) CheckResult {
	mode := strings.TrimSpace(outputs["controller_security_mode"])
	switch mode {
	case "private-auth":
		return CheckResult{
			Name:    "controller_security_mode",
			Status:  StatusWarn,
			Summary: "Controller security mode is private-auth compatibility mode",
			Fix:     "Use controller_security_mode=tls after the TLS hardening path is enabled.",
		}
	case "tls":
		return CheckResult{
			Name:    "controller_security_mode",
			Status:  StatusPass,
			Summary: "Controller security mode is tls",
		}
	case "mtls":
		return CheckResult{
			Name:    "controller_security_mode",
			Status:  StatusPass,
			Summary: "Controller security mode is mtls",
		}
	case "":
		return CheckResult{
			Name:    "controller_security_mode",
			Status:  StatusWarn,
			Summary: "Controller security mode output is missing",
			Fix:     "Redeploy or recover the provider-native fleet so hardening posture outputs are available.",
		}
	default:
		return CheckResult{
			Name:    "controller_security_mode",
			Status:  StatusFail,
			Summary: fmt.Sprintf("Controller security mode %q is unsupported", mode),
			Fix:     "Use one of: private-auth, tls, mtls.",
		}
	}
}

func checkOutputNATSAuth(outputs map[string]string) CheckResult {
	if outputBool(outputs["nats_auth_enabled"]) || (strings.TrimSpace(outputs["nats_user"]) != "" && strings.TrimSpace(outputs["nats_password"]) != "") {
		return CheckResult{
			Name:    "nats_auth_posture",
			Status:  StatusPass,
			Summary: "NATS authentication is enabled in provider outputs",
		}
	}
	return CheckResult{
		Name:    "nats_auth_posture",
		Status:  StatusFail,
		Summary: "NATS authentication is not enabled in provider outputs",
		Fix:     "Redeploy with authenticated NATS before using this controller fleet.",
	}
}

func checkOutputNATSTLS(outputs map[string]string) CheckResult {
	mode := strings.TrimSpace(outputs["controller_security_mode"])
	enabled := outputBool(outputs["nats_tls_enabled"])
	natsURL := strings.TrimSpace(outputs["nats_url"])
	if mode == "tls" || mode == "mtls" {
		if enabled && strings.HasPrefix(natsURL, "tls://") {
			return CheckResult{Name: "nats_tls_posture", Status: StatusPass, Summary: "NATS TLS is enabled"}
		}
		return CheckResult{
			Name:    "nats_tls_posture",
			Status:  StatusFail,
			Summary: "Controller security mode requires NATS TLS, but outputs do not show a tls:// NATS endpoint",
			Fix:     "Redeploy after enabling NATS TLS support and verify nats_tls_enabled plus nats_url.",
		}
	}
	if enabled || strings.HasPrefix(natsURL, "tls://") {
		return CheckResult{Name: "nats_tls_posture", Status: StatusPass, Summary: "NATS TLS is enabled"}
	}
	return CheckResult{
		Name:    "nats_tls_posture",
		Status:  StatusWarn,
		Summary: "NATS TLS is disabled in private-auth compatibility mode",
		Fix:     "Restrict NATS to the private fleet network and migrate to controller_security_mode=tls when available.",
	}
}

func checkOutputMinIOTLS(outputs map[string]string) CheckResult {
	mode := strings.TrimSpace(outputs["controller_security_mode"])
	enabled := outputBool(outputs["minio_tls_enabled"])
	endpoint := strings.TrimSpace(outputs["s3_endpoint"])
	if mode == "tls" || mode == "mtls" {
		if enabled && strings.HasPrefix(endpoint, "https://") {
			return CheckResult{Name: "minio_tls_posture", Status: StatusPass, Summary: "MinIO TLS is enabled"}
		}
		return CheckResult{
			Name:    "minio_tls_posture",
			Status:  StatusFail,
			Summary: "Controller security mode requires MinIO TLS, but outputs do not show an https:// S3 endpoint",
			Fix:     "Redeploy after enabling MinIO TLS support and verify minio_tls_enabled plus s3_endpoint.",
		}
	}
	if enabled || strings.HasPrefix(endpoint, "https://") {
		return CheckResult{Name: "minio_tls_posture", Status: StatusPass, Summary: "MinIO TLS is enabled"}
	}
	return CheckResult{
		Name:    "minio_tls_posture",
		Status:  StatusWarn,
		Summary: "MinIO TLS is disabled in private-auth compatibility mode",
		Fix:     "Restrict MinIO to the private fleet network and migrate to controller_security_mode=tls when available.",
	}
}

func checkOutputRegistryTLS(outputs map[string]string) CheckResult {
	mode := strings.TrimSpace(outputs["controller_security_mode"])
	enabled := outputBool(outputs["registry_tls_enabled"])
	registryURL := strings.TrimSpace(outputs["registry_url"])
	if mode == "tls" || mode == "mtls" {
		if enabled && hasHTTPSScheme(registryURL) {
			return CheckResult{Name: "registry_tls_posture", Status: StatusPass, Summary: "Registry TLS is enabled"}
		}
		return CheckResult{
			Name:    "registry_tls_posture",
			Status:  StatusFail,
			Summary: "Controller security mode requires registry TLS, but outputs do not show an HTTPS registry endpoint",
			Fix:     "Redeploy after enabling registry TLS and update Docker trust bootstrap.",
		}
	}
	if enabled || hasHTTPSScheme(registryURL) {
		return CheckResult{Name: "registry_tls_posture", Status: StatusPass, Summary: "Registry TLS is enabled"}
	}
	return CheckResult{
		Name:    "registry_tls_posture",
		Status:  StatusWarn,
		Summary: "Registry TLS is disabled in private-auth compatibility mode",
		Fix:     "Restrict the registry to the private fleet network and migrate to controller_security_mode=tls when available.",
	}
}

func checkOutputRegistryAuth(outputs map[string]string) CheckResult {
	if outputBool(outputs["registry_auth_enabled"]) {
		return CheckResult{Name: "registry_auth_posture", Status: StatusPass, Summary: "Registry authentication is enabled"}
	}
	return CheckResult{
		Name:    "registry_auth_posture",
		Status:  StatusWarn,
		Summary: "Registry authentication is disabled",
		Fix:     "Keep the registry private-network-only and enable registry auth in the credential-scoping hardening slice.",
	}
}

const credentialRotationStaleAfter = 90 * 24 * time.Hour

type credentialRotationOutputMetadata struct {
	Component     string
	Label         string
	GenerationKey string
	RotatedAtKey  string
}

var providerCredentialRotationMetadata = []credentialRotationOutputMetadata{
	{
		Component:     "nats",
		Label:         "NATS",
		GenerationKey: "nats_credential_generation",
		RotatedAtKey:  "nats_credential_rotated_at",
	},
	{
		Component:     "minio",
		Label:         "MinIO",
		GenerationKey: "minio_credential_generation",
		RotatedAtKey:  "minio_credential_rotated_at",
	},
	{
		Component:     "registry",
		Label:         "Registry",
		GenerationKey: "registry_credential_generation",
		RotatedAtKey:  "registry_credential_rotated_at",
	},
}

func checkOutputCredentialRotationAge(outputs map[string]string, meta credentialRotationOutputMetadata, now time.Time) CheckResult {
	generation := strings.TrimSpace(outputs[meta.GenerationKey])
	rotatedRaw := strings.TrimSpace(outputs[meta.RotatedAtKey])
	name := meta.Component + "_credential_rotation_age"
	if rotatedRaw == "" {
		summary := fmt.Sprintf("%s credentials have not been rotated after bootstrap", meta.Label)
		if generation != "" && generation != "bootstrap" {
			summary = fmt.Sprintf("%s credential rotation timestamp is missing for generation %s", meta.Label, generation)
		}
		return CheckResult{
			Name:    name,
			Status:  StatusWarn,
			Summary: summary,
			Fix:     fmt.Sprintf("Run 'heph infra rotate credentials --component %s' after the fleet is healthy.", meta.Component),
		}
	}
	rotatedAt, err := time.Parse(time.RFC3339, rotatedRaw)
	if err != nil {
		return CheckResult{
			Name:    name,
			Status:  StatusWarn,
			Summary: fmt.Sprintf("%s credential rotation timestamp %q is not RFC3339", meta.Label, rotatedRaw),
			Fix:     fmt.Sprintf("Run 'heph infra rotate credentials --component %s' to write fresh rotation metadata.", meta.Component),
		}
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()
	rotatedAt = rotatedAt.UTC()
	if rotatedAt.After(now.Add(5 * time.Minute)) {
		return CheckResult{
			Name:    name,
			Status:  StatusWarn,
			Summary: fmt.Sprintf("%s credential rotation timestamp is in the future: %s", meta.Label, rotatedAt.Format(time.RFC3339)),
			Fix:     "Check local clock skew and Terraform output metadata.",
		}
	}
	age := now.Sub(rotatedAt)
	ageText := credentialRotationAgeText(age)
	if age > credentialRotationStaleAfter {
		return CheckResult{
			Name:    name,
			Status:  StatusWarn,
			Summary: fmt.Sprintf("%s credentials were last rotated at %s (%s)", meta.Label, rotatedAt.Format(time.RFC3339), ageText),
			Fix:     fmt.Sprintf("Run 'heph infra rotate credentials --component %s' during the next maintenance window.", meta.Component),
		}
	}
	return CheckResult{
		Name:    name,
		Status:  StatusPass,
		Summary: fmt.Sprintf("%s credentials were last rotated at %s (%s)", meta.Label, rotatedAt.Format(time.RFC3339), ageText),
	}
}

func credentialRotationAgeText(age time.Duration) string {
	if age < time.Hour {
		minutes := int(age.Minutes())
		if minutes < 1 {
			minutes = 0
		}
		return fmt.Sprintf("%d %s ago", minutes, pluralize(minutes, "minute"))
	}
	days := int(age.Hours() / 24)
	if days < 1 {
		hours := int(age.Hours())
		return fmt.Sprintf("%d %s ago", hours, pluralize(hours, "hour"))
	}
	return fmt.Sprintf("%d %s ago", days, pluralize(days, "day"))
}

func pluralize(count int, singular string) string {
	if count == 1 {
		return singular
	}
	return singular + "s"
}

func checkOutputControllerCA(outputs map[string]string) CheckResult {
	mode := strings.TrimSpace(outputs["controller_security_mode"])
	fingerprint := strings.TrimSpace(outputs["controller_ca_fingerprint_sha256"])
	caPEM := strings.TrimSpace(outputs["controller_ca_pem"])
	if fingerprint != "" && caPEM != "" {
		return CheckResult{
			Name:    "controller_ca_posture",
			Status:  StatusPass,
			Summary: fmt.Sprintf("Controller CA is available (sha256:%s)", fingerprint),
		}
	}
	status := StatusWarn
	if mode == "tls" || mode == "mtls" {
		status = StatusFail
	}
	return CheckResult{
		Name:    "controller_ca_posture",
		Status:  status,
		Summary: "Controller CA metadata is missing from provider outputs",
		Fix:     "Redeploy the provider-native fleet so PR 6.10 TLS trust outputs are available.",
	}
}

func checkOutputControllerCertExpiry(outputs map[string]string) CheckResult {
	mode := strings.TrimSpace(outputs["controller_security_mode"])
	raw := strings.TrimSpace(outputs["controller_cert_not_after"])
	if raw == "" {
		status := StatusWarn
		if mode == "tls" || mode == "mtls" {
			status = StatusFail
		}
		return CheckResult{
			Name:    "controller_cert_expiry",
			Status:  status,
			Summary: "Controller certificate expiry output is missing",
			Fix:     "Redeploy the provider-native fleet so controller certificate metadata is available.",
		}
	}
	expiresAt, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return CheckResult{
			Name:    "controller_cert_expiry",
			Status:  StatusFail,
			Summary: fmt.Sprintf("Controller certificate expiry %q is not RFC3339", raw),
			Fix:     "Redeploy the provider-native fleet with a valid controller_cert_not_after output.",
		}
	}
	now := time.Now().UTC()
	switch {
	case !expiresAt.After(now):
		return CheckResult{
			Name:    "controller_cert_expiry",
			Status:  StatusFail,
			Summary: fmt.Sprintf("Controller certificate expired at %s", expiresAt.Format(time.RFC3339)),
			Fix:     "Rotate or redeploy controller certificates before using this fleet.",
		}
	case expiresAt.Before(now.Add(30 * 24 * time.Hour)):
		return CheckResult{
			Name:    "controller_cert_expiry",
			Status:  StatusWarn,
			Summary: fmt.Sprintf("Controller certificate expires soon at %s", expiresAt.Format(time.RFC3339)),
			Fix:     "Plan certificate rotation or redeploy before expiry.",
		}
	default:
		return CheckResult{
			Name:    "controller_cert_expiry",
			Status:  StatusPass,
			Summary: fmt.Sprintf("Controller certificate expires at %s", expiresAt.Format(time.RFC3339)),
		}
	}
}

func outputBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func hasHTTPSScheme(value string) bool {
	return strings.HasPrefix(value, "https://")
}

func checkOutputDir(deps Deps) CheckResult {
	cfg, err := deps.LoadConfig()
	if err != nil {
		return CheckResult{
			Name:    "output_dir",
			Status:  StatusWarn,
			Summary: fmt.Sprintf("Could not load operator config: %v", err),
		}
	}

	if cfg.OutputDir == "" {
		return CheckResult{
			Name:    "output_dir",
			Status:  StatusPass,
			Summary: "No output directory configured (results stay in S3)",
		}
	}

	// Try to ensure the directory is creatable.
	if err := os.MkdirAll(cfg.OutputDir, 0o755); err != nil {
		return CheckResult{
			Name:    "output_dir",
			Status:  StatusFail,
			Summary: fmt.Sprintf("Output directory %s cannot be created: %v", cfg.OutputDir, err),
			Fix:     fmt.Sprintf("Fix permissions on %s or choose a different path with 'heph init'.", cfg.OutputDir),
		}
	}

	return CheckResult{
		Name:    "output_dir",
		Status:  StatusPass,
		Summary: fmt.Sprintf("Output directory %s exists and is creatable", cfg.OutputDir),
	}
}
