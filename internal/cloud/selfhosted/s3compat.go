// Package selfhosted contains cloud.Provider implementations that target
// operator-owned infrastructure (MinIO for object storage, NATS JetStream
// for queuing) instead of AWS managed services.
//
// Storage and queue are fully implemented. Compute remains a stub until
// PR 6.2 lands selfhosted compute (docker run over SSH).
package selfhosted

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"heph4estus/internal/logger"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3API is the subset of the S3 SDK used by the S3-compatible storage client.
// It mirrors internal/cloud/aws.S3API so test fakes can be reused.
type S3API interface {
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	ListObjectsV2(ctx context.Context, params *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
}

// StorageConfig describes an S3-compatible endpoint such as MinIO. Callers
// pass this explicitly; nothing is read from the ambient AWS environment so
// operator misconfiguration surfaces as a clear error rather than an
// accidental AWS call.
type StorageConfig struct {
	Endpoint  string
	Region    string
	AccessKey string
	Secret    string
	// PathStyle forces path-style addressing. MinIO and most other
	// S3-compatible endpoints require this because they do not support the
	// virtual-host request shape AWS uses.
	PathStyle bool
}

// Storage is a cloud.Storage implementation that talks to any S3-compatible
// endpoint using the AWS S3 SDK with a custom BaseEndpoint.
type Storage struct {
	client S3API
	logger logger.Logger
}

// NewStorage builds a Storage client from explicit S3-compatible settings.
func NewStorage(cfg StorageConfig, log logger.Logger) (*Storage, error) {
	if log == nil {
		return nil, fmt.Errorf("selfhosted: logger is required")
	}
	endpoint, err := normalizeEndpoint(cfg.Endpoint)
	if err != nil {
		return nil, err
	}
	if cfg.AccessKey == "" || cfg.Secret == "" {
		return nil, fmt.Errorf("selfhosted: S3 access key and secret are required")
	}

	region := cfg.Region
	if region == "" {
		// The AWS SDK requires a region even when the target endpoint
		// ignores it. us-east-1 is the conventional placeholder MinIO uses.
		region = "us-east-1"
	}

	pathStyle := cfg.PathStyle
	awsCfg := aws.Config{
		Region:      region,
		Credentials: credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.Secret, ""),
	}
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = pathStyle
	})
	return &Storage{client: client, logger: log}, nil
}

// NewStorageWithClient wraps an injected S3 client. It exists so tests can
// substitute a fake without spinning up a real MinIO.
func NewStorageWithClient(client S3API, log logger.Logger) *Storage {
	return &Storage{client: client, logger: log}
}

// Upload uploads data to an S3-compatible bucket.
func (s *Storage) Upload(ctx context.Context, bucket, key string, data []byte) error {
	s.logger.Info("Uploading object to S3-compatible: %s/%s", bucket, key)
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(data),
	})
	return err
}

// Download retrieves an object from an S3-compatible bucket.
func (s *Storage) Download(ctx context.Context, bucket, key string) ([]byte, error) {
	s.logger.Info("Downloading object from S3-compatible: %s/%s", bucket, key)
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
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
func (s *Storage) List(ctx context.Context, bucket, prefix string) ([]string, error) {
	s.logger.Info("Listing objects in S3-compatible: %s/%s", bucket, prefix)
	var keys []string
	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String(prefix),
	}
	for {
		out, err := s.client.ListObjectsV2(ctx, input)
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

// Count returns the number of objects matching a prefix without materializing keys.
func (s *Storage) Count(ctx context.Context, bucket, prefix string) (int, error) {
	s.logger.Info("Counting objects in S3-compatible: %s/%s", bucket, prefix)
	count := 0
	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String(prefix),
	}
	for {
		out, err := s.client.ListObjectsV2(ctx, input)
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

// normalizeEndpoint cleans the raw endpoint string so later TLS/host errors
// do not become hard-to-diagnose runtime failures.
func normalizeEndpoint(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("selfhosted: S3 endpoint is required")
	}
	if !strings.Contains(trimmed, "://") {
		trimmed = "https://" + trimmed
	}
	// Validate that something follows the scheme separator before we strip
	// trailing slashes, otherwise "https://" collapses to "https:".
	schemeIdx := strings.Index(trimmed, "://")
	host := strings.TrimLeft(trimmed[schemeIdx+3:], "/")
	if host == "" {
		return "", fmt.Errorf("selfhosted: S3 endpoint host is empty: %q", raw)
	}
	return strings.TrimRight(trimmed, "/"), nil
}
