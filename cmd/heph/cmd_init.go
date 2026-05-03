package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"heph4estus/internal/cloud"
	"heph4estus/internal/fleet"
	"heph4estus/internal/logger"
	"heph4estus/internal/operator"
)

func runInit(args []string, log logger.Logger) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	region := fs.String("region", "", "AWS region")
	profile := fs.String("profile", "", "AWS profile")
	workers := fs.Int("workers", 0, "Default worker count")
	computeMode := fs.String("compute-mode", "", "Default compute mode: auto, fargate, or spot")
	cloudValue := fs.String("cloud", "", "Default cloud provider: "+cloud.SupportedKindsText())
	placementMode := fs.String("placement", "", "Default fleet placement policy: diversity or throughput")
	maxWorkersPerHost := fs.Int("max-workers-per-host", 0, "Default maximum admitted workers per host/public IP (0 = policy default)")
	minUniqueIPs := fs.Int("min-unique-ips", 0, "Default minimum unique public IPv4 addresses before scan start")
	ipv6Required := fs.Bool("ipv6-required", false, "Default to requiring IPv6-validated workers")
	dualStackRequired := fs.Bool("dual-stack-required", false, "Default to requiring workers with public IPv4 and IPv6-ready public IPv6")
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
		return runInitNonInteractive(existing, explicit, *region, *profile, *workers, *computeMode, *cloudValue, *placementMode, *maxWorkersPerHost, *minUniqueIPs, *ipv6Required, *dualStackRequired, *cleanupPolicy, *outputDir)
	}

	return runInitInteractive(existing)
}

