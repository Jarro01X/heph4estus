package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"heph4estus/internal/cloud"
	"heph4estus/internal/fleet"
	"heph4estus/internal/infra"
	"heph4estus/internal/jobs"
	"heph4estus/internal/logger"
	"heph4estus/internal/modules"
	"heph4estus/internal/operator"
	"heph4estus/internal/scanruntime"
	targetlisttool "heph4estus/internal/tools/targetlist"
	wordlisttool "heph4estus/internal/tools/wordlist"
	"heph4estus/internal/worker"
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

	// Validate local inputs before any lifecycle side effects.
	var targetMeta *targetlisttool.Metadata
	var wordlistMeta *wordlisttool.Metadata
	if mod.InputType == modules.InputTypeWordlist {
		wordlistMeta, err = preflightWordlistFile(*tool, *wordlistFile, *runtimeTarget, *options, *chunks, *workers)
		if err != nil {
			return err
		}
	} else {
		targetMeta, err = preflightTargetListFile(*inputFile, *workers)
		if err != nil {
			return err
		}
	}

	ctx := mainContext()
	env, err := scanruntime.Setup(ctx, scanruntime.SetupOptions{
		ToolName:  *tool,
		CloudKind: cloudKind,
		Workers:   *workers,
		LifecyclePolicy: infra.LifecyclePolicy{
			NoDeploy:     *noDeploy,
			AutoApprove:  *autoApprove,
			DestroyAfter: *destroyAfter,
		},
		PromptFunc: deployPrompt,
		Stream:     os.Stderr,
		Log:        log,
	})
	if err != nil {
		return err
	}

	provider, err := scanruntime.BuildProvider(ctx, scanruntime.ProviderOptions{
		CloudKind:       cloudKind,
		Outputs:         env.Outputs,
		Log:             log,
		ProviderBuilder: buildRuntimeProvider,
	})
	if err != nil {
		return fmt.Errorf("building cloud provider: %w", err)
	}
	queue := provider.Queue()
	storage := provider.Storage()
	compute := provider.Compute()

	jobID := jobs.NewID(*tool)

	// Track the job.
	tracker := newTracker()
	cleanupPolicy := scanruntime.CleanupPolicy(*destroyAfter)
	_ = scanruntime.CreateJobRecord(scanruntime.JobRecordOptions{
		Tracker:       tracker,
		JobID:         jobID,
		ToolName:      *tool,
		Workers:       *workers,
		ComputeMode:   *computeMode,
		CloudKind:     cloudKind,
		CleanupPolicy: cleanupPolicy,
		Bucket:        env.Bucket,
		Outputs:       env.Outputs,
		Placement:     placementPolicy,
	})

	var (
		scanErr error
		started bool
	)
	if mod.InputType == modules.InputTypeWordlist {
		started, scanErr = runWordlistScan(ctx, *tool, jobID, *wordlistFile, wordlistMeta, *runtimeTarget, *options, *chunks, *workers, *computeMode, *format, queue, storage, compute, env.Outputs, env.Bucket, env.QueueURL, tracker, cloudKind, placementPolicy)
	} else {
		started, scanErr = runTargetListScan(ctx, *tool, jobID, *inputFile, targetMeta, mod, *options, *workers, *computeMode, *format, queue, storage, compute, env.Outputs, env.Bucket, env.QueueURL, tracker, cloudKind, placementPolicy)
	}

	finalized, finalizeErr := scanruntime.Finalize(ctx, scanruntime.FinalizeOptions{
		JobID:        jobID,
		ToolName:     *tool,
		Tracker:      tracker,
		Started:      started,
		ScanErr:      scanErr,
		OutDir:       *outDir,
		Storage:      storage,
		Bucket:       env.Bucket,
		DestroyAfter: *destroyAfter,
		CloudKind:    cloudKind,
		ToolConfig:   env.ToolConfig,
		Stream:       os.Stderr,
		Log:          log,
		Statusf:      logStatus,
	})
	if finalizeErr != nil {
		return finalizeErr
	}

	// Print run summary.
	if started {
		printRunSummary(jobID, *tool, env.Reused, cleanupPolicy, finalized.ExportDir)
	}

	return scanErr
}

