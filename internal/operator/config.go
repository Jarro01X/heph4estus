package operator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"heph4estus/internal/cloud"
)

const appName = "heph4estus"

// OperatorConfig holds persisted per-user operator defaults.
type OperatorConfig struct {
	Region        string `json:"region,omitempty"`
	Profile       string `json:"profile,omitempty"`
	WorkerCount   int    `json:"worker_count,omitempty"`
	ComputeMode   string `json:"compute_mode,omitempty"`
	CleanupPolicy string `json:"cleanup_policy,omitempty"` // "reuse" or "destroy-after"
	OutputDir     string `json:"output_dir,omitempty"`
	// Cloud is the persisted default cloud kind ("aws" or "selfhosted").
	// Empty means "use the built-in default" (AWS).
	Cloud string `json:"cloud,omitempty"`
}

// ConfigDir returns the operator config directory path.
// It uses os.UserConfigDir() as the parent, creating <config-dir>/heph4estus/.
func ConfigDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolving config dir: %w", err)
	}
	return filepath.Join(base, appName), nil
}

// configPath returns the full path to the operator config file.
func configPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// LoadConfig loads the operator config from the default path.
// Returns a zero-value config (not an error) if the file does not exist.
func LoadConfig() (*OperatorConfig, error) {
	p, err := configPath()
	if err != nil {
		return &OperatorConfig{}, nil
	}
	return LoadConfigFrom(p)
}

// LoadConfigFrom loads the operator config from a specific path.
// Returns a zero-value config (not an error) if the file does not exist.
func LoadConfigFrom(path string) (*OperatorConfig, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &OperatorConfig{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading operator config: %w", err)
	}

	var cfg OperatorConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing operator config %s: %w", path, err)
	}
	return &cfg, nil
}

// SaveConfig writes the operator config to the default path atomically.
func SaveConfig(cfg *OperatorConfig) error {
	p, err := configPath()
	if err != nil {
		return err
	}
	return SaveConfigTo(cfg, p)
}

// SaveConfigTo writes the operator config to a specific path atomically.
func SaveConfigTo(cfg *OperatorConfig, path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling operator config: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("writing operator config: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("committing operator config: %w", err)
	}
	return nil
}

// Defaults holds the built-in default values for operator settings.
var Defaults = OperatorConfig{
	WorkerCount: 10,
	ComputeMode: "auto",
}

// ResolveWorkers returns the effective worker count given an explicit flag
// value (0 means unset) and saved operator config.
func ResolveWorkers(explicit int, cfg *OperatorConfig) int {
	if explicit > 0 {
		return explicit
	}
	if cfg != nil && cfg.WorkerCount > 0 {
		return cfg.WorkerCount
	}
	return Defaults.WorkerCount
}

// ResolveComputeMode returns the effective compute mode given an explicit
// flag value ("" means unset) and saved operator config.
func ResolveComputeMode(explicit string, cfg *OperatorConfig) string {
	if explicit != "" {
		return explicit
	}
	if cfg != nil && cfg.ComputeMode != "" {
		return cfg.ComputeMode
	}
	return Defaults.ComputeMode
}

// ResolveCleanupPolicy returns the effective cleanup policy given an explicit
// value ("" means unset) and saved operator config. Defaults to "reuse".
func ResolveCleanupPolicy(explicit string, cfg *OperatorConfig) string {
	if explicit != "" {
		return explicit
	}
	if cfg != nil && cfg.CleanupPolicy != "" {
		return cfg.CleanupPolicy
	}
	return "reuse"
}

// ResolveCloud returns the effective cloud kind given an explicit flag
// value ("" means unset) and saved operator config. The result is validated
// through cloud.ParseKind so callers receive a typed Kind.
func ResolveCloud(explicit string, cfg *OperatorConfig) (cloud.Kind, error) {
	if explicit != "" {
		return cloud.ParseKind(explicit)
	}
	if cfg != nil && cfg.Cloud != "" {
		return cloud.ParseKind(cfg.Cloud)
	}
	return cloud.DefaultKind, nil
}

// ResolveOutputDir returns the effective output directory given an explicit
// value ("" means unset) and saved operator config.
func ResolveOutputDir(explicit string, cfg *OperatorConfig) string {
	if explicit != "" {
		return explicit
	}
	if cfg != nil && cfg.OutputDir != "" {
		return cfg.OutputDir
	}
	return ""
}

// ResolveRegion returns the effective region given an explicit flag value
// ("" means unset) and saved operator config. Falls back to us-east-1.
func ResolveRegion(explicit string, cfg *OperatorConfig) string {
	if explicit != "" {
		return explicit
	}
	if v := os.Getenv("AWS_REGION"); v != "" {
		return v
	}
	if v := os.Getenv("AWS_DEFAULT_REGION"); v != "" {
		return v
	}
	if cfg != nil && cfg.Region != "" {
		return cfg.Region
	}
	return "us-east-1"
}

// ApplyEnvDefaults sets AWS environment variables from saved config when
// the corresponding env vars are not already set. This should be called
// once near CLI/TUI startup so all subprocesses see the same defaults.
func ApplyEnvDefaults(cfg *OperatorConfig) {
	if cfg.Region != "" {
		if os.Getenv("AWS_REGION") == "" {
			_ = os.Setenv("AWS_REGION", cfg.Region)
		}
		if os.Getenv("AWS_DEFAULT_REGION") == "" {
			_ = os.Setenv("AWS_DEFAULT_REGION", cfg.Region)
		}
	}
	if cfg.Profile != "" {
		if os.Getenv("AWS_PROFILE") == "" {
			_ = os.Setenv("AWS_PROFILE", cfg.Profile)
		}
	}
}
