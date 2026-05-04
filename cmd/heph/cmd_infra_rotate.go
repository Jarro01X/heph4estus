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
		return fmt.Errorf("infra rotate requires a subcommand: credentials or certs")
	}
	sub := args[0]
	switch sub {
	case "credentials":
		return runInfraRotateCredentials(args[1:], log)
	case "certs":
		return runInfraRotateCerts(args[1:], log)
	default:
		return fmt.Errorf("infra rotate: unknown subcommand %q (expected credentials or certs)", sub)
	}
}

func runInfraRotateCerts(args []string, log logger.Logger) error {
	fs := flag.NewFlagSet("infra rotate certs", flag.ContinueOnError)
	tool := fs.String("tool", "", "Tool whose provider-native certificates should be rotated")
	cloudFlag := fs.String("cloud", "", "Cloud provider: "+cloud.SupportedKindsText()+" (default: from config or aws)")
	component := fs.String("component", "", "Certificate component: controller, worker, ca, or all")
	dryRun := fs.Bool("dry-run", false, "Show certificate rotation preflight and blast radius without changing anything")
	autoApprove := fs.Bool("auto-approve", false, "Skip interactive approval prompt")
	sshUser := fs.String("ssh-user", "root", "SSH user for controller certificate updates")
	sshKey := fs.String("ssh-key", "", "SSH private key for controller certificate updates (default: env or ~/.ssh)")
	sshPort := fs.Int("ssh-port", 22, "SSH port for controller certificate updates")
	trustDir := fs.String("trust-dir", "", "Directory for Heph's local controller CA trust cache during CA rotation")
	dockerCertsDir := fs.String("docker-certs-dir", "", "Docker registry certs directory during CA rotation (default: /etc/docker/certs.d or HEPH_DOCKER_CERTS_DIR)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tool == "" {
		return fmt.Errorf("--tool flag is required")
	}
	if strings.TrimSpace(*component) == "" {
		return fmt.Errorf("--component flag is required (controller, worker, ca, or all)")
	}
	components, err := infra.ParseCertificateRotationComponents(*component)
	if err != nil {
		return err
	}
	if !*dryRun && (len(components) != 1 || !supportedCertificateMutationComponent(components[0])) {
		return fmt.Errorf("certificate rotation mutation currently supports only --component controller or --component ca; rerun other components with --dry-run")
	}

	opCfg, _ := operator.LoadConfig()
	cloudKind, err := resolveCLICloud(*cloudFlag, opCfg)
	if err != nil {
		return err
	}
	if !cloudKind.IsProviderNative() {
		return fmt.Errorf("infra rotate certs only supports provider-native clouds, got %q", cloudKind.Canonical())
	}

	cfg, err := infra.ResolveToolConfig(*tool, cloudKind)
	if err != nil {
		return err
	}
	probe := infra.Probe(mainContext(), infra.NewTerraformClient(log), cloudKind, cfg.TerraformDir, *tool)
	plan, err := infra.PlanCertificateRotation(cloudKind, *tool, *component, probe)
	if err != nil {
		return err
	}
	if !*dryRun {
		if failures := failingProviderNativeOutputChecks(cloudKind, probe.Outputs); len(failures) > 0 {
			return fmt.Errorf("certificate rotation preflight failed security posture checks: %s", strings.Join(failures, "; "))
		}
		return runCertificateRotationMutation(rotationMutationOpts{
			ToolConfig:     cfg,
			Cloud:          cloudKind,
			Probe:          probe,
			AutoApprove:    *autoApprove,
			SSHUser:        *sshUser,
			SSHKey:         *sshKey,
			SSHPort:        *sshPort,
			TrustDir:       *trustDir,
			DockerCertsDir: *dockerCertsDir,
			Log:            log,
		}, components[0])
	}
	return outputCertificateRotationPlanText(os.Stdout, plan)
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
		case infra.CredentialComponentRegistry:
			return runRegistryCredentialRotationMutation(opts)
		default:
			return fmt.Errorf("unsupported credential rotation component %q", components[0])
		}
	}
	return outputCredentialRotationPlanText(os.Stdout, plan)
}

type rotationMutationOpts struct {
	ToolConfig     *infra.ToolConfig
	Cloud          cloud.Kind
	Probe          infra.ProbeResult
	AutoApprove    bool
	SSHUser        string
	SSHKey         string
	SSHPort        int
	TrustDir       string
	DockerCertsDir string
	Log            logger.Logger
}

