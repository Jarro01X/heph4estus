package main

import (
	"fmt"
	"os"

	"heph4estus/internal/logger"
)

func runStatus(args []string, log logger.Logger) error {
	_ = args
	fmt.Fprintln(os.Stderr, "status: not yet implemented (planned for Phase 3)")
	return nil
}
