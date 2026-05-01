package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"

	"heph4estus/internal/cloud"
	"heph4estus/internal/fleet"
	"heph4estus/internal/fleetstate"
	"heph4estus/internal/infra"
	"heph4estus/internal/logger"
	"heph4estus/internal/operator"
)

// resolveToolConfig validates the backend flag and delegates to infra.ResolveToolConfig.
func resolveToolConfig(tool, backend string, kind ...cloud.Kind) (*infra.ToolConfig, error) {
	if backend == "dedicated" {
		return nil, fmt.Errorf("--backend dedicated is no longer supported; use --backend generic for %q", tool)
	}
	return infra.ResolveToolConfig(tool, kind...)
}

func runInfra(args []string, log logger.Logger) error {
	if len(args) == 0 {
		return fmt.Errorf("infra requires a subcommand: deploy, destroy, backup, recover")
	}

	sub := args[0]
	switch sub {
	case "deploy":
		return runInfraDeploy(args[1:], log)
	case "destroy":
		return runInfraDestroy(args[1:], log)
	case "backup":
		return runInfraBackup(args[1:], log)
	case "recover":
		return runInfraRecover(args[1:], log)
	default:
		return fmt.Errorf("infra: unknown subcommand %q (expected deploy, destroy, backup, or recover)", sub)
	}
}

func runInfraDeploy(args []string, log logger.Logger) error {
	fs := flag.NewFlagSet("infra deploy", flag.ContinueOnError)
	tool := fs.String("tool", "", "Tool to deploy infrastructure for (e.g. nmap)")
	backend := fs.String("backend", "generic", "Infrastructure backend (generic)")
	autoApprove := fs.Bool("auto-approve", false, "Skip interactive approval prompt")
	region := fs.String("region", "", "AWS region (default: from AWS_REGION or us-east-1)")
	cloudFlag := fs.String("cloud", "", "Cloud provider: "+cloud.SupportedKindsText()+" (default: from config or aws)")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tool == "" {
		return fmt.Errorf("--tool flag is required")
	}
	if *backend != "generic" {
		return fmt.Errorf("--backend must be generic (got %q)", *backend)
	}

	// Resolve region from operator config when not explicitly set.
	opCfg, _ := operator.LoadConfig()
	*region = operator.ResolveRegion(*region, opCfg)

	cloudKind, err := resolveCLICloud(*cloudFlag, opCfg)
	if err != nil {
		return err
	}
	if err := requireDeploySupport(cloudKind); err != nil {
		return err
	}

	cfg, err := resolveToolConfig(*tool, *backend, cloudKind)
	if err != nil {
		return err
	}

	_, err = infra.RunDeploy(mainContext(), infra.DeployOpts{
		ToolConfig:  cfg,
		Cloud:       cloudKind,
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
	cloudFlag := fs.String("cloud", "", "Cloud provider: "+cloud.SupportedKindsText()+" (default: from config or aws)")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tool == "" {
		return fmt.Errorf("--tool flag is required")
	}
	if *backend != "generic" {
		return fmt.Errorf("--backend must be generic (got %q)", *backend)
	}

	opCfg, _ := operator.LoadConfig()
	cloudKind, err := resolveCLICloud(*cloudFlag, opCfg)
	if err != nil {
		return err
	}
	if err := requireDeploySupport(cloudKind); err != nil {
		return err
	}

	cfg, err := resolveToolConfig(*tool, *backend, cloudKind)
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

func runInfraBackup(args []string, log logger.Logger) error {
	fs := flag.NewFlagSet("infra backup", flag.ContinueOnError)
	tool := fs.String("tool", "", "Tool whose provider-native infrastructure should be backed up")
	cloudFlag := fs.String("cloud", "", "Cloud provider: "+cloud.SupportedKindsText()+" (required)")
	outputPath := fs.String("output", "", "Path to write the recovery manifest (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tool == "" {
		return fmt.Errorf("--tool flag is required")
	}
	if strings.TrimSpace(*outputPath) == "" {
		return fmt.Errorf("--output flag is required")
	}

	opCfg, _ := operator.LoadConfig()
	cloudKind, err := resolveCLICloud(*cloudFlag, opCfg)
	if err != nil {
		return err
	}
	if !cloudKind.IsProviderNative() {
		return fmt.Errorf("infra backup only supports provider-native clouds, got %q", cloudKind.Canonical())
	}

	fctx, err := loadProviderFleetContext(mainContext(), *tool, cloudKind, fleet.PlacementPolicy{}, true, log)
	if err != nil {
		return err
	}
	manifest := &fleetstate.RecoveryManifest{
		ToolName:   *tool,
		Cloud:      string(cloudKind.Canonical()),
		Outputs:    fctx.Outputs,
		Rollout:    fctx.Rollout,
		Reputation: fctx.Reputation,
	}
	if err := fleetstate.WriteRecoveryManifest(*outputPath, manifest); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(os.Stdout, "Wrote recovery manifest to %s\n", *outputPath); err != nil {
		return err
	}
	return nil
}

func runInfraRecover(args []string, log logger.Logger) error {
	fs := flag.NewFlagSet("infra recover", flag.ContinueOnError)
	tool := fs.String("tool", "", "Tool whose provider-native infrastructure should be recovered")
	cloudFlag := fs.String("cloud", "", "Cloud provider: "+cloud.SupportedKindsText()+" (required)")
	inputPath := fs.String("from", "", "Path to a recovery manifest created by infra backup (required)")
	autoApprove := fs.Bool("auto-approve", false, "Skip interactive approval prompt")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tool == "" {
		return fmt.Errorf("--tool flag is required")
	}
	if strings.TrimSpace(*inputPath) == "" {
		return fmt.Errorf("--from flag is required")
	}
	opCfg, _ := operator.LoadConfig()
	cloudKind, err := resolveCLICloud(*cloudFlag, opCfg)
	if err != nil {
		return err
	}
	if !cloudKind.IsProviderNative() {
		return fmt.Errorf("infra recover only supports provider-native clouds, got %q", cloudKind.Canonical())
	}
	manifest, err := fleetstate.ReadRecoveryManifest(*inputPath)
	if err != nil {
		return err
	}
	if manifest.ToolName != *tool {
		return fmt.Errorf("recovery manifest tool mismatch: %q != %q", manifest.ToolName, *tool)
	}
	if manifest.Cloud != "" && manifest.Cloud != string(cloudKind.Canonical()) {
		return fmt.Errorf("recovery manifest cloud mismatch: %q != %q", manifest.Cloud, cloudKind.Canonical())
	}
	cfg, err := infra.ResolveToolConfig(*tool, cloudKind)
	if err != nil {
		return err
	}
	_, err = infra.RunDeploy(mainContext(), infra.DeployOpts{
		ToolConfig:  cfg,
		Cloud:       cloudKind,
		AutoApprove: *autoApprove,
		Stream:      os.Stderr,
		PromptFunc:  deployPrompt,
	}, log)
	if err != nil {
		return err
	}
	repStore, err := fleetstate.NewReputationStore()
	if err != nil {
		return err
	}
	for _, rec := range manifest.Reputation {
		if err := repStore.Upsert(rec); err != nil {
			return err
		}
	}
	if manifest.Rollout != nil {
		rolloutStore, err := fleetstate.NewRolloutStore()
		if err != nil {
			return err
		}
		if err := rolloutStore.Save(manifest.Rollout); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(os.Stdout, "Recovered %s infrastructure for %s from %s\n", cloudKind.Canonical(), *tool, *inputPath); err != nil {
		return err
	}
	return nil
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
