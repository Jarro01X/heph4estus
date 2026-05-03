package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"heph4estus/internal/cloud"
	awscloud "heph4estus/internal/cloud/aws"
	"heph4estus/internal/cloud/factory"
	"heph4estus/internal/fleet"
	"heph4estus/internal/infra"
	"heph4estus/internal/jobs"
	"heph4estus/internal/logger"
	"heph4estus/internal/operator"
	"heph4estus/internal/tools/nmap"
	"heph4estus/internal/worker"
)

const (
	spotThreshold  = 50
	pollInterval   = 2 * time.Second
	enqueueTimeout = 5 * time.Minute
	launchTimeout  = 5 * time.Minute
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

	// Track the job.
	tracker := newTracker()
	cleanupPolicy := "reuse"
	if *destroyAfter {
		cleanupPolicy = "destroy-after"
	}

	var (
		outputs map[string]string
		bucket  string
		reused  bool
		toolCfg *infra.ToolConfig
	)

	if cloudKind.IsProviderNative() {
		// Provider-native (Hetzner): Terraform deploy + selfhosted runtime.
		toolCfg, err = infra.ResolveToolConfig("nmap", cloudKind)
		if err != nil {
			return err
		}
		toolCfg.TerraformVars["worker_count"] = strconv.Itoa(*workers)
		ensureResult, ensureErr := infra.EnsureInfra(ctx, toolCfg, infra.LifecyclePolicy{
			NoDeploy:     *noDeploy,
			AutoApprove:  *autoApprove,
			DestroyAfter: *destroyAfter,
		}, "", os.Stderr, deployPrompt, log)
		if ensureErr != nil {
			return ensureErr
		}
		outputs = ensureResult.Outputs
		reused = ensureResult.Reused
		bucket = outputs["s3_bucket_name"]
	} else if cloudKind.IsSelfhostedFamily() {
		// Manual selfhosted: no Terraform/deploy — read queue ID and bucket from env.
		shCfg := factory.SelfhostedConfigFromEnv()
		if shCfg.QueueID == "" || shCfg.Bucket == "" {
			return fmt.Errorf("%s requires SELFHOSTED_QUEUE_ID and SELFHOSTED_BUCKET environment variables", cloudKind.Canonical())
		}
		bucket = shCfg.Bucket
		outputs = map[string]string{
			"sqs_queue_url":  shCfg.QueueID,
			"s3_bucket_name": shCfg.Bucket,
		}
	} else {
		// AWS: resolve tool config and ensure infrastructure.
		toolCfg, err = infra.ResolveToolConfig("nmap")
		if err != nil {
			return err
		}
		region := infra.AWSRegion()
		ensureResult, ensureErr := infra.EnsureInfra(ctx, toolCfg, infra.LifecyclePolicy{
			NoDeploy:     *noDeploy,
			AutoApprove:  *autoApprove,
			DestroyAfter: *destroyAfter,
		}, region, os.Stderr, deployPrompt, log)
		if ensureErr != nil {
			return ensureErr
		}
		outputs = ensureResult.Outputs
		reused = ensureResult.Reused
		bucket = outputs["s3_bucket_name"]
	}

	_ = tracker.Create(&operator.JobRecord{
		JobID:                 jobID,
		ToolName:              "nmap",
		Phase:                 operator.PhaseEnqueuing,
		TotalTasks:            len(tasks),
		WorkerCount:           *workers,
		ComputeMode:           *computeMode,
		Cloud:                 string(cloudKind),
		CleanupPolicy:         cleanupPolicy,
		Bucket:                bucket,
		Placement:             placementPolicy,
		ExpectedWorkerVersion: outputs["docker_image"],
		NATSUrl:               outputs["nats_url"],
		ControllerIP:          outputs["controller_ip"],
		GenerationID:          outputs["generation_id"],
	})

	// Run the scan.
	started, scanErr := runNmapScan(ctx, tasks, *workers, *computeMode, *jitterMax, *format, outputs, log, tracker, jobID, placementPolicy, cloudKind)

	if scanErr != nil {
		_ = tracker.Fail(jobID, scanErr)
	} else if started {
		_ = tracker.Complete(jobID)
	}

	// Export results locally before any cleanup.
	var exportDir string
	if *outDir != "" && scanErr == nil && started {
		logStatus("Exporting results to %s...", *outDir)

		exportProvider, provErr := buildRuntimeProvider(ctx, cloudKind, outputs, log)
		if provErr != nil {
			return fmt.Errorf("building cloud provider for export: %w", provErr)
		}
		storage := exportProvider.Storage()

		result, exportErr := operator.ExportJob(ctx, storage, bucket, "nmap", jobID, *outDir)
		if exportErr != nil {
			return fmt.Errorf("export failed: %w", exportErr)
		}
		exportDir = result.Dir
		logStatus("Exported %d results, %d artifacts to %s", result.ResultCount, result.ArtifactCount, result.Dir)

		// Record the local output path in the job record.
		if store := tracker.Store(); store != nil {
			if rec, loadErr := store.Load(jobID); loadErr == nil {
				rec.LocalOutputDir = result.Dir
				_ = store.Update(rec)
			}
		}
	}

	// Destroy only after execution has actually started and export is done.
	if *destroyAfter && started {
		if cloudKind.IsSelfhostedFamily() && !cloudKind.IsProviderNative() {
			logStatus("Skipping destroy: %s does not support auto-destroy", cloudKind.Canonical())
		} else if toolCfg != nil {
			logStatus("Destroying infrastructure (--destroy-after)...")
			if destroyErr := infra.RunDestroy(ctx, toolCfg, os.Stderr, log); destroyErr != nil {
				if scanErr != nil {
					return fmt.Errorf("scan failed: %w; additionally, destroy failed: %v", scanErr, destroyErr)
				}
				return fmt.Errorf("scan completed but destroy failed: %w", destroyErr)
			}
		}
	}

	// Print run summary.
	if started {
		printRunSummary(jobID, "nmap", reused, cleanupPolicy, exportDir)
	}

	return scanErr
}

