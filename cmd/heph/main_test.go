package main

import (
	"strings"
	"testing"

	"heph4estus/internal/logger"
)

func testLogger() logger.Logger {
	return logger.NewSimpleLogger()
}

func TestNoCommand(t *testing.T) {
	err := run([]string{}, testLogger())
	if err == nil {
		t.Fatal("expected error for no command")
	}
	if !strings.Contains(err.Error(), "no command specified") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUnknownCommand(t *testing.T) {
	err := run([]string{"bogus"}, testLogger())
	if err == nil {
		t.Fatal("expected error for unknown command")
	}
	if !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHelpFlag(t *testing.T) {
	for _, flag := range []string{"--help", "-help", "-h"} {
		err := run([]string{flag}, testLogger())
		if err != nil {
			t.Fatalf("help flag %q returned error: %v", flag, err)
		}
	}
}

// --- nmap subcommand ---

func TestNmapMissingFile(t *testing.T) {
	err := run([]string{"nmap"}, testLogger())
	if err == nil {
		t.Fatal("expected error for nmap without --file")
	}
	if !strings.Contains(err.Error(), "--file flag is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNmapHelp(t *testing.T) {
	err := run([]string{"nmap", "--help"}, testLogger())
	if err == nil {
		t.Fatal("expected flag.ErrHelp wrapped error")
	}
	if !strings.Contains(err.Error(), "flag: help requested") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNmapInvalidComputeMode(t *testing.T) {
	err := run([]string{"nmap", "--file", "targets.txt", "--compute-mode", "gpu"}, testLogger())
	if err == nil {
		t.Fatal("expected error for invalid compute mode")
	}
	if !strings.Contains(err.Error(), "--compute-mode must be") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNmapInvalidFormat(t *testing.T) {
	err := run([]string{"nmap", "--file", "targets.txt", "--format", "xml"}, testLogger())
	if err == nil {
		t.Fatal("expected error for invalid format")
	}
	if !strings.Contains(err.Error(), "--format must be") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNmapInvalidWorkers(t *testing.T) {
	err := run([]string{"nmap", "--file", "targets.txt", "--workers", "0"}, testLogger())
	if err == nil {
		t.Fatal("expected error for zero workers")
	}
	if !strings.Contains(err.Error(), "--workers must be positive") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNmapNonexistentFile(t *testing.T) {
	err := run([]string{"nmap", "--file", "/nonexistent/targets.txt"}, testLogger())
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "reading target file") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- scan subcommand ---

func TestScanMissingTool(t *testing.T) {
	err := run([]string{"scan"}, testLogger())
	if err == nil {
		t.Fatal("expected error for scan without --tool")
	}
	if !strings.Contains(err.Error(), "--tool flag is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestScanMissingFile(t *testing.T) {
	err := run([]string{"scan", "--tool", "httpx"}, testLogger())
	if err == nil {
		t.Fatal("expected error for scan without --file")
	}
	if !strings.Contains(err.Error(), "--file flag is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestScanUnknownTool(t *testing.T) {
	err := run([]string{"scan", "--tool", "nonexistent", "--file", "targets.txt"}, testLogger())
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
	if !strings.Contains(err.Error(), "unknown tool") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestScanWordlistRequiresWordlistFlag(t *testing.T) {
	err := run([]string{"scan", "--tool", "ffuf", "--file", "targets.txt"}, testLogger())
	if err == nil {
		t.Fatal("expected error for wordlist tool with --file")
	}
	if !strings.Contains(err.Error(), "--file is not valid for wordlist tool") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestScanWordlistRequiresTarget(t *testing.T) {
	err := run([]string{"scan", "--tool", "ffuf", "--wordlist", "words.txt"}, testLogger())
	if err == nil {
		t.Fatal("expected error for wordlist tool without --target")
	}
	if !strings.Contains(err.Error(), "--target flag is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestScanTargetListRejectsWordlistFlag(t *testing.T) {
	err := run([]string{"scan", "--tool", "httpx", "--wordlist", "words.txt"}, testLogger())
	if err == nil {
		t.Fatal("expected error for target_list tool with --wordlist")
	}
	if !strings.Contains(err.Error(), "--wordlist is not valid for target_list tool") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestScanTargetListRejectsChunks(t *testing.T) {
	err := run([]string{"scan", "--tool", "httpx", "--file", "targets.txt", "--chunks", "5"}, testLogger())
	if err == nil {
		t.Fatal("expected error for target_list tool with --chunks")
	}
	if !strings.Contains(err.Error(), "--chunks is not valid") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestScanInvalidFormat(t *testing.T) {
	err := run([]string{"scan", "--tool", "httpx", "--file", "targets.txt", "--format", "xml"}, testLogger())
	if err == nil {
		t.Fatal("expected error for invalid format")
	}
	if !strings.Contains(err.Error(), "--format must be") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestScanInvalidComputeMode(t *testing.T) {
	err := run([]string{"scan", "--tool", "httpx", "--file", "targets.txt", "--compute-mode", "gpu"}, testLogger())
	if err == nil {
		t.Fatal("expected error for invalid compute mode")
	}
	if !strings.Contains(err.Error(), "--compute-mode must be") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestScanInvalidWorkers(t *testing.T) {
	err := run([]string{"scan", "--tool", "httpx", "--file", "targets.txt", "--workers", "0"}, testLogger())
	if err == nil {
		t.Fatal("expected error for zero workers")
	}
	if !strings.Contains(err.Error(), "--workers must be positive") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- infra subcommand ---

func TestInfraNoSubcommand(t *testing.T) {
	err := run([]string{"infra"}, testLogger())
	if err == nil {
		t.Fatal("expected error for infra without subcommand")
	}
	if !strings.Contains(err.Error(), "requires a subcommand") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInfraUnknownSubcommand(t *testing.T) {
	err := run([]string{"infra", "bogus"}, testLogger())
	if err == nil {
		t.Fatal("expected error for unknown infra subcommand")
	}
	if !strings.Contains(err.Error(), "unknown subcommand") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInfraDeployMissingTool(t *testing.T) {
	err := run([]string{"infra", "deploy"}, testLogger())
	if err == nil {
		t.Fatal("expected error for deploy without --tool")
	}
	if !strings.Contains(err.Error(), "--tool flag is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInfraDeployUnknownTool(t *testing.T) {
	err := run([]string{"infra", "deploy", "--tool", "hashcat", "--backend", "generic"}, testLogger())
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
	if !strings.Contains(err.Error(), "unknown tool") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInfraDeployDedicatedRejected(t *testing.T) {
	// Dedicated backend is no longer supported for any tool.
	for _, tool := range []string{"nmap", "httpx"} {
		err := run([]string{"infra", "deploy", "--tool", tool, "--backend", "dedicated"}, testLogger())
		if err == nil {
			t.Fatalf("expected error for dedicated %s", tool)
		}
		if !strings.Contains(err.Error(), "must be generic") {
			t.Fatalf("unexpected error for %s: %v", tool, err)
		}
	}
}

func TestInfraDeployInvalidBackend(t *testing.T) {
	err := run([]string{"infra", "deploy", "--tool", "nmap", "--backend", "generci"}, testLogger())
	if err == nil {
		t.Fatal("expected error for typo in --backend")
	}
	if !strings.Contains(err.Error(), "must be generic") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInfraDestroyInvalidBackend(t *testing.T) {
	err := run([]string{"infra", "destroy", "--tool", "nmap", "--backend", "typo"}, testLogger())
	if err == nil {
		t.Fatal("expected error for typo in --backend")
	}
	if !strings.Contains(err.Error(), "must be generic") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInfraDestroyMissingTool(t *testing.T) {
	err := run([]string{"infra", "destroy"}, testLogger())
	if err == nil {
		t.Fatal("expected error for destroy without --tool")
	}
	if !strings.Contains(err.Error(), "--tool flag is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInfraDestroyUnknownTool(t *testing.T) {
	err := run([]string{"infra", "destroy", "--tool", "hashcat", "--backend", "generic"}, testLogger())
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
	if !strings.Contains(err.Error(), "unknown tool") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- status subcommand ---

func TestStatusPlaceholder(t *testing.T) {
	err := run([]string{"status"}, testLogger())
	if err != nil {
		t.Fatalf("status placeholder returned error: %v", err)
	}
}

// --- helper functions ---

func TestResolveComputeMode(t *testing.T) {
	tests := []struct {
		mode    string
		workers int
		want    bool // true = spot
	}{
		{"spot", 1, true},
		{"spot", 100, true},
		{"fargate", 1, false},
		{"fargate", 100, false},
		{"auto", 10, false},
		{"auto", 49, false},
		{"auto", 50, true},
		{"auto", 100, true},
	}
	for _, tt := range tests {
		got := resolveComputeMode(tt.mode, tt.workers)
		if got != tt.want {
			t.Errorf("resolveComputeMode(%q, %d) = %v, want %v", tt.mode, tt.workers, got, tt.want)
		}
	}
}

func TestRegionFromECR(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"123456789.dkr.ecr.us-east-1.amazonaws.com/repo", "us-east-1"},
		{"123456789.dkr.ecr.eu-west-1.amazonaws.com/repo", "eu-west-1"},
		{"invalid-url", "us-east-1"},
	}
	for _, tt := range tests {
		got := regionFromECR(tt.url)
		if got != tt.want {
			t.Errorf("regionFromECR(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}
}

func TestExtractTargetFromKey(t *testing.T) {
	tests := []struct {
		key  string
		want string
	}{
		{"scans/nmap/job-123/results/192.168.1.1_1709913600.json", "192.168.1.1"},
		{"scans/nmap/job-123/results/example.com_1709913600.json", "example.com"},
		{"scans/nmap/job-123/results/example.com_line1/example.com_chunk0_of_5_1700000000.json", "example.com"},
		{"scans/nmap/job-123/results/10.0.0.1_line1/10.0.0.1_chunk2_of_5_1700000000.json", "10.0.0.1"},
		{"scans/nmap/job-123/artifacts/10.0.0.1_1709913600.xml", "10.0.0.1"},
	}
	for _, tt := range tests {
		got := extractTargetFromKey(tt.key)
		if got != tt.want {
			t.Errorf("extractTargetFromKey(%q) = %q, want %q", tt.key, got, tt.want)
		}
	}
}

func TestSplitOutputList(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"[subnet-abc subnet-def]", 2},
		{"subnet-abc", 1},
		{"", 0},
	}
	for _, tt := range tests {
		got := splitOutputList(tt.input)
		if len(got) != tt.want {
			t.Errorf("splitOutputList(%q) returned %d items, want %d", tt.input, len(got), tt.want)
		}
	}
}