func runTargetListScan(ctx context.Context, tool, jobID, inputFile string, preflight *targetlisttool.Metadata, mod *modules.ModuleDefinition, options string, workers int, computeMode, format string, queue cloud.Queue, storage cloud.Storage, compute cloud.Compute, outputs infra.TerraformOutputs, bucket, queueURL string, tracker *operator.Tracker, cloudKind cloud.Kind, placementPolicy fleet.PlacementPolicy) (bool, error) {
	fileBacked := targetListUsesFileInput(mod)
	var (
		tempDir string
		err     error
	)
	if fileBacked {
		if err := targetlisttool.CleanupStaleTempDirs(targetlisttool.DefaultStaleTempAge); err != nil {
			logStatus("Warning: failed to clean stale target-list temp dirs: %v", err)
		}
		tempDir, err = os.MkdirTemp("", "heph-targetlist-*")
		if err != nil {
			return false, fmt.Errorf("creating target-list temp dir: %w", err)
		}
		defer func() {
			if err := os.RemoveAll(tempDir); err != nil {
				logStatus("Warning: failed to remove target-list temp dir %s: %v", tempDir, err)
			}
		}()
	}

	plan, err := jobs.PlanTargetListFile(tool, jobID, options, inputFile, tempDir, workers, fileBacked)
	if err != nil {
		return false, fmt.Errorf("planning target-list job: %w", err)
	}
	defer func() {
		if err := plan.Cleanup(); err != nil {
			logStatus("Warning: failed to clean temporary target-list chunks: %v", err)
		}
	}()

	sourceBytes := plan.TotalSourceBytes
	if preflight != nil && preflight.TotalSourceBytes > 0 {
		sourceBytes = preflight.TotalSourceBytes
	}
	if plan.FileBacked {
		logStatus("Parsed %d targets from %s (%s); chunks effective=%d target=%s max=%s [job %s]",
			plan.TotalTargets,
			inputFile,
			formatByteSize(sourceBytes),
			plan.EffectiveChunks,
			formatByteSize(plan.TargetChunkSize),
			formatByteSize(plan.MaxChunkSize),
			jobID,
		)
	} else {
		logStatus("Parsed %d targets from %s [job %s]", plan.TotalTargets, inputFile, jobID)
	}

	if plan.FileBacked {
		_ = tracker.UpdatePhase(jobID, operator.PhaseUploading)
		logStatus("Uploading %d target-list chunks to s3://%s/...", plan.EffectiveChunks, bucket)
		uploadCtx, uploadCancel := context.WithTimeout(ctx, scanruntime.EnqueueTimeout)
		defer uploadCancel()
		if err := jobs.UploadTargetListChunks(uploadCtx, storage, bucket, plan); err != nil {
			return false, fmt.Errorf("uploading target-list chunks: %w", err)
		}
	}

	if plan.FileBacked {
		if err := plan.Cleanup(); err != nil {
			logStatus("Warning: failed to clean temporary target-list chunks: %v", err)
		}
	}

	// Update job record with total task count.
	if store := tracker.Store(); store != nil {
		if rec, loadErr := store.Load(jobID); loadErr == nil {
			rec.TotalTasks = len(plan.Tasks)
			rec.TotalTargets = plan.TotalTargets
			_ = store.Update(rec)
		}
	}

	unitLabel := "targets"
	if plan.FileBacked {
		unitLabel = "chunks"
	}
	return scanruntime.ExecuteQueuedScan(ctx, scanruntime.ExecuteOptions{
		ToolName:      tool,
		JobID:         jobID,
		Tasks:         plan.Tasks,
		EnqueueLabel:  "target tasks",
		Workers:       workers,
		ComputeMode:   computeMode,
		Queue:         queue,
		Storage:       storage,
		Compute:       compute,
		Outputs:       outputs,
		QueueURL:      queueURL,
		Bucket:        bucket,
		CloudKind:     cloudKind,
		Placement:     placementPolicy,
		Tracker:       tracker,
		ProgressLabel: unitLabel,
		CompleteLabel: unitLabel,
		RenderResults: func(renderCtx context.Context, renderStorage cloud.Storage, renderBucket, prefix string) error {
			return outputGenericResults(renderCtx, renderStorage, renderBucket, prefix, format)
		},
		Statusf:     logStatus,
		FleetWaiter: waitForProviderNativeFleetFunc,
	})
}

