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

	// Fleet heartbeat settings (selfhosted/Hetzner workers).
	FleetHeartbeat bool   // FLEET_HEARTBEAT; enables heartbeat publishing
	WorkerID       string // WORKER_ID; unique worker identifier
	WorkerHost     string // WORKER_HOST; private IP or hostname
	NATSURL        string // NATS_URL; NATS server for heartbeats
	WorkerVersion  string // WORKER_VERSION; image tag/version for fleet reporting
	GenerationID   string // FLEET_GENERATION_ID; provider-native fleet generation marker
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

	fleetHeartbeat := os.Getenv("FLEET_HEARTBEAT") == "true"
	workerID := os.Getenv("WORKER_ID")
	if workerID == "" {
		hostname, _ := os.Hostname()
		workerID = hostname
	}

	return &WorkerConfig{
		Cloud:            cloudVal,
		QueueID:          queueID,
		Bucket:           bucket,
		ToolName:         toolName,
		JitterMaxSeconds: jitterMax,
		FleetHeartbeat:   fleetHeartbeat,
		WorkerID:         workerID,
		WorkerHost:       os.Getenv("WORKER_HOST"),
		NATSURL:          os.Getenv("NATS_URL"),
		WorkerVersion:    os.Getenv("WORKER_VERSION"),
		GenerationID:     os.Getenv("FLEET_GENERATION_ID"),
	}, nil
}
