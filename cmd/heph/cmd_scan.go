package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"heph4estus/internal/cloud"
	awscloud "heph4estus/internal/cloud/aws"
	"heph4estus/internal/infra"
	"heph4estus/internal/jobs"
	"heph4estus/internal/logger"
	"heph4estus/internal/modules"
	"heph4estus/internal/operator"
	"heph4estus/internal/worker"

	awscfg "github.com/aws/aws-sdk-go-v2/config"
)

func runScan(args []string, log logger.Logger) error {
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	tool := fs.String("tool", "", "Tool to run (e.g. httpx, nuclei, subfinder, ffuf)")
	inputFile := fs.String("file", "", "Path to file containing targets (target_list modules)")
	wordlistFile := fs.String("wordlist", "", "Path to wordlist file (wordlist modules)")
	runtimeTarget := fs.String("target", "", "Runtime target / URL (wordlist modules, e.g. https://example.com/FUZZ)")
	chunks := fs.Int("chunks", 0, "Number of wordlist chunks (default: worker count)")
	options := fs.String("options", "", "Extra tool-specific options")
	workers := fs.Int("workers", 0, "Number of worker tasks to launch (default: from config or 10)")
	computeMode := fs.String("compute-mode", "", "Compute mode: auto, fargate, or spot (default: from config or auto)")
	format := fs.String("format", "text", "Output format: text or json")
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

	if *tool == "" {
		return fmt.Errorf("--tool flag is required")
	}
	if *format != "text" && *format != "json" {
		return fmt.Errorf("--format must be text or json")
	}
	if *computeMode != "auto" && *computeMode != "fargate" && *computeMode != "spot" {
		return fmt.Errorf("--compute-mode must be auto, fargate, or spot")
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
	var targetContent string
	var wordlistContent string
	if mod.InputType == modules.InputTypeWordlist {
		wordlistContent, err = preflightWordlistFile(*tool, *wordlistFile, *runtimeTarget, *options, *chunks, *workers)
		if err != nil {
			return err
		}
	} else {
		targetContent, err = preflightTargetListFile(*inputFile)
		if err != nil {
			return err
		}
	}

	// Resolve tool config and ensure infrastructure.
	cfg, err := infra.ResolveToolConfig(*tool)
	if err != nil {
		return err
	}

	ctx := mainContext()
	region := infra.AWSRegion()

	ensureResult, err := infra.EnsureInfra(ctx, cfg, infra.LifecyclePolicy{
		NoDeploy:     *noDeploy,
		AutoApprove:  *autoApprove,
		DestroyAfter: *destroyAfter,
	}, region, os.Stderr, deployPrompt, log)
	if err != nil {
		return err
	}
	outputs := ensureResult.Outputs

	queueURL := outputs["sqs_queue_url"]
	bucket := outputs["s3_bucket_name"]
	if queueURL == "" || bucket == "" {
		return fmt.Errorf("terraform outputs missing sqs_queue_url or s3_bucket_name")
	}

	// Initialize AWS provider.
	awsConfig, err := awscfg.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("loading AWS config: %w", err)
	}
	provider := awscloud.NewProvider(awsConfig, log)
	queue := provider.Queue()
	storage := provider.Storage()
	compute := provider.Compute()

	jobID := jobs.NewID(*tool)

	// Track the job.
	tracker := newTracker()
	cleanupPolicy := "reuse"
	if *destroyAfter {
		cleanupPolicy = "destroy-after"
	}
	tracker.Create(&operator.JobRecord{
		JobID:         jobID,
		ToolName:      *tool,
		Phase:         operator.PhaseEnqueuing,
		WorkerCount:   *workers,
		ComputeMode:   *computeMode,
		CleanupPolicy: cleanupPolicy,
		Bucket:        bucket,
	})

	var (
		scanErr error
		started bool
	)
	if mod.InputType == modules.InputTypeWordlist {
		started, scanErr = runWordlistScan(ctx, *tool, jobID, *wordlistFile, wordlistContent, *runtimeTarget, *options, *chunks, *workers, *computeMode, *format, queue, storage, compute, outputs, bucket, queueURL, tracker)
	} else {
		started, scanErr = runTargetListScan(ctx, *tool, jobID, *inputFile, targetContent, *options, *workers, *computeMode, *format, queue, storage, compute, outputs, bucket, queueURL, tracker)
	}

	if scanErr != nil {
		tracker.Fail(jobID, scanErr)
	} else if started {
		tracker.Complete(jobID)
	}

	// Export results locally before any cleanup.
	var exportDir string
	if *outDir != "" && scanErr == nil && started {
		logStatus("Exporting results to %s...", *outDir)
		result, exportErr := operator.ExportJob(ctx, storage, bucket, *tool, jobID, *outDir)
		if exportErr != nil {
			return fmt.Errorf("export failed: %w", exportErr)
		}
		exportDir = result.Dir
		logStatus("Exported %d results, %d artifacts to %s", result.ResultCount, result.ArtifactCount, result.Dir)

		// Record the local output path in the job record.
		if store := tracker.Store(); store != nil {
			if rec, loadErr := store.Load(jobID); loadErr == nil {
				rec.LocalOutputDir = result.Dir
				store.Update(rec)
			}
		}
	}

	// Destroy only after execution has actually started and export is done.
	if *destroyAfter && started {
		logStatus("Destroying infrastructure (--destroy-after)...")
		if destroyErr := infra.RunDestroy(ctx, cfg, os.Stderr, log); destroyErr != nil {
			if scanErr != nil {
				return fmt.Errorf("scan failed: %w; additionally, destroy failed: %v", scanErr, destroyErr)
			}
			return fmt.Errorf("scan completed but destroy failed: %w", destroyErr)
		}
	}

	// Print run summary.
	if started {
		printRunSummary(jobID, *tool, ensureResult.Reused, cleanupPolicy, exportDir)
	}

	return scanErr
}

