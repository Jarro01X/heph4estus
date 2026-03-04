package main

import (
	"fmt"
	"os"

	"heph4estus/internal/logger"
)

func runInfra(args []string, log logger.Logger) error {
	if len(args) == 0 {
		return fmt.Errorf("infra requires a subcommand: deploy, destroy")
	}

	sub := args[0]
	switch sub {
	case "deploy":
		fmt.Fprintln(os.Stderr, "infra deploy: not yet implemented (planned for Phase 4)")
		return nil
	case "destroy":
		fmt.Fprintln(os.Stderr, "infra destroy: not yet implemented (planned for Phase 4)")
		return nil
	default:
		return fmt.Errorf("infra: unknown subcommand %q (expected deploy or destroy)", sub)
	}
}
