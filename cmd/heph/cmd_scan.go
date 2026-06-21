package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"heph4estus/internal/cloud"
	"heph4estus/internal/fleet"
	"heph4estus/internal/infra"
	"heph4estus/internal/logger"
	"heph4estus/internal/modules"
	"heph4estus/internal/operator"
	"heph4estus/internal/scanapp"
)

func runScan(args []string, log logger.Logger) error {
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	tool := fs.String("tool", "", "Tool to run (e.g. httpx, nuclei, subfinder, ffuf)")
	inputFile := fs.String("file", "", "Path to file containing targets (target_list modules)")
	wordlistFile := fs.String("wordlist", "", "Path to wordlist file (wordlist modules)")
	runtimeTarget := fs.String("target", "", "Runtime target / URL (wordlist modules, e.g. https://example.com/FUZZ)")
	chunks := fs.Int("chunks", 0, "Number of wordlist chunks (default: auto-size from file size and workers)")
	options := fs.String("options", "", "Extra tool-specific options")
	workers := fs.Int("workers", 0, "Number of worker tasks to launch (default: from config or 10)")
	computeMode := fs.String("compute-mode", "", "Compute mode: auto, fargate, or spot (default: from config or auto)")
	placementMode := fs.String("placement", "", "Fleet placement policy: diversity or throughput (default: from config or diversity)")
	maxWorkersPerHost := fs.Int("max-workers-per-host", 0, "Maximum admitted workers per host/public IP (default: from config or policy)")
	minUniqueIPs := fs.Int("min-unique-ips", 0, "Minimum unique public IPv4 addresses required before scan start")
	ipv6Required := fs.Bool("ipv6-required", false, "Require IPv6-validated workers before scan start")
	dualStackRequired := fs.Bool("dual-stack-required", false, "Require workers with both public IPv4 and IPv6-ready public IPv6")
	format := fs.String("format", "text", "Output format: text or json")
	outDir := fs.String("out", "", "Download results/artifacts to this directory after completion")

	// Lifecycle flags.
	noDeploy := fs.Bool("no-deploy", false, "Fail instead of deploying or redeploying infrastructure")
	autoApprove := fs.Bool("auto-approve", false, "Skip deploy confirmation prompts when lifecycle requires deploy")
	destroyAfter := fs.Bool("destroy-after", false, "Destroy infrastructure after the run completes")
	cloudFlag := fs.String("cloud", "", "Cloud provider: "+cloud.SupportedKindsText()+" (default: from config or aws)")

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

	if *tool == "" {
		return fmt.Errorf("--tool flag is required")
	}
	if *format != "text" && *format != "json" {
		return fmt.Errorf("--format must be text or json")
	}
	if *workers <= 0 {
		return fmt.Errorf("--workers must be positive")
	}

	// Load and validate the module from the registry.
	reg, err := modules.NewDefaultRegistry()
	if err != nil {
		return fmt.Errorf("loading module registry: %w", err)
	}
	mod, err := reg.Get(*tool)
	if err != nil {
		return fmt.Errorf("unknown tool: %q (available: %s)", *tool, strings.Join(reg.Names(), ", "))
	}

	// Validate flag combinations based on module input type.
	if mod.InputType == modules.InputTypeWordlist {
		if *inputFile != "" {
			return fmt.Errorf("--file is not valid for wordlist tool %q — use --wordlist instead", *tool)
		}
		if *wordlistFile == "" {
			return fmt.Errorf("--wordlist flag is required for tool %q", *tool)
		}
		if mod.NeedsTarget() && *runtimeTarget == "" {
			return fmt.Errorf("--target flag is required for tool %q", *tool)
		}
		if *chunks < 0 {
			return fmt.Errorf("--chunks must be positive")
		}
	} else {
		// target_list module
		if *wordlistFile != "" {
			return fmt.Errorf("--wordlist is not valid for target_list tool %q — use --file instead", *tool)
		}
		if *chunks != 0 {
			return fmt.Errorf("--chunks is not valid for target_list tool %q", *tool)
		}
		if *runtimeTarget != "" {
			return fmt.Errorf("--target is not valid for target_list tool %q", *tool)
		}
		if *inputFile == "" {
			return fmt.Errorf("--file flag is required")
		}
	}

	result, err := scanapp.RunGeneric(mainContext(), scanapp.GenericOptions{
		ToolName:      *tool,
		InputFile:     *inputFile,
		WordlistFile:  *wordlistFile,
		RuntimeTarget: *runtimeTarget,
		ToolOptions:   *options,
		Chunks:        *chunks,
		Workers:       *workers,
		ComputeMode:   *computeMode,
		Format:        *format,
		OutDir:        *outDir,
		CloudKind:     cloudKind,
		Placement:     placementPolicy,
		LifecyclePolicy: infra.LifecyclePolicy{
			NoDeploy:     *noDeploy,
			AutoApprove:  *autoApprove,
			DestroyAfter: *destroyAfter,
		},
		Module:          mod,
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
