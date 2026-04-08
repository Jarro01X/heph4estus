package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"heph4estus/internal/logger"
	"heph4estus/internal/operator"
)

func runInit(args []string, log logger.Logger) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	region := fs.String("region", "", "AWS region")
	profile := fs.String("profile", "", "AWS profile")
	workers := fs.Int("workers", 0, "Default worker count")
	computeMode := fs.String("compute-mode", "", "Default compute mode: auto, fargate, or spot")
	cleanupPolicy := fs.String("cleanup-policy", "", "Default cleanup policy: reuse or destroy-after")
	outputDir := fs.String("output-dir", "", "Default output directory for results")
	show := fs.Bool("show", false, "Show current config and exit")

	if err := fs.Parse(args); err != nil {
		return err
	}

	existing, err := operator.LoadConfig()
	if err != nil {
		log.Error("Warning: could not load existing config: %v", err)
		existing = &operator.OperatorConfig{}
	}

	if *show {
		return printConfig(existing)
	}

	// Determine whether any flags were explicitly passed.
	explicit := flagsSet(fs)

	if len(explicit) > 0 {
		return runInitNonInteractive(existing, explicit, *region, *profile, *workers, *computeMode, *cleanupPolicy, *outputDir)
	}

	return runInitInteractive(existing)
}

func runInitNonInteractive(cfg *operator.OperatorConfig, explicit map[string]bool, region, profile string, workers int, computeMode, cleanupPolicy, outputDir string) error {
	if explicit["region"] {
		cfg.Region = region
	}
	if explicit["profile"] {
		cfg.Profile = profile
	}
	if explicit["workers"] {
		if workers <= 0 {
			return fmt.Errorf("--workers must be positive")
		}
		cfg.WorkerCount = workers
	}
	if explicit["compute-mode"] {
		if computeMode != "auto" && computeMode != "fargate" && computeMode != "spot" {
			return fmt.Errorf("--compute-mode must be auto, fargate, or spot")
		}
		cfg.ComputeMode = computeMode
	}
	if explicit["cleanup-policy"] {
		if cleanupPolicy != "reuse" && cleanupPolicy != "destroy-after" {
			return fmt.Errorf("--cleanup-policy must be reuse or destroy-after")
		}
		cfg.CleanupPolicy = cleanupPolicy
	}
	if explicit["output-dir"] {
		cfg.OutputDir = outputDir
	}

	if err := operator.SaveConfig(cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	fmt.Fprintln(os.Stderr, "Config saved.")
	return printConfig(cfg)
}

func runInitInteractive(cfg *operator.OperatorConfig) error {
	reader := bufio.NewReader(os.Stdin)

	fmt.Fprintln(os.Stderr, "Heph4estus — operator setup")
	fmt.Fprintln(os.Stderr, "Press Enter to keep the current value.")
	fmt.Fprintln(os.Stderr)

	cfg.Region = promptField(reader, "AWS region", cfg.Region, "")
	cfg.Profile = promptField(reader, "AWS profile", cfg.Profile, "")

	wStr := promptField(reader, "Worker count", intOrEmpty(cfg.WorkerCount), strconv.Itoa(operator.Defaults.WorkerCount))
	if wStr != "" {
		w, err := strconv.Atoi(wStr)
		if err != nil || w <= 0 {
			return fmt.Errorf("worker count must be a positive integer")
		}
		cfg.WorkerCount = w
	}

	cfg.ComputeMode = promptField(reader, "Compute mode (auto/fargate/spot)", cfg.ComputeMode, operator.Defaults.ComputeMode)
	if cfg.ComputeMode != "" && cfg.ComputeMode != "auto" && cfg.ComputeMode != "fargate" && cfg.ComputeMode != "spot" {
		return fmt.Errorf("compute mode must be auto, fargate, or spot")
	}

	cfg.CleanupPolicy = promptField(reader, "Cleanup policy (reuse/destroy-after)", cfg.CleanupPolicy, "")
	if cfg.CleanupPolicy != "" && cfg.CleanupPolicy != "reuse" && cfg.CleanupPolicy != "destroy-after" {
		return fmt.Errorf("cleanup policy must be reuse or destroy-after")
	}

	cfg.OutputDir = promptField(reader, "Output directory", cfg.OutputDir, "")

	if err := operator.SaveConfig(cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	fmt.Fprintln(os.Stderr, "\nConfig saved.")
	return printConfig(cfg)
}

func promptField(reader *bufio.Reader, label, current, fallback string) string {
	display := current
	if display == "" {
		display = fallback
	}
	if display != "" {
		fmt.Fprintf(os.Stderr, "  %s [%s]: ", label, display)
	} else {
		fmt.Fprintf(os.Stderr, "  %s: ", label)
	}
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return current
	}
	return line
}

func printConfig(cfg *operator.OperatorConfig) error {
	fmt.Fprintf(os.Stdout, "region:         %s\n", valueOrDash(cfg.Region))
	fmt.Fprintf(os.Stdout, "profile:        %s\n", valueOrDash(cfg.Profile))
	fmt.Fprintf(os.Stdout, "worker_count:   %s\n", intValueOrDash(cfg.WorkerCount))
	fmt.Fprintf(os.Stdout, "compute_mode:   %s\n", valueOrDash(cfg.ComputeMode))
	fmt.Fprintf(os.Stdout, "cleanup_policy: %s\n", valueOrDash(cfg.CleanupPolicy))
	fmt.Fprintf(os.Stdout, "output_dir:     %s\n", valueOrDash(cfg.OutputDir))

	dir, err := operator.ConfigDir()
	if err == nil {
		fmt.Fprintf(os.Stdout, "\nConfig path: %s/config.json\n", dir)
	}
	return nil
}

func valueOrDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func intValueOrDash(n int) string {
	if n == 0 {
		return "-"
	}
	return strconv.Itoa(n)
}

func intOrEmpty(n int) string {
	if n == 0 {
		return ""
	}
	return strconv.Itoa(n)
}

func flagsSet(fs *flag.FlagSet) map[string]bool {
	m := make(map[string]bool)
	fs.Visit(func(f *flag.Flag) {
		m[f.Name] = true
	})
	return m
}
