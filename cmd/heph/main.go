package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"heph4estus/internal/logger"
)

// mainContext returns a background context for top-level CLI operations.
func mainContext() context.Context {
	return context.Background()
}

const usage = `Usage: heph <command> [options]

Commands:
  nmap     Run an nmap scan (auto-deploys infrastructure if needed)
  scan     Run a generic tool scan (e.g. httpx, nuclei, ffuf; auto-deploys if needed)
  infra    Manage cloud infrastructure explicitly (deploy/destroy)
  status   Check job status (planned)

Run 'heph <command> --help' for command-specific usage.`

func run(args []string, log logger.Logger) error {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, usage)
		return fmt.Errorf("no command specified")
	}

	cmd := args[0]
	cmdArgs := args[1:]

	switch cmd {
	case "nmap":
		return runNmap(cmdArgs, log)
	case "scan":
		return runScan(cmdArgs, log)
	case "infra":
		return runInfra(cmdArgs, log)
	case "status":
		return runStatus(cmdArgs, log)
	case "--help", "-help", "-h":
		fmt.Fprintln(os.Stderr, usage)
		return nil
	default:
		fmt.Fprintln(os.Stderr, usage)
		return fmt.Errorf("unknown command: %s", cmd)
	}
}

func main() {
	log := logger.NewSimpleLogger()

	err := run(os.Args[1:], log)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		log.Error("%v", err)
		os.Exit(1)
	}
}
