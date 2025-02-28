package aws

import (
	"context"
	"nmap-scanner/internal/logger"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sfn"
)

// SFNClient is a wrapper around the Step Functions client
type SFNClient struct {
	client *sfn.Client
	logger logger.Logger
}

// NewSFNClient creates a new Step Functions client
func NewSFNClient(cfg aws.Config, logger logger.Logger) *SFNClient {
	return &SFNClient{
		client: sfn.NewFromConfig(cfg),
		logger: logger,
	}
}

// StartExecution starts a Step Functions execution
func (c *SFNClient) StartExecution(ctx context.Context, stateMachineARN string, input string) (*sfn.StartExecutionOutput, error) {
	c.logger.Info("Starting Step Functions execution: %s", stateMachineARN)
	return c.client.StartExecution(ctx, &sfn.StartExecutionInput{
		StateMachineArn: aws.String(stateMachineARN),
		Input:           aws.String(input),
	})
}
