package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"heph4estus/internal/cloud"
	"heph4estus/internal/doctor"
	"heph4estus/internal/infra"
	"heph4estus/internal/logger"
	"heph4estus/internal/operator"
)

func runInfraRotate(args []string, log logger.Logger) error {
	if len(args) == 0 {
		return fmt.Errorf("infra rotate requires a subcommand: credentials")
	}
	sub := args[0]
	switch sub {
	case "credentials":
		return runInfraRotateCredentials(args[1:], log)
	default:
		return fmt.Errorf("infra rotate: unknown subcommand %q (expected credentials)", sub)
	}
}

func runInfraRotateCredentials(args []string, log logger.Logger) error {
	fs := flag.NewFlagSet("infra rotate credentials", flag.ContinueOnError)
	tool := fs.String("tool", "", "Tool whose provider-native credentials should be rotated")
	cloudFlag := fs.String("cloud", "", "Cloud provider: "+cloud.SupportedKindsText()+" (default: from config or aws)")
	component := fs.String("component", "", "Credential component: nats, minio, registry, or all")
	dryRun := fs.Bool("dry-run", false, "Show credential rotation preflight and blast radius without changing anything")
	autoApprove := fs.Bool("auto-approve", false, "Skip interactive approval prompt once mutation support is implemented")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tool == "" {
		return fmt.Errorf("--tool flag is required")
	}
	if strings.TrimSpace(*component) == "" {
		return fmt.Errorf("--component flag is required (nats, minio, registry, or all)")
	}
	if _, err := infra.ParseCredentialRotationComponents(*component); err != nil {
		return err
	}
	if !*dryRun {
		_ = autoApprove
		return fmt.Errorf("credential rotation mutation is not implemented yet; rerun with --dry-run")
	}

	opCfg, _ := operator.LoadConfig()
	cloudKind, err := resolveCLICloud(*cloudFlag, opCfg)
	if err != nil {
		return err
	}
	if !cloudKind.IsProviderNative() {
		return fmt.Errorf("infra rotate credentials only supports provider-native clouds, got %q", cloudKind.Canonical())
	}

	cfg, err := infra.ResolveToolConfig(*tool, cloudKind)
	if err != nil {
		return err
	}
	probe := infra.Probe(mainContext(), infra.NewTerraformClient(log), cloudKind, cfg.TerraformDir, *tool)
	plan, err := infra.PlanCredentialRotation(cloudKind, *tool, *component, probe)
	if err != nil {
		return err
	}
	if failures := failingProviderNativeOutputChecks(cloudKind, probe.Outputs); len(failures) > 0 {
		return fmt.Errorf("credential rotation preflight failed security posture checks: %s", strings.Join(failures, "; "))
	}
	return outputCredentialRotationPlanText(os.Stdout, plan)
}

func failingProviderNativeOutputChecks(kind cloud.Kind, outputs map[string]string) []string {
	checks := doctor.RunProviderNativeOutputChecks(kind, outputs)
	failures := make([]string, 0, len(checks))
	for _, check := range checks {
		if check.Status == doctor.StatusFail {
			failures = append(failures, fmt.Sprintf("%s: %s", check.Name, check.Summary))
		}
	}
	return failures
}

func outputCredentialRotationPlanText(w io.Writer, plan *infra.CredentialRotationPlan) error {
	if plan == nil {
		return fmt.Errorf("rotation plan is nil")
	}
	if _, err := fmt.Fprintln(w, "Credential rotation dry run"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Tool:        %s\n", plan.Tool); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Cloud:       %s\n", plan.Cloud); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Components:  %s\n", credentialComponentList(plan.Components)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Generation:  %s\n", plan.GenerationID); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Scope:       %s\n", plan.CredentialScopeVersion); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Mode:        %s\n", plan.ControllerSecurityMode); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "\nPreflight:"); err != nil {
		return err
	}
	for _, line := range []string{
		"Terraform outputs are present and match the requested tool/provider.",
		"Provider-native security posture has no failing output checks.",
		"No credentials will be changed in this dry run.",
	} {
		if _, err := fmt.Fprintf(w, "  - %s\n", line); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(w, "\nBlast radius:"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  - Controller services affected: %s\n", strings.Join(plan.ControllerServices, ", ")); err != nil {
		return err
	}
	workerAction := fmt.Sprintf("replace or restart %s workers", plan.WorkerCount)
	if !plan.WorkerRecycleRequired {
		workerAction = "not required"
	}
	if _, err := fmt.Fprintf(w, "  - Worker reconcile: %s\n", workerAction); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  - Operator-facing outputs refreshed: %s\n", strings.Join(plan.OperatorOutputKeys, ", ")); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "\nPlanned actions:"); err != nil {
		return err
	}
	for _, action := range plan.Actions {
		if _, err := fmt.Fprintf(w, "  - %s\n", action); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(w, "\nPost-rotation verification:"); err != nil {
		return err
	}
	for _, check := range plan.Verification {
		if _, err := fmt.Fprintf(w, "  - %s\n", check); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(w, "\nDry run: no Terraform apply, service restart, worker replacement, or credential write was performed."); err != nil {
		return err
	}
	return nil
}

func credentialComponentList(components []infra.CredentialRotationComponent) string {
	parts := make([]string, 0, len(components))
	for _, component := range components {
		parts = append(parts, string(component))
	}
	return strings.Join(parts, ", ")
}
