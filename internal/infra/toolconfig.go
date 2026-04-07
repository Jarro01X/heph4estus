package infra

import (
	"fmt"
	"os"
	"strings"

	"heph4estus/internal/modules"
)

// ToolConfig holds all deploy metadata derived from a module definition.
// This is the single source of truth used by CLI, TUI, and lifecycle logic.
type ToolConfig struct {
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
func ResolveToolConfig(tool string) (*ToolConfig, error) {
	reg, err := modules.NewDefaultRegistry()
	if err != nil {
		return nil, fmt.Errorf("loading module registry: %w", err)
	}
	mod, err := reg.Get(tool)
	if err != nil {
		names := reg.Names()
		return nil, fmt.Errorf("unknown tool: %q (available: %s)", tool, strings.Join(names, ", "))
	}

	return &ToolConfig{
		ToolName:     tool,
		TerraformDir: "deployments/aws/generic/environments/dev",
		Dockerfile:   "containers/generic/Dockerfile",
		DockerCtx:    ".",
		DockerTag:    fmt.Sprintf("heph-%s-worker:latest", tool),
		ECRRepoName:  fmt.Sprintf("heph-dev-%s", tool),
		BuildArgs:    InstallCmdToBuildArgs(mod.InstallCmd),
		TerraformVars: map[string]string{
			"tool_name":   tool,
			"task_cpu":    fmt.Sprintf("%d", mod.DefaultCPU),
			"task_memory": fmt.Sprintf("%d", mod.DefaultMemory),
		},
	}, nil
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

// RequiredOutputKeys lists the Terraform output keys that must be present for a scan to proceed.
// This includes spot-mode keys (ami_id, instance_profile_arn) because the generic terraform
// module always outputs them, and their absence indicates stale or partial infrastructure.
// tool_name is required to detect mismatches — without it, legacy state cannot be classified.
var RequiredOutputKeys = []string{
	"tool_name",
	"sqs_queue_url",
	"s3_bucket_name",
	"ecr_repo_url",
	"ecs_cluster_name",
	"task_definition_arn",
	"subnet_ids",
	"security_group_id",
	"ami_id",
	"instance_profile_arn",
}
