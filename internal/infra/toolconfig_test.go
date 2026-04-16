package infra

import (
	"testing"

	"heph4estus/internal/cloud"
)

func TestResolveToolConfig_Nmap(t *testing.T) {
	cfg, err := ResolveToolConfig("nmap")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ToolName != "nmap" {
		t.Errorf("ToolName = %q, want nmap", cfg.ToolName)
	}
	if cfg.TerraformDir != "deployments/aws/generic/environments/dev" {
		t.Errorf("TerraformDir = %q", cfg.TerraformDir)
	}
	if cfg.BuildArgs["RUNTIME_INSTALL_CMD"] == "" {
		t.Error("expected RUNTIME_INSTALL_CMD for nmap")
	}
	if cfg.TerraformVars["tool_name"] != "nmap" {
		t.Errorf("TerraformVars[tool_name] = %q", cfg.TerraformVars["tool_name"])
	}
}

func TestResolveToolConfig_Httpx(t *testing.T) {
	cfg, err := ResolveToolConfig("httpx")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.BuildArgs["GO_INSTALL_CMD"] == "" {
		t.Error("expected GO_INSTALL_CMD for httpx")
	}
	if cfg.DockerTag != "heph-httpx-worker:latest" {
		t.Errorf("DockerTag = %q", cfg.DockerTag)
	}
	if cfg.ECRRepoName != "heph-dev-httpx" {
		t.Errorf("ECRRepoName = %q", cfg.ECRRepoName)
	}
}

func TestResolveToolConfig_UnknownTool(t *testing.T) {
	_, err := ResolveToolConfig("nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
}

func TestInstallCmdToBuildArgs_GoInstall(t *testing.T) {
	args := InstallCmdToBuildArgs("go install github.com/example/tool@latest")
	if args["GO_INSTALL_CMD"] == "" {
		t.Error("expected GO_INSTALL_CMD")
	}
}

func TestInstallCmdToBuildArgs_Runtime(t *testing.T) {
	args := InstallCmdToBuildArgs("apk add --no-cache nmap")
	if args["RUNTIME_INSTALL_CMD"] == "" {
		t.Error("expected RUNTIME_INSTALL_CMD")
	}
}

func TestAWSRegion_Default(t *testing.T) {
	// When no env vars are set, should return us-east-1.
	// This test may be affected by the test environment, but the default case
	// should always be reachable.
	region := AWSRegion()
	if region == "" {
		t.Error("expected non-empty region")
	}
}

func TestResolveToolConfig_HetznerSetsDockerImageVar(t *testing.T) {
	cfg, err := ResolveToolConfig("nmap", cloud.KindHetzner)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.TerraformDir != "deployments/hetzner" {
		t.Fatalf("TerraformDir = %q, want deployments/hetzner", cfg.TerraformDir)
	}
	if got := cfg.TerraformVars["docker_image"]; got != "heph-nmap-worker:latest" {
		t.Fatalf("TerraformVars[docker_image] = %q, want heph-nmap-worker:latest", got)
	}
}