func runNmapScan(ctx context.Context, tasks []nmap.ScanTask, workers int, computeMode string, jitterMax int, format string, outputs map[string]string, log logger.Logger, tracker *operator.Tracker, jobID string, placementPolicy fleet.PlacementPolicy, cloudKind cloud.Kind) (bool, error) {
	queueURL := outputs["sqs_queue_url"]
	bucket := outputs["s3_bucket_name"]
	if queueURL == "" || bucket == "" {
		return false, fmt.Errorf("terraform outputs missing sqs_queue_url or s3_bucket_name")
	}

	// Build the cloud provider. For provider-native paths, use Terraform
	// outputs as the config source rather than environment variables.
	var (
		provider cloud.Provider
		provErr  error
	)
	provider, provErr = buildRuntimeProvider(ctx, cloudKind, outputs, log)
	if provErr != nil {
		return false, fmt.Errorf("building cloud provider: %w", provErr)
	}
	return runNmapScanWithDeps(ctx, tasks, workers, computeMode, jitterMax, format, outputs, provider.Queue(), provider.Storage(), provider.Compute(), tracker, jobID, placementPolicy, cloudKind)
}

func runNmapScanWithDeps(ctx context.Context, tasks []nmap.ScanTask, workers int, computeMode string, jitterMax int, format string, outputs map[string]string, queue cloud.Queue, storage cloud.Storage, compute cloud.Compute, tracker *operator.Tracker, jobID string, placementPolicy fleet.PlacementPolicy, cloudKind ...cloud.Kind) (bool, error) {
	queueURL := outputs["sqs_queue_url"]
	bucket := outputs["s3_bucket_name"]
	if queueURL == "" || bucket == "" {
		return false, fmt.Errorf("terraform outputs missing sqs_queue_url or s3_bucket_name")
	}

	// Enqueue targets.
	logStatus("Enqueueing %d targets...", len(tasks))
	enqueueCtx, enqueueCancel := context.WithTimeout(ctx, enqueueTimeout)
	defer enqueueCancel()

	const sqsMaxPayload = 256 * 1024 // 256 KB SQS message size limit
	bodies := make([]string, len(tasks))
	for i, t := range tasks {
		gt := worker.Task{
			ToolName:    "nmap",
			JobID:       t.JobID,
			Target:      t.Target,
			Options:     t.Options,
			GroupID:     t.GroupID,
			ChunkIdx:    t.ChunkIdx,
			TotalChunks: t.TotalChunks,
		}
		b, err := json.Marshal(gt)
		if err != nil {
			return false, fmt.Errorf("marshaling task %d: %w", i, err)
		}
		if len(b) > sqsMaxPayload {
			return false, fmt.Errorf("task %d exceeds SQS 256KB limit (%d bytes)", i, len(b))
		}
		bodies[i] = string(b)
	}
	if err := queue.SendBatch(enqueueCtx, queueURL, bodies); err != nil {
		return false, fmt.Errorf("enqueueing targets: %w", err)
	}
	logStatus("Enqueued %d targets", len(tasks))

	_ = tracker.UpdatePhase(jobID, operator.PhaseLaunching)

	// Launch workers.
	logStatus("Launching %d workers (mode: %s)...", workers, computeMode)
	launchCtx, launchCancel := context.WithTimeout(ctx, launchTimeout)
	defer launchCancel()

	if len(cloudKind) > 0 && cloudKind[0].IsProviderNative() {
		ready, err := waitForProviderNativeFleetFunc(launchCtx, cloudKind[0], outputs, placementPolicy)
		if err != nil {
			return false, err
		}
		logStatus("Using provider-native %s fleet (%d eligible workers, policy: %s)", cloudKind[0].Canonical(), ready, placementPolicy.Summary())
	} else {
		containerName := "nmap-worker"
		workerEnv := map[string]string{
			"QUEUE_URL":          queueURL,
			"S3_BUCKET":          bucket,
			"JITTER_MAX_SECONDS": strconv.Itoa(jitterMax),
			"TOOL_NAME":          "nmap",
		}

		// Selfhosted only supports RunContainer (no spot instances).
		isSelfhosted := len(cloudKind) > 0 && cloudKind[0].IsSelfhostedFamily()
		useSpot := !isSelfhosted && resolveComputeMode(computeMode, workers)
		if useSpot {
			ecrURL := outputs["ecr_repo_url"]
			userData := awscloud.GenerateUserData(awscloud.UserDataOpts{
				ECRRepoURL: ecrURL,
				ImageTag:   "latest",
				Region:     regionFromECR(ecrURL),
				EnvVars:    workerEnv,
			})
			ids, err := compute.RunSpotInstances(launchCtx, cloud.SpotOpts{
				AMI:             outputs["ami_id"],
				Count:           workers,
				SecurityGroups:  []string{outputs["security_group_id"]},
				SubnetIDs:       splitOutputList(outputs["subnet_ids"]),
				InstanceProfile: outputs["instance_profile_arn"],
				UserData:        userData,
				Tags: map[string]string{
					"Project": "heph4estus",
					"Tool":    "nmap",
				},
			})
			if err != nil {
				return false, fmt.Errorf("launching spot instances: %w", err)
			}
			logStatus("Launched %d spot instances", len(ids))
		} else {
			_, err := compute.RunContainer(launchCtx, cloud.ContainerOpts{
				Cluster:        outputs["ecs_cluster_name"],
				TaskDefinition: outputs["task_definition_arn"],
				ContainerName:  containerName,
				Subnets:        splitOutputList(outputs["subnet_ids"]),
				SecurityGroups: []string{outputs["security_group_id"]},
				Env:            workerEnv,
				Count:          workers,
			})
			if err != nil {
				return false, fmt.Errorf("launching workers: %w", err)
			}
			logStatus("Launched %d workers", workers)
		}
	}

	_ = tracker.UpdatePhase(jobID, operator.PhaseScanning)

	// Poll for progress.
	logStatus("Scanning...")
	startTime := time.Now()
	totalTargets := len(tasks)
	scanPrefix := jobs.ResultPrefix("nmap", jobID)

	for {
		count, err := storage.Count(ctx, bucket, scanPrefix)
		if err != nil {
			logStatus("Warning: progress check failed: %v", err)
		} else {
			elapsed := time.Since(startTime).Truncate(time.Second)
			pct := float64(count) / float64(totalTargets) * 100
			logStatus("Progress: %d/%d (%.1f%%) — elapsed %s", count, totalTargets, pct, elapsed)

			if count >= totalTargets {
				break
			}
		}
		time.Sleep(pollInterval)
	}

	elapsed := time.Since(startTime).Truncate(time.Second)
	logStatus("Scan complete: %d targets in %s", totalTargets, elapsed)

	// Output results.
	return true, outputResults(ctx, storage, bucket, scanPrefix, format)
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
	switch mode {
	case "spot":
		return true
	case "fargate":
		return false
	default: // "auto"
		return workers >= spotThreshold
	}
}

// regionFromECR extracts the AWS region from an ECR repo URL.
func regionFromECR(url string) string {
	parts := strings.Split(url, ".")
	for i, p := range parts {
		if p == "ecr" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return "us-east-1"
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
	s = strings.Trim(s, "[]")
	parts := strings.Split(s, " ")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
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