func supportedCertificateMutationComponent(component infra.CertificateRotationComponent) bool {
	return component == infra.CertificateComponentController || component == infra.CertificateComponentCA
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
		return nil, fmt.Errorf("rotation requires controller_ip output")
	}
	sshKey := strings.TrimSpace(opts.SSHKey)
	if sshKey == "" {
		sshKey = infra.DefaultSSHPrivateKeyPath()
	}
	if sshKey == "" {
		return nil, fmt.Errorf("rotation requires an SSH private key; set --ssh-key, HEPH_SSH_PRIVATE_KEY_PATH, SSH_PRIVATE_KEY_PATH, or SELFHOSTED_SSH_KEY_PATH")
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
			return printStderrLine("Cancelled.")
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

	if err := printStdout("Rotating NATS credentials for %s/%s\n", opts.Cloud.Canonical(), opts.ToolConfig.ToolName); err != nil {
		return err
	}
	if err := printStdoutLine("Updating controller NATS auth with grace credentials..."); err != nil {
		return err
	}
	update.Mode = infra.NATSAuthUpdateGrace
	if err := infra.UpdateControllerNATSAuth(mainContext(), prepared.Runner, prepared.ControllerHost, update); err != nil {
		return fmt.Errorf("adding grace NATS credentials on controller: %w", err)
	}

	vars := infra.NATSTerraformVars(creds)
	if _, err := infra.MergeRotationAutoVars(opts.ToolConfig.TerraformDir, vars); err != nil {
		return fmt.Errorf("persisting rotated NATS Terraform vars: %w", err)
	}

	if err := printStdout("Replacing %d workers with rotated NATS credentials...\n", prepared.WorkerCount); err != nil {
		return err
	}
	if err := replaceWorkerIndexes(mainContext(), opts.ToolConfig, opts.Cloud, allWorkerIndexes(prepared.WorkerCount), vars, opts.Log); err != nil {
		return fmt.Errorf("replacing workers with rotated NATS credentials: %w", err)
	}

	tf := infra.NewTerraformClient(opts.Log)
	rotatedOutputs, err := tf.ReadOutputs(mainContext(), opts.ToolConfig.TerraformDir)
	if err != nil {
		return fmt.Errorf("reading rotated Terraform outputs: %w", err)
	}

	if err := printStdoutLine("Verifying rotated workers can heartbeat..."); err != nil {
		return err
	}
	if _, err := waitForProviderNativeFleet(mainContext(), opts.Cloud, rotatedOutputs, fleet.PlacementPolicy{}); err != nil {
		return fmt.Errorf("verifying rotated NATS credentials: %w", err)
	}

	if err := printStdoutLine("Removing previous NATS users from controller auth..."); err != nil {
		return err
	}
	update.Mode = infra.NATSAuthUpdateFinal
	if err := infra.UpdateControllerNATSAuth(mainContext(), prepared.Runner, prepared.ControllerHost, update); err != nil {
		return fmt.Errorf("finalizing NATS credentials on controller: %w", err)
	}

	if err := printStdoutLine("Verifying final NATS auth after cleanup..."); err != nil {
		return err
	}
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
			return printStderrLine("Cancelled.")
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

	if err := printStdout("Rotating MinIO credentials for %s/%s\n", opts.Cloud.Canonical(), opts.ToolConfig.ToolName); err != nil {
		return err
	}
	if err := printStdoutLine("Adding rotated MinIO users and verifying grace access..."); err != nil {
		return err
	}
	update.Mode = infra.MinIOAuthUpdateGrace
	if err := infra.UpdateControllerMinIOAuth(mainContext(), prepared.Runner, prepared.ControllerHost, update); err != nil {
		return fmt.Errorf("adding grace MinIO credentials on controller: %w", err)
	}

	vars := infra.MinIOTerraformVars(creds)
	if _, err := infra.MergeRotationAutoVars(opts.ToolConfig.TerraformDir, vars); err != nil {
		return fmt.Errorf("persisting rotated MinIO Terraform vars: %w", err)
	}

	if err := printStdout("Replacing %d workers with rotated MinIO credentials...\n", prepared.WorkerCount); err != nil {
		return err
	}
	if err := replaceWorkerIndexes(mainContext(), opts.ToolConfig, opts.Cloud, allWorkerIndexes(prepared.WorkerCount), vars, opts.Log); err != nil {
		return fmt.Errorf("replacing workers with rotated MinIO credentials: %w", err)
	}

	tf := infra.NewTerraformClient(opts.Log)
	rotatedOutputs, err := tf.ReadOutputs(mainContext(), opts.ToolConfig.TerraformDir)
	if err != nil {
		return fmt.Errorf("reading rotated Terraform outputs: %w", err)
	}

	if err := printStdoutLine("Verifying rotated workers can heartbeat..."); err != nil {
		return err
	}
	if _, err := waitForProviderNativeFleet(mainContext(), opts.Cloud, rotatedOutputs, fleet.PlacementPolicy{}); err != nil {
		return fmt.Errorf("verifying rotated MinIO worker rollout: %w", err)
	}

	if err := printStdoutLine("Removing previous rotated MinIO users from controller auth..."); err != nil {
		return err
	}
	update.Mode = infra.MinIOAuthUpdateFinal
	if err := infra.UpdateControllerMinIOAuth(mainContext(), prepared.Runner, prepared.ControllerHost, update); err != nil {
		return fmt.Errorf("finalizing MinIO credentials on controller: %w", err)
	}

	if _, err := fmt.Fprintf(os.Stdout, "Rotated MinIO credentials. Generation: %s\n", creds.Generation); err != nil {
		return err
	}
	return nil
}

