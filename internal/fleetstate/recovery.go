package fleetstate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type RecoveryManifest struct {
	Version              int                `json:"version"`
	CreatedAt            time.Time          `json:"created_at"`
	ToolName             string             `json:"tool_name"`
	Cloud                string             `json:"cloud"`
	ControllerGeneration string             `json:"controller_generation,omitempty"`
	WorkerCount          int                `json:"worker_count,omitempty"`
	Outputs              map[string]string  `json:"outputs"`
	OutputKeys           []string           `json:"output_keys,omitempty"`
	RecoverableArtifacts []string           `json:"recoverable_artifacts,omitempty"`
	Warnings             []string           `json:"warnings,omitempty"`
	Rollout              *RolloutRecord     `json:"rollout,omitempty"`
	Reputation           []ReputationRecord `json:"reputation,omitempty"`
}

func BuildRecoveryManifest(tool, cloud string, outputs map[string]string, rollout *RolloutRecord, reputation []ReputationRecord) *RecoveryManifest {
	manifest := &RecoveryManifest{
		Version:    1,
		CreatedAt:  time.Now().UTC(),
		ToolName:   strings.TrimSpace(tool),
		Cloud:      strings.TrimSpace(cloud),
		Outputs:    copyOutputs(outputs),
		Rollout:    rollout,
		Reputation: append([]ReputationRecord(nil), reputation...),
		Warnings: []string{
			"backup captures local recovery metadata only; it does not restore live NATS messages or MinIO object contents",
		},
	}
	if generation := strings.TrimSpace(manifest.Outputs["generation_id"]); generation != "" {
		manifest.ControllerGeneration = generation
	} else if rollout != nil && rollout.DesiredGeneration != "" {
		manifest.ControllerGeneration = rollout.DesiredGeneration
	}
	if rawWorkers := strings.TrimSpace(manifest.Outputs["worker_count"]); rawWorkers != "" {
		if workers, err := strconv.Atoi(rawWorkers); err == nil {
			manifest.WorkerCount = workers
		}
	}
	for key := range manifest.Outputs {
		manifest.OutputKeys = append(manifest.OutputKeys, key)
	}
	sort.Strings(manifest.OutputKeys)
	manifest.RecoverableArtifacts = []string{"terraform_outputs"}
	if rollout != nil {
		manifest.RecoverableArtifacts = append(manifest.RecoverableArtifacts, "rollout_state")
	}
	if len(reputation) > 0 {
		manifest.RecoverableArtifacts = append(manifest.RecoverableArtifacts, "reputation_state")
	}
	return manifest
}

func (m *RecoveryManifest) Validate(expectedTool, expectedCloud string) error {
	if m == nil {
		return fmt.Errorf("recovery manifest is required")
	}
	if strings.TrimSpace(m.ToolName) == "" {
		return fmt.Errorf("recovery manifest missing tool name")
	}
	if strings.TrimSpace(m.Cloud) == "" {
		return fmt.Errorf("recovery manifest missing cloud")
	}
	if expectedTool != "" && m.ToolName != expectedTool {
		return fmt.Errorf("recovery manifest tool mismatch: %q != %q", m.ToolName, expectedTool)
	}
	if expectedCloud != "" && m.Cloud != expectedCloud {
		return fmt.Errorf("recovery manifest cloud mismatch: %q != %q", m.Cloud, expectedCloud)
	}
	if len(m.Outputs) == 0 {
		return fmt.Errorf("recovery manifest contains no terraform outputs")
	}
	return nil
}

func (m *RecoveryManifest) SummaryLines() []string {
	if m == nil {
		return nil
	}
	lines := []string{
		fmt.Sprintf("Manifest:    v%d", m.Version),
		fmt.Sprintf("Created:     %s", m.CreatedAt.Format(time.RFC3339)),
		fmt.Sprintf("Tool:        %s", m.ToolName),
		fmt.Sprintf("Cloud:       %s", m.Cloud),
	}
	if m.ControllerGeneration != "" {
		lines = append(lines, fmt.Sprintf("Generation:  %s", m.ControllerGeneration))
	}
	if m.WorkerCount > 0 {
		lines = append(lines, fmt.Sprintf("Workers:     %d", m.WorkerCount))
	}
	if len(m.OutputKeys) > 0 {
		lines = append(lines, fmt.Sprintf("Outputs:     %s", strings.Join(m.OutputKeys, ", ")))
	}
	if len(m.RecoverableArtifacts) > 0 {
		lines = append(lines, fmt.Sprintf("Restores:    %s", strings.Join(m.RecoverableArtifacts, ", ")))
	}
	if len(m.Warnings) > 0 {
		for _, warning := range m.Warnings {
			lines = append(lines, fmt.Sprintf("Warning:     %s", warning))
		}
	}
	return lines
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

func copyOutputs(src map[string]string) map[string]string {
	if src == nil {
		return map[string]string{}
	}
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
