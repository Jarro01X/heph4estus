package infra

import (
	"fmt"
	"strings"
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

func TestResolveToolConfig_HighPriorityToolImagesUseGenericContainer(t *testing.T) {
	tests := []struct {
		tool        string
		buildArgKey string
	}{
		{"nmap", "RUNTIME_INSTALL_CMD"},
		{"nuclei", "GO_INSTALL_CMD"},
		{"subfinder", "GO_INSTALL_CMD"},
		{"httpx", "GO_INSTALL_CMD"},
		{"masscan", "RUNTIME_INSTALL_CMD"},
	}

	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			cfg, err := ResolveToolConfig(tt.tool)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg.Dockerfile != "containers/generic/Dockerfile" {
				t.Fatalf("Dockerfile = %q, want generic Dockerfile", cfg.Dockerfile)
			}
			if cfg.DockerCtx != "." {
				t.Fatalf("DockerCtx = %q, want .", cfg.DockerCtx)
			}
			wantTag := fmt.Sprintf("heph-%s-worker:latest", tt.tool)
			if cfg.DockerTag != wantTag {
				t.Fatalf("DockerTag = %q, want %q", cfg.DockerTag, wantTag)
			}
			if cfg.BuildArgs[tt.buildArgKey] == "" {
				t.Fatalf("BuildArgs missing %s", tt.buildArgKey)
			}
		})
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
	t.Setenv("HCLOUD_TOKEN", "hcloud-test")
	t.Setenv("HEPH_SSH_PUBLIC_KEY", "ssh-ed25519 hetzner-test")
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
	if got := cfg.TerraformVars["hcloud_token"]; got != "hcloud-test" {
		t.Fatalf("TerraformVars[hcloud_token] = %q, want hcloud-test", got)
	}
	if got := cfg.TerraformVars["ssh_public_key"]; got != "ssh-ed25519 hetzner-test" {
		t.Fatalf("TerraformVars[ssh_public_key] = %q, want test key", got)
	}
}

func TestResolveToolConfig_LinodeSetsDockerImageVar(t *testing.T) {
	t.Setenv("LINODE_TOKEN", "linode-test")
	t.Setenv("HEPH_SSH_PUBLIC_KEY", "ssh-ed25519 linode-test")
	cfg, err := ResolveToolConfig("nmap", cloud.KindLinode)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.TerraformDir != "deployments/linode" {
		t.Fatalf("TerraformDir = %q, want deployments/linode", cfg.TerraformDir)
	}
	if got := cfg.TerraformVars["docker_image"]; got != "heph-nmap-worker:latest" {
		t.Fatalf("TerraformVars[docker_image] = %q, want heph-nmap-worker:latest", got)
	}
	if cfg.Cloud != cloud.KindLinode {
		t.Fatalf("Cloud = %q, want linode", cfg.Cloud)
	}
	if got := cfg.TerraformVars["linode_token"]; got != "linode-test" {
		t.Fatalf("TerraformVars[linode_token] = %q, want linode-test", got)
	}
	if got := cfg.TerraformVars["ssh_public_key"]; got != "ssh-ed25519 linode-test" {
		t.Fatalf("TerraformVars[ssh_public_key] = %q, want test key", got)
	}
}

func TestResolveToolConfig_VultrSetsProviderVars(t *testing.T) {
	t.Setenv("VULTR_API_KEY", "vultr-test")
	t.Setenv("HEPH_SSH_PUBLIC_KEY", "ssh-ed25519 vultr-test")
	cfg, err := ResolveToolConfig("nmap", cloud.KindVultr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.TerraformDir != "deployments/vultr" {
		t.Fatalf("TerraformDir = %q, want deployments/vultr", cfg.TerraformDir)
	}
	if got := cfg.TerraformVars["vultr_api_key"]; got != "vultr-test" {
		t.Fatalf("TerraformVars[vultr_api_key] = %q, want vultr-test", got)
	}
	if got := cfg.TerraformVars["ssh_public_key"]; got != "ssh-ed25519 vultr-test" {
		t.Fatalf("TerraformVars[ssh_public_key] = %q, want test key", got)
	}
}

func TestResolveToolConfigProviderNativeRequiresToken(t *testing.T) {
	err := ValidateProviderNativeTerraformVars(cloud.KindHetzner, map[string]string{
		"ssh_public_key": "ssh-ed25519 test",
	})
	if err == nil {
		t.Fatal("expected missing token error")
	}
	if !strings.Contains(err.Error(), "HCLOUD_TOKEN") {
		t.Fatalf("error = %v, want HCLOUD_TOKEN", err)
	}
}

func TestResolveToolConfigProviderNativeRequiresSSHKey(t *testing.T) {
	err := ValidateProviderNativeTerraformVars(cloud.KindHetzner, map[string]string{
		"hcloud_token": "hcloud-test",
	})
	if err == nil {
		t.Fatal("expected missing SSH public key error")
	}
	if !strings.Contains(err.Error(), "SSH public key") {
		t.Fatalf("error = %v, want SSH public key", err)
	}
}
