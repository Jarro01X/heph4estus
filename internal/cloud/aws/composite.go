package aws

import (
	"context"

	"heph4estus/internal/cloud"
)

// Compile-time interface check.
var _ cloud.Compute = (*CompositeCompute)(nil)

// CompositeCompute delegates container operations to ECS (Fargate) and spot
// operations to EC2. This allows the TUI to auto-select compute mode.
type CompositeCompute struct {
	ecs *ECSClient
	ec2 *EC2Client
}

func (c *CompositeCompute) RunContainer(ctx context.Context, opts cloud.ContainerOpts) (string, error) {
	return c.ecs.RunContainer(ctx, opts)
}

func (c *CompositeCompute) RunSpotInstances(ctx context.Context, opts cloud.SpotOpts) ([]string, error) {
	return c.ec2.RunSpotInstances(ctx, opts)
}

func (c *CompositeCompute) GetSpotStatus(ctx context.Context, instanceIDs []string) ([]cloud.SpotStatus, error) {
	return c.ec2.GetSpotStatus(ctx, instanceIDs)
}
