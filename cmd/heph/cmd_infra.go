package main

import (
	"bufio"
	"encoding/json"
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
	if len(args) > 0 && args[0] == "inspect" {
		return runInfraBackupInspect(args[1:], log)
	}

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
	manifest := fleetstate.BuildRecoveryManifest(*tool, string(cloudKind.Canonical()), fctx.Outputs, fctx.Rollout, fctx.Reputation)
	if err := fleetstate.WriteRecoveryManifest(*outputPath, manifest); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(os.Stdout, "Wrote recovery manifest to %s\n", *outputPath); err != nil {
		return err
	}
	return nil
}

func runInfraBackupInspect(args []string, log logger.Logger) error {
	fs := flag.NewFlagSet("infra backup inspect", flag.ContinueOnError)
	inputPath := fs.String("from", "", "Path to a recovery manifest (required)")
	format := fs.String("format", "text", "Output format: text or json")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*inputPath) == "" {
		return fmt.Errorf("--from flag is required")
	}
	manifest, err := fleetstate.ReadRecoveryManifest(*inputPath)
	if err != nil {
		return err
	}
	if *format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(manifest)
	}
	return outputRecoveryManifestText(manifest)
}

func runInfraRecover(args []string, log logger.Logger) error {
	fs := flag.NewFlagSet("infra recover", flag.ContinueOnError)
	tool := fs.String("tool", "", "Tool whose provider-native infrastructure should be recovered")
	cloudFlag := fs.String("cloud", "", "Cloud provider: "+cloud.SupportedKindsText()+" (required)")
	inputPath := fs.String("from", "", "Path to a recovery manifest created by infra backup (required)")
	autoApprove := fs.Bool("auto-approve", false, "Skip interactive approval prompt")
	dryRun := fs.Bool("dry-run", false, "Show the recovery plan without deploying or restoring state")
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
	if err := manifest.Validate(*tool, string(cloudKind.Canonical())); err != nil {
		return err
	}
	cfg, err := infra.ResolveToolConfig(*tool, cloudKind)
	if err != nil {
		return err
	}
	tf := infra.NewTerraformClient(log)
	probe := infra.Probe(mainContext(), tf, cloudKind, cfg.TerraformDir, *tool)
	shouldDeploy, action, reason, err := planRecoveryAction(manifest, probe, *autoApprove)
	if err != nil {
		return err
	}
	if *dryRun {
		return outputRecoveryPlanText(manifest, probe, action, reason, shouldDeploy)
	}
	if shouldDeploy {
		if _, err := infra.RunDeploy(mainContext(), infra.DeployOpts{
			ToolConfig:  cfg,
			Cloud:       cloudKind,
			AutoApprove: *autoApprove,
			Stream:      os.Stderr,
			PromptFunc:  deployPrompt,
		}, log); err != nil {
			return err
		}
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
	mode := "reused"
	if shouldDeploy {
		mode = "redeployed"
	}
	if _, err := fmt.Fprintf(os.Stdout, "Recovered %s infrastructure for %s from %s (%s)\n", cloudKind.Canonical(), *tool, *inputPath, mode); err != nil {
		return err
	}
	return nil
}

func planRecoveryAction(manifest *fleetstate.RecoveryManifest, probe infra.ProbeResult, autoApprove bool) (bool, string, string, error) {
	if probe.Status == infra.StatusReady {
		if mismatch := recoveryProbeMismatch(manifest, probe.Outputs); mismatch != "" {
			return true, "redeploy infrastructure and restore local recovery state", mismatch, nil
		}
		return false, "reuse current infrastructure and restore local recovery state", "", nil
	}
	decision := infra.Decide(probe, infra.LifecyclePolicy{AutoApprove: autoApprove})
	if decision.Decision == infra.DecisionBlock {
		return false, "", "", fmt.Errorf("recovery blocked: %s", decision.Message)
	}
	return decision.Decision == infra.DecisionDeploy, decision.Message, "", nil
}

func recoveryProbeMismatch(manifest *fleetstate.RecoveryManifest, outputs map[string]string) string {
	if manifest == nil || outputs == nil {
		return ""
	}
	if manifest.ControllerGeneration != "" && outputs["generation_id"] != "" && outputs["generation_id"] != manifest.ControllerGeneration {
		return fmt.Sprintf("generation mismatch: current=%s backup=%s", outputs["generation_id"], manifest.ControllerGeneration)
	}
	if manifest.WorkerCount > 0 && outputs["worker_count"] != "" && outputs["worker_count"] != fmt.Sprint(manifest.WorkerCount) {
		return fmt.Sprintf("worker count mismatch: current=%s backup=%d", outputs["worker_count"], manifest.WorkerCount)
	}
	return ""
}

func outputRecoveryManifestText(manifest *fleetstate.RecoveryManifest) error {
	for _, line := range manifest.SummaryLines() {
		if _, err := fmt.Fprintln(os.Stdout, line); err != nil {
			return err
		}
	}
	return nil
}

func outputRecoveryPlanText(manifest *fleetstate.RecoveryManifest, probe infra.ProbeResult, action, reason string, shouldDeploy bool) error {
	for _, line := range manifest.SummaryLines() {
		if _, err := fmt.Fprintln(os.Stdout, line); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(os.Stdout, "Current:     %s\n", probe.Status); err != nil {
		return err
	}
	if probe.Outputs != nil {
		if generation := probe.Outputs["generation_id"]; generation != "" {
			if _, err := fmt.Fprintf(os.Stdout, "Current Gen: %s\n", generation); err != nil {
				return err
			}
		}
		if workers := probe.Outputs["worker_count"]; workers != "" {
			if _, err := fmt.Fprintf(os.Stdout, "Current Workers: %s\n", workers); err != nil {
				return err
			}
		}
	}
	if _, err := fmt.Fprintf(os.Stdout, "Action:      %s\n", action); err != nil {
		return err
	}
	if reason != "" {
		if _, err := fmt.Fprintf(os.Stdout, "Reason:      %s\n", reason); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(os.Stdout, "Deploy:      %t\n", shouldDeploy); err != nil {
		return err
	}
	return nil
}

func deployPrompt(_ string) bool {
	return cliPrompt("Apply these changes?")
}

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
