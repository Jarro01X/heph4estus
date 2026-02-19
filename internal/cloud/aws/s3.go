package aws

import (
	"bytes"
	"context"
	"heph4estus/internal/logger"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3Client is a wrapper around the S3 client
type S3Client struct {
	client *s3.Client
	logger logger.Logger
}

// NewS3Client creates a new S3 client
func NewS3Client(cfg aws.Config, logger logger.Logger) *S3Client {
	return &S3Client{
		client: s3.NewFromConfig(cfg),
		logger: logger,
	}
}

// PutObject uploads an object to S3
func (c *S3Client) PutObject(ctx context.Context, bucket, key string, data []byte) error {
	c.logger.Info("Uploading object to S3: %s/%s", bucket, key)
	_, err := c.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(data),
	})
	return err
}
