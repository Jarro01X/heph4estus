package aws

import (
	"context"
	"fmt"
	"strings"

	"heph4estus/internal/cloud"
	"heph4estus/internal/logger"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
)

// ECSAPI is the subset of the ECS SDK we use.
type ECSAPI interface {
	RunTask(ctx context.Context, params *ecs.RunTaskInput, optFns ...func(*ecs.Options)) (*ecs.RunTaskOutput, error)
}

// ECSClient wraps the ECS SDK client.
type ECSClient struct {
	client ECSAPI
	logger logger.Logger
}

// NewECSClient creates a new ECS client.
func NewECSClient(cfg aws.Config, logger logger.Logger) *ECSClient {
	return &ECSClient{
		client: ecs.NewFromConfig(cfg),
		logger: logger,
	}
}

// RunContainer launches Fargate tasks. ECS RunTask supports up to 10 per call,
// so we loop for larger counts. Returns all task ARNs joined by commas.
func (c *ECSClient) RunContainer(ctx context.Context, opts cloud.ContainerOpts) (string, error) {
	count := opts.Count
	if count <= 0 {
		count = 1
	}

	var envOverrides []ecstypes.KeyValuePair
	for k, v := range opts.Env {
		envOverrides = append(envOverrides, ecstypes.KeyValuePair{
			Name:  aws.String(k),
			Value: aws.String(v),
		})
	}

	var allARNs []string
	const maxPerCall = 10
	for remaining := count; remaining > 0; {
		batch := remaining
		if batch > maxPerCall {
			batch = maxPerCall
		}

		c.logger.Info("Launching %d Fargate task(s) in cluster %s", batch, opts.Cluster)

		input := &ecs.RunTaskInput{
			Cluster:        aws.String(opts.Cluster),
			TaskDefinition: aws.String(opts.TaskDefinition),
			LaunchType:     ecstypes.LaunchTypeFargate,
			Count:          aws.Int32(int32(batch)),
			NetworkConfiguration: &ecstypes.NetworkConfiguration{
				AwsvpcConfiguration: &ecstypes.AwsVpcConfiguration{
					Subnets:        opts.Subnets,
					SecurityGroups: opts.SecurityGroups,
					AssignPublicIp: ecstypes.AssignPublicIpEnabled,
				},
			},
		}

		if len(envOverrides) > 0 {
			containerName := opts.ContainerName
			if containerName == "" {
				containerName = "worker"
			}
			input.Overrides = &ecstypes.TaskOverride{
				ContainerOverrides: []ecstypes.ContainerOverride{
					{
						Name:        aws.String(containerName),
						Environment: envOverrides,
					},
				},
			}
		}

		out, err := c.client.RunTask(ctx, input)
		if err != nil {
			return strings.Join(allARNs, ","), fmt.Errorf("RunTask: %w", err)
		}
		for _, task := range out.Tasks {
			allARNs = append(allARNs, aws.ToString(task.TaskArn))
		}

		remaining -= batch
	}

	return strings.Join(allARNs, ","), nil
}

// RunSpotInstances is not implemented for ECS.
func (c *ECSClient) RunSpotInstances(_ context.Context, _ cloud.SpotOpts) ([]string, error) {
	return nil, cloud.ErrNotImplemented
}

// GetSpotStatus is not implemented for ECS.
func (c *ECSClient) GetSpotStatus(_ context.Context, _ []string) ([]cloud.SpotStatus, error) {
	return nil, cloud.ErrNotImplemented
}
