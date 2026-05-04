package infra

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const RotationAutoVarsFileName = "heph_rotation.auto.tfvars.json"

func RotationAutoVarsPath(terraformDir string) string {
	return filepath.Join(terraformDir, RotationAutoVarsFileName)
}

func ReadRotationAutoVars(terraformDir string) (map[string]string, error) {
	path := RotationAutoVarsPath(terraformDir)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("reading rotation auto vars: %w", err)
	}
	var vars map[string]string
	if err := json.Unmarshal(data, &vars); err != nil {
		return nil, fmt.Errorf("parsing rotation auto vars: %w", err)
	}
	if vars == nil {
		vars = map[string]string{}
	}
	return vars, nil
}

func MergeRotationAutoVars(terraformDir string, updates map[string]string) (map[string]string, error) {
	vars, err := ReadRotationAutoVars(terraformDir)
	if err != nil {
		return nil, err
	}
	for key, value := range updates {
		if value == "" {
			continue
		}
		vars[key] = value
	}
	if err := WriteRotationAutoVars(terraformDir, vars); err != nil {
		return nil, err
	}
	return vars, nil
}

func WriteRotationAutoVars(terraformDir string, vars map[string]string) error {
	if vars == nil {
		vars = map[string]string{}
	}
	data, err := json.MarshalIndent(vars, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding rotation auto vars: %w", err)
	}
	data = append(data, '\n')
	path := RotationAutoVarsPath(terraformDir)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("writing rotation auto vars: %w", err)
	}
	return nil
}
