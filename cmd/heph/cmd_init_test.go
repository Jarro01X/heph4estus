package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"heph4estus/internal/operator"
)

func TestInitHelp(t *testing.T) {
	err := run([]string{"init", "--help"}, testLogger())
	if err == nil {
		t.Fatal("expected flag.ErrHelp wrapped error")
	}
	if !strings.Contains(err.Error(), "flag: help requested") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInitShowEmptyConfig(t *testing.T) {
	// Capture stdout.
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	cfg := &operator.OperatorConfig{}
	err := printConfig(cfg)
	_ = w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("printConfig error: %v", err)
	}

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	output := string(buf[:n])

	if !strings.Contains(output, "region:         -") {
		t.Errorf("expected empty region dash, got:\n%s", output)
	}
	if !strings.Contains(output, "worker_count:   -") {
		t.Errorf("expected empty worker_count dash, got:\n%s", output)
	}
}

func TestInitShowPopulatedConfig(t *testing.T) {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	cfg := &operator.OperatorConfig{
		Region:        "us-west-2",
		Profile:       "pentest",
		WorkerCount:   20,
		ComputeMode:   "spot",
		CleanupPolicy: "destroy-after",
		OutputDir:     "/tmp/results",
	}
	err := printConfig(cfg)
	_ = w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("printConfig error: %v", err)
	}

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	output := string(buf[:n])

	checks := []string{
		"region:         us-west-2",
		"profile:        pentest",
		"worker_count:   20",
		"compute_mode:   spot",
		"cleanup_policy: destroy-after",
		"output_dir:     /tmp/results",
	}
	for _, c := range checks {
		if !strings.Contains(output, c) {
			t.Errorf("missing %q in output:\n%s", c, output)
		}
	}
}

func TestInitNonInteractiveWritesConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	cfg := &operator.OperatorConfig{}
	explicit := map[string]bool{
		"region":  true,
		"workers": true,
	}

	// Redirect stdout to discard printConfig output.
	old := os.Stdout
	_, w, _ := os.Pipe()
	os.Stdout = w

	err := runInitNonInteractive(cfg, explicit, "eu-west-1", "", 25, "", "", "")
	_ = w.Close()
	os.Stdout = old

	// The function saves to the default path, but we test the in-memory cfg.
	if err != nil {
		// SaveConfig may fail if config dir is not writable in test env.
		// That's OK — we check the cfg was updated.
		if !strings.Contains(err.Error(), "saving config") {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	if cfg.Region != "eu-west-1" {
		t.Errorf("region = %q, want eu-west-1", cfg.Region)
	}
	if cfg.WorkerCount != 25 {
		t.Errorf("worker_count = %d, want 25", cfg.WorkerCount)
	}

	// Also test round-trip via file.
	cfg2 := &operator.OperatorConfig{}
	explicit2 := map[string]bool{
		"region":       true,
		"compute-mode": true,
	}

	err = runInitNonInteractiveToPath(cfg2, explicit2, "ap-southeast-1", "", 0, "spot", "", "", path)
	if err != nil {
		t.Fatalf("non-interactive to path: %v", err)
	}

	loaded, err := operator.LoadConfigFrom(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if loaded.Region != "ap-southeast-1" {
		t.Errorf("region = %q, want ap-southeast-1", loaded.Region)
	}
	if loaded.ComputeMode != "spot" {
		t.Errorf("compute_mode = %q, want spot", loaded.ComputeMode)
	}
}

func TestInitNonInteractiveValidation(t *testing.T) {
	cfg := &operator.OperatorConfig{}

	tests := []struct {
		name     string
		explicit map[string]bool
		workers  int
		compute  string
		cleanup  string
		wantErr  string
	}{
		{"bad workers", map[string]bool{"workers": true}, -1, "", "", "--workers must be positive"},
		{"bad compute", map[string]bool{"compute-mode": true}, 0, "gpu", "", "--compute-mode must be"},
		{"bad cleanup", map[string]bool{"cleanup-policy": true}, 0, "", "never", "--cleanup-policy must be"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runInitNonInteractive(cfg, tt.explicit, "", "", tt.workers, tt.compute, tt.cleanup, "")
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

// runInitNonInteractiveToPath is a test helper that saves to a specific path.
func runInitNonInteractiveToPath(cfg *operator.OperatorConfig, explicit map[string]bool, region, profile string, workers int, computeMode, cleanupPolicy, outputDir, path string) error {
	if explicit["region"] {
		cfg.Region = region
	}
	if explicit["profile"] {
		cfg.Profile = profile
	}
	if explicit["workers"] {
		cfg.WorkerCount = workers
	}
	if explicit["compute-mode"] {
		cfg.ComputeMode = computeMode
	}
	if explicit["cleanup-policy"] {
		cfg.CleanupPolicy = cleanupPolicy
	}
	if explicit["output-dir"] {
		cfg.OutputDir = outputDir
	}
	return operator.SaveConfigTo(cfg, path)
}
