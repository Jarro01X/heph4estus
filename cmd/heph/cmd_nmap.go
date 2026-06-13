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
	"heph4estus/internal/operator"
	"heph4estus/internal/scanruntime"
	"heph4estus/internal/tools/nmap"
	"heph4estus/internal/worker"
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

	content, err := os.ReadFile(*inputFile)
	if err != nil {
		return fmt.Errorf("reading target file: %w", err)
	}

	// Parse targets.
	scanner := nmap.NewScanner(log)
	tasks := scanner.ParseTargetsWithMode(string(content), *defaultOptions, *mode, *portChunks)

	// Inject nmap-specific options into each task at enqueue time (producer-side).
	if *noRDNS {
		for i := range tasks {
			tasks[i].Options = "-n " + tasks[i].Options
		}
	}
	if *timingTemplate != "" {
		for i := range tasks {
			tasks[i].Options = fmt.Sprintf("-T%s %s", *timingTemplate, tasks[i].Options)
		}
	}
	if *dnsServers != "" {
		for i := range tasks {
			tasks[i].Options = fmt.Sprintf("--dns-servers %s %s", *dnsServers, tasks[i].Options)
		}
	}

	if len(tasks) == 0 {
		return fmt.Errorf("no targets found in %s", *inputFile)
	}
	jobID := jobs.NewID("nmap")
	for i := range tasks {
		tasks[i].JobID = jobID
	}
	if *mode == "target-ports" {
		groups := countGroups(tasks)
		logStatus("Mode: target-ports — %d target groups, %d total tasks (%d chunks/target) [job %s]", groups, len(tasks), *portChunks, jobID)
	} else {
		logStatus("Parsed %d targets from %s [job %s]", len(tasks), *inputFile, jobID)
	}

	ctx := mainContext()
	env, err := scanruntime.Setup(ctx, scanruntime.SetupOptions{
		ToolName:  "nmap",
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

	// Track the job.
	tracker := newTracker()
	cleanupPolicy := scanruntime.CleanupPolicy(*destroyAfter)
	_ = scanruntime.CreateJobRecord(scanruntime.JobRecordOptions{
		Tracker:       tracker,
		JobID:         jobID,
		ToolName:      "nmap",
		TotalTasks:    len(tasks),
		Workers:       *workers,
		ComputeMode:   *computeMode,
		CloudKind:     cloudKind,
		CleanupPolicy: cleanupPolicy,
		Bucket:        env.Bucket,
		Outputs:       env.Outputs,
		Placement:     placementPolicy,
	})

	// Run the scan.
	var (
		provider cloud.Provider
		scanErr  error
		started  bool
	)
	provider, err = scanruntime.BuildProvider(ctx, scanruntime.ProviderOptions{
		CloudKind:       cloudKind,
		Outputs:         env.Outputs,
		Log:             log,
		ProviderBuilder: buildRuntimeProvider,
	})
	if err != nil {
		scanErr = fmt.Errorf("building cloud provider: %w", err)
	} else {
		started, scanErr = runNmapScanWithDeps(ctx, tasks, *workers, *computeMode, *jitterMax, *format, env.Outputs, provider.Queue(), provider.Storage(), provider.Compute(), tracker, jobID, placementPolicy, cloudKind)
	}

	var storage cloud.Storage
	if provider != nil {
		storage = provider.Storage()
	}
	finalized, finalizeErr := scanruntime.Finalize(ctx, scanruntime.FinalizeOptions{
		JobID:        jobID,
		ToolName:     "nmap",
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
		printRunSummary(jobID, "nmap", env.Reused, cleanupPolicy, finalized.ExportDir)
	}

	return scanErr
}

func runNmapScanWithDeps(ctx context.Context, tasks []nmap.ScanTask, workers int, computeMode string, jitterMax int, format string, outputs infra.TerraformOutputs, queue cloud.Queue, storage cloud.Storage, compute cloud.Compute, tracker *operator.Tracker, jobID string, placementPolicy fleet.PlacementPolicy, cloudKind ...cloud.Kind) (bool, error) {
	genericTasks := make([]worker.Task, len(tasks))
	for i, t := range tasks {
		genericTasks[i] = worker.Task{
			ToolName:    "nmap",
			JobID:       t.JobID,
			Target:      t.Target,
			Options:     t.Options,
			GroupID:     t.GroupID,
			ChunkIdx:    t.ChunkIdx,
			TotalChunks: t.TotalChunks,
		}
	}

	kind := cloud.KindAWS
	if len(cloudKind) > 0 {
		kind = cloudKind[0]
	}
	queueURL := outputs.AWS.SQSQueueURL
	if queueURL == "" {
		queueURL = outputs.Selfhosted.QueueURL
	}
	bucket := outputs.AWS.S3BucketName
	if bucket == "" {
		bucket = outputs.Selfhosted.S3BucketName
	}
	return scanruntime.ExecuteQueuedScan(ctx, scanruntime.ExecuteOptions{
		ToolName:     "nmap",
		JobID:        jobID,
		Tasks:        genericTasks,
		EnqueueLabel: "targets",
		Workers:      workers,
		ComputeMode:  computeMode,
		Queue:        queue,
		Storage:      storage,
		Compute:      compute,
		Outputs:      outputs,
		QueueURL:     queueURL,
		Bucket:       bucket,
		CloudKind:    kind,
		Placement:    placementPolicy,
		Tracker:      tracker,
		WorkerEnv: map[string]string{
			"QUEUE_URL":          queueURL,
			"S3_BUCKET":          bucket,
			"JITTER_MAX_SECONDS": strconv.Itoa(jitterMax),
			"TOOL_NAME":          "nmap",
		},
		ContainerName: "nmap-worker",
		CompleteLabel: "targets",
		RenderResults: func(renderCtx context.Context, renderStorage cloud.Storage, renderBucket, prefix string) error {
			return outputResults(renderCtx, renderStorage, renderBucket, prefix, format)
		},
		Statusf:     logStatus,
		FleetWaiter: waitForProviderNativeFleetFunc,
	})
}

func outputResults(ctx context.Context, storage cloud.Storage, bucket, prefix, format string) error {
	keys, err := storage.List(ctx, bucket, prefix)
	if err != nil {
		return fmt.Errorf("listing results: %w", err)
	}

	if format == "json" {
		encoder := json.NewEncoder(os.Stdout)
		for _, key := range keys {
			// Only process .json result files (skip .xml output files from generic worker).
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
		fmt.Printf("\n%-40s %s\n", "TARGET", "STATUS")
		fmt.Println(strings.Repeat("─", 50))
		for _, key := range keys {
			if !strings.HasSuffix(key, ".json") {
				continue
			}
			target := extractTargetFromKey(key)
			fmt.Printf("%-40s %s\n", target, "done")
		}
		fmt.Printf("\n%d results written to s3://%s/%s\n", len(keys), bucket, prefix)
	}
	return nil
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

func countGroups(tasks []nmap.ScanTask) int {
	seen := make(map[string]bool)
	for _, t := range tasks {
		if t.GroupID != "" {
			seen[t.GroupID] = true
		}
	}
	return len(seen)
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