func runWordlistScan(ctx context.Context, tool, jobID, wordlistFile string, preflight *wordlisttool.Metadata, runtimeTarget, options string, chunks, workers int, computeMode, format string, queue cloud.Queue, storage cloud.Storage, compute cloud.Compute, outputs infra.TerraformOutputs, bucket, queueURL string, tracker *operator.Tracker, cloudKind cloud.Kind, placementPolicy fleet.PlacementPolicy) (bool, error) {
	if err := wordlisttool.CleanupStaleTempDirs(wordlisttool.DefaultStaleTempAge); err != nil {
		logStatus("Warning: failed to clean stale wordlist temp dirs: %v", err)
	}

	tempDir, err := os.MkdirTemp("", "heph-wordlist-*")
	if err != nil {
		return false, fmt.Errorf("creating wordlist temp dir: %w", err)
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			logStatus("Warning: failed to remove wordlist temp dir %s: %v", tempDir, err)
		}
	}()

	plan, err := jobs.PlanWordlistFile(tool, jobID, runtimeTarget, options, wordlistFile, tempDir, chunks, workers)
	if err != nil {
		return false, fmt.Errorf("planning wordlist job: %w", err)
	}
	defer func() {
		if err := plan.Cleanup(); err != nil {
			logStatus("Warning: failed to clean temporary wordlist chunks: %v", err)
		}
	}()

	requested := "auto"
	if chunks > 0 {
		requested = strconv.Itoa(chunks)
	}
	sourceBytes := plan.TotalSourceBytes
	if preflight != nil && preflight.TotalSourceBytes > 0 {
		sourceBytes = preflight.TotalSourceBytes
	}
	logStatus("Parsed %d entries from %s (%s); chunks requested=%s effective=%d target=%s max=%s [job %s]",
		plan.TotalWords,
		wordlistFile,
		formatByteSize(sourceBytes),
		requested,
		plan.EffectiveChunks,
		formatByteSize(plan.TargetChunkSize),
		formatByteSize(plan.MaxChunkSize),
		jobID,
	)
	if runtimeTarget != "" {
		logStatus("Target: %s", runtimeTarget)
	}

	// Update job record with wordlist metadata.
	_ = tracker.UpdatePhase(jobID, operator.PhaseUploading)
	if store := tracker.Store(); store != nil {
		if rec, loadErr := store.Load(jobID); loadErr == nil {
			rec.TotalTasks = plan.EffectiveChunks
			rec.TotalWords = plan.TotalWords
			rec.RuntimeTarget = runtimeTarget
			_ = store.Update(rec)
		}
	}

	// Upload chunks.
	logStatus("Uploading %d chunks to s3://%s/...", plan.EffectiveChunks, bucket)
	uploadCtx, uploadCancel := context.WithTimeout(ctx, scanruntime.EnqueueTimeout)
	defer uploadCancel()
	if err := jobs.UploadChunks(uploadCtx, storage, bucket, plan); err != nil {
		return false, fmt.Errorf("uploading wordlist chunks: %w", err)
	}

	if err := plan.Cleanup(); err != nil {
		logStatus("Warning: failed to clean temporary wordlist chunks: %v", err)
	}

	return scanruntime.ExecuteQueuedScan(ctx, scanruntime.ExecuteOptions{
		ToolName:      tool,
		JobID:         jobID,
		Tasks:         plan.Tasks,
		EnqueueLabel:  "chunk tasks",
		Workers:       workers,
		ComputeMode:   computeMode,
		Queue:         queue,
		Storage:       storage,
		Compute:       compute,
		Outputs:       outputs,
		QueueURL:      queueURL,
		Bucket:        bucket,
		CloudKind:     cloudKind,
		Placement:     placementPolicy,
		Tracker:       tracker,
		ProgressLabel: "chunks",
		CompleteLabel: "chunks",
		RenderResults: func(renderCtx context.Context, renderStorage cloud.Storage, renderBucket, prefix string) error {
			return outputGenericResults(renderCtx, renderStorage, renderBucket, prefix, format)
		},
		Statusf:     logStatus,
		FleetWaiter: waitForProviderNativeFleetFunc,
	})
}

