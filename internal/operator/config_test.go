package operator

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigFrom_MissingFile(t *testing.T) {
	cfg, err := LoadConfigFrom("/nonexistent/config.json")
	if err != nil {
		t.Fatalf("missing config should not error: %v", err)
	}
	if cfg.Region != "" || cfg.Profile != "" || cfg.WorkerCount != 0 {
		t.Fatal("expected zero-value config for missing file")
	}
}

func TestSaveAndLoadConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	cfg := &OperatorConfig{
		Region:        "us-west-2",
		Profile:       "pentest",
		WorkerCount:   20,
		ComputeMode:   "spot",
		CleanupPolicy: "destroy-after",
		OutputDir:     "/tmp/results",
	}

	if err := SaveConfigTo(cfg, path); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	loaded, err := LoadConfigFrom(path)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if loaded.Region != "us-west-2" {
		t.Errorf("region = %q, want us-west-2", loaded.Region)
	}
	if loaded.Profile != "pentest" {
		t.Errorf("profile = %q, want pentest", loaded.Profile)
	}
	if loaded.WorkerCount != 20 {
		t.Errorf("worker_count = %d, want 20", loaded.WorkerCount)
	}
	if loaded.ComputeMode != "spot" {
		t.Errorf("compute_mode = %q, want spot", loaded.ComputeMode)
	}
	if loaded.CleanupPolicy != "destroy-after" {
		t.Errorf("cleanup_policy = %q, want destroy-after", loaded.CleanupPolicy)
	}
	if loaded.OutputDir != "/tmp/results" {
		t.Errorf("output_dir = %q, want /tmp/results", loaded.OutputDir)
	}
}

func TestSaveConfig_CreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "deep", "config.json")

	cfg := &OperatorConfig{Region: "eu-west-1"}
	if err := SaveConfigTo(cfg, path); err != nil {
		t.Fatalf("save to nested path failed: %v", err)
	}

	loaded, err := LoadConfigFrom(path)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if loaded.Region != "eu-west-1" {
		t.Errorf("region = %q, want eu-west-1", loaded.Region)
	}
}

func TestLoadConfigFrom_MalformedJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	os.WriteFile(path, []byte("{invalid json"), 0o600)

	_, err := LoadConfigFrom(path)
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

func TestSaveConfig_Atomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	// Write initial config.
	if err := SaveConfigTo(&OperatorConfig{Region: "us-east-1"}, path); err != nil {
		t.Fatal(err)
	}

	// Overwrite with new config.
	if err := SaveConfigTo(&OperatorConfig{Region: "ap-southeast-1"}, path); err != nil {
		t.Fatal(err)
	}

	// No .tmp file should remain.
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("temporary file should not remain after atomic write")
	}

	loaded, err := LoadConfigFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Region != "ap-southeast-1" {
		t.Errorf("region = %q, want ap-southeast-1", loaded.Region)
	}
}

func TestApplyEnvDefaults_SetsWhenEmpty(t *testing.T) {
	// Save and restore env.
	for _, k := range []string{"AWS_REGION", "AWS_DEFAULT_REGION", "AWS_PROFILE"} {
		old := os.Getenv(k)
		t.Cleanup(func() { os.Setenv(k, old) })
		os.Unsetenv(k)
	}

	cfg := &OperatorConfig{Region: "eu-central-1", Profile: "dev"}
	ApplyEnvDefaults(cfg)

	if v := os.Getenv("AWS_REGION"); v != "eu-central-1" {
		t.Errorf("AWS_REGION = %q, want eu-central-1", v)
	}
	if v := os.Getenv("AWS_DEFAULT_REGION"); v != "eu-central-1" {
		t.Errorf("AWS_DEFAULT_REGION = %q, want eu-central-1", v)
	}
	if v := os.Getenv("AWS_PROFILE"); v != "dev" {
		t.Errorf("AWS_PROFILE = %q, want dev", v)
	}
}

func TestApplyEnvDefaults_DoesNotOverride(t *testing.T) {
	for _, k := range []string{"AWS_REGION", "AWS_DEFAULT_REGION", "AWS_PROFILE"} {
		old := os.Getenv(k)
		t.Cleanup(func() { os.Setenv(k, old) })
	}

	os.Setenv("AWS_REGION", "existing-region")
	os.Setenv("AWS_DEFAULT_REGION", "existing-default")
	os.Setenv("AWS_PROFILE", "existing-profile")

	cfg := &OperatorConfig{Region: "new-region", Profile: "new-profile"}
	ApplyEnvDefaults(cfg)

	if v := os.Getenv("AWS_REGION"); v != "existing-region" {
		t.Errorf("AWS_REGION = %q, want existing-region", v)
	}
	if v := os.Getenv("AWS_DEFAULT_REGION"); v != "existing-default" {
		t.Errorf("AWS_DEFAULT_REGION = %q, want existing-default", v)
	}
	if v := os.Getenv("AWS_PROFILE"); v != "existing-profile" {
		t.Errorf("AWS_PROFILE = %q, want existing-profile", v)
	}
}

