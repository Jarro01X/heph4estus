package aws

import (
	"context"
	"heph4estus/internal/cloud"
	"heph4estus/internal/logger"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

// SQSAPI is the subset of the SQS SDK we use.
type SQSAPI interface {
	SendMessage(ctx context.Context, params *sqs.SendMessageInput, optFns ...func(*sqs.Options)) (*sqs.SendMessageOutput, error)
	ReceiveMessage(ctx context.Context, params *sqs.ReceiveMessageInput, optFns ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error)
	DeleteMessage(ctx context.Context, params *sqs.DeleteMessageInput, optFns ...func(*sqs.Options)) (*sqs.DeleteMessageOutput, error)
}

// SQSClient is a wrapper around the SQS client
type SQSClient struct {
	client SQSAPI
	logger logger.Logger
}

// NewSQSClient creates a new SQS client
func NewSQSClient(cfg aws.Config, logger logger.Logger) *SQSClient {
	return &SQSClient{
		client: sqs.NewFromConfig(cfg),
		logger: logger,
	}
}

// Send publishes a message to a queue.
func (c *SQSClient) Send(ctx context.Context, queueID, body string) error {
	c.logger.Info("Sending message to SQS queue: %s", queueID)
	_, err := c.client.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:    &queueID,
		MessageBody: &body,
	})
	return err
}

// Receive polls for a single message. Returns (nil, nil) when the queue is empty.
func (c *SQSClient) Receive(ctx context.Context, queueID string) (*cloud.Message, error) {
	c.logger.Info("Receiving messages from SQS queue: %s", queueID)
	out, err := c.client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
		QueueUrl:            &queueID,
		MaxNumberOfMessages: 1,
		WaitTimeSeconds:     20,
	})
	if err != nil {
		return nil, err
	}
	if len(out.Messages) == 0 {
		return nil, nil
	}
	msg := out.Messages[0]
	return &cloud.Message{
		ID:            aws.ToString(msg.MessageId),
		Body:          aws.ToString(msg.Body),
		ReceiptHandle: aws.ToString(msg.ReceiptHandle),
	}, nil
}

// Delete removes a processed message from the queue.
func (c *SQSClient) Delete(ctx context.Context, queueID, receiptHandle string) error {
	c.logger.Info("Deleting message from SQS queue: %s", queueID)
	_, err := c.client.DeleteMessage(ctx, &sqs.DeleteMessageInput{
		QueueUrl:      &queueID,
		ReceiptHandle: &receiptHandle,
	})
	return err
}

// --- Backward-compatible methods (used by cmd/workers/nmap/main.go) ---

// ReceiveMessage receives a message from SQS (legacy signature).
func (c *SQSClient) ReceiveMessage(ctx context.Context, queueURL string) (*sqs.ReceiveMessageOutput, error) {
	c.logger.Info("Receiving messages from SQS queue: %s", queueURL)
	return c.client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
		QueueUrl:            &queueURL,
		MaxNumberOfMessages: 1,
		WaitTimeSeconds:     20,
	})
}

// DeleteMessage deletes a message from SQS (legacy signature).
func (c *SQSClient) DeleteMessage(ctx context.Context, queueURL string, receiptHandle *string) error {
	c.logger.Info("Deleting message from SQS queue: %s", queueURL)
	_, err := c.client.DeleteMessage(ctx, &sqs.DeleteMessageInput{
		QueueUrl:      &queueURL,
		ReceiptHandle: receiptHandle,
	})
	return err
}

// SendMessage sends a message to SQS (legacy signature).
func (c *SQSClient) SendMessage(ctx context.Context, queueURL string, messageBody string) (*sqs.SendMessageOutput, error) {
	c.logger.Info("Sending message to SQS queue: %s", queueURL)
	return c.client.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:    &queueURL,
		MessageBody: &messageBody,
	})
}
