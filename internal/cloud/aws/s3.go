package aws

import (
	"bytes"
	"context"
	"heph4estus/internal/logger"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3API is the subset of the S3 SDK we use.
type S3API interface {
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	ListObjectsV2(ctx context.Context, params *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
}

// S3Client is a wrapper around the S3 client
type S3Client struct {
	client S3API
	logger logger.Logger
}

// NewS3Client creates a new S3 client
func NewS3Client(cfg aws.Config, logger logger.Logger) *S3Client {
	return &S3Client{
		client: s3.NewFromConfig(cfg),
		logger: logger,
	}
}

// Upload uploads data to an object store bucket.
func (c *S3Client) Upload(ctx context.Context, bucket, key string, data []byte) error {
	c.logger.Info("Uploading object to S3: %s/%s", bucket, key)
	_, err := c.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(data),
	})
	return err
}

// Download retrieves an object from the store.
func (c *S3Client) Download(ctx context.Context, bucket, key string) ([]byte, error) {
	c.logger.Info("Downloading object from S3: %s/%s", bucket, key)
	out, err := c.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, err
	}
	defer out.Body.Close() //nolint:errcheck // best-effort close on read path
	return io.ReadAll(out.Body)
}

// List returns all object keys matching a prefix, paginating as needed.
func (c *S3Client) List(ctx context.Context, bucket, prefix string) ([]string, error) {
	c.logger.Info("Listing objects in S3: %s/%s", bucket, prefix)
	var keys []string
	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String(prefix),
	}
	for {
		out, err := c.client.ListObjectsV2(ctx, input)
		if err != nil {
			return nil, err
		}
		for _, obj := range out.Contents {
			keys = append(keys, aws.ToString(obj.Key))
		}
		if !aws.ToBool(out.IsTruncated) {
			break
		}
		input.ContinuationToken = out.NextContinuationToken
	}
	return keys, nil
}

// Count returns the number of objects matching a prefix without fetching all keys.
func (c *S3Client) Count(ctx context.Context, bucket, prefix string) (int, error) {
	c.logger.Info("Counting objects in S3: %s/%s", bucket, prefix)
	count := 0
	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String(prefix),
	}
	for {
		out, err := c.client.ListObjectsV2(ctx, input)
		if err != nil {
			return 0, err
		}
		count += len(out.Contents)
		if !aws.ToBool(out.IsTruncated) {
			break
		}
		input.ContinuationToken = out.NextContinuationToken
	}
	return count, nil
}

// PutObject uploads an object to S3 (backward-compat alias for Upload).
func (c *S3Client) PutObject(ctx context.Context, bucket, key string, data []byte) error {
	return c.Upload(ctx, bucket, key, data)
}
