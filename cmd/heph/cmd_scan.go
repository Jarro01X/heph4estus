package main

import (
	"flag"
	"fmt"
	"os"

	"heph4estus/internal/logger"
)

func runScan(args []string, log logger.Logger) error {
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	tool := fs.String("tool", "", "Tool to run (e.g. nuclei, masscan)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *tool == "" {
		return fmt.Errorf("--tool flag is required")
	}

	fmt.Fprintf(os.Stderr, "scan: tool %q not yet implemented (planned for Phase 3)\n", *tool)
	return nil
}
