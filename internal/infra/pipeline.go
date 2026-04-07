package infra

import (
	"context"
	"fmt"
	"io"

	"heph4estus/internal/logger"
)

// DeployResult holds the outputs and metadata from a successful deploy.
type DeployResult struct {
	Outputs map[string]string
}

// DeployOpts configures a deploy pipeline run.
type DeployOpts struct {
	ToolConfig  *ToolConfig
	Region      string
	AutoApprove bool
	Stream      io.Writer // where to write progress (typically os.Stderr)

	// PromptFunc is called for interactive approval when AutoApprove is false.
	// It should return true to proceed or false to abort.
	// If nil and AutoApprove is false, deploy proceeds without prompting.
	PromptFunc func(summary string) bool
}

// RunDeploy executes the full deploy pipeline: terraform init/plan/apply,
// docker build, ECR auth, docker tag+push. Returns the terraform outputs.
func RunDeploy(ctx context.Context, opts DeployOpts, log logger.Logger) (*DeployResult, error) {
	cfg := opts.ToolConfig
	tf := NewTerraformClient(log)
	docker := NewDockerClient(log)
	ecr := NewECRClient(log)

	// 1. Terraform init
	fmt.Fprintln(opts.Stream, "==> Terraform init")
	if err := tf.Init(ctx, cfg.TerraformDir); err != nil {
		return nil, err
	}

	// 2. Terraform plan
	fmt.Fprintln(opts.Stream, "==> Terraform plan")
	summary, err := tf.Plan(ctx, cfg.TerraformDir, cfg.TerraformVars)
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(opts.Stream, "    %s\n", summary)

	// 3. Approval
	if !opts.AutoApprove && opts.PromptFunc != nil {
		if !opts.PromptFunc(summary) {
			return nil, fmt.Errorf("deploy cancelled by operator")
		}
	}

	// 4. Terraform apply
	fmt.Fprintln(opts.Stream, "==> Terraform apply")
	if err := tf.Apply(ctx, cfg.TerraformDir, cfg.TerraformVars, opts.Stream); err != nil {
		return nil, err
	}

	// 5. Read outputs
	fmt.Fprintln(opts.Stream, "==> Reading outputs")
	outputs, err := tf.ReadOutputs(ctx, cfg.TerraformDir)
	if err != nil {
		return nil, err
	}
	for k, v := range outputs {
		fmt.Fprintf(opts.Stream, "    %s = %s\n", k, v)
	}

	// 6. Docker build
	fmt.Fprintln(opts.Stream, "==> Docker build")
	if len(cfg.BuildArgs) > 0 {
		if err := docker.BuildWithArgs(ctx, cfg.Dockerfile, cfg.DockerCtx, cfg.DockerTag, cfg.BuildArgs, opts.Stream); err != nil {
			return nil, err
		}
	} else {
		if err := docker.Build(ctx, cfg.Dockerfile, cfg.DockerCtx, cfg.DockerTag, opts.Stream); err != nil {
			return nil, err
		}
	}

	// 7. ECR auth
	fmt.Fprintln(opts.Stream, "==> ECR authenticate")
	if err := ecr.Authenticate(ctx, opts.Region); err != nil {
		return nil, err
	}

	// 8. Docker tag + push
	ecrURL := outputs["ecr_repo_url"]
	if ecrURL == "" {
		return nil, fmt.Errorf("terraform output missing ecr_repo_url")
	}
	remoteTag := ecrURL + ":latest"

	fmt.Fprintln(opts.Stream, "==> Docker push")
	if err := docker.Tag(ctx, cfg.DockerTag, remoteTag); err != nil {
		return nil, err
	}
	if err := docker.Push(ctx, remoteTag, opts.Stream); err != nil {
		return nil, err
	}

	fmt.Fprintln(opts.Stream, "==> Infrastructure deployed successfully")
	return &DeployResult{Outputs: outputs}, nil
}

// RunDestroy executes a terraform destroy for the given tool config.
func RunDestroy(ctx context.Context, cfg *ToolConfig, stream io.Writer, log logger.Logger) error {
	tf := NewTerraformClient(log)

	fmt.Fprintln(stream, "==> Terraform destroy")
	fmt.Fprintln(stream, "    Note: Empty the S3 bucket first if destroy fails.")
	if err := tf.Destroy(ctx, cfg.TerraformDir, stream); err != nil {
		return err
	}

	fmt.Fprintln(stream, "==> Infrastructure destroyed")
	return nil
}

// EnsureInfra runs the lifecycle check and, if needed, deploys infrastructure.
// Returns the terraform outputs ready for use by scan commands.
func EnsureInfra(ctx context.Context, cfg *ToolConfig, policy LifecyclePolicy, region string, stream io.Writer, promptFunc func(string) bool, log logger.Logger) (map[string]string, error) {
	tf := NewTerraformClient(log)

	// Probe current state.
	probe := Probe(ctx, tf, cfg.TerraformDir, cfg.ToolName)
	decision := Decide(probe, policy)

	fmt.Fprintf(stream, "==> Lifecycle: %s\n", decision.Message)

	switch decision.Decision {
	case DecisionReuse:
		return probe.Outputs, nil

	case DecisionBlock:
		return nil, fmt.Errorf("lifecycle blocked: %s", decision.Message)

	case DecisionDeploy:
		result, err := RunDeploy(ctx, DeployOpts{
			ToolConfig:  cfg,
			Region:      region,
			AutoApprove: policy.AutoApprove,
			Stream:      stream,
			PromptFunc:  promptFunc,
		}, log)
		if err != nil {
			return nil, err
		}
		return result.Outputs, nil

	default:
		return nil, fmt.Errorf("unexpected lifecycle decision: %s", decision.Decision)
	}
}
