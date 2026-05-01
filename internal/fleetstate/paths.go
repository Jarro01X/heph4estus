package fleetstate

import (
	"fmt"
	"os"
	"path/filepath"
)

const appName = "heph4estus"

func stateDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolving user config dir: %w", err)
	}
	return filepath.Join(base, appName, "fleet"), nil
}

func stateFile(name string) (string, error) {
	dir, err := stateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}