func runInitNonInteractive(cfg *operator.OperatorConfig, explicit map[string]bool, region, profile string, workers int, computeMode, cloudValue, placementMode string, maxWorkersPerHost, minUniqueIPs int, ipv6Required, dualStackRequired bool, cleanupPolicy, outputDir string) error {
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
	if explicit["cloud"] {
		kind, err := cloud.ParseKind(cloudValue)
		if err != nil {
			return fmt.Errorf("--cloud: %w", err)
		}
		cfg.Cloud = string(kind.Canonical())
	}
	if explicit["placement"] {
		cfg.PlacementMode = strings.TrimSpace(placementMode)
	}
	if explicit["max-workers-per-host"] {
		if maxWorkersPerHost < 0 {
			return fmt.Errorf("--max-workers-per-host must be non-negative")
		}
		cfg.MaxWorkersPerHost = maxWorkersPerHost
	}
	if explicit["min-unique-ips"] {
		if minUniqueIPs < 0 {
			return fmt.Errorf("--min-unique-ips must be non-negative")
		}
		cfg.MinUniqueIPs = minUniqueIPs
	}
	if explicit["ipv6-required"] {
		cfg.IPv6Required = ipv6Required
	}
	if explicit["dual-stack-required"] {
		cfg.DualStackRequired = dualStackRequired
		if dualStackRequired {
			cfg.IPv6Required = true
		}
	}
	if hasPlacementDefaults(explicit) {
		if err := validatePlacementDefaults(cfg); err != nil {
			return err
		}
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

	cfg.Cloud = promptField(reader, "Cloud (aws/manual/hetzner/linode/scaleway/vultr)", cfg.Cloud, cloud.DefaultKind.String())
	if cfg.Cloud != "" {
		kind, err := cloud.ParseKind(cfg.Cloud)
		if err != nil {
			return err
		}
		cfg.Cloud = string(kind.Canonical())
	}

	cfg.PlacementMode = promptField(reader, "Placement (diversity/throughput)", cfg.PlacementMode, operator.Defaults.PlacementMode)
	if cfg.PlacementMode != "" {
		cfg.PlacementMode = strings.TrimSpace(cfg.PlacementMode)
	}

	maxStr := promptField(reader, "Max workers per host/IP (0=policy default)", intOrEmpty(cfg.MaxWorkersPerHost), "0")
	maxWorkersPerHost, err := parseOptionalNonNegativeInt(maxStr, "max workers per host")
	if err != nil {
		return err
	}
	cfg.MaxWorkersPerHost = maxWorkersPerHost

	minStr := promptField(reader, "Minimum unique IPv4s", intOrEmpty(cfg.MinUniqueIPs), "0")
	minUniqueIPs, err := parseOptionalNonNegativeInt(minStr, "minimum unique IPv4s")
	if err != nil {
		return err
	}
	cfg.MinUniqueIPs = minUniqueIPs

	ipv6Str := promptField(reader, "Require IPv6-ready workers (true/false)", boolOrEmpty(cfg.IPv6Required), "false")
	ipv6Required, err := parseBoolDefaultFalse(ipv6Str, "require IPv6-ready workers")
	if err != nil {
		return err
	}
	cfg.IPv6Required = ipv6Required

	dualStr := promptField(reader, "Require dual-stack workers (true/false)", boolOrEmpty(cfg.DualStackRequired), "false")
	dualStackRequired, err := parseBoolDefaultFalse(dualStr, "require dual-stack workers")
	if err != nil {
		return err
	}
	cfg.DualStackRequired = dualStackRequired
	if cfg.DualStackRequired {
		cfg.IPv6Required = true
	}
	if err := validatePlacementDefaults(cfg); err != nil {
		return err
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
	_, _ = fmt.Fprintf(os.Stdout, "region:         %s\n", valueOrDash(cfg.Region))
	_, _ = fmt.Fprintf(os.Stdout, "profile:        %s\n", valueOrDash(cfg.Profile))
	_, _ = fmt.Fprintf(os.Stdout, "worker_count:   %s\n", intValueOrDash(cfg.WorkerCount))
	_, _ = fmt.Fprintf(os.Stdout, "compute_mode:   %s\n", valueOrDash(cfg.ComputeMode))
	_, _ = fmt.Fprintf(os.Stdout, "cloud:          %s\n", valueOrDash(cfg.Cloud))
	_, _ = fmt.Fprintf(os.Stdout, "placement:      %s\n", valueOrDash(cfg.PlacementMode))
	_, _ = fmt.Fprintf(os.Stdout, "max_per_host:   %s\n", intValueOrDash(cfg.MaxWorkersPerHost))
	_, _ = fmt.Fprintf(os.Stdout, "min_unique_ips: %s\n", intValueOrDash(cfg.MinUniqueIPs))
	_, _ = fmt.Fprintf(os.Stdout, "ipv6_required:  %t\n", cfg.IPv6Required)
	_, _ = fmt.Fprintf(os.Stdout, "dual_stack:     %t\n", cfg.DualStackRequired)
	_, _ = fmt.Fprintf(os.Stdout, "cleanup_policy: %s\n", valueOrDash(cfg.CleanupPolicy))
	_, _ = fmt.Fprintf(os.Stdout, "output_dir:     %s\n", valueOrDash(cfg.OutputDir))

	dir, err := operator.ConfigDir()
	if err == nil {
		_, _ = fmt.Fprintf(os.Stdout, "\nConfig path: %s/config.json\n", dir)
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

func boolOrEmpty(v bool) string {
	if !v {
		return ""
	}
	return "true"
}

func parseOptionalNonNegativeInt(value, label string) (int, error) {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" || value == "auto" {
		return 0, nil
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%s must be a non-negative integer", label)
	}
	return n, nil
}

func parseBoolDefaultFalse(value, label string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "0", "f", "false", "n", "no", "off":
		return false, nil
	case "1", "t", "true", "y", "yes", "on":
		return true, nil
	default:
		return false, fmt.Errorf("%s must be true or false", label)
	}
}

func hasPlacementDefaults(explicit map[string]bool) bool {
	for _, name := range []string{"placement", "max-workers-per-host", "min-unique-ips", "ipv6-required", "dual-stack-required"} {
		if explicit[name] {
			return true
		}
	}
	return false
}

func validatePlacementDefaults(cfg *operator.OperatorConfig) error {
	policy := fleet.PlacementPolicy{
		Mode:              fleet.PlacementMode(strings.TrimSpace(cfg.PlacementMode)),
		MaxWorkersPerHost: cfg.MaxWorkersPerHost,
		MinUniqueIPs:      cfg.MinUniqueIPs,
		IPv6Required:      cfg.IPv6Required,
		DualStackRequired: cfg.DualStackRequired,
	}
	if err := policy.Normalize(0).Validate(); err != nil {
		return fmt.Errorf("placement defaults: %w", err)
	}
	return nil
}

func flagsSet(fs *flag.FlagSet) map[string]bool {
	m := make(map[string]bool)
	fs.Visit(func(f *flag.Flag) {
		m[f.Name] = true
	})
	return m
}
