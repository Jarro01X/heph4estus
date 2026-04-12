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

// requireComputeSupport returns an error when the selected cloud does not
// yet have compute/deploy support. AWS is always supported; selfhosted
// compute and deploy land in PR 6.2/6.3.
func requireComputeSupport(kind cloud.Kind) error {
	if kind == cloud.KindSelfhosted {
		return fmt.Errorf("selfhosted compute and deploy support land in PR 6.2/6.3 — queue and storage are available but end-to-end scan execution requires compute")
	}
	return nil
}

// requireDeploySupport returns an error when the selected cloud does not
// yet support infrastructure deploy/destroy.
func requireDeploySupport(kind cloud.Kind) error {
	if kind == cloud.KindSelfhosted {
		return fmt.Errorf("selfhosted infrastructure deploy/destroy lands in PR 6.2/6.3")
	}
	return nil
}