func runRegistryCredentialRotationMutation(opts rotationMutationOpts) error {
	if opts.ToolConfig == nil {
		return fmt.Errorf("tool config is required")
	}
	prepared, err := prepareRotationMutation(opts)
	if err != nil {
		return err
	}
	if !opts.AutoApprove {
		question := fmt.Sprintf("Rotate registry credentials for %s/%s and replace %d workers?", opts.Cloud.Canonical(), opts.ToolConfig.ToolName, prepared.WorkerCount)
		if !cliPrompt(question) {
			return printStderrLine("Cancelled.")
		}
	}

	creds, err := infra.GenerateRegistryCredentials(time.Now().UTC())
	if err != nil {
		return err
	}
	update := infra.RegistryControllerAuthUpdate{
		Credentials: creds,
		TLSEnabled:  rotationOutputBool(prepared.Outputs["registry_tls_enabled"]),
		RegistryURL: prepared.Outputs["registry_url"],
	}

	if err := printStdout("Rotating registry credentials for %s/%s\n", opts.Cloud.Canonical(), opts.ToolConfig.ToolName); err != nil {
		return err
	}
	if err := printStdoutLine("Adding rotated registry users and verifying grace access..."); err != nil {
		return err
	}
	update.Mode = infra.RegistryAuthUpdateGrace
	if err := infra.UpdateControllerRegistryAuth(mainContext(), prepared.Runner, prepared.ControllerHost, update); err != nil {
		return fmt.Errorf("adding grace registry credentials on controller: %w", err)
	}

	vars := infra.RegistryTerraformVars(creds)
	if _, err := infra.MergeRotationAutoVars(opts.ToolConfig.TerraformDir, vars); err != nil {
		return fmt.Errorf("persisting rotated registry Terraform vars: %w", err)
	}

	if err := printStdout("Replacing %d workers with rotated registry credentials...\n", prepared.WorkerCount); err != nil {
		return err
	}
	if err := replaceWorkerIndexes(mainContext(), opts.ToolConfig, opts.Cloud, allWorkerIndexes(prepared.WorkerCount), vars, opts.Log); err != nil {
		return fmt.Errorf("replacing workers with rotated registry credentials: %w", err)
	}

	tf := infra.NewTerraformClient(opts.Log)
	rotatedOutputs, err := tf.ReadOutputs(mainContext(), opts.ToolConfig.TerraformDir)
	if err != nil {
		return fmt.Errorf("reading rotated Terraform outputs: %w", err)
	}

	if err := printStdoutLine("Verifying rotated workers can heartbeat..."); err != nil {
		return err
	}
	if _, err := waitForProviderNativeFleet(mainContext(), opts.Cloud, rotatedOutputs, fleet.PlacementPolicy{}); err != nil {
		return fmt.Errorf("verifying rotated registry worker rollout: %w", err)
	}

	if err := printStdoutLine("Removing previous registry users from controller auth..."); err != nil {
		return err
	}
	update.Mode = infra.RegistryAuthUpdateFinal
	if err := infra.UpdateControllerRegistryAuth(mainContext(), prepared.Runner, prepared.ControllerHost, update); err != nil {
		return fmt.Errorf("finalizing registry credentials on controller: %w", err)
	}

	if _, err := fmt.Fprintf(os.Stdout, "Rotated registry credentials. Generation: %s\n", creds.Generation); err != nil {
		return err
	}
	return nil
}

