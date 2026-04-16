package infra

import (
	"fmt"
	"os"
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
		cfg.TerraformVars = map[string]string{
			"tool_name":    tool,
			"docker_image": cfg.DockerTag,
		}
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
