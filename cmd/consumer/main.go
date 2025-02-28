package main

import (
    "bytes"      // Needed for handling binary data (for S3 uploads)
    "context"    // For AWS API context management
    "encoding/json"  // For JSON marshaling/unmarshaling
    "fmt"        // For string formatting and printing
    "log"        // For logging messages and errors
    "os"         // For environment variables and file operations
    "os/exec"    // For executing nmap commands
    "strings"    // For string manipulation functions
    "time"       // For timestamps and delays

    "github.com/aws/aws-sdk-go-v2/aws"  // Core AWS SDK functionality
    "github.com/aws/aws-sdk-go-v2/config"  // AWS configuration management
    "github.com/aws/aws-sdk-go-v2/service/s3"  // AWS S3 operations
    "github.com/aws/aws-sdk-go-v2/service/sqs"  // AWS SQS operations
)

type ScanTask struct {
    Target  string `json:"target"`
    Options string `json:"options"`
}

type ScanResult struct {
    Target     string    `json:"target"`
    Output     string    `json:"output"`
    Error      string    `json:"error,omitempty"`
    Timestamp  time.Time `json:"timestamp"`
}

func main() {
    // Initialize AWS clients
    cfg, err := config.LoadDefaultConfig(context.TODO())
    if err != nil {
        log.Fatalf("Unable to load SDK config: %v", err)
    }

    sqsClient := sqs.NewFromConfig(cfg)
    s3Client := s3.NewFromConfig(cfg)
    queueUrl := os.Getenv("QUEUE_URL")

    // Main processing loop
    for {
        // Receive messages from SQS
        result, err := sqsClient.ReceiveMessage(context.TODO(), &sqs.ReceiveMessageInput{
            QueueUrl:            &queueUrl,
            MaxNumberOfMessages: 1,
            WaitTimeSeconds:     20, // Long polling
        })
        if err != nil {
            log.Printf("Error receiving message: %v", err)
            time.Sleep(time.Second)
            continue
        }

        // Process each message
        for _, message := range result.Messages {
            var task ScanTask
            if err := json.Unmarshal([]byte(*message.Body), &task); err != nil {
                log.Printf("Error unmarshaling task: %v", err)
                continue
            }

            // Execute nmap scan
            cmd := exec.Command("nmap", append([]string{task.Target}, strings.Fields(task.Options)...)...)
            output, err := cmd.CombinedOutput()

            // Prepare scan result
            result := ScanResult{
                Target:    task.Target,
                Output:    string(output),
                Timestamp: time.Now(),
            }
            if err != nil {
                result.Error = err.Error()
            }

            // Upload result to S3
            resultJSON, _ := json.Marshal(result)
            _, err = s3Client.PutObject(context.TODO(), &s3.PutObjectInput{
                Bucket: aws.String(os.Getenv("S3_BUCKET")),
                Key:    aws.String(fmt.Sprintf("scans/%s_%d.json", task.Target, time.Now().Unix())),
                Body:   bytes.NewReader(resultJSON),
            })
            if err != nil {
                log.Printf("Error uploading to S3: %v", err)
            }

            // Delete processed message
            _, err = sqsClient.DeleteMessage(context.TODO(), &sqs.DeleteMessageInput{
                QueueUrl:      &queueUrl,
                ReceiptHandle: message.ReceiptHandle,
            })
            if err != nil {
                log.Printf("Error deleting message: %v", err)
            }
        }
    }
}