func runCertificateRotationMutation(opts rotationMutationOpts, component infra.CertificateRotationComponent) error {
	switch component {
	case infra.CertificateComponentController:
		return runControllerCertificateRotationMutation(opts)
	case infra.CertificateComponentCA:
		return runControllerCARotationMutation(opts)
	default:
		return fmt.Errorf("unsupported certificate rotation component %q", component)
	}
}

func runControllerCertificateRotationMutation(opts rotationMutationOpts) error {
	if opts.ToolConfig == nil {
		return fmt.Errorf("tool config is required")
	}
	prepared, err := prepareRotationMutation(opts)
	if err != nil {
		return err
	}
	services := infra.CertificateTLSEnabledServices(prepared.Outputs)
	if !opts.AutoApprove {
		question := fmt.Sprintf("Rotate controller certificate for %s/%s and restart %s?", opts.Cloud.Canonical(), opts.ToolConfig.ToolName, certificateControllerServiceText(services))
		if !cliPrompt(question) {
			return printStderrLine("Cancelled.")
		}
	}

	caPrivateKeyPEM, err := controllerCAPrivateKeyForRotation(opts)
	if err != nil {
		return err
	}
	material, err := infra.GenerateControllerCertificateMaterial(time.Now().UTC(), prepared.Outputs, caPrivateKeyPEM)
	if err != nil {
		return err
	}

	if err := printStdout("Rotating controller certificate for %s/%s\n", opts.Cloud.Canonical(), opts.ToolConfig.ToolName); err != nil {
		return err
	}
	if err := printStdoutLine("Installing replacement TLS material on controller..."); err != nil {
		return err
	}
	update := infra.ControllerCertificateUpdate{
		Material: material,
		Services: services,
	}
	if err := infra.UpdateControllerCertificate(mainContext(), prepared.Runner, prepared.ControllerHost, update); err != nil {
		return fmt.Errorf("updating controller certificate: %w", err)
	}

	vars := infra.ControllerCertificateTerraformVars(material)
	if _, err := infra.MergeRotationAutoVars(opts.ToolConfig.TerraformDir, vars); err != nil {
		return fmt.Errorf("persisting rotated controller certificate Terraform vars: %w", err)
	}

	if err := printStdoutLine("Verifying workers can heartbeat after controller TLS restart..."); err != nil {
		return err
	}
	if _, err := waitForProviderNativeFleet(mainContext(), opts.Cloud, prepared.Outputs, fleet.PlacementPolicy{}); err != nil {
		return fmt.Errorf("verifying controller certificate rotation: %w", err)
	}

	if _, err := fmt.Fprintf(os.Stdout, "Rotated controller certificate. Generation: %s\n", material.Generation); err != nil {
		return err
	}
	return nil
}

