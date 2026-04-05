package deploy

import (
	"context"
	"io"

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
	ECRAuthenticate(ctx context.Context, region string) error
	DockerTag(ctx context.Context, source, target string) error
	DockerPush(ctx context.Context, tag string, stream io.Writer) error
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

func (d *RealDeployer) ECRAuthenticate(ctx context.Context, region string) error {
	return d.ecr.Authenticate(ctx, region)
}

func (d *RealDeployer) DockerTag(ctx context.Context, source, target string) error {
	return d.docker.Tag(ctx, source, target)
}

func (d *RealDeployer) DockerPush(ctx context.Context, tag string, stream io.Writer) error {
	return d.docker.Push(ctx, tag, stream)
}

func (d *RealDeployer) TerraformDestroy(ctx context.Context, workDir string, stream io.Writer) error {
	return d.tf.Destroy(ctx, workDir, stream)
}
