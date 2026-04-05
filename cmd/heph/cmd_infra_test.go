package main

import "testing"

func TestResolveToolPaths_NmapDedicated(t *testing.T) {
	paths, err := resolveToolPaths("nmap", "dedicated")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if paths.TerraformDir != "deployments/aws/nmap/environments/dev" {
		t.Errorf("TerraformDir = %q, want nmap-specific path", paths.TerraformDir)
	}
	if paths.Dockerfile != "containers/nmap/Dockerfile" {
		t.Errorf("Dockerfile = %q, want nmap container", paths.Dockerfile)
	}
	if paths.BuildArgs != nil {
		t.Error("dedicated nmap should not have BuildArgs")
	}
	if paths.TerraformVars != nil {
		t.Error("dedicated nmap should not have TerraformVars")
	}
}

func TestResolveToolPaths_NmapGeneric(t *testing.T) {
	paths, err := resolveToolPaths("nmap", "generic")
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
	_, err := resolveToolPaths("unknown", "dedicated")
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
}
