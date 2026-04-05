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
	"heph4estus/internal/worker"

	awscfg "github.com/aws/aws-sdk-go-v2/config"
)

func runScan(args []string, log logger.Logger) error {
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	tool := fs.String("tool", "", "Tool to run (e.g. httpx, nuclei, subfinder)")
	inputFile := fs.String("file", "", "Path to file containing targets (required)")
	options := fs.String("options", "", "Extra tool-specific options")
	workers := fs.Int("workers", 10, "Number of worker tasks to launch")
	computeMode := fs.String("compute-mode", "auto", "Compute mode: auto, fargate, or spot")
	format := fs.String("format", "text", "Output format: text or json")
	terraformDir := fs.String("terraform-dir", "deployments/aws/generic/environments/dev", "Terraform working directory")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *tool == "" {
		return fmt.Errorf("--tool flag is required")
	}
	if *inputFile == "" {
		return fmt.Errorf("--file flag is required")
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
	if mod.InputType == modules.InputTypeWordlist {
		return fmt.Errorf("tool %q requires wordlist input — planned for PR 5.7", *tool)
	}

	// Read target file.
	content, err := os.ReadFile(*inputFile)
	if err != nil {
		return fmt.Errorf("reading target file: %w", err)
	}

	targets := parseTargetLines(string(content))
	if len(targets) == 0 {
		return fmt.Errorf("no targets found in %s", *inputFile)
	}

	jobID := jobs.NewID(*tool)
	logStatus("Parsed %d targets from %s [job %s]", len(targets), *inputFile, jobID)

	// Build tasks.
	tasks := make([]worker.Task, len(targets))
	for i, t := range targets {
		tasks[i] = worker.Task{
			ToolName: *tool,
			JobID:    jobID,
			Target:   t,
			Options:  *options,
		}
	}

	// Read terraform outputs.
	tf := infra.NewTerraformClient(log)
	ctx := context.Background()
	outputs, err := tf.ReadOutputs(ctx, *terraformDir)
	if err != nil {
		return fmt.Errorf("reading terraform outputs (is infrastructure deployed?): %w", err)
	}

	queueURL := outputs["sqs_queue_url"]
	bucket := outputs["s3_bucket_name"]
	if queueURL == "" || bucket == "" {
		return fmt.Errorf("terraform outputs missing sqs_queue_url or s3_bucket_name")
	}

	// Guard: verify the deployed infra matches the requested tool.
	// The generic Terraform directory is shared — if it was last deployed for
	// a different tool, the ECR image and task definition won't have the right
	// binary installed.
	if deployedTool := outputs["tool_name"]; deployedTool != "" && deployedTool != *tool {
		return fmt.Errorf("infrastructure was deployed for %q but scan requested %q — run 'heph infra deploy --tool %s --backend generic' first", deployedTool, *tool, *tool)
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

	// Enqueue targets.
	logStatus("Enqueueing %d targets...", len(tasks))
	enqueueCtx, enqueueCancel := context.WithTimeout(ctx, enqueueTimeout)
	defer enqueueCancel()

	bodies := make([]string, len(tasks))
	for i, t := range tasks {
		b, err := json.Marshal(t)
		if err != nil {
			return fmt.Errorf("marshaling task %d: %w", i, err)
		}
		bodies[i] = string(b)
	}
	if err := queue.SendBatch(enqueueCtx, queueURL, bodies); err != nil {
		return fmt.Errorf("enqueueing targets: %w", err)
	}
	logStatus("Enqueued %d targets", len(tasks))

	// Launch workers.
	logStatus("Launching %d workers (mode: %s)...", *workers, *computeMode)
	launchCtx, launchCancel := context.WithTimeout(ctx, launchTimeout)
	defer launchCancel()

	containerName := fmt.Sprintf("%s-worker", *tool)
	workerEnv := map[string]string{
		"QUEUE_URL": queueURL,
		"S3_BUCKET": bucket,
		"TOOL_NAME": *tool,
	}

	useSpot := resolveComputeMode(*computeMode, *workers)
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
			Count:           *workers,
			SecurityGroups:  []string{outputs["security_group_id"]},
			SubnetIDs:       splitOutputList(outputs["subnet_ids"]),
			InstanceProfile: outputs["instance_profile_arn"],
			UserData:        userData,
			Tags: map[string]string{
				"Project": "heph4estus",
				"Tool":    *tool,
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
			Count:          *workers,
		})
		if err != nil {
			return fmt.Errorf("launching Fargate tasks: %w", err)
		}
		logStatus("Launched %d Fargate tasks", *workers)
	}

	// Poll for progress.
	logStatus("Scanning...")
	startTime := time.Now()
	totalTargets := len(tasks)
	scanPrefix := jobs.ResultPrefix(*tool, jobID)

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
	return outputGenericResults(ctx, storage, bucket, scanPrefix, *format)
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
		fmt.Printf("\n%-40s %s\n", "TARGET", "STATUS")
		fmt.Println(strings.Repeat("─", 50))
		var failures int
		for _, key := range keys {
			if !strings.HasSuffix(key, ".json") {
				continue
			}
			target := extractTargetFromKey(key)
			status := "OK"
			data, err := storage.Download(ctx, bucket, key)
			if err != nil {
				status = "???"
			} else {
				var result worker.Result
				if err := json.Unmarshal(data, &result); err == nil && result.Error != "" {
					status = "ERROR"
					failures++
				}
			}
			fmt.Printf("%-40s %s\n", target, status)
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

