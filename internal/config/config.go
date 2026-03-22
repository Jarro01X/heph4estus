package config

import (
	"errors"
	"os"
	"strconv"
)

// ConsumerConfig represents the configuration for the consumer application
type ConsumerConfig struct {
	QueueURL           string
	S3Bucket           string
	JitterMaxSeconds   int    // JITTER_MAX_SECONDS; 0 = disabled
	NmapTimingTemplate string // NMAP_TIMING_TEMPLATE; e.g. "3" for -T3
	DNSServers         string // DNS_SERVERS; e.g. "8.8.8.8,8.8.4.4"
}

// NewConsumerConfig creates a new consumer configuration from environment variables
func NewConsumerConfig() (*ConsumerConfig, error) {
	queueURL := os.Getenv("QUEUE_URL")
	if queueURL == "" {
		return nil, errors.New("QUEUE_URL environment variable is required")
	}

	s3Bucket := os.Getenv("S3_BUCKET")
	if s3Bucket == "" {
		return nil, errors.New("S3_BUCKET environment variable is required")
	}

	jitterMax := 0
	if v := os.Getenv("JITTER_MAX_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			jitterMax = n
		}
	}

	return &ConsumerConfig{
		QueueURL:           queueURL,
		S3Bucket:           s3Bucket,
		JitterMaxSeconds:   jitterMax,
		NmapTimingTemplate: os.Getenv("NMAP_TIMING_TEMPLATE"),
		DNSServers:         os.Getenv("DNS_SERVERS"),
	}, nil
}

// ProducerConfig represents the configuration for the producer application
type ProducerConfig struct {
	StateMachineARN string
}

// NewProducerConfig creates a new producer configuration from environment variables
func NewProducerConfig() (*ProducerConfig, error) {
	stateMachineARN := os.Getenv("STATE_MACHINE_ARN")
	if stateMachineARN == "" {
		return nil, errors.New("STATE_MACHINE_ARN environment variable is required")
	}

	return &ProducerConfig{
		StateMachineARN: stateMachineARN,
	}, nil
}
