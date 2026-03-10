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
	"heph4estus/internal/logger"
	"heph4estus/internal/tools/nmap"

	awscfg "github.com/aws/aws-sdk-go-v2/config"
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
	workers := fs.Int("workers", 10, "Number of worker tasks to launch")
	computeMode := fs.String("compute-mode", "auto", "Compute mode: auto, fargate, or spot")
	format := fs.String("format", "text", "Output format: text or json")
	terraformDir := fs.String("terraform-dir", "deployments/aws/nmap/environments/dev", "Terraform working directory")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *inputFile == "" {
		return fmt.Errorf("--file flag is required")
	}
	if *computeMode != "auto" && *computeMode != "fargate" && *computeMode != "spot" {
		return fmt.Errorf("--compute-mode must be auto, fargate, or spot")
	}
	if *format != "text" && *format != "json" {
		return fmt.Errorf("--format must be text or json")
	}
	if *workers <= 0 {
		return fmt.Errorf("--workers must be positive")
	}

	content, err := os.ReadFile(*inputFile)
	if err != nil {
		return fmt.Errorf("reading target file: %w", err)
	}

	// Parse targets.
	scanner := nmap.NewScanner(log)
	tasks := scanner.ParseTargets(string(content), *defaultOptions)
	if len(tasks) == 0 {
		return fmt.Errorf("no targets found in %s", *inputFile)
	}
	logStatus("Parsed %d targets from %s", len(tasks), *inputFile)

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

	useSpot := resolveComputeMode(*computeMode, *workers)
	if useSpot {
		ecrURL := outputs["ecr_repo_url"]
		userData := awscloud.GenerateUserData(awscloud.UserDataOpts{
			ECRRepoURL: ecrURL,
			ImageTag:   "latest",
			Region:     regionFromECR(ecrURL),
			EnvVars: map[string]string{
				"QUEUE_URL": queueURL,
				"S3_BUCKET": bucket,
			},
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
				"Tool":    "nmap",
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
			ContainerName:  "nmap-scanner",
			Subnets:        splitOutputList(outputs["subnet_ids"]),
			SecurityGroups: []string{outputs["security_group_id"]},
			Env: map[string]string{
				"QUEUE_URL": queueURL,
				"S3_BUCKET": bucket,
			},
			Count: *workers,
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

	for {
		count, err := storage.Count(ctx, bucket, "scans/")
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
	return outputResults(ctx, storage, bucket, *format)
}

func outputResults(ctx context.Context, storage cloud.Storage, bucket, format string) error {
	keys, err := storage.List(ctx, bucket, "scans/")
	if err != nil {
		return fmt.Errorf("listing results: %w", err)
	}

	if format == "json" {
		encoder := json.NewEncoder(os.Stdout)
		for _, key := range keys {
			data, err := storage.Download(ctx, bucket, key)
			if err != nil {
				logStatus("Warning: failed to download %s: %v", key, err)
				continue
			}
			var result nmap.ScanResult
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
			target := extractTargetFromKey(key)
			fmt.Printf("%-40s %s\n", target, "done")
		}
		fmt.Printf("\n%d results written to s3://%s/scans/\n", len(keys), bucket)
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
	key = strings.TrimPrefix(key, "scans/")
	key = strings.TrimSuffix(key, ".json")
	idx := strings.LastIndex(key, "_")
	if idx > 0 {
		return key[:idx]
	}
	return key
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
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}