func TestResolveWorkers(t *testing.T) {
	tests := []struct {
		name     string
		explicit int
		cfg      *OperatorConfig
		want     int
	}{
		{"explicit wins", 20, &OperatorConfig{WorkerCount: 5}, 20},
		{"config used when explicit=0", 0, &OperatorConfig{WorkerCount: 30}, 30},
		{"built-in default when both empty", 0, &OperatorConfig{}, Defaults.WorkerCount},
		{"nil config falls to default", 0, nil, Defaults.WorkerCount},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveWorkers(tt.explicit, tt.cfg)
			if got != tt.want {
				t.Errorf("ResolveWorkers(%d, cfg) = %d, want %d", tt.explicit, got, tt.want)
			}
		})
	}
}

func TestResolveComputeMode(t *testing.T) {
	tests := []struct {
		name     string
		explicit string
		cfg      *OperatorConfig
		want     string
	}{
		{"explicit wins", "spot", &OperatorConfig{ComputeMode: "fargate"}, "spot"},
		{"config used when explicit empty", "", &OperatorConfig{ComputeMode: "fargate"}, "fargate"},
		{"built-in default when both empty", "", &OperatorConfig{}, Defaults.ComputeMode},
		{"nil config falls to default", "", nil, Defaults.ComputeMode},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveComputeMode(tt.explicit, tt.cfg)
			if got != tt.want {
				t.Errorf("ResolveComputeMode(%q, cfg) = %q, want %q", tt.explicit, got, tt.want)
			}
		})
	}
}

func TestResolveRegion(t *testing.T) {
	// Clear env for clean tests.
	for _, k := range []string{"AWS_REGION", "AWS_DEFAULT_REGION"} {
		old := os.Getenv(k)
		t.Cleanup(func() { os.Setenv(k, old) })
		os.Unsetenv(k)
	}

	t.Run("explicit wins", func(t *testing.T) {
		got := ResolveRegion("eu-west-1", &OperatorConfig{Region: "us-west-2"})
		if got != "eu-west-1" {
			t.Errorf("got %q, want eu-west-1", got)
		}
	})

	t.Run("env wins over config", func(t *testing.T) {
		os.Setenv("AWS_REGION", "ap-south-1")
		t.Cleanup(func() { os.Unsetenv("AWS_REGION") })
		got := ResolveRegion("", &OperatorConfig{Region: "us-west-2"})
		if got != "ap-south-1" {
			t.Errorf("got %q, want ap-south-1", got)
		}
	})

	t.Run("config used when env empty", func(t *testing.T) {
		os.Unsetenv("AWS_REGION")
		os.Unsetenv("AWS_DEFAULT_REGION")
		got := ResolveRegion("", &OperatorConfig{Region: "us-west-2"})
		if got != "us-west-2" {
			t.Errorf("got %q, want us-west-2", got)
		}
	})

	t.Run("falls back to us-east-1", func(t *testing.T) {
		os.Unsetenv("AWS_REGION")
		os.Unsetenv("AWS_DEFAULT_REGION")
		got := ResolveRegion("", &OperatorConfig{})
		if got != "us-east-1" {
			t.Errorf("got %q, want us-east-1", got)
		}
	})
}

func TestResolveCleanupPolicy(t *testing.T) {
	tests := []struct {
		name     string
		explicit string
		cfg      *OperatorConfig
		want     string
	}{
		{"explicit wins", "destroy-after", &OperatorConfig{CleanupPolicy: "reuse"}, "destroy-after"},
		{"config used when explicit empty", "", &OperatorConfig{CleanupPolicy: "destroy-after"}, "destroy-after"},
		{"defaults to reuse when both empty", "", &OperatorConfig{}, "reuse"},
		{"nil config defaults to reuse", "", nil, "reuse"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveCleanupPolicy(tt.explicit, tt.cfg)
			if got != tt.want {
				t.Errorf("ResolveCleanupPolicy(%q, cfg) = %q, want %q", tt.explicit, got, tt.want)
			}
		})
	}
}

func TestResolveOutputDir(t *testing.T) {
	tests := []struct {
		name     string
		explicit string
		cfg      *OperatorConfig
		want     string
	}{
		{"explicit wins", "/explicit", &OperatorConfig{OutputDir: "/saved"}, "/explicit"},
		{"config used when explicit empty", "", &OperatorConfig{OutputDir: "/saved"}, "/saved"},
		{"empty when both empty", "", &OperatorConfig{}, ""},
		{"nil config returns empty", "", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveOutputDir(tt.explicit, tt.cfg)
			if got != tt.want {
				t.Errorf("ResolveOutputDir(%q, cfg) = %q, want %q", tt.explicit, got, tt.want)
			}
		})
	}
}

func TestApplyEnvDefaults_EmptyConfig(t *testing.T) {
	for _, k := range []string{"AWS_REGION", "AWS_DEFAULT_REGION", "AWS_PROFILE"} {
		old := os.Getenv(k)
		t.Cleanup(func() { os.Setenv(k, old) })
		os.Unsetenv(k)
	}

	cfg := &OperatorConfig{}
	ApplyEnvDefaults(cfg)

	if v := os.Getenv("AWS_REGION"); v != "" {
		t.Errorf("AWS_REGION should remain empty, got %q", v)
	}
}
