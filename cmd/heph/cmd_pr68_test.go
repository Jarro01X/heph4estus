package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"heph4estus/internal/bench"
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

func TestRunBenchCompareRequiresTwoReports(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	err := runBenchCompare([]string{}, testLogger())
	if err == nil {
		t.Fatal("expected compare error without enough history")
	}
	if !strings.Contains(err.Error(), "at least two benchmark reports") {
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
	err := writeBenchReport(path, bench.FleetReport{
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

func TestOutputBenchComparisonText(t *testing.T) {
	comparison := bench.CompareFleetReports(
		bench.FleetReport{
			Tool:                "httpx",
			Cloud:               "hetzner",
			GeneratedAt:         time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC),
			SteadyStateDuration: 3 * time.Minute,
			UniqueIPv4Count:     10,
		},
		bench.FleetReport{
			Tool:                "httpx",
			Cloud:               "hetzner",
			GeneratedAt:         time.Date(2026, 5, 2, 11, 0, 0, 0, time.UTC),
			SteadyStateDuration: 2 * time.Minute,
			UniqueIPv4Count:     12,
		},
	)

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	err := outputBenchComparisonText(comparison)
	_ = w.Close()
	os.Stdout = old
	if err != nil {
		t.Fatalf("outputBenchComparisonText: %v", err)
	}
	buf := make([]byte, 2048)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	for _, want := range []string{"Baseline:", "Candidate:", "Steady:     -1m0s", "IPv4:       +2"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, out)
		}
	}
}

func TestRunBenchHistoryJSON(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	store, err := bench.NewStore()
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if _, err := store.Save(bench.FleetReport{
		Tool:        "httpx",
		Cloud:       "hetzner",
		GeneratedAt: time.Date(2026, 5, 2, 11, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	err = runBenchHistory([]string{"--tool", "httpx", "--cloud", "hetzner", "--format", "json"}, testLogger())
	_ = w.Close()
	os.Stdout = old
	if err != nil {
		t.Fatalf("runBenchHistory: %v", err)
	}
	var reports []bench.FleetReport
	if err := json.NewDecoder(r).Decode(&reports); err != nil {
		t.Fatalf("decode history: %v", err)
	}
	if len(reports) != 1 || reports[0].Tool != "httpx" {
		t.Fatalf("unexpected reports: %+v", reports)
	}
}