func runTargetListScan(ctx context.Context, tool, jobID, inputFile, content, options string, workers int, computeMode, format string, queue cloud.Queue, storage cloud.Storage, compute cloud.Compute, outputs map[string]string, bucket, queueURL string, tracker *operator.Tracker) (bool, error) {
	targets := parseTargetLines(content)
	if len(targets) == 0 {
		return false, fmt.Errorf("no targets found in %s", inputFile)
	}

	logStatus("Parsed %d targets from %s [job %s]", len(targets), inputFile, jobID)

	// Build tasks.
	tasks := make([]worker.Task, len(targets))
	for i, t := range targets {
		tasks[i] = worker.Task{
			ToolName: tool,
			JobID:    jobID,
			Target:   t,
			Options:  options,
		}
	}

	// Enqueue targets.
	logStatus("Enqueueing %d targets...", len(tasks))
	enqueueCtx, enqueueCancel := context.WithTimeout(ctx, enqueueTimeout)
	defer enqueueCancel()

	bodies := make([]string, len(tasks))
	for i, t := range tasks {
		b, err := json.Marshal(t)
		if err != nil {
			return false, fmt.Errorf("marshaling task %d: %w", i, err)
		}
		bodies[i] = string(b)
	}
	if err := queue.SendBatch(enqueueCtx, queueURL, bodies); err != nil {
		return false, fmt.Errorf("enqueueing targets: %w", err)
	}
	logStatus("Enqueued %d targets", len(tasks))

	// Update job record with total task count.
	if store := tracker.Store(); store != nil {
		if rec, loadErr := store.Load(jobID); loadErr == nil {
			rec.TotalTasks = len(tasks)
			store.Update(rec)
		}
	}
	tracker.UpdatePhase(jobID, operator.PhaseLaunching)

	// Launch workers.
	if err := launchGenericWorkers(ctx, tool, workers, computeMode, compute, outputs, queueURL, bucket); err != nil {
		return false, err
	}

	tracker.UpdatePhase(jobID, operator.PhaseScanning)

	// Poll for progress.
	return true, pollAndOutput(ctx, storage, bucket, tool, jobID, len(tasks), "targets", format)
}

