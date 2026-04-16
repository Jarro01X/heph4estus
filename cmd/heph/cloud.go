package main

import (
	"fmt"

	"heph4estus/internal/cloud"
	"heph4estus/internal/operator"
)

// resolveCLICloud parses the --cloud flag value, falls back to saved operator
// config, and validates the result.
func resolveCLICloud(explicit string, cfg *operator.OperatorConfig) (cloud.Kind, error) {
	kind, err := operator.ResolveCloud(explicit, cfg)
	if err != nil {
		return "", fmt.Errorf("--cloud: %w", err)
	}
	return kind, nil
}

// requireDeploySupport returns an error when the selected cloud does not
// yet support infrastructure deploy/destroy.
func requireDeploySupport(kind cloud.Kind) error {
	if kind.IsProviderNative() {
		return nil // Hetzner has provider-native deploy support
	}
	if kind.IsSelfhostedFamily() {
		return fmt.Errorf("%s infrastructure deploy/destroy is not supported — use 'hetzner' for provider-native VPS deploy, or 'manual' with your own infrastructure", kind.Canonical())
	}
	return nil
}

// ValidateComputeMode delegates to cloud.ValidateComputeMode — the single
// authority for cloud-specific compute-mode policy.
func ValidateComputeMode(kind cloud.Kind, mode string) error {
	return cloud.ValidateComputeMode(kind, mode)
}
