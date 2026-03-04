package aws

import (
	"heph4estus/internal/cloud"
	"heph4estus/internal/logger"

	"github.com/aws/aws-sdk-go-v2/aws"
)

// Compile-time interface check.
var _ cloud.Provider = (*AWSProvider)(nil)

// AWSProvider implements cloud.Provider for AWS.
type AWSProvider struct {
	s3  *S3Client
	sqs *SQSClient
	sfn *SFNClient
}

// NewProvider creates an AWSProvider from a shared AWS config.
func NewProvider(cfg aws.Config, log logger.Logger) *AWSProvider {
	return &AWSProvider{
		s3:  NewS3Client(cfg, log),
		sqs: NewSQSClient(cfg, log),
		sfn: NewSFNClient(cfg, log),
	}
}

func (p *AWSProvider) Storage() cloud.Storage { return p.s3 }
func (p *AWSProvider) Queue() cloud.Queue     { return p.sqs }
func (p *AWSProvider) Compute() cloud.Compute { return stubCompute{} }

// SFN returns the Step Functions client (not part of cloud.Provider).
func (p *AWSProvider) SFN() *SFNClient { return p.sfn }
