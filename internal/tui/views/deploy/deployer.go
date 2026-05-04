package deploy

import (
	"context"
	"io"

	"heph4estus/internal/cloud"
	"heph4estus/internal/infra"
	"heph4estus/internal/logger"
)

// Deployer abstracts infrastructure deployment operations for testability.
type Deployer interface {
	TerraformInit(ctx context.Context, workDir string) error
	TerraformPlan(ctx context.Context, workDir string, vars map[string]string) (string, error)
	TerraformApply(ctx context.Context, workDir string, vars map[string]string, stream io.Writer) error
	TerraformReadOutputs(ctx context.Context, workDir string) (map[string]string, error)
	DockerBuild(ctx context.Context, dockerfile, buildCtx, tag string, stream io.Writer) error
	DockerBuildWithArgs(ctx context.Context, dockerfile, buildCtx, tag string, buildArgs map[string]string, stream io.Writer) error

	// RegistryAuth authenticates Docker to the appropriate image registry.
	// For AWS this does the three-step ECR auth flow; for selfhosted it runs
	// docker login to the controller registry (or no-ops when no credentials
	// are configured).
	RegistryAuth(ctx context.Context, kind cloud.Kind, region string, outputs map[string]string) error

	// ImagePublish tags a local image and pushes it to the provider's registry.
	// For AWS this pushes to ECR; for selfhosted it pushes to the controller registry.
	ImagePublish(ctx context.Context, kind cloud.Kind, localTag string, outputs map[string]string, stream io.Writer) error

	TerraformDestroy(ctx context.Context, workDir string, stream io.Writer) error
}

// RealDeployer delegates to infra CLI wrappers.
type RealDeployer struct {
	tf     *infra.TerraformClient
	docker *infra.DockerClient
	ecr    *infra.ECRClient
}

// NewRealDeployer creates a Deployer backed by real CLI tools.
func NewRealDeployer(log logger.Logger) *RealDeployer {
	return &RealDeployer{
		tf:     infra.NewTerraformClient(log),
		docker: infra.NewDockerClient(log),
		ecr:    infra.NewECRClient(log),
	}
}

func (d *RealDeployer) TerraformInit(ctx context.Context, workDir string) error {
	return d.tf.Init(ctx, workDir)
}

func (d *RealDeployer) TerraformPlan(ctx context.Context, workDir string, vars map[string]string) (string, error) {
	return d.tf.Plan(ctx, workDir, vars)
}

func (d *RealDeployer) TerraformApply(ctx context.Context, workDir string, vars map[string]string, stream io.Writer) error {
	return d.tf.Apply(ctx, workDir, vars, stream)
}

func (d *RealDeployer) TerraformReadOutputs(ctx context.Context, workDir string) (map[string]string, error) {
	return d.tf.ReadOutputs(ctx, workDir)
}

func (d *RealDeployer) DockerBuild(ctx context.Context, dockerfile, buildCtx, tag string, stream io.Writer) error {
	return d.docker.Build(ctx, dockerfile, buildCtx, tag, stream)
}

func (d *RealDeployer) DockerBuildWithArgs(ctx context.Context, dockerfile, buildCtx, tag string, buildArgs map[string]string, stream io.Writer) error {
	return d.docker.BuildWithArgs(ctx, dockerfile, buildCtx, tag, buildArgs, stream)
}

func (d *RealDeployer) RegistryAuth(ctx context.Context, kind cloud.Kind, region string, outputs map[string]string) error {
	if _, err := infra.EnsureProviderRegistryTrust(kind, outputs); err != nil {
		return err
	}
	pub := infra.NewPublisher(kind, d.docker, d.ecr, outputs, region)
	return pub.Authenticate(ctx)
}

func (d *RealDeployer) ImagePublish(ctx context.Context, kind cloud.Kind, localTag string, outputs map[string]string, stream io.Writer) error {
	pub := infra.NewPublisher(kind, d.docker, d.ecr, outputs, "")
	_, err := pub.Publish(ctx, localTag, stream)
	return err
}

func (d *RealDeployer) TerraformDestroy(ctx context.Context, workDir string, stream io.Writer) error {
	return d.tf.Destroy(ctx, workDir, stream)
}
