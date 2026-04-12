package config

import (
	"errors"
	"os"
	"strconv"
)

// WorkerConfig represents the configuration for the generic worker.
type WorkerConfig struct {
	Cloud            string // CLOUD; defaults to "aws"
	QueueID          string // QUEUE_URL — logical queue identifier
	Bucket           string // S3_BUCKET — storage bucket name
	ToolName         string
	JitterMaxSeconds int // JITTER_MAX_SECONDS; 0 = disabled
}

// NewWorkerConfig creates a new generic worker configuration from environment variables.
func NewWorkerConfig() (*WorkerConfig, error) {
	queueID := os.Getenv("QUEUE_URL")
	if queueID == "" {
		return nil, errors.New("QUEUE_URL environment variable is required")
	}

	bucket := os.Getenv("S3_BUCKET")
	if bucket == "" {
		return nil, errors.New("S3_BUCKET environment variable is required")
	}

	toolName := os.Getenv("TOOL_NAME")
	if toolName == "" {
		return nil, errors.New("TOOL_NAME environment variable is required")
	}

	cloudVal := os.Getenv("CLOUD")
	if cloudVal == "" {
		cloudVal = "aws"
	}

	jitterMax := 0
	if v := os.Getenv("JITTER_MAX_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			jitterMax = n
		}
	}

	return &WorkerConfig{
		Cloud:            cloudVal,
		QueueID:          queueID,
		Bucket:           bucket,
		ToolName:         toolName,
		JitterMaxSeconds: jitterMax,
	}, nil
}
