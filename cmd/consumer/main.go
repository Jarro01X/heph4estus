package main

import (
	"context"
	"encoding/json"
	"fmt"
	"nmap-scanner/internal/aws"
	appconfig "nmap-scanner/internal/config"
	"nmap-scanner/internal/logger"
	"nmap-scanner/internal/models"
	"nmap-scanner/internal/scanner"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
)

func main() {
	log := logger.NewSimpleLogger()
	log.Info("Scanner consumer application starting...")

	// Load configuration
	cfg, err := appconfig.NewConsumerConfig()
	if err != nil {
		log.Fatal("Failed to load configuration: %v", err)
	}

	log.Info("Using queue URL: %s", cfg.QueueURL)
	log.Info("Using S3 bucket: %s", cfg.S3Bucket)

	// Initialize AWS clients
	log.Info("Initializing AWS clients...")
	awsCfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		log.Fatal("Unable to load SDK config: %v", err)
	}

	sqsClient := aws.NewSQSClient(awsCfg, log)
	s3Client := aws.NewS3Client(awsCfg, log)
	scannerSvc := scanner.NewScanner(log)

	// Process messages
	processMessage(log, cfg, sqsClient, s3Client, scannerSvc)
}

func processMessage(
	log logger.Logger,
	cfg *appconfig.ConsumerConfig,
	sqsClient *aws.SQSClient,
	s3Client *aws.S3Client,
	scannerSvc *scanner.Scanner,
) {
	// Set a timeout for the entire processing (10 minutes)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	log.Info("Waiting for message from SQS...")
	sqsReceiveCtx, sqsReceiveCancel := context.WithTimeout(ctx, 30*time.Second)
	defer sqsReceiveCancel()

	sqsResult, err := sqsClient.ReceiveMessage(sqsReceiveCtx, cfg.QueueURL)
	if err != nil {
		log.Fatal("Error receiving message: %v", err)
	}

	if len(sqsResult.Messages) == 0 {
		log.Info("No messages received within timeout period, exiting")
		return
	}

	message := sqsResult.Messages[0]
	log.Info("Received message, processing...")

	// Process the message
	var task models.ScanTask
	if err := json.Unmarshal([]byte(*message.Body), &task); err != nil {
		log.Error("Error unmarshaling task: %v", err)
		// Delete the message anyway to prevent it from getting stuck in the queue
		if err = sqsClient.DeleteMessage(ctx, cfg.QueueURL, message.ReceiptHandle); err != nil {
			log.Error("Error deleting malformed message: %v", err)
		}
		return
	}

	log.Info("Starting scan for target: %s", task.Target)
	scanResult := scannerSvc.RunScan(task)
	log.Info("Scan completed for target: %s, success: %v", task.Target, scanResult.Error == "")

	// Upload result to S3
	log.Info("Uploading result to S3 for target: %s", task.Target)
	resultJSON, err := scannerSvc.FormatResult(scanResult)
	if err != nil {
		log.Error("Error formatting result for target %s: %v", task.Target, err)
		// Continue to delete the message
	} else {
		s3Key := fmt.Sprintf("scans/%s_%d.json", task.Target, time.Now().Unix())
		s3UploadCtx, s3UploadCancel := context.WithTimeout(ctx, 1*time.Minute)
		defer s3UploadCancel()

		if err = s3Client.PutObject(s3UploadCtx, cfg.S3Bucket, s3Key, resultJSON); err != nil {
			log.Error("Error uploading to S3 for target %s: %v", task.Target, err)
			// Continue to delete the message
		} else {
			log.Info("Result uploaded to S3: %s", s3Key)
		}
	}

	// Delete processed message regardless of scan outcome
	log.Info("Deleting message from SQS for target: %s", task.Target)
	if err = sqsClient.DeleteMessage(ctx, cfg.QueueURL, message.ReceiptHandle); err != nil {
		log.Error("Error deleting message for target %s: %v", task.Target, err)
	} else {
		log.Info("Message deleted from SQS for target: %s", task.Target)
	}

	log.Info("Message processing complete for target: %s", task.Target)
}
