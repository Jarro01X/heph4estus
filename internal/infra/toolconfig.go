package infra

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"heph4estus/internal/cloud"
	"heph4estus/internal/modules"
)

// ToolConfig holds all deploy metadata derived from a module definition.
// This is the single source of truth used by CLI, TUI, and lifecycle logic.
type ToolConfig struct {
	Cloud         cloud.Kind // Provider family (empty defaults to AWS)
	ToolName      string
	TerraformDir  string
	Dockerfile    string
	DockerCtx     string
	DockerTag     string
	ECRRepoName   string
	BuildArgs     map[string]string
	TerraformVars map[string]string
}

// ResolveToolConfig derives Docker/Terraform configuration from a module definition.
// When kind is empty or AWS, it returns the AWS Terraform path. For Hetzner,
// it returns the Hetzner Terraform path.
func ResolveToolConfig(tool string, kind ...cloud.Kind) (*ToolConfig, error) {
	reg, err := modules.NewDefaultRegistry()
	if err != nil {
		return nil, fmt.Errorf("loading module registry: %w", err)
	}
	mod, err := reg.Get(tool)
	if err != nil {
		names := reg.Names()
		return nil, fmt.Errorf("unknown tool: %q (available: %s)", tool, strings.Join(names, ", "))
	}

	var cloudKind cloud.Kind
	if len(kind) > 0 {
		cloudKind = kind[0]
	}

	cfg := &ToolConfig{
		Cloud:       cloudKind,
		ToolName:    tool,
		Dockerfile:  "containers/generic/Dockerfile",
		DockerCtx:   ".",
		DockerTag:   fmt.Sprintf("heph-%s-worker:latest", tool),
		ECRRepoName: fmt.Sprintf("heph-dev-%s", tool),
		BuildArgs:   InstallCmdToBuildArgs(mod.InstallCmd),
	}

	switch cloudKind.Canonical() {
	case cloud.KindHetzner:
		cfg.TerraformDir = "deployments/hetzner"
		cfg.TerraformVars = providerNativeTerraformVars(cloudKind, tool, cfg.DockerTag)
	case cloud.KindLinode:
		cfg.TerraformDir = "deployments/linode"
		cfg.TerraformVars = providerNativeTerraformVars(cloudKind, tool, cfg.DockerTag)
	case cloud.KindVultr:
		cfg.TerraformDir = "deployments/vultr"
		cfg.TerraformVars = providerNativeTerraformVars(cloudKind, tool, cfg.DockerTag)
	default:
		cfg.TerraformDir = "deployments/aws/generic/environments/dev"
		cfg.TerraformVars = map[string]string{
			"tool_name":   tool,
			"task_cpu":    fmt.Sprintf("%d", mod.DefaultCPU),
			"task_memory": fmt.Sprintf("%d", mod.DefaultMemory),
		}
	}

	return cfg, nil
}

func providerNativeTerraformVars(kind cloud.Kind, tool, dockerTag string) map[string]string {
	vars := map[string]string{
		"tool_name":    tool,
		"docker_image": dockerTag,
	}
	if key := defaultSSHPublicKey(); key != "" {
		vars["ssh_public_key"] = key
	}
	switch kind.Canonical() {
	case cloud.KindHetzner:
		if token := strings.TrimSpace(os.Getenv("HCLOUD_TOKEN")); token != "" {
			vars["hcloud_token"] = token
		}
	case cloud.KindLinode:
		if token := strings.TrimSpace(os.Getenv("LINODE_TOKEN")); token != "" {
			vars["linode_token"] = token
		}
	case cloud.KindVultr:
		if token := strings.TrimSpace(os.Getenv("VULTR_API_KEY")); token != "" {
			vars["vultr_api_key"] = token
		}
	}
	return vars
}

// ValidateProviderNativeTerraformVars checks the variables required before a
// provider-native Terraform apply can run non-interactively.
func ValidateProviderNativeTerraformVars(kind cloud.Kind, vars map[string]string) error {
	if !kind.IsProviderNative() {
		return nil
	}
	if strings.TrimSpace(vars["ssh_public_key"]) == "" {
		return fmt.Errorf("%s deploy requires an SSH public key; set HEPH_SSH_PUBLIC_KEY, SSH_PUBLIC_KEY, HEPH_SSH_PUBLIC_KEY_PATH, SSH_PUBLIC_KEY_PATH, or create ~/.ssh/id_ed25519.pub", kind.Canonical())
	}
	switch kind.Canonical() {
	case cloud.KindHetzner:
		if strings.TrimSpace(vars["hcloud_token"]) == "" {
			return fmt.Errorf("hetzner deploy requires HCLOUD_TOKEN")
		}
	case cloud.KindLinode:
		if strings.TrimSpace(vars["linode_token"]) == "" {
			return fmt.Errorf("linode deploy requires LINODE_TOKEN")
		}
	case cloud.KindVultr:
		if strings.TrimSpace(vars["vultr_api_key"]) == "" {
			return fmt.Errorf("vultr deploy requires VULTR_API_KEY")
		}
	}
	return nil
}

func defaultSSHPublicKey() string {
	for _, env := range []string{"HEPH_SSH_PUBLIC_KEY", "SSH_PUBLIC_KEY"} {
		if key := strings.TrimSpace(os.Getenv(env)); key != "" {
			return key
		}
	}
	for _, env := range []string{"HEPH_SSH_PUBLIC_KEY_PATH", "SSH_PUBLIC_KEY_PATH"} {
		if key := readPublicKeyFile(os.Getenv(env)); key != "" {
			return key
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	for _, name := range []string{"id_ed25519.pub", "id_rsa.pub"} {
		if key := readPublicKeyFile(filepath.Join(home, ".ssh", name)); key != "" {
			return key
		}
	}
	return ""
}

func readPublicKeyFile(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// InstallCmdToBuildArgs maps a module's install_cmd to the correct Docker build arg.
// "go install ..." -> GO_INSTALL_CMD; everything else -> RUNTIME_INSTALL_CMD.
func InstallCmdToBuildArgs(installCmd string) map[string]string {
	if strings.HasPrefix(installCmd, "go install ") {
		return map[string]string{
			"GO_INSTALL_CMD": installCmd,
		}
	}
	return map[string]string{
		"RUNTIME_INSTALL_CMD": installCmd,
	}
}

// AWSRegion returns the configured AWS region from environment, defaulting to us-east-1.
func AWSRegion() string {
	if r := os.Getenv("AWS_REGION"); r != "" {
		return r
	}
	if r := os.Getenv("AWS_DEFAULT_REGION"); r != "" {
		return r
	}
	return "us-east-1"
}

// RequiredOutputKeys is the legacy AWS-only required-output list. New code
// should call RequiredOutputKeysForCloud(kind) instead, which returns the
// correct set for any cloud.Kind. This alias is kept so existing callers
// outside this package continue to compile without churn.
var RequiredOutputKeys = AWSRequiredOutputKeys
