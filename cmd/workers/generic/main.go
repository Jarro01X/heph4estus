package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"

	"heph4estus/internal/cloud"
	"heph4estus/internal/cloud/aws"
	appconfig "heph4estus/internal/config"
	"heph4estus/internal/jobs"
	"heph4estus/internal/logger"
	"heph4estus/internal/modules"
	"heph4estus/internal/worker"
)

// taskExecutor abstracts command execution so tests can inject mock results.
type taskExecutor interface {
	Execute(ctx context.Context, mod *modules.ModuleDefinition, task worker.Task) (worker.Result, []byte, error)
}

func main() {
	log := logger.NewSimpleLogger()
	log.Info("Generic worker starting...")

	cfg, err := appconfig.NewWorkerConfig()
	if err != nil {
		log.Fatal("Failed to load configuration: %v", err)
	}

	log.Info("Tool: %s, Queue: %s, Bucket: %s", cfg.ToolName, cfg.QueueURL, cfg.S3Bucket)

	registry, err := modules.NewDefaultRegistry()
	if err != nil {
		log.Fatal("Failed to load module registry: %v", err)
	}

	mod, err := registry.Get(cfg.ToolName)
	if err != nil {
		log.Fatal("Unknown tool %q: %v", cfg.ToolName, err)
	}

	awsCfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		log.Fatal("Unable to load SDK config: %v", err)
	}

	provider := aws.NewProvider(awsCfg, log)
	executor := worker.NewExecutor(log, provider.Storage(), cfg.S3Bucket)

	ctx := context.Background()
	for {
		processed, err := processMessage(ctx, log, cfg, mod, provider.Queue(), provider.Storage(), executor)
		if err != nil {
			log.Error("Error processing message: %v", err)
		}
		if !processed {
			log.Info("Queue empty, exiting")
			break
		}
	}
}

// processMessage polls for one message, executes the tool, uploads results,
// and deletes the message. Returns true if a message was processed.
func processMessage(
	ctx context.Context,
	log logger.Logger,
	cfg *appconfig.WorkerConfig,
	mod *modules.ModuleDefinition,
	queue cloud.Queue,
	storage cloud.Storage,
	executor taskExecutor,
) (bool, error) {
	msg, err := queue.Receive(ctx, cfg.QueueURL)
	if err != nil {
		return false, fmt.Errorf("receiving message: %w", err)
	}
	if msg == nil {
		return false, nil
	}

	log.Info("Received message (attempt %d), processing...", msg.ReceiveCount)

	var task worker.Task
	if err := json.Unmarshal([]byte(msg.Body), &task); err != nil {
		log.Error("Error unmarshaling task: %v", err)
		if delErr := queue.Delete(ctx, cfg.QueueURL, msg.ReceiptHandle); delErr != nil {
			log.Error("Error deleting malformed message: %v", delErr)
		}
		return true, fmt.Errorf("unmarshaling task: %w", err)
	}

	// Apply pre-scan jitter to spread worker timing.
	if cfg.JitterMaxSeconds > 0 {
		d := worker.ApplyJitter(cfg.JitterMaxSeconds)
		log.Info("Applied jitter: %v", d)
	}

	log.Info("Executing %s for target: %s", mod.Name, task.Target)
	result, outputBytes, execErr := executor.Execute(ctx, mod, task)
	if execErr != nil {
		return true, fmt.Errorf("executing %s for %s: %w", mod.Name, task.Target, execErr)
	}
	if result.ToolName == "" {
		result.ToolName = mod.Name
	}
	if result.JobID == "" {
		result.JobID = task.JobID
	}
	if result.Target == "" {
		result.Target = task.Target
	}
	// Propagate chunk metadata from task to result.
	result.GroupID = task.GroupID
	result.ChunkIdx = task.ChunkIdx
	result.TotalChunks = task.TotalChunks

	log.Info("Execution completed for target: %s, success: %v", task.Target, result.Error == "")

	// Classify errors for retry decisions.
	if result.Error != "" {
		kind := worker.ClassifyError(result.Output, result.Error)
		if kind == worker.ErrorTransient {
			log.Info("Transient error for %s (attempt %d), will retry via SQS: %s",
				task.Target, msg.ReceiveCount, result.Error)
			return true, nil
		}
		log.Info("Permanent error for %s, recording failure: %s", task.Target, result.Error)
	}

	ts := time.Now().Unix()
	uploadCtx, uploadCancel := context.WithTimeout(ctx, 1*time.Minute)
	defer uploadCancel()

	// Upload output file first so the structured result can point to it explicitly.
	if len(outputBytes) > 0 {
		outputKey := jobs.ArtifactKey(mod.Name, task.JobID, task.Target, task.GroupID, task.ChunkIdx, task.TotalChunks, ts, mod.OutputExt)
		if err := storage.Upload(uploadCtx, cfg.S3Bucket, outputKey, outputBytes); err != nil {
			return true, fmt.Errorf("uploading output to S3 for %s: %w", task.Target, err)
		}
		result.OutputKey = outputKey
		log.Info("Output file uploaded to S3: %s", outputKey)
	}

	// Upload result JSON to S3.
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return true, fmt.Errorf("marshaling result for %s: %w", task.Target, err)
	}

	s3Key := jobs.ResultKey(mod.Name, task.JobID, task.Target, task.GroupID, task.ChunkIdx, task.TotalChunks, ts, "json")
	if err := storage.Upload(uploadCtx, cfg.S3Bucket, s3Key, resultJSON); err != nil {
		return true, fmt.Errorf("uploading result to S3 for %s: %w", task.Target, err)
	}
	log.Info("Result uploaded to S3: %s", s3Key)

	// Delete message only after successful upload.
	if err := queue.Delete(ctx, cfg.QueueURL, msg.ReceiptHandle); err != nil {
		log.Error("Error deleting message for target %s: %v", task.Target, err)
	}

	log.Info("Message processing complete for target: %s", task.Target)
	return true, nil
}
