package doctor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

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
	}
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
	// Check common SSH key paths.
	home := deps.Getenv("HOME")
	if home == "" {
		return CheckResult{
			Name:    "hetzner_ssh_key",
			Status:  StatusWarn,
			Summary: "Cannot determine HOME directory for SSH key check",
		}
	}
	for _, name := range []string{"id_ed25519", "id_rsa"} {
		path := filepath.Join(home, ".ssh", name+".pub")
		if _, err := os.Stat(path); err == nil {
			return CheckResult{
				Name:    "hetzner_ssh_key",
				Status:  StatusPass,
				Summary: fmt.Sprintf("SSH public key found at %s", path),
			}
		}
	}
	return CheckResult{
		Name:    "hetzner_ssh_key",
		Status:  StatusWarn,
		Summary: "No SSH public key found in ~/.ssh/ (needed for Hetzner VM access)",
		Fix:     "Generate an SSH key with 'ssh-keygen -t ed25519' or skip if not using Hetzner.",
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
