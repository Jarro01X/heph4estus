package aws

import (
	"context"
	"fmt"
	"strings"

	"heph4estus/internal/cloud"
	"heph4estus/internal/logger"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// EC2API is the subset of the EC2 SDK we use.
type EC2API interface {
	CreateFleet(ctx context.Context, params *ec2.CreateFleetInput, optFns ...func(*ec2.Options)) (*ec2.CreateFleetOutput, error)
	DescribeInstances(ctx context.Context, params *ec2.DescribeInstancesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error)
	TerminateInstances(ctx context.Context, params *ec2.TerminateInstancesInput, optFns ...func(*ec2.Options)) (*ec2.TerminateInstancesOutput, error)
	CreateLaunchTemplate(ctx context.Context, params *ec2.CreateLaunchTemplateInput, optFns ...func(*ec2.Options)) (*ec2.CreateLaunchTemplateOutput, error)
	DeleteLaunchTemplate(ctx context.Context, params *ec2.DeleteLaunchTemplateInput, optFns ...func(*ec2.Options)) (*ec2.DeleteLaunchTemplateOutput, error)
}

// EC2Client wraps the EC2 SDK client for spot instance operations.
type EC2Client struct {
	client EC2API
	logger logger.Logger
}

// NewEC2Client creates a new EC2 client.
func NewEC2Client(cfg aws.Config, logger logger.Logger) *EC2Client {
	return &EC2Client{
		client: ec2.NewFromConfig(cfg),
		logger: logger,
	}
}

// RunSpotInstances creates an ephemeral launch template and uses CreateFleet
// to provision spot instances. Returns instance IDs.
func (c *EC2Client) RunSpotInstances(ctx context.Context, opts cloud.SpotOpts) ([]string, error) {
	if opts.Count <= 0 {
		return nil, nil
	}

	// Build launch template data
	ltData := &ec2types.RequestLaunchTemplateData{
		ImageId:  aws.String(opts.AMI),
		UserData: aws.String(opts.UserData),
	}

	if opts.InstanceProfile != "" {
		ltData.IamInstanceProfile = &ec2types.LaunchTemplateIamInstanceProfileSpecificationRequest{
			Arn: aws.String(opts.InstanceProfile),
		}
	}

	if len(opts.SecurityGroups) > 0 {
		ltData.NetworkInterfaces = []ec2types.LaunchTemplateInstanceNetworkInterfaceSpecificationRequest{
			{
				DeviceIndex:              aws.Int32(0),
				AssociatePublicIpAddress: aws.Bool(true),
				Groups:                   opts.SecurityGroups,
			},
		}
		if len(opts.SubnetIDs) > 0 {
			ltData.NetworkInterfaces[0].SubnetId = aws.String(opts.SubnetIDs[0])
		}
	}

	// Create ephemeral launch template
	ltName := fmt.Sprintf("heph-spot-%d", hashOpts(opts))
	ltOut, err := c.client.CreateLaunchTemplate(ctx, &ec2.CreateLaunchTemplateInput{
		LaunchTemplateName: aws.String(ltName),
		LaunchTemplateData: ltData,
		TagSpecifications: []ec2types.TagSpecification{
			{
				ResourceType: ec2types.ResourceTypeLaunchTemplate,
				Tags:         mapToEC2Tags(opts.Tags),
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("CreateLaunchTemplate: %w", err)
	}
	ltID := aws.ToString(ltOut.LaunchTemplate.LaunchTemplateId)
	c.logger.Info("Created launch template %s", ltID)

	// Build instance type overrides
	instanceTypes := opts.InstanceTypes
	if len(instanceTypes) == 0 {
		instanceTypes = []string{"c5.xlarge", "c5a.xlarge", "m5.xlarge", "m5a.xlarge"}
	}
	var overrides []ec2types.FleetLaunchTemplateOverridesRequest
	for _, it := range instanceTypes {
		override := ec2types.FleetLaunchTemplateOverridesRequest{
			InstanceType: ec2types.InstanceType(it),
		}
		overrides = append(overrides, override)
	}

	// CreateFleet with instant type (one-shot, not maintained)
	fleetInput := &ec2.CreateFleetInput{
		Type: ec2types.FleetTypeInstant,
		LaunchTemplateConfigs: []ec2types.FleetLaunchTemplateConfigRequest{
			{
				LaunchTemplateSpecification: &ec2types.FleetLaunchTemplateSpecificationRequest{
					LaunchTemplateId: aws.String(ltID),
					Version:          aws.String("$Latest"),
				},
				Overrides: overrides,
			},
		},
		TargetCapacitySpecification: &ec2types.TargetCapacitySpecificationRequest{
			TotalTargetCapacity:       aws.Int32(int32(opts.Count)),
			DefaultTargetCapacityType: ec2types.DefaultTargetCapacityTypeSpot,
		},
		SpotOptions: &ec2types.SpotOptionsRequest{
			AllocationStrategy: ec2types.SpotAllocationStrategyCapacityOptimizedPrioritized,
		},
	}

	if opts.MaxPrice != "" {
		fleetInput.SpotOptions.MaxTotalPrice = aws.String(opts.MaxPrice)
	}

	c.logger.Info("Creating spot fleet: %d instances", opts.Count)
	fleetOut, err := c.client.CreateFleet(ctx, fleetInput)
	if err != nil {
		// Clean up launch template on fleet failure
		_, _ = c.client.DeleteLaunchTemplate(ctx, &ec2.DeleteLaunchTemplateInput{
			LaunchTemplateId: aws.String(ltID),
		})
		return nil, fmt.Errorf("CreateFleet: %w", err)
	}

	var instanceIDs []string
	for _, inst := range fleetOut.Instances {
		instanceIDs = append(instanceIDs, inst.InstanceIds...)
	}

	c.logger.Info("Launched %d spot instances", len(instanceIDs))
	return instanceIDs, nil
}

// GetSpotStatus returns the current state of the given instances.
func (c *EC2Client) GetSpotStatus(ctx context.Context, instanceIDs []string) ([]cloud.SpotStatus, error) {
	if len(instanceIDs) == 0 {
		return nil, nil
	}

	out, err := c.client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
		InstanceIds: instanceIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("DescribeInstances: %w", err)
	}

	var statuses []cloud.SpotStatus
	for _, res := range out.Reservations {
		for _, inst := range res.Instances {
			statuses = append(statuses, cloud.SpotStatus{
				InstanceID: aws.ToString(inst.InstanceId),
				State:      string(inst.State.Name),
				PublicIP:   aws.ToString(inst.PublicIpAddress),
			})
		}
	}
	return statuses, nil
}

// RunContainer is not implemented for EC2 (use ECS for Fargate).
func (c *EC2Client) RunContainer(_ context.Context, _ cloud.ContainerOpts) (string, error) {
	return "", cloud.ErrNotImplemented
}

func mapToEC2Tags(m map[string]string) []ec2types.Tag {
	tags := make([]ec2types.Tag, 0, len(m))
	for k, v := range m {
		tags = append(tags, ec2types.Tag{
			Key:   aws.String(k),
			Value: aws.String(v),
		})
	}
	return tags
}

func hashOpts(opts cloud.SpotOpts) uint32 {
	// Simple hash for unique launch template name
	h := uint32(17)
	h = h*31 + uint32(opts.Count)
	h = h*31 + uint32(len(opts.AMI))
	h = h*31 + uint32(len(strings.Join(opts.InstanceTypes, ",")))
	return h
}
