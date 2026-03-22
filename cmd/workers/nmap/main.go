package main

import (
	"context"
	"encoding/json"
	"fmt"
	"heph4estus/internal/cloud"
	"heph4estus/internal/cloud/aws"
	appconfig "heph4estus/internal/config"
	"heph4estus/internal/logger"
	"heph4estus/internal/tools/nmap"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
)

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
func processMessage(
	ctx context.Context,
	log logger.Logger,
	cfg *appconfig.ConsumerConfig,
	queue cloud.Queue,
	storage cloud.Storage,
	scannerSvc *nmap.Scanner,
) (bool, error) {
	msg, err := queue.Receive(ctx, cfg.QueueURL)
	if err != nil {
		return false, fmt.Errorf("receiving message: %w", err)
	}
	if msg == nil {
		return false, nil
	}

	log.Info("Received message, processing...")

	var task nmap.ScanTask
	if err := json.Unmarshal([]byte(msg.Body), &task); err != nil {
		log.Error("Error unmarshaling task: %v", err)
		// Malformed messages are unrecoverable — delete to prevent poison-pill loop.
		if delErr := queue.Delete(ctx, cfg.QueueURL, msg.ReceiptHandle); delErr != nil {
			log.Error("Error deleting malformed message: %v", delErr)
		}
		return true, fmt.Errorf("unmarshaling task: %w", err)
	}

	log.Info("Starting scan for target: %s", task.Target)
	scanResult := scannerSvc.RunScan(task)
	log.Info("Scan completed for target: %s, success: %v", task.Target, scanResult.Error == "")

	// Upload result to S3 — even if scan errored, we record the failure.
	resultJSON, err := scannerSvc.FormatResult(scanResult)
	if err != nil {
		return true, fmt.Errorf("formatting result for %s: %w", task.Target, err)
	}

	var s3Key string
	if task.GroupID != "" {
		s3Key = fmt.Sprintf("scans/%s/%s_chunk%d_of_%d_%d.json",
			task.GroupID, task.Target, task.ChunkIdx, task.TotalChunks, time.Now().Unix())
	} else {
		s3Key = fmt.Sprintf("scans/%s_%d.json", task.Target, time.Now().Unix())
	}
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
