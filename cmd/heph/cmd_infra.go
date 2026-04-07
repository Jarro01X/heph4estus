package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"

	"heph4estus/internal/infra"
	"heph4estus/internal/logger"
)

// resolveToolConfig validates the backend flag and delegates to infra.ResolveToolConfig.
func resolveToolConfig(tool, backend string) (*infra.ToolConfig, error) {
	if backend == "dedicated" {
		return nil, fmt.Errorf("--backend dedicated is no longer supported; use --backend generic for %q", tool)
	}
	return infra.ResolveToolConfig(tool)
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
	backend := fs.String("backend", "generic", "Infrastructure backend (generic)")
	autoApprove := fs.Bool("auto-approve", false, "Skip interactive approval prompt")
	region := fs.String("region", "", "AWS region (default: from AWS_REGION or us-east-1)")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tool == "" {
		return fmt.Errorf("--tool flag is required")
	}
	if *backend != "generic" {
		return fmt.Errorf("--backend must be generic (got %q)", *backend)
	}

	cfg, err := resolveToolConfig(*tool, *backend)
	if err != nil {
		return err
	}

	if *region == "" {
		*region = infra.AWSRegion()
	}

	_, err = infra.RunDeploy(mainContext(), infra.DeployOpts{
		ToolConfig:  cfg,
		Region:      *region,
		AutoApprove: *autoApprove,
		Stream:      os.Stderr,
		PromptFunc:  deployPrompt,
	}, log)
	return err
}

func runInfraDestroy(args []string, log logger.Logger) error {
	fs := flag.NewFlagSet("infra destroy", flag.ContinueOnError)
	tool := fs.String("tool", "", "Tool whose infrastructure to destroy")
	backend := fs.String("backend", "generic", "Infrastructure backend (generic)")
	autoApprove := fs.Bool("auto-approve", false, "Skip interactive approval prompt")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tool == "" {
		return fmt.Errorf("--tool flag is required")
	}
	if *backend != "generic" {
		return fmt.Errorf("--backend must be generic (got %q)", *backend)
	}

	cfg, err := resolveToolConfig(*tool, *backend)
	if err != nil {
		return err
	}

	if !*autoApprove {
		if !cliPrompt("Destroy all infrastructure? This cannot be undone.") {
			fmt.Fprintln(os.Stderr, "Cancelled.")
			return nil
		}
	}

	return infra.RunDestroy(mainContext(), cfg, os.Stderr, log)
}

func deployPrompt(_ string) bool {
	return cliPrompt("Apply these changes?")
}

// cliPrompt asks the operator a yes/no question on stderr/stdin.
func cliPrompt(question string) bool {
	if strings.TrimSpace(question) == "" {
		question = "Apply these changes?"
	}
	fmt.Fprintf(os.Stderr, "\n%s [y/N]: ", question)
	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))
	return answer == "y" || answer == "yes"
}