func runControllerCARotationMutation(opts rotationMutationOpts) error {
	if opts.ToolConfig == nil {
		return fmt.Errorf("tool config is required")
	}
	prepared, err := prepareRotationMutation(opts)
	if err != nil {
		return err
	}
	services := infra.CertificateTLSEnabledServices(prepared.Outputs)
	if !opts.AutoApprove {
		question := fmt.Sprintf("Rotate controller CA for %s/%s, restart %s, and replace %d workers?", opts.Cloud.Canonical(), opts.ToolConfig.ToolName, certificateControllerServiceText(services), prepared.WorkerCount)
		if !cliPrompt(question) {
			return printStderrLine("Cancelled.")
		}
	}

	material, err := infra.GenerateControllerCAMaterial(time.Now().UTC(), prepared.Outputs)
	if err != nil {
		return err
	}
	vars := infra.ControllerCATerraformVars(material)

	if err := printStdout("Rotating controller CA for %s/%s\n", opts.Cloud.Canonical(), opts.ToolConfig.ToolName); err != nil {
		return err
	}
	if err := printStdoutLine("Checking controller SSH access..."); err != nil {
		return err
	}
	if err := prepared.Runner.Run(mainContext(), prepared.ControllerHost, "true"); err != nil {
		return fmt.Errorf("checking controller SSH access: %w", err)
	}

	trustResult, err := installRotatedRegistryTrust(opts, prepared.Outputs, material)
	if err != nil {
		return err
	}
	if trustResult != nil && trustResult.Required {
		if err := printStdout("Installed operator registry CA trust at %s\n", trustResult.DockerCAPath); err != nil {
			return err
		}
	}

	if err := printStdoutLine("Installing replacement CA and TLS material on controller..."); err != nil {
		return err
	}
	update := infra.ControllerCertificateUpdate{
		Material: material.ControllerCertificateMaterial,
		Services: services,
	}
	if err := infra.UpdateControllerCertificate(mainContext(), prepared.Runner, prepared.ControllerHost, update); err != nil {
		return fmt.Errorf("updating controller CA: %w", err)
	}

	if _, err := infra.MergeRotationAutoVars(opts.ToolConfig.TerraformDir, vars); err != nil {
		return fmt.Errorf("persisting rotated controller CA Terraform vars: %w", err)
	}

	if err := printStdout("Replacing %d workers with rotated controller CA trust...\n", prepared.WorkerCount); err != nil {
		return err
	}
	if err := replaceWorkerIndexes(mainContext(), opts.ToolConfig, opts.Cloud, allWorkerIndexes(prepared.WorkerCount), vars, opts.Log); err != nil {
		return fmt.Errorf("replacing workers with rotated controller CA trust: %w", err)
	}

	tf := infra.NewTerraformClient(opts.Log)
	rotatedOutputs, err := tf.ReadOutputs(mainContext(), opts.ToolConfig.TerraformDir)
	if err != nil {
		return fmt.Errorf("reading rotated Terraform outputs: %w", err)
	}
	if _, err := infra.EnsureProviderRegistryTrust(opts.Cloud, rotatedOutputs); err != nil {
		return fmt.Errorf("verifying rotated operator registry trust: %w", err)
	}

	if err := printStdoutLine("Verifying workers can heartbeat with rotated controller CA..."); err != nil {
		return err
	}
	if _, err := waitForProviderNativeFleet(mainContext(), opts.Cloud, rotatedOutputs, fleet.PlacementPolicy{}); err != nil {
		return fmt.Errorf("verifying rotated controller CA: %w", err)
	}

	if _, err := fmt.Fprintf(os.Stdout, "Rotated controller CA. Generation: %s\n", material.Generation); err != nil {
		return err
	}
	return nil
}

func controllerCAPrivateKeyForRotation(opts rotationMutationOpts) (string, error) {
	vars, err := infra.ReadRotationAutoVars(opts.ToolConfig.TerraformDir)
	if err != nil {
		return "", fmt.Errorf("reading rotation vars for controller CA key: %w", err)
	}
	if key := strings.TrimSpace(vars["controller_ca_key_pem_override"]); key != "" {
		return key, nil
	}
	tf := infra.NewTerraformClient(opts.Log)
	stateJSON, err := tf.ShowJSON(mainContext(), opts.ToolConfig.TerraformDir)
	if err != nil {
		return "", fmt.Errorf("reading Terraform state for controller CA key: %w", err)
	}
	return infra.ControllerCAPrivateKeyFromTerraformShow(stateJSON)
}

func installRotatedRegistryTrust(opts rotationMutationOpts, outputs map[string]string, material infra.ControllerCAMaterial) (*infra.RegistryTrustResult, error) {
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(outputs["registry_url"])), "https://") {
		return nil, nil
	}
	installCfg := infra.RegistryTrustInstallConfig{
		RegistryTrustConfig: infra.RegistryTrustConfig{
			RegistryURL:                   outputs["registry_url"],
			ControllerCAPEM:               material.CAPEM,
			ControllerCAFingerprintSHA256: material.CAFingerprintSHA256,
			ControllerCertNotAfter:        material.NotAfter,
			TrustDir:                      opts.TrustDir,
			DockerCertsDir:                opts.DockerCertsDir,
		},
	}
	if _, err := infra.InstallRegistryTrust(infra.RegistryTrustInstallConfig{
		RegistryTrustConfig: installCfg.RegistryTrustConfig,
		DryRun:              true,
	}); err != nil {
		return nil, fmt.Errorf("planning rotated operator registry trust: %w", err)
	}
	result, err := infra.InstallRegistryTrust(installCfg)
	if err != nil {
		return nil, fmt.Errorf("installing rotated operator registry trust: %w", err)
	}
	return result, nil
}