func runWordlistScan(ctx context.Context, tool, jobID, wordlistFile, content, runtimeTarget, options string, chunks, workers int, computeMode, format string, queue cloud.Queue, storage cloud.Storage, compute cloud.Compute, outputs map[string]string, bucket, queueURL string, tracker *operator.Tracker) (bool, error) {
	if chunks <= 0 {
		chunks = workers
	}

	plan, err := jobs.PlanWordlistJob(tool, jobID, runtimeTarget, options, content, chunks)
	if err != nil {
		return false, fmt.Errorf("planning wordlist job: %w", err)
	}

	logStatus("Parsed %d entries from %s, splitting into %d chunks [job %s]", plan.TotalWords, wordlistFile, len(plan.Tasks), jobID)
	if runtimeTarget != "" {
		logStatus("Target: %s", runtimeTarget)
	}

	// Update job record with wordlist metadata.
	tracker.UpdatePhase(jobID, operator.PhaseUploading)
	if store := tracker.Store(); store != nil {
		if rec, loadErr := store.Load(jobID); loadErr == nil {
			rec.TotalTasks = len(plan.Tasks)
			rec.TotalWords = plan.TotalWords
			rec.RuntimeTarget = runtimeTarget
			store.Update(rec)
		}
	}

	// Upload chunks.
	logStatus("Uploading %d chunks to s3://%s/...", len(plan.Tasks), bucket)
	uploadCtx, uploadCancel := context.WithTimeout(ctx, enqueueTimeout)
	defer uploadCancel()
	if err := jobs.UploadChunks(uploadCtx, storage, bucket, plan); err != nil {
		return false, fmt.Errorf("uploading wordlist chunks: %w", err)
	}

	// Enqueue tasks.
	tracker.UpdatePhase(jobID, operator.PhaseEnqueuing)
	logStatus("Enqueueing %d chunk tasks...", len(plan.Tasks))
	enqueueCtx, enqueueCancel := context.WithTimeout(ctx, enqueueTimeout)
	defer enqueueCancel()

	bodies := make([]string, len(plan.Tasks))
	for i, t := range plan.Tasks {
		b, err := json.Marshal(t)
		if err != nil {
			return false, fmt.Errorf("marshaling task %d: %w", i, err)
		}
		bodies[i] = string(b)
	}
	if err := queue.SendBatch(enqueueCtx, queueURL, bodies); err != nil {
		return false, fmt.Errorf("enqueueing chunk tasks: %w", err)
	}
	logStatus("Enqueued %d chunk tasks", len(plan.Tasks))

	tracker.UpdatePhase(jobID, operator.PhaseLaunching)

	// Launch workers.
	if err := launchGenericWorkers(ctx, tool, workers, computeMode, compute, outputs, queueURL, bucket); err != nil {
		return false, err
	}

	tracker.UpdatePhase(jobID, operator.PhaseScanning)

	// Poll for progress.
	return true, pollAndOutput(ctx, storage, bucket, tool, jobID, len(plan.Tasks), "chunks", format)
}

func preflightTargetListFile(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading target file: %w", err)
	}
	if len(parseTargetLines(string(content))) == 0 {
		return "", fmt.Errorf("no targets found in %s", path)
	}
	return string(content), nil
}

func preflightWordlistFile(tool, path, runtimeTarget, options string, chunks, workers int) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading wordlist file: %w", err)
	}
	if chunks <= 0 {
		chunks = workers
	}
	if _, err := jobs.PlanWordlistJob(tool, "preflight", runtimeTarget, options, string(content), chunks); err != nil {
		return "", fmt.Errorf("planning wordlist job: %w", err)
	}
	return string(content), nil
}

func launchGenericWorkers(ctx context.Context, tool string, workers int, computeMode string, compute cloud.Compute, outputs map[string]string, queueURL, bucket string) error {
	logStatus("Launching %d workers (mode: %s)...", workers, computeMode)
	launchCtx, launchCancel := context.WithTimeout(ctx, launchTimeout)
	defer launchCancel()

	containerName := fmt.Sprintf("%s-worker", tool)
	workerEnv := map[string]string{
		"QUEUE_URL": queueURL,
		"S3_BUCKET": bucket,
		"TOOL_NAME": tool,
	}

	useSpot := resolveComputeMode(computeMode, workers)
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
				"Tool":    tool,
			},
		})
		if err != nil {
			return fmt.Errorf("launching spot instances: %w", err)
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
			return fmt.Errorf("launching Fargate tasks: %w", err)
		}
		logStatus("Launched %d Fargate tasks", workers)
	}
	return nil
}

func pollAndOutput(ctx context.Context, storage cloud.Storage, bucket, tool, jobID string, totalTasks int, unitLabel, format string) error {
	logStatus("Scanning...")
	startTime := time.Now()
	scanPrefix := jobs.ResultPrefix(tool, jobID)

	for {
		count, err := storage.Count(ctx, bucket, scanPrefix)
		if err != nil {
			logStatus("Warning: progress check failed: %v", err)
		} else {
			elapsed := time.Since(startTime).Truncate(time.Second)
			pct := float64(count) / float64(totalTasks) * 100
			logStatus("Progress: %d/%d %s (%.1f%%) — elapsed %s", count, totalTasks, unitLabel, pct, elapsed)

			if count >= totalTasks {
				break
			}
		}
		time.Sleep(pollInterval)
	}

	elapsed := time.Since(startTime).Truncate(time.Second)
	logStatus("Scan complete: %d %s in %s", totalTasks, unitLabel, elapsed)

	// Output results.
	return outputGenericResults(ctx, storage, bucket, scanPrefix, format)
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

// parseTargetLines splits content into non-empty, non-comment lines.
func parseTargetLines(content string) []string {
	var targets []string
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		targets = append(targets, line)
	}
	return targets
}
