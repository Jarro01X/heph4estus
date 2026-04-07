package config

import (
	"errors"
	"os"
)

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
