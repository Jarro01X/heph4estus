package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"heph4estus/internal/infra"
	"heph4estus/internal/logger"
)

// toolPaths maps tool names to their infrastructure paths.
type toolPaths struct {
	TerraformDir  string
	Dockerfile    string
	DockerCtx     string
	DockerTag     string
	ECRRepoName   string
	BuildArgs     map[string]string // Docker --build-arg flags (nil for dedicated containers)
	TerraformVars map[string]string // Terraform -var flags (nil for dedicated infra)
}

func resolveToolPaths(tool, backend string) (*toolPaths, error) {
	switch tool {
	case "nmap":
		if backend == "generic" {
			return &toolPaths{
				TerraformDir: "deployments/aws/generic/environments/dev",
				Dockerfile:   "containers/generic/Dockerfile",
				DockerCtx:    ".",
				DockerTag:    "heph-nmap-worker:latest",
				ECRRepoName:  "heph-dev-nmap",
				BuildArgs: map[string]string{
					"RUNTIME_INSTALL_CMD": "apk add --no-cache nmap nmap-scripts",
				},
				TerraformVars: map[string]string{
					"tool_name": "nmap",
				},
			}, nil
		}
		return &toolPaths{
			TerraformDir: "deployments/aws/nmap/environments/dev",
			Dockerfile:   "containers/nmap/Dockerfile",
			DockerCtx:    ".",
			DockerTag:    "nmap-scanner:latest",
			ECRRepoName:  "nmap-scanner",
		}, nil
	default:
		return nil, fmt.Errorf("unknown tool: %q (supported: nmap)", tool)
	}
}

func runInfra(args []string, log logger.Logger) error {
	if len(args) == 0 {
		return fmt.Errorf("infra requires a subcommand: deploy, destroy")
	}

	sub := args[0]
	switch sub {
	case "deploy":
		return runInfraDeploy(args[1:], log)
	case "destroy":
		return runInfraDestroy(args[1:], log)
	default:
		return fmt.Errorf("infra: unknown subcommand %q (expected deploy or destroy)", sub)
	}
}

func runInfraDeploy(args []string, log logger.Logger) error {
	fs := flag.NewFlagSet("infra deploy", flag.ContinueOnError)
	tool := fs.String("tool", "", "Tool to deploy infrastructure for (e.g. nmap)")
	backend := fs.String("backend", "dedicated", "Infrastructure backend: dedicated or generic")
	autoApprove := fs.Bool("auto-approve", false, "Skip interactive approval prompt")
	region := fs.String("region", "", "AWS region (default: from AWS_REGION or us-east-1)")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tool == "" {
		return fmt.Errorf("--tool flag is required")
	}
	if *backend != "dedicated" && *backend != "generic" {
		return fmt.Errorf("--backend must be dedicated or generic")
	}

	paths, err := resolveToolPaths(*tool, *backend)
	if err != nil {
		return err
	}

	if *region == "" {
		*region = awsRegion()
	}

	ctx := context.Background()
	tf := infra.NewTerraformClient(log)
	docker := infra.NewDockerClient(log)
	ecr := infra.NewECRClient(log)

	// 1. Terraform init
	fmt.Fprintln(os.Stderr, "==> Terraform init")
	if err := tf.Init(ctx, paths.TerraformDir); err != nil {
		return err
	}

	// 2. Terraform plan
	fmt.Fprintln(os.Stderr, "==> Terraform plan")
	summary, err := tf.Plan(ctx, paths.TerraformDir, paths.TerraformVars)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "    %s\n", summary)

	// 3. Approval
	if !*autoApprove {
		fmt.Fprint(os.Stderr, "\nApply these changes? [y/N]: ")
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			fmt.Fprintln(os.Stderr, "Cancelled.")
			return nil
		}
	}

	// 4. Terraform apply
	fmt.Fprintln(os.Stderr, "==> Terraform apply")
	if err := tf.Apply(ctx, paths.TerraformDir, paths.TerraformVars, os.Stderr); err != nil {
		return err
	}

	// 5. Read outputs
	fmt.Fprintln(os.Stderr, "==> Reading outputs")
	outputs, err := tf.ReadOutputs(ctx, paths.TerraformDir)
	if err != nil {
		return err
	}
	for k, v := range outputs {
		fmt.Fprintf(os.Stderr, "    %s = %s\n", k, v)
	}

	// 6. Docker build
	fmt.Fprintln(os.Stderr, "==> Docker build")
	if len(paths.BuildArgs) > 0 {
		if err := docker.BuildWithArgs(ctx, paths.Dockerfile, paths.DockerCtx, paths.DockerTag, paths.BuildArgs, os.Stderr); err != nil {
			return err
		}
	} else {
		if err := docker.Build(ctx, paths.Dockerfile, paths.DockerCtx, paths.DockerTag, os.Stderr); err != nil {
			return err
		}
	}

	// 7. ECR auth
	fmt.Fprintln(os.Stderr, "==> ECR authenticate")
	if err := ecr.Authenticate(ctx, *region); err != nil {
		return err
	}

	// 8. Docker tag + push
	ecrURL := outputs["ecr_repo_url"]
	if ecrURL == "" {
		return fmt.Errorf("terraform output missing ecr_repo_url")
	}
	remoteTag := ecrURL + ":latest"

	fmt.Fprintln(os.Stderr, "==> Docker push")
	if err := docker.Tag(ctx, paths.DockerTag, remoteTag); err != nil {
		return err
	}
	if err := docker.Push(ctx, remoteTag, os.Stderr); err != nil {
		return err
	}

	fmt.Fprintln(os.Stderr, "==> Infrastructure deployed successfully")
	return nil
}

func runInfraDestroy(args []string, log logger.Logger) error {
	fs := flag.NewFlagSet("infra destroy", flag.ContinueOnError)
	tool := fs.String("tool", "", "Tool whose infrastructure to destroy")
	backend := fs.String("backend", "dedicated", "Infrastructure backend: dedicated or generic")
	autoApprove := fs.Bool("auto-approve", false, "Skip interactive approval prompt")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tool == "" {
		return fmt.Errorf("--tool flag is required")
	}

	paths, err := resolveToolPaths(*tool, *backend)
	if err != nil {
		return err
	}

	if !*autoApprove {
		fmt.Fprint(os.Stderr, "Destroy all infrastructure? This cannot be undone. [y/N]: ")
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			fmt.Fprintln(os.Stderr, "Cancelled.")
			return nil
		}
	}

	ctx := context.Background()
	tf := infra.NewTerraformClient(log)

	fmt.Fprintln(os.Stderr, "==> Terraform destroy")
	fmt.Fprintln(os.Stderr, "    Note: Empty the S3 bucket first if destroy fails.")
	if err := tf.Destroy(ctx, paths.TerraformDir, os.Stderr); err != nil {
		return err
	}

	fmt.Fprintln(os.Stderr, "==> Infrastructure destroyed")
	return nil
}

func awsRegion() string {
	if r := os.Getenv("AWS_REGION"); r != "" {
		return r
	}
	if r := os.Getenv("AWS_DEFAULT_REGION"); r != "" {
		return r
	}
	return "us-east-1"
}
