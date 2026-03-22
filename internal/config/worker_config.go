package config

import (
	"errors"
	"os"
	"strconv"
)

// WorkerConfig represents the configuration for the generic worker.
type WorkerConfig struct {
	QueueURL         string
	S3Bucket         string
	ToolName         string
	JitterMaxSeconds int // JITTER_MAX_SECONDS; 0 = disabled
}

// NewWorkerConfig creates a new generic worker configuration from environment variables.
func NewWorkerConfig() (*WorkerConfig, error) {
	queueURL := os.Getenv("QUEUE_URL")
	if queueURL == "" {
		return nil, errors.New("QUEUE_URL environment variable is required")
	}

	s3Bucket := os.Getenv("S3_BUCKET")
	if s3Bucket == "" {
		return nil, errors.New("S3_BUCKET environment variable is required")
	}

	toolName := os.Getenv("TOOL_NAME")
	if toolName == "" {
		return nil, errors.New("TOOL_NAME environment variable is required")
	}

	jitterMax := 0
	if v := os.Getenv("JITTER_MAX_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			jitterMax = n
		}
	}

	return &WorkerConfig{
		QueueURL:         queueURL,
		S3Bucket:         s3Bucket,
		ToolName:         toolName,
		JitterMaxSeconds: jitterMax,
	}, nil
}
