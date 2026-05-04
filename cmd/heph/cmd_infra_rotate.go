package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"heph4estus/internal/cloud"
	"heph4estus/internal/doctor"
	"heph4estus/internal/fleet"
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
	autoApprove := fs.Bool("auto-approve", false, "Skip interactive approval prompt")
	sshUser := fs.String("ssh-user", "root", "SSH user for controller credential updates")
	sshKey := fs.String("ssh-key", "", "SSH private key for controller credential updates (default: env or ~/.ssh)")
	sshPort := fs.Int("ssh-port", 22, "SSH port for controller credential updates")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tool == "" {
		return fmt.Errorf("--tool flag is required")
	}
	if strings.TrimSpace(*component) == "" {
		return fmt.Errorf("--component flag is required (nats, minio, registry, or all)")
	}
	components, err := infra.ParseCredentialRotationComponents(*component)
	if err != nil {
		return err
	}
	if !*dryRun && len(components) != 1 {
		return fmt.Errorf("credential rotation mutation currently supports one component at a time; rerun --component all with --dry-run")
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
	if !*dryRun {
		opts := rotationMutationOpts{
			ToolConfig:  cfg,
			Cloud:       cloudKind,
			Probe:       probe,
			AutoApprove: *autoApprove,
			SSHUser:     *sshUser,
			SSHKey:      *sshKey,
			SSHPort:     *sshPort,
			Log:         log,
		}
		switch components[0] {
		case infra.CredentialComponentNATS:
			return runNATSCredentialRotationMutation(opts)
		case infra.CredentialComponentMinIO:
			return runMinIOCredentialRotationMutation(opts)
		default:
			return fmt.Errorf("credential rotation mutation currently supports --component nats or minio; rerun other components with --dry-run")
		}
	}
	return outputCredentialRotationPlanText(os.Stdout, plan)
}

type rotationMutationOpts struct {
	ToolConfig  *infra.ToolConfig
	Cloud       cloud.Kind
	Probe       infra.ProbeResult
	AutoApprove bool
	SSHUser     string
	SSHKey      string
	SSHPort     int
	Log         logger.Logger
}

type preparedRotationMutation struct {
	Outputs        map[string]string
	WorkerCount    int
	ControllerHost string
	Runner         infra.SSHRemoteRunner
}

func prepareRotationMutation(opts rotationMutationOpts) (*preparedRotationMutation, error) {
	outputs := opts.Probe.Outputs
	workerCount, err := parseRotationWorkerCount(outputs)
	if err != nil {
		return nil, err
	}
	controllerHost := strings.TrimSpace(outputs["controller_ip"])
	if controllerHost == "" {
		return nil, fmt.Errorf("credential rotation requires controller_ip output")
	}
	sshKey := strings.TrimSpace(opts.SSHKey)
	if sshKey == "" {
		sshKey = infra.DefaultSSHPrivateKeyPath()
	}
	if sshKey == "" {
		return nil, fmt.Errorf("credential rotation requires an SSH private key; set --ssh-key, HEPH_SSH_PRIVATE_KEY_PATH, SSH_PRIVATE_KEY_PATH, or SELFHOSTED_SSH_KEY_PATH")
	}
	runner := infra.SSHRemoteRunner{
		User:    strings.TrimSpace(opts.SSHUser),
		KeyPath: sshKey,
		Port:    opts.SSHPort,
	}
	return &preparedRotationMutation{
		Outputs:        outputs,
		WorkerCount:    workerCount,
		ControllerHost: controllerHost,
		Runner:         runner,
	}, nil
}

