package main

import (
	"context"
	"encoding/json"
	"fmt"
	"heph4estus/internal/cloud"
	"heph4estus/internal/cloud/aws"
	appconfig "heph4estus/internal/config"
	"heph4estus/internal/jobs"
	"heph4estus/internal/logger"
	"heph4estus/internal/tools/nmap"
	"heph4estus/internal/worker"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
)

// scanRunner abstracts scan execution so tests can inject mock results.
type scanRunner interface {
	RunScan(task nmap.ScanTask) nmap.ScanResult
	FormatResult(result nmap.ScanResult) ([]byte, error)
}

func main() {
	log := logger.NewSimpleLogger()
	log.Info("Scanner consumer application starting...")

	cfg, err := appconfig.NewConsumerConfig()
	if err != nil {
		log.Fatal("Failed to load configuration: %v", err)
	}

	log.Info("Using queue URL: %s", cfg.QueueURL)
	log.Info("Using S3 bucket: %s", cfg.S3Bucket)

	awsCfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		log.Fatal("Unable to load SDK config: %v", err)
	}

	provider := aws.NewProvider(awsCfg, log)
	scannerSvc := nmap.NewScanner(log)

	ctx := context.Background()
	for {
		processed, err := processMessage(ctx, log, cfg, provider.Queue(), provider.Storage(), scannerSvc)
		if err != nil {
			log.Error("Error processing message: %v", err)
		}
		if !processed {
			log.Info("Queue empty, exiting")
			break
		}
	}
}

// processMessage polls for one message, scans the target, uploads the result,
// and deletes the message. Returns true if a message was processed.
//
// Error handling:
//   - Malformed messages: deleted immediately (poison-pill prevention)
//   - Transient scan errors: no upload, no delete (SQS visibility timeout retries)
//   - Permanent scan errors: upload error result, delete message
//   - Upload failures: no delete (SQS visibility timeout retries)
func processMessage(
	ctx context.Context,
	log logger.Logger,
	cfg *appconfig.ConsumerConfig,
	queue cloud.Queue,
	storage cloud.Storage,
	scanner scanRunner,
) (bool, error) {
	msg, err := queue.Receive(ctx, cfg.QueueURL)
	if err != nil {
		return false, fmt.Errorf("receiving message: %w", err)
	}
	if msg == nil {
		return false, nil
	}

	log.Info("Received message (attempt %d), processing...", msg.ReceiveCount)

	var task nmap.ScanTask
	if err := json.Unmarshal([]byte(msg.Body), &task); err != nil {
		log.Error("Error unmarshaling task: %v", err)
		// Malformed messages are unrecoverable — delete to prevent poison-pill loop.
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

	// Inject timing template and DNS servers into nmap options.
	if cfg.NmapTimingTemplate != "" {
		task.Options = fmt.Sprintf("-T%s %s", cfg.NmapTimingTemplate, task.Options)
	}
	if cfg.DNSServers != "" {
		task.Options = fmt.Sprintf("--dns-servers %s %s", cfg.DNSServers, task.Options)
	}
	if cfg.NoRDNS {
		task.Options = "-n " + task.Options
	}

	log.Info("Starting scan for target: %s", task.Target)
	scanResult := scanner.RunScan(task)
	if scanResult.JobID == "" {
		scanResult.JobID = task.JobID
	}
	log.Info("Scan completed for target: %s, success: %v", task.Target, scanResult.Error == "")

	// Classify scan errors for retry decisions.
	if scanResult.Error != "" {
		kind := worker.ClassifyError(scanResult.Output, scanResult.Error)
		if kind == worker.ErrorTransient {
			log.Info("Transient error for %s (attempt %d), will retry via SQS: %s",
				task.Target, msg.ReceiveCount, scanResult.Error)
			return true, nil
		}
		log.Info("Permanent error for %s, recording failure: %s", task.Target, scanResult.Error)
	}

	// Upload result to S3 — success or permanent error.
	resultJSON, err := scanner.FormatResult(scanResult)
	if err != nil {
		return true, fmt.Errorf("formatting result for %s: %w", task.Target, err)
	}

	s3Key := jobs.ResultKey("nmap", task.JobID, task.Target, task.GroupID, task.ChunkIdx, task.TotalChunks, time.Now().Unix(), "json")
	uploadCtx, uploadCancel := context.WithTimeout(ctx, 1*time.Minute)
	defer uploadCancel()

	if err := storage.Upload(uploadCtx, cfg.S3Bucket, s3Key, resultJSON); err != nil {
		// Do NOT delete message — visibility timeout will cause automatic retry.
		return true, fmt.Errorf("uploading to S3 for %s: %w", task.Target, err)
	}
	log.Info("Result uploaded to S3: %s", s3Key)

	// Delete message only after successful upload.
	if err := queue.Delete(ctx, cfg.QueueURL, msg.ReceiptHandle); err != nil {
		log.Error("Error deleting message for target %s: %v", task.Target, err)
	}

	log.Info("Message processing complete for target: %s", task.Target)
	return true, nil
}
