package aws

import (
	"context"
	"heph4estus/internal/cloud"
)

// stubCompute satisfies cloud.Compute but returns ErrNotImplemented for all methods.
type stubCompute struct{}

func (stubCompute) RunContainer(_ context.Context, _ cloud.ContainerOpts) (string, error) {
	return "", cloud.ErrNotImplemented
}

func (stubCompute) RunSpotInstances(_ context.Context, _ cloud.SpotOpts) ([]string, error) {
	return nil, cloud.ErrNotImplemented
}

func (stubCompute) GetSpotStatus(_ context.Context, _ []string) ([]cloud.SpotStatus, error) {
	return nil, cloud.ErrNotImplemented
}
