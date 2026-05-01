package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunFleetNoSubcommand(t *testing.T) {
	err := run([]string{"fleet"}, testLogger())
	if err == nil {
		t.Fatal("expected error for fleet without subcommand")
	}
	if !strings.Contains(err.Error(), "requires a subcommand") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunBenchNoSubcommand(t *testing.T) {
	err := run([]string{"bench"}, testLogger())
	if err == nil {
		t.Fatal("expected error for bench without subcommand")
	}
	if !strings.Contains(err.Error(), "requires a subcommand") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunInfraBackupRequiresOutput(t *testing.T) {
	err := runInfraBackup([]string{"--tool", "httpx", "--cloud", "hetzner"}, testLogger())
	if err == nil {
		t.Fatal("expected missing output error")
	}
	if !strings.Contains(err.Error(), "--output flag is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunInfraRecoverRequiresFrom(t *testing.T) {
	err := runInfraRecover([]string{"--tool", "httpx", "--cloud", "hetzner"}, testLogger())
	if err == nil {
		t.Fatal("expected missing --from error")
	}
	if !strings.Contains(err.Error(), "--from flag is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunFleetReputationFlagsWithoutListSubcommand(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := runFleetReputation([]string{"--cloud", "hetzner"}, testLogger())
	_ = w.Close()
	os.Stdout = old
	_, _ = r.Read(make([]byte, 256))

	if err != nil {
		t.Fatalf("runFleetReputation with direct flags should succeed: %v", err)
	}
}

func TestWriteBenchReport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reports", "fleet.json")
	err := writeBenchReport(path, fleetBenchReport{
		Tool:        "httpx",
		Cloud:       "hetzner",
		GeneratedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("writeBenchReport: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), `"tool": "httpx"`) {
		t.Fatalf("expected report JSON, got:\n%s", string(data))
	}
}
