package main

import (
	"strings"
	"testing"

	"heph4estus/internal/infra"
)

func TestResolveToolPaths_DedicatedRejected(t *testing.T) {
	_, err := resolveToolConfig("nmap", "dedicated")
	if err == nil {
		t.Fatal("expected error for --backend dedicated")
	}
	if !strings.Contains(err.Error(), "no longer supported") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveToolPaths_NmapGeneric(t *testing.T) {
	paths, err := resolveToolConfig("nmap", "generic")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if paths.TerraformDir != "deployments/aws/generic/environments/dev" {
		t.Errorf("TerraformDir = %q, want generic path", paths.TerraformDir)
	}
	if paths.Dockerfile != "containers/generic/Dockerfile" {
		t.Errorf("Dockerfile = %q, want generic container", paths.Dockerfile)
	}
	if paths.BuildArgs == nil {
		t.Fatal("generic nmap should have BuildArgs")
	}
	if paths.BuildArgs["RUNTIME_INSTALL_CMD"] == "" {
		t.Error("BuildArgs missing RUNTIME_INSTALL_CMD")
	}
	if paths.TerraformVars == nil {
		t.Fatal("generic nmap should have TerraformVars")
	}
	if paths.TerraformVars["tool_name"] != "nmap" {
		t.Errorf("TerraformVars[tool_name] = %q, want nmap", paths.TerraformVars["tool_name"])
	}
}

func TestResolveToolPaths_UnknownTool(t *testing.T) {
	_, err := resolveToolConfig("unknown", "generic")
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
}

func TestResolveToolPaths_DedicatedNonNmapRejected(t *testing.T) {
	_, err := resolveToolConfig("httpx", "dedicated")
	if err == nil {
		t.Fatal("expected error for --backend dedicated with non-nmap tool")
	}
	if !strings.Contains(err.Error(), "no longer supported") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveToolPaths_Httpx(t *testing.T) {
	paths, err := resolveToolConfig("httpx", "generic")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if paths.TerraformDir != "deployments/aws/generic/environments/dev" {
		t.Errorf("TerraformDir = %q, want generic path", paths.TerraformDir)
	}
	if paths.BuildArgs == nil {
		t.Fatal("httpx should have BuildArgs")
	}
	// httpx uses go install, so should have GO_INSTALL_CMD
	if paths.BuildArgs["GO_INSTALL_CMD"] == "" {
		t.Error("httpx BuildArgs missing GO_INSTALL_CMD")
	}
	if paths.BuildArgs["RUNTIME_INSTALL_CMD"] != "" {
		t.Error("httpx should not have RUNTIME_INSTALL_CMD")
	}
	if paths.TerraformVars["tool_name"] != "httpx" {
		t.Errorf("TerraformVars[tool_name] = %q, want httpx", paths.TerraformVars["tool_name"])
	}
	if paths.DockerTag != "heph-httpx-worker:latest" {
		t.Errorf("DockerTag = %q, want heph-httpx-worker:latest", paths.DockerTag)
	}
}

func TestResolveToolPaths_AllRegistryTools(t *testing.T) {
	// Every registered tool should be resolvable via generic backend.
	tools := []string{"dalfox", "dnsx", "gospider", "gowitness", "httpx", "katana", "masscan", "massdns", "nmap", "nuclei", "subfinder", "feroxbuster", "ffuf", "gobuster"}
	for _, tool := range tools {
		paths, err := resolveToolConfig(tool, "generic")
		if err != nil {
			t.Errorf("resolveToolConfig(%q, generic) failed: %v", tool, err)
			continue
		}
		if paths.TerraformVars["tool_name"] != tool {
			t.Errorf("resolveToolConfig(%q): tool_name = %q", tool, paths.TerraformVars["tool_name"])
		}
	}
}

func TestInstallCmdToBuildArgs_GoInstall(t *testing.T) {
	args := infra.InstallCmdToBuildArgs("go install github.com/example/tool@latest")
	if args["GO_INSTALL_CMD"] == "" {
		t.Error("expected GO_INSTALL_CMD for go install command")
	}
	if args["RUNTIME_INSTALL_CMD"] != "" {
		t.Error("should not have RUNTIME_INSTALL_CMD for go install command")
	}
}

func TestInstallCmdToBuildArgs_RuntimeInstall(t *testing.T) {
	args := infra.InstallCmdToBuildArgs("apk add --no-cache nmap")
	if args["RUNTIME_INSTALL_CMD"] == "" {
		t.Error("expected RUNTIME_INSTALL_CMD for runtime install command")
	}
	if args["GO_INSTALL_CMD"] != "" {
		t.Error("should not have GO_INSTALL_CMD for runtime install command")
	}
}

func TestResolveToolPaths_GowitnessForwardsModuleSizing(t *testing.T) {
	paths, err := resolveToolConfig("gowitness", "generic")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// gowitness declares 512 CPU / 1024 memory in its module definition.
	if paths.TerraformVars["task_cpu"] != "512" {
		t.Errorf("task_cpu = %q, want 512", paths.TerraformVars["task_cpu"])
	}
	if paths.TerraformVars["task_memory"] != "1024" {
		t.Errorf("task_memory = %q, want 1024", paths.TerraformVars["task_memory"])
	}
}

func TestResolveToolPaths_HttpxDefaultSizing(t *testing.T) {
	paths, err := resolveToolConfig("httpx", "generic")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// httpx declares 256 CPU / 512 memory.
	if paths.TerraformVars["task_cpu"] != "256" {
		t.Errorf("task_cpu = %q, want 256", paths.TerraformVars["task_cpu"])
	}
	if paths.TerraformVars["task_memory"] != "512" {
		t.Errorf("task_memory = %q, want 512", paths.TerraformVars["task_memory"])
	}
}

func TestResolveToolPaths_ErrorContainsAvailable(t *testing.T) {
	_, err := resolveToolConfig("nonexistent", "generic")
	if err == nil {
		t.Fatal("expected error")
	}
	// Error should list available tools.
	if !strings.Contains(err.Error(), "httpx") {
		t.Errorf("error should list available tools, got: %v", err)
	}
}