func printStdout(format string, args ...interface{}) error {
	_, err := fmt.Fprintf(os.Stdout, format, args...)
	return err
}

func printStdoutLine(line string) error {
	_, err := fmt.Fprintln(os.Stdout, line)
	return err
}

func printStderrLine(line string) error {
	_, err := fmt.Fprintln(os.Stderr, line)
	return err
}

func parseRotationWorkerCount(outputs map[string]string) (int, error) {
	raw := strings.TrimSpace(outputs["worker_count"])
	if raw == "" {
		return 0, fmt.Errorf("rotation requires worker_count output")
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("rotation requires positive worker_count output, got %q", raw)
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
	if _, err := fmt.Fprintf(w, "NATS creds:  %s (rotated: %s)\n", plan.NATSCredentialGeneration, plan.NATSCredentialRotatedAt); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "MinIO creds: %s (rotated: %s)\n", plan.MinIOCredentialGeneration, plan.MinIOCredentialRotatedAt); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Registry:    %s (rotated: %s)\n", plan.RegistryCredentialGeneration, plan.RegistryCredentialRotatedAt); err != nil {
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

func outputCertificateRotationPlanText(w io.Writer, plan *infra.CertificateRotationPlan) error {
	if plan == nil {
		return fmt.Errorf("certificate rotation plan is nil")
	}
	if _, err := fmt.Fprintln(w, "Certificate rotation dry run"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Tool:        %s\n", plan.Tool); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Cloud:       %s\n", plan.Cloud); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Components:  %s\n", certificateComponentList(plan.Components)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Generation:  %s\n", plan.GenerationID); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Cert gen:    %s (rotated: %s)\n", plan.CertificateGeneration, plan.CertificateRotatedAt); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Mode:        %s\n", plan.ControllerSecurityMode); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "CA sha256:   %s\n", plan.ControllerCAFingerprintSHA256); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Cert expiry: %s\n", plan.ControllerCertNotAfter); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "TLS services:%s\n", optionalJoinedList(plan.TLSEnabledServices)); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "\nPreflight:"); err != nil {
		return err
	}
	for _, line := range []string{
		"Terraform outputs are present and match the requested tool/provider.",
		"Current controller CA fingerprint and certificate expiry metadata are available when present in outputs.",
		"No certificates will be changed in this dry run.",
	} {
		if _, err := fmt.Fprintf(w, "  - %s\n", line); err != nil {
			return err
		}
	}
	if len(plan.Warnings) > 0 {
		if _, err := fmt.Fprintln(w, "\nWarnings:"); err != nil {
			return err
		}
		for _, warning := range plan.Warnings {
			if _, err := fmt.Fprintf(w, "  - %s\n", warning); err != nil {
				return err
			}
		}
	}
	if _, err := fmt.Fprintln(w, "\nBlast radius:"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  - Controller services restarted: %s\n", certificateControllerServiceText(plan.ControllerServices)); err != nil {
		return err
	}
	workerAction := fmt.Sprintf("replace or restart %s workers", plan.WorkerCount)
	if !plan.WorkerRecycleRequired {
		workerAction = "not required"
	}
	if _, err := fmt.Fprintf(w, "  - Worker reconcile: %s\n", workerAction); err != nil {
		return err
	}
	trustAction := "not required"
	if plan.OperatorTrustRefreshRequired {
		trustAction = "refresh operator-side controller CA trust"
	}
	if _, err := fmt.Fprintf(w, "  - Operator trust refresh: %s\n", trustAction); err != nil {
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
	if _, err := fmt.Fprintln(w, "\nDry run: no Terraform apply, service restart, worker replacement, trust write, or certificate write was performed."); err != nil {
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

func certificateComponentList(components []infra.CertificateRotationComponent) string {
	parts := make([]string, 0, len(components))
	for _, component := range components {
		parts = append(parts, string(component))
	}
	return strings.Join(parts, ", ")
}

func optionalJoinedList(values []string) string {
	if len(values) == 0 {
		return " none"
	}
	return " " + strings.Join(values, ", ")
}

func certificateControllerServiceText(services []string) string {
	if len(services) == 0 {
		return "none"
	}
	return strings.Join(services, ", ")
}
