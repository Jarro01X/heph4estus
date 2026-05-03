package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"heph4estus/internal/cloud"
	"heph4estus/internal/doctor"
	"heph4estus/internal/infra"
	"heph4estus/internal/logger"
)

func runDoctor(args []string, log logger.Logger) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	format := fs.String("format", "text", "Output format: text or json")
	cloudFlag := fs.String("cloud", "", "Run checks for a specific cloud provider (aws, hetzner, linode, vultr, manual)")
	tool := fs.String("tool", "", "Tool whose provider-native Terraform outputs should be checked")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *format != "text" && *format != "json" {
		return fmt.Errorf("--format must be text or json")
	}

	ctx := mainContext()
	deps := doctor.DefaultDeps()

	var results []doctor.CheckResult
	if *cloudFlag != "" {
		kind, err := cloud.ParseKind(*cloudFlag)
		if err != nil {
			return err
		}
		results = doctor.RunForCloud(ctx, deps, kind)
		if *tool != "" {
			results = append(results, runDoctorProviderOutputChecks(ctx, *tool, kind, log)...)
		}
	} else {
		if *tool != "" {
			return fmt.Errorf("--tool requires --cloud")
		}
		results = doctor.RunAll(ctx, deps)
	}

	if *format == "json" {
		return outputDoctorJSON(results)
	}
	return outputDoctorText(results)
}

func runDoctorProviderOutputChecks(ctx context.Context, tool string, kind cloud.Kind, log logger.Logger) []doctor.CheckResult {
	if !kind.IsProviderNative() {
		return []doctor.CheckResult{{
			Name:    "provider_outputs",
			Status:  doctor.StatusWarn,
			Summary: fmt.Sprintf("Provider-native output checks do not apply to cloud %q", kind.Canonical()),
		}}
	}
	cfg, err := infra.ResolveToolConfig(tool, kind)
	if err != nil {
		return []doctor.CheckResult{{
			Name:    "provider_outputs",
			Status:  doctor.StatusFail,
			Summary: fmt.Sprintf("Could not resolve Terraform outputs for %s/%s: %v", kind.Canonical(), tool, err),
		}}
	}
	outputs, err := infra.NewTerraformClient(log).ReadOutputs(ctx, cfg.TerraformDir)
	if err != nil {
		return []doctor.CheckResult{{
			Name:    "provider_outputs",
			Status:  doctor.StatusWarn,
			Summary: fmt.Sprintf("Could not read Terraform outputs for %s/%s", kind.Canonical(), tool),
			Fix:     "Deploy infrastructure first, or run terraform init/output in the provider module to inspect an existing fleet.",
		}}
	}
	if len(outputs) == 0 {
		return []doctor.CheckResult{{
			Name:    "provider_outputs",
			Status:  doctor.StatusWarn,
			Summary: fmt.Sprintf("No Terraform outputs found for %s/%s", kind.Canonical(), tool),
			Fix:     "Deploy infrastructure first.",
		}}
	}
	results := []doctor.CheckResult{{
		Name:    "provider_outputs",
		Status:  doctor.StatusPass,
		Summary: fmt.Sprintf("Terraform outputs found for %s/%s", kind.Canonical(), tool),
	}}
	return append(results, doctor.RunProviderNativeOutputChecks(kind, outputs)...)
}

func outputDoctorJSON(results []doctor.CheckResult) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(results)
}

func outputDoctorText(results []doctor.CheckResult) error {
	passCount, warnCount, failCount := 0, 0, 0

	for _, r := range results {
		var icon string
		switch r.Status {
		case doctor.StatusPass:
			icon = "[PASS]"
			passCount++
		case doctor.StatusWarn:
			icon = "[WARN]"
			warnCount++
		case doctor.StatusFail:
			icon = "[FAIL]"
			failCount++
		}
		_, _ = fmt.Fprintf(os.Stdout, "  %s %s\n", icon, r.Summary)
		if r.Fix != "" {
			_, _ = fmt.Fprintf(os.Stdout, "         -> %s\n", r.Fix)
		}
	}

	_, _ = fmt.Fprintln(os.Stdout)
	_, _ = fmt.Fprintf(os.Stdout, "Results: %d passed, %d warnings, %d failed\n", passCount, warnCount, failCount)

	if failCount > 0 {
		return exitError{code: 1}
	}
	return nil
}

// exitError signals a non-zero exit without printing an additional error message.
type exitError struct{ code int }

func (e exitError) Error() string { return fmt.Sprintf("exit status %d", e.code) }
