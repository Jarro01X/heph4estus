package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"heph4estus/internal/logger"
	"heph4estus/internal/operator"
)

// mainContext returns a background context for top-level CLI operations.
func mainContext() context.Context {
	return context.Background()
}

const usage = `Usage: heph <command> [options]

Commands:
  nmap     Run an nmap scan (auto-deploys infrastructure if needed)
  scan     Run a generic tool scan (e.g. httpx, nuclei, ffuf; auto-deploys if needed)
  infra    Manage cloud infrastructure explicitly (deploy/destroy/backup/recover/trust/rotate)
  fleet    Inspect and manage provider-native fleet state
  bench    Run provider-native fleet benchmark probes
  status   Check job status (--job-id required)
  doctor   Check prerequisites and environment health
  init     Set up or update operator defaults (region, profile, workers, etc.)

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
	case "fleet":
		return runFleet(cmdArgs, log)
	case "bench":
		return runBench(cmdArgs, log)
	case "status":
		return runStatus(cmdArgs, log)
	case "doctor":
		return runDoctor(cmdArgs, log)
	case "init":
		return runInit(cmdArgs, log)
	case "--help", "-help", "-h":
		fmt.Fprintln(os.Stderr, usage)
		return nil
	default:
		fmt.Fprintln(os.Stderr, usage)
		return fmt.Errorf("unknown command: %s", cmd)
	}
}

// newTracker returns a job tracker backed by the default store.
// If the config directory is unavailable, returns a noop tracker
// so CLI commands still work without job persistence.
func newTracker() *operator.Tracker {
	store, err := operator.NewJobStore()
	if err != nil {
		return operator.NoopTracker()
	}
	return operator.NewTracker(store)
}

func main() {
	log := logger.NewSimpleLogger()

	// Apply saved operator defaults (region, profile) when env is unset.
	if cfg, err := operator.LoadConfig(); err == nil {
		operator.ApplyEnvDefaults(cfg)
	}

	err := run(os.Args[1:], log)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		var ee exitError
		if errors.As(err, &ee) {
			os.Exit(ee.code)
		}
		log.Error("%v", err)
		os.Exit(1)
	}
}
