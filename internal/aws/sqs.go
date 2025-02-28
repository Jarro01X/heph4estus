package aws

import (
	"context"
	"nmap-scanner/internal/logger"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

// SQSClient is a wrapper around the SQS client
type SQSClient struct {
	client *sqs.Client
	logger logger.Logger
}

// NewSQSClient creates a new SQS client
func NewSQSClient(cfg aws.Config, logger logger.Logger) *SQSClient {
	return &SQSClient{
		client: sqs.NewFromConfig(cfg),
		logger: logger,
	}
}

// ReceiveMessage receives a message from SQS
func (c *SQSClient) ReceiveMessage(ctx context.Context, queueURL string) (*sqs.ReceiveMessageOutput, error) {
	c.logger.Info("Receiving messages from SQS queue: %s", queueURL)
	return c.client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
		QueueUrl:            &queueURL,
		MaxNumberOfMessages: 1,
		WaitTimeSeconds:     20, // Long polling
	})
}

// DeleteMessage deletes a message from SQS
func (c *SQSClient) DeleteMessage(ctx context.Context, queueURL string, receiptHandle *string) error {
	c.logger.Info("Deleting message from SQS queue: %s", queueURL)
	_, err := c.client.DeleteMessage(ctx, &sqs.DeleteMessageInput{
		QueueUrl:      &queueURL,
		ReceiptHandle: receiptHandle,
	})
	return err
}

// SendMessage sends a message to SQS
func (c *SQSClient) SendMessage(ctx context.Context, queueURL string, messageBody string) (*sqs.SendMessageOutput, error) {
	c.logger.Info("Sending message to SQS queue: %s", queueURL)
	return c.client.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:    &queueURL,
		MessageBody: &messageBody,
	})
}
