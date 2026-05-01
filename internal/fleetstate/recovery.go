package fleetstate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type RecoveryManifest struct {
	Version    int                `json:"version"`
	CreatedAt  time.Time          `json:"created_at"`
	ToolName   string             `json:"tool_name"`
	Cloud      string             `json:"cloud"`
	Outputs    map[string]string  `json:"outputs"`
	Rollout    *RolloutRecord     `json:"rollout,omitempty"`
	Reputation []ReputationRecord `json:"reputation,omitempty"`
}

func WriteRecoveryManifest(path string, manifest *RecoveryManifest) error {
	if manifest == nil {
		return fmt.Errorf("recovery manifest is required")
	}
	if manifest.Version == 0 {
		manifest.Version = 1
	}
	if manifest.CreatedAt.IsZero() {
		manifest.CreatedAt = time.Now().UTC()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating recovery manifest dir: %w", err)
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling recovery manifest: %w", err)
	}
	return os.WriteFile(path, data, 0o600)
}

func ReadRecoveryManifest(path string) (*RecoveryManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading recovery manifest: %w", err)
	}
	var manifest RecoveryManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parsing recovery manifest: %w", err)
	}
	return &manifest, nil
}
