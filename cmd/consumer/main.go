package main

import (
	"bytes"         // Needed for handling binary data (for S3 uploads)
	"context"       // For AWS API context management
	"encoding/json" // For JSON marshaling/unmarshaling
	"fmt"           // For string formatting and printing
	"log"           // For logging messages and errors
	"os"            // For environment variables and file operations
	"os/exec"       // For executing nmap commands
	"strings"       // For string manipulation functions
	"time"          // For timestamps and delays

	"github.com/aws/aws-sdk-go-v2/aws"         // Core AWS SDK functionality
	"github.com/aws/aws-sdk-go-v2/config"      // AWS configuration management
	"github.com/aws/aws-sdk-go-v2/service/s3"  // AWS S3 operations
	"github.com/aws/aws-sdk-go-v2/service/sqs" // AWS SQS operations
)

type ScanTask struct {
	Target  string `json:"target"`
	Options string `json:"options"`
}

type ScanResult struct {
	Target    string    `json:"target"`
	Output    string    `json:"output"`
	Error     string    `json:"error,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

func main() {
	// Initialize AWS clients
	log.Println("Scanner application starting...")
	cfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		log.Fatalf("Unable to load SDK config: %v", err)
	}
	log.Println("AWS SDK config loaded successfully")

	sqsClient := sqs.NewFromConfig(cfg)
	s3Client := s3.NewFromConfig(cfg)
	queueUrl := os.Getenv("QUEUE_URL")

	log.Printf("Using queue URL: %s", queueUrl)
	log.Printf("Using S3 bucket: %s", os.Getenv("S3_BUCKET"))

	// Set a timeout for message retrieval
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	log.Println("Waiting for message from SQS...")
	sqsResult, err := sqsClient.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
		QueueUrl:            &queueUrl,
		MaxNumberOfMessages: 1,
		WaitTimeSeconds:     20, // Long polling
	})

	if err != nil {
		log.Fatalf("Error receiving message: %v", err)
	}

	if len(sqsResult.Messages) == 0 {
		log.Println("No messages received within timeout period, exiting")
		return
	}

	log.Printf("Received message, processing...")

	// Process the message
	message := sqsResult.Messages[0]
	var task ScanTask
	if err := json.Unmarshal([]byte(*message.Body), &task); err != nil {
		log.Fatalf("Error unmarshaling task: %v", err)
	}

	// Execute nmap scan
	log.Printf("Running nmap scan for target: %s with options: %s", task.Target, task.Options)
	cmd := exec.Command("nmap", append([]string{task.Target}, strings.Fields(task.Options)...)...)
	output, err := cmd.CombinedOutput()

	// Prepare scan result
	scanResult := ScanResult{
		Target:    task.Target,
		Output:    string(output),
		Timestamp: time.Now(),
	}
	if err != nil {
		scanResult.Error = err.Error()
		log.Printf("Scan error: %v", err)
	} else {
		log.Println("Scan completed successfully")
	}

	// Upload result to S3
	log.Println("Uploading result to S3")
	resultJSON, _ := json.Marshal(scanResult)
	s3Key := fmt.Sprintf("scans/%s_%d.json", task.Target, time.Now().Unix())

	_, err = s3Client.PutObject(context.TODO(), &s3.PutObjectInput{
		Bucket: aws.String(os.Getenv("S3_BUCKET")),
		Key:    aws.String(s3Key),
		Body:   bytes.NewReader(resultJSON),
	})
	if err != nil {
		log.Fatalf("Error uploading to S3: %v", err)
	}
	log.Printf("Result uploaded to S3: %s", s3Key)

	// Delete processed message
	log.Println("Deleting message from SQS")
	_, err = sqsClient.DeleteMessage(context.TODO(), &sqs.DeleteMessageInput{
		QueueUrl:      &queueUrl,
		ReceiptHandle: message.ReceiptHandle,
	})
	if err != nil {
		log.Fatalf("Error deleting message: %v", err)
	}

	log.Println("Message processing complete, exiting")
}