func runNATSCredentialRotationMutation(opts rotationMutationOpts) error {
	if opts.ToolConfig == nil {
		return fmt.Errorf("tool config is required")
	}
	prepared, err := prepareRotationMutation(opts)
	if err != nil {
		return err
	}
	if !opts.AutoApprove {
		question := fmt.Sprintf("Rotate NATS credentials for %s/%s and replace %d workers?", opts.Cloud.Canonical(), opts.ToolConfig.ToolName, prepared.WorkerCount)
		if !cliPrompt(question) {
			fmt.Fprintln(os.Stderr, "Cancelled.")
			return nil
		}
	}

	creds, err := infra.GenerateNATSCredentials(time.Now().UTC())
	if err != nil {
		return err
	}
	update := infra.NATSControllerAuthUpdate{
		Credentials: creds,
		TLSEnabled:  rotationOutputBool(prepared.Outputs["nats_tls_enabled"]),
	}

	fmt.Fprintf(os.Stdout, "Rotating NATS credentials for %s/%s\n", opts.Cloud.Canonical(), opts.ToolConfig.ToolName)
	fmt.Fprintln(os.Stdout, "Updating controller NATS auth with grace credentials...")
	update.Mode = infra.NATSAuthUpdateGrace
	if err := infra.UpdateControllerNATSAuth(mainContext(), prepared.Runner, prepared.ControllerHost, update); err != nil {
		return fmt.Errorf("adding grace NATS credentials on controller: %w", err)
	}

	vars := infra.NATSTerraformVars(creds)
	if _, err := infra.MergeRotationAutoVars(opts.ToolConfig.TerraformDir, vars); err != nil {
		return fmt.Errorf("persisting rotated NATS Terraform vars: %w", err)
	}

	fmt.Fprintf(os.Stdout, "Replacing %d workers with rotated NATS credentials...\n", prepared.WorkerCount)
	if err := replaceWorkerIndexes(mainContext(), opts.ToolConfig, opts.Cloud, allWorkerIndexes(prepared.WorkerCount), vars, opts.Log); err != nil {
		return fmt.Errorf("replacing workers with rotated NATS credentials: %w", err)
	}

	tf := infra.NewTerraformClient(opts.Log)
	rotatedOutputs, err := tf.ReadOutputs(mainContext(), opts.ToolConfig.TerraformDir)
	if err != nil {
		return fmt.Errorf("reading rotated Terraform outputs: %w", err)
	}

	fmt.Fprintln(os.Stdout, "Verifying rotated workers can heartbeat...")
	if _, err := waitForProviderNativeFleet(mainContext(), opts.Cloud, rotatedOutputs, fleet.PlacementPolicy{}); err != nil {
		return fmt.Errorf("verifying rotated NATS credentials: %w", err)
	}

	fmt.Fprintln(os.Stdout, "Removing previous NATS users from controller auth...")
	update.Mode = infra.NATSAuthUpdateFinal
	if err := infra.UpdateControllerNATSAuth(mainContext(), prepared.Runner, prepared.ControllerHost, update); err != nil {
		return fmt.Errorf("finalizing NATS credentials on controller: %w", err)
	}

	fmt.Fprintln(os.Stdout, "Verifying final NATS auth after cleanup...")
	if _, err := waitForProviderNativeFleet(mainContext(), opts.Cloud, rotatedOutputs, fleet.PlacementPolicy{}); err != nil {
		return fmt.Errorf("verifying finalized NATS credentials: %w", err)
	}

	if _, err := fmt.Fprintf(os.Stdout, "Rotated NATS credentials. Generation: %s\n", creds.Generation); err != nil {
		return err
	}
	return nil
}

