package main

import (
	"flag"
	"fmt"
	"os"

	"heph4estus/internal/cloud"
	"heph4estus/internal/fleet"
	"heph4estus/internal/infra"
	"heph4estus/internal/jobs"
	"heph4estus/internal/logger"
	"heph4estus/internal/operator"
	"heph4estus/internal/scanapp"
	"heph4estus/internal/scanruntime"
)

func runNmap(args []string, log logger.Logger) error {
	fs := flag.NewFlagSet("nmap", flag.ContinueOnError)
	inputFile := fs.String("file", "", "Path to file containing targets (required)")
	defaultOptions := fs.String("default-options", "-sS", "Default nmap options")
	workers := fs.Int("workers", 0, "Number of worker tasks to launch (default: from config or 10)")
	computeMode := fs.String("compute-mode", "", "Compute mode: auto, fargate, or spot (default: from config or auto)")
	placementMode := fs.String("placement", "", "Fleet placement policy: diversity or throughput (default: from config or diversity)")
	maxWorkersPerHost := fs.Int("max-workers-per-host", 0, "Maximum admitted workers per host/public IP (default: from config or policy)")
	minUniqueIPs := fs.Int("min-unique-ips", 0, "Minimum unique public IPv4 addresses required before scan start")
	ipv6Required := fs.Bool("ipv6-required", false, "Require IPv6-validated workers before scan start")
	dualStackRequired := fs.Bool("dual-stack-required", false, "Require workers with both public IPv4 and IPv6-ready public IPv6")
	mode := fs.String("mode", "target-only", "Distribution mode: target-only or target-ports")
	portChunks := fs.Int("port-chunks", 5, "Number of port chunks per target (target-ports mode only)")
	dnsServers := fs.String("dns-servers", "", "DNS servers for nmap (comma-separated)")
	timingTemplate := fs.String("timing-template", "", "Nmap timing template (0-5)")
	jitterMax := fs.Int("jitter-max", 0, "Maximum jitter seconds before each scan (0 = disabled)")
	noRDNS := fs.Bool("no-rdns", false, "Disable reverse DNS resolution (-n)")
	format := fs.String("format", "text", "Output format: text or json")
	cloudFlag := fs.String("cloud", "", "Cloud provider: "+cloud.SupportedKindsText()+" (default: from config or aws)")

	outDir := fs.String("out", "", "Download results/artifacts to this directory after completion")

	// Lifecycle flags.
	noDeploy := fs.Bool("no-deploy", false, "Fail instead of deploying or redeploying infrastructure")
	autoApprove := fs.Bool("auto-approve", false, "Skip deploy confirmation prompts when lifecycle requires deploy")
	destroyAfter := fs.Bool("destroy-after", false, "Destroy infrastructure after the run completes")

	if err := fs.Parse(args); err != nil {
		return err
	}

	// Resolve defaults from operator config.
	opCfg, _ := operator.LoadConfig()
	*workers = operator.ResolveWorkers(*workers, opCfg)
	*computeMode = operator.ResolveComputeMode(*computeMode, opCfg)
	if *outDir == "" && opCfg != nil && opCfg.OutputDir != "" {
		*outDir = opCfg.OutputDir
	}
	placementPolicy, err := operator.ResolvePlacementPolicy(fleet.PlacementPolicy{
		Mode:              fleet.PlacementMode(*placementMode),
		MaxWorkersPerHost: *maxWorkersPerHost,
		MinUniqueIPs:      *minUniqueIPs,
		IPv6Required:      *ipv6Required,
		DualStackRequired: *dualStackRequired,
	}, opCfg, *workers)
	if err != nil {
		return err
	}

	cloudKind, err := resolveCLICloud(*cloudFlag, opCfg)
	if err != nil {
		return err
	}
	if err := ValidateComputeMode(cloudKind, *computeMode); err != nil {
		return err
	}

	if *inputFile == "" {
		return fmt.Errorf("--file flag is required")
	}
	if *format != "text" && *format != "json" {
		return fmt.Errorf("--format must be text or json")
	}
	if *workers <= 0 {
		return fmt.Errorf("--workers must be positive")
	}
	if *mode != "target-only" && *mode != "target-ports" {
		return fmt.Errorf("--mode must be target-only or target-ports")
	}
	if *portChunks <= 0 {
		return fmt.Errorf("--port-chunks must be positive")
	}

	result, err := scanapp.RunNmap(mainContext(), scanapp.NmapOptions{
		InputFile:      *inputFile,
		DefaultOptions: *defaultOptions,
		Workers:        *workers,
		ComputeMode:    *computeMode,
		Placement:      placementPolicy,
		Mode:           *mode,
		PortChunks:     *portChunks,
		DNSServers:     *dnsServers,
		TimingTemplate: *timingTemplate,
		JitterMax:      *jitterMax,
		NoRDNS:         *noRDNS,
		Format:         *format,
		OutDir:         *outDir,
		CloudKind:      cloudKind,
		LifecyclePolicy: infra.LifecyclePolicy{
			NoDeploy:     *noDeploy,
			AutoApprove:  *autoApprove,
			DestroyAfter: *destroyAfter,
		},
		PromptFunc:      deployPrompt,
		Stream:          os.Stderr,
		Output:          os.Stdout,
		Log:             log,
		Statusf:         logStatus,
		ProviderBuilder: buildRuntimeProvider,
		FleetWaiter:     waitForProviderNativeFleetFunc,
	})
	if result.Started {
		printRunSummary(result.JobID, result.Tool, result.Reused, result.CleanupPolicy, result.ExportDir)
	}
	return err
}

func resolveComputeMode(mode string, workers int) bool {
	return scanruntime.ResolveComputeMode(mode, workers)
}

// regionFromECR extracts the AWS region from an ECR repo URL.
func regionFromECR(url string) string {
	return scanruntime.RegionFromECR(url)
}

func extractTargetFromKey(key string) string {
	return jobs.TargetFromKey(key)
}

func splitOutputList(s string) []string {
	return scanruntime.SplitOutputList(s)
}

// logStatus prints a status line to stderr (keeps stdout clean for results).
func logStatus(format string, args ...interface{}) {
	_, _ = fmt.Fprintf(os.Stderr, format+"\n", args...)
}

// printRunSummary writes a concise post-run summary to stderr.
func printRunSummary(jobID, tool string, reused bool, cleanupPolicy, localOutputDir string) {
	_, _ = fmt.Fprintln(os.Stderr, "")
	_, _ = fmt.Fprintln(os.Stderr, "── Run Summary ──")
	_, _ = fmt.Fprintf(os.Stderr, "  Job:      %s\n", jobID)
	_, _ = fmt.Fprintf(os.Stderr, "  Tool:     %s\n", tool)
	if reused {
		_, _ = fmt.Fprintln(os.Stderr, "  Infra:    reused existing")
	} else {
		_, _ = fmt.Fprintln(os.Stderr, "  Infra:    freshly deployed")
	}
	_, _ = fmt.Fprintf(os.Stderr, "  Cleanup:  %s\n", cleanupPolicy)
	if localOutputDir != "" {
		_, _ = fmt.Fprintf(os.Stderr, "  Output:   %s\n", localOutputDir)
	}
}
