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
	if err := writeLine(opts.Stream, "==> Terraform init"); err != nil {
		return nil, err
	}
	if err := tf.Init(ctx, cfg.TerraformDir); err != nil {
		return nil, err
	}

	// 2. Terraform plan
	if err := writeLine(opts.Stream, "==> Terraform plan"); err != nil {
		return nil, err
	}
	summary, err := tf.Plan(ctx, cfg.TerraformDir, cfg.TerraformVars)
	if err != nil {
		return nil, err
	}
	if err := writef(opts.Stream, "    %s\n", summary); err != nil {
		return nil, err
	}

	// 3. Approval
	if !opts.AutoApprove && opts.PromptFunc != nil {
		if !opts.PromptFunc(summary) {
			return nil, fmt.Errorf("deploy cancelled by operator")
		}
	}

	// 4. Terraform apply
	if err := writeLine(opts.Stream, "==> Terraform apply"); err != nil {
		return nil, err
	}
	if err := tf.Apply(ctx, cfg.TerraformDir, cfg.TerraformVars, opts.Stream); err != nil {
		return nil, err
	}

	// 5. Read outputs
	if err := writeLine(opts.Stream, "==> Reading outputs"); err != nil {
		return nil, err
	}
	outputs, err := tf.ReadOutputs(ctx, cfg.TerraformDir)
	if err != nil {
		return nil, err
	}
	for k, v := range outputs {
		if err := writef(opts.Stream, "    %s = %s\n", k, v); err != nil {
			return nil, err
		}
	}

	// 6. Docker build
	if err := writeLine(opts.Stream, "==> Docker build"); err != nil {
		return nil, err
	}
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
	if err := writeLine(opts.Stream, "==> ECR authenticate"); err != nil {
		return nil, err
	}
	if err := ecr.Authenticate(ctx, opts.Region); err != nil {
		return nil, err
	}

	// 8. Docker tag + push
	ecrURL := outputs["ecr_repo_url"]
	if ecrURL == "" {
		return nil, fmt.Errorf("terraform output missing ecr_repo_url")
	}
	remoteTag := ecrURL + ":latest"

	if err := writeLine(opts.Stream, "==> Docker push"); err != nil {
		return nil, err
	}
	if err := docker.Tag(ctx, cfg.DockerTag, remoteTag); err != nil {
		return nil, err
	}
	if err := docker.Push(ctx, remoteTag, opts.Stream); err != nil {
		return nil, err
	}

	if err := writeLine(opts.Stream, "==> Infrastructure deployed successfully"); err != nil {
		return nil, err
	}
	return &DeployResult{Outputs: outputs}, nil
}

// RunDestroy executes a terraform destroy for the given tool config.
func RunDestroy(ctx context.Context, cfg *ToolConfig, stream io.Writer, log logger.Logger) error {
	tf := NewTerraformClient(log)

	if err := writeLine(stream, "==> Terraform destroy"); err != nil {
		return err
	}
	if err := writeLine(stream, "    Note: Empty the S3 bucket first if destroy fails."); err != nil {
		return err
	}
	if err := tf.Destroy(ctx, cfg.TerraformDir, stream); err != nil {
		return err
	}

	if err := writeLine(stream, "==> Infrastructure destroyed"); err != nil {
		return err
	}
	return nil
}

// EnsureInfra runs the lifecycle check and, if needed, deploys infrastructure.
// Returns the terraform outputs ready for use by scan commands.
func EnsureInfra(ctx context.Context, cfg *ToolConfig, policy LifecyclePolicy, region string, stream io.Writer, promptFunc func(string) bool, log logger.Logger) (map[string]string, error) {
	tf := NewTerraformClient(log)

	// Probe current state.
	probe := Probe(ctx, tf, cfg.TerraformDir, cfg.ToolName)
	decision := Decide(probe, policy)

	if err := writef(stream, "==> Lifecycle: %s\n", decision.Message); err != nil {
		return nil, err
	}

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

func writeLine(w io.Writer, line string) error {
	if w == nil {
		return nil
	}
	_, err := fmt.Fprintln(w, line)
	return err
}

func writef(w io.Writer, format string, args ...interface{}) error {
	if w == nil {
		return nil
	}
	_, err := fmt.Fprintf(w, format, args...)
	return err
}