func preflightTargetListFile(path string, workers int) (*targetlisttool.Metadata, error) {
	meta, err := targetlisttool.InspectFile(path, targetlisttool.Policy{WorkerCount: workers})
	if err != nil {
		return nil, fmt.Errorf("validating target file: %w", err)
	}
	return meta, nil
}

func preflightWordlistFile(_, path, _, _ string, chunks, workers int) (*wordlisttool.Metadata, error) {
	meta, err := wordlisttool.InspectFile(path, wordlisttool.Policy{
		RequestedChunks: chunks,
		WorkerCount:     workers,
	})
	if err != nil {
		return nil, fmt.Errorf("validating wordlist file: %w", err)
	}
	return meta, nil
}

func formatByteSize(n int64) string {
	const mib = 1024 * 1024
	if n%mib == 0 && n >= mib {
		return fmt.Sprintf("%d MiB", n/mib)
	}
	return fmt.Sprintf("%d bytes", n)
}

func outputGenericResults(ctx context.Context, storage cloud.Storage, bucket, prefix, format string) error {
	keys, err := storage.List(ctx, bucket, prefix)
	if err != nil {
		return fmt.Errorf("listing results: %w", err)
	}

	if format == "json" {
		encoder := json.NewEncoder(os.Stdout)
		for _, key := range keys {
			if !strings.HasSuffix(key, ".json") {
				continue
			}
			data, err := storage.Download(ctx, bucket, key)
			if err != nil {
				logStatus("Warning: failed to download %s: %v", key, err)
				continue
			}
			var result worker.Result
			if err := json.Unmarshal(data, &result); err != nil {
				logStatus("Warning: failed to parse %s: %v", key, err)
				continue
			}
			if err := encoder.Encode(result); err != nil {
				return fmt.Errorf("encoding result: %w", err)
			}
		}
	} else {
		fmt.Printf("\n%-40s %-10s %s\n", "TARGET", "CHUNK", "STATUS")
		fmt.Println(strings.Repeat("─", 60))
		var failures int
		for _, key := range keys {
			if !strings.HasSuffix(key, ".json") {
				continue
			}
			target := extractTargetFromKey(key)
			status := "OK"
			chunkLabel := ""
			data, err := storage.Download(ctx, bucket, key)
			if err != nil {
				status = "???"
			} else {
				var result worker.Result
				if err := json.Unmarshal(data, &result); err == nil {
					if result.Target != "" {
						target = result.Target
					}
					if result.TotalChunks > 0 {
						chunkLabel = fmt.Sprintf("%d/%d", result.ChunkIdx+1, result.TotalChunks)
					}
					if result.Error != "" {
						status = "ERROR"
						failures++
					}
				}
			}
			fmt.Printf("%-40s %-10s %s\n", target, chunkLabel, status)
		}
		fmt.Printf("\n%d results written to s3://%s/%s", len(keys), bucket, prefix)
		if failures > 0 {
			fmt.Printf(" (%d failed)", failures)
		}
		fmt.Println()
	}
	return nil
}

func targetListUsesFileInput(mod *modules.ModuleDefinition) bool {
	return mod != nil && mod.NeedsInput() && !mod.NeedsTarget()
}