func runMinIOCredentialRotationMutation(opts rotationMutationOpts) error {
	if opts.ToolConfig == nil {
		return fmt.Errorf("tool config is required")
	}
	prepared, err := prepareRotationMutation(opts)
	if err != nil {
		return err
	}
	if !opts.AutoApprove {
		question := fmt.Sprintf("Rotate MinIO credentials for %s/%s and replace %d workers?", opts.Cloud.Canonical(), opts.ToolConfig.ToolName, prepared.WorkerCount)
		if !cliPrompt(question) {
			fmt.Fprintln(os.Stderr, "Cancelled.")
			return nil
		}
	}

	priorVars, err := infra.ReadRotationAutoVars(opts.ToolConfig.TerraformDir)
	if err != nil {
		return fmt.Errorf("reading previous rotation vars: %w", err)
	}
	creds, err := infra.GenerateMinIOCredentials(time.Now().UTC())
	if err != nil {
		return err
	}
	update := infra.MinIOControllerAuthUpdate{
		Credentials:       creds,
		TLSEnabled:        rotationOutputBool(prepared.Outputs["minio_tls_enabled"]),
		Bucket:            prepared.Outputs["s3_bucket_name"],
		Endpoint:          prepared.Outputs["s3_endpoint"],
		OldOperatorKey:    prepared.Outputs["s3_operator_access_key"],
		PreviousWorkerKey: priorVars["minio_worker_access_key_override"],
	}

	fmt.Fprintf(os.Stdout, "Rotating MinIO credentials for %s/%s\n", opts.Cloud.Canonical(), opts.ToolConfig.ToolName)
	fmt.Fprintln(os.Stdout, "Adding rotated MinIO users and verifying grace access...")
	update.Mode = infra.MinIOAuthUpdateGrace
	if err := infra.UpdateControllerMinIOAuth(mainContext(), prepared.Runner, prepared.ControllerHost, update); err != nil {
		return fmt.Errorf("adding grace MinIO credentials on controller: %w", err)
	}

	vars := infra.MinIOTerraformVars(creds)
	if _, err := infra.MergeRotationAutoVars(opts.ToolConfig.TerraformDir, vars); err != nil {
		return fmt.Errorf("persisting rotated MinIO Terraform vars: %w", err)
	}

	fmt.Fprintf(os.Stdout, "Replacing %d workers with rotated MinIO credentials...\n", prepared.WorkerCount)
	if err := replaceWorkerIndexes(mainContext(), opts.ToolConfig, opts.Cloud, allWorkerIndexes(prepared.WorkerCount), vars, opts.Log); err != nil {
		return fmt.Errorf("replacing workers with rotated MinIO credentials: %w", err)
	}

	tf := infra.NewTerraformClient(opts.Log)
	rotatedOutputs, err := tf.ReadOutputs(mainContext(), opts.ToolConfig.TerraformDir)
	if err != nil {
		return fmt.Errorf("reading rotated Terraform outputs: %w", err)
	}

	fmt.Fprintln(os.Stdout, "Verifying rotated workers can heartbeat...")
	if _, err := waitForProviderNativeFleet(mainContext(), opts.Cloud, rotatedOutputs, fleet.PlacementPolicy{}); err != nil {
		return fmt.Errorf("verifying rotated MinIO worker rollout: %w", err)
	}

	fmt.Fprintln(os.Stdout, "Removing previous rotated MinIO users from controller auth...")
	update.Mode = infra.MinIOAuthUpdateFinal
	if err := infra.UpdateControllerMinIOAuth(mainContext(), prepared.Runner, prepared.ControllerHost, update); err != nil {
		return fmt.Errorf("finalizing MinIO credentials on controller: %w", err)
	}

	if _, err := fmt.Fprintf(os.Stdout, "Rotated MinIO credentials. Generation: %s\n", creds.Generation); err != nil {
		return err
	}
	return nil
}

func parseRotationWorkerCount(outputs map[string]string) (int, error) {
	raw := strings.TrimSpace(outputs["worker_count"])
	if raw == "" {
		return 0, fmt.Errorf("credential rotation requires worker_count output")
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("credential rotation requires positive worker_count output, got %q", raw)
	}
	return n, nil
}

func allWorkerIndexes(count int) []int {
	indexes := make([]int, count)
	for i := range indexes {
		indexes[i] = i
	}
	return indexes
}

func rotationOutputBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1", "yes", "y":
		return true
	default:
		return false
	}
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
	if _, err := fmt.Fprintf(w, "NATS creds:  %s\n", plan.NATSCredentialGeneration); err != nil {
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
