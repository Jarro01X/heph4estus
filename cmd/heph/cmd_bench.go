package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"heph4estus/internal/cloud"
	"heph4estus/internal/fleet"
	"heph4estus/internal/fleetstate"
	"heph4estus/internal/infra"
	"heph4estus/internal/logger"
	"heph4estus/internal/operator"
)

type fleetBenchReport struct {
	Tool                    string         `json:"tool"`
	Cloud                   string         `json:"cloud"`
	GeneratedAt             time.Time      `json:"generated_at"`
	DeployDuration          time.Duration  `json:"deploy_duration"`
	FirstRegisteredDuration time.Duration  `json:"first_registered_duration"`
	FirstAdmittedDuration   time.Duration  `json:"first_admitted_duration"`
	SteadyStateDuration     time.Duration  `json:"steady_state_duration"`
	Placement               string         `json:"placement"`
	DesiredWorkers          int            `json:"desired_workers"`
	ControllerCount         int            `json:"controller_count"`
	UniqueIPv4Count         int            `json:"unique_ipv4_count"`
	IPv6ReadyCount          int            `json:"ipv6_ready_count"`
	DiversityEligible       int            `json:"diversity_eligible"`
	ThroughputEligible      int            `json:"throughput_eligible"`
	ExcludedByReason        map[string]int `json:"excluded_by_reason,omitempty"`
	VersionCounts           map[string]int `json:"version_counts,omitempty"`
	RolloutPhase            string         `json:"rollout_phase,omitempty"`
	RollbackReason          string         `json:"rollback_reason,omitempty"`
}

func runBench(args []string, log logger.Logger) error {
	if len(args) == 0 {
		return fmt.Errorf("bench requires a subcommand: fleet")
	}
	switch args[0] {
	case "fleet":
		return runBenchFleet(args[1:], log)
	default:
		return fmt.Errorf("bench: unknown subcommand %q", args[0])
	}
}

func runBenchFleet(args []string, log logger.Logger) error {
	fs := flag.NewFlagSet("bench fleet", flag.ContinueOnError)
	tool := fs.String("tool", "", "Tool whose provider-native fleet should be benchmarked")
	format := fs.String("format", "text", "Output format: text or json")
	outputPath := fs.String("output", "", "Optional path to also write the benchmark report as JSON")
	autoApprove := fs.Bool("auto-approve", false, "Skip interactive approval prompt")
	cloudFlag := fs.String("cloud", "", "Cloud provider: "+cloud.SupportedKindsText()+" (required)")
	timeoutFlag := fs.String("timeout", "10m", "How long to wait for steady-state fleet readiness")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tool == "" {
		return fmt.Errorf("--tool flag is required")
	}
	opCfg, _ := operator.LoadConfig()
	kind, err := resolveCLICloud(*cloudFlag, opCfg)
	if err != nil {
		return err
	}
	if !kind.IsProviderNative() {
		return fmt.Errorf("bench fleet only supports provider-native clouds, got %q", kind.Canonical())
	}
	timeout, err := parseDurationFlag(*timeoutFlag, 10*time.Minute)
	if err != nil {
		return err
	}
	cfg, err := infra.ResolveToolConfig(*tool, kind)
	if err != nil {
		return err
	}

	start := time.Now()
	ensureResult, err := infra.EnsureInfra(mainContext(), cfg, infra.LifecyclePolicy{
		AutoApprove: *autoApprove,
	}, "", os.Stderr, deployPrompt, log)
	if err != nil {
		return err
	}
	deployDuration := time.Since(start)

	placement, err := operator.ResolvePlacementPolicy(fleet.PlacementPolicy{}, opCfg, fleetWorkerCount(ensureResult.Outputs))
	if err != nil {
		return err
	}

	reputationStore, err := fleetstate.NewReputationStore()
	if err != nil {
		return err
	}
	reputation, err := reputationStore.List(string(kind.Canonical()))
	if err != nil {
		return err
	}
	rolloutStore, err := fleetstate.NewRolloutStore()
	if err != nil {
		return err
	}
	rollout, err := rolloutStore.Load(string(kind.Canonical()), *tool)
	if err != nil {
		return err
	}

	deadline := time.Now().Add(timeout)
	var firstRegistered, firstAdmitted, steadyState time.Duration
	var finalState *fleet.FleetState
	for {
		snapshot, err := fleet.QueryFleetSnapshot(context.Background(), fleet.NATSFleetManagerConfig{
			NATSURL:         ensureResult.Outputs["nats_url"],
			DesiredWorkers:  fleetWorkerCount(ensureResult.Outputs),
			ControllerIP:    ensureResult.Outputs["controller_ip"],
			GenerationID:    ensureResult.Outputs["generation_id"],
			Cloud:           string(kind.Canonical()),
			Placement:       placement,
			ExpectedVersion: ensureResult.Outputs["docker_image"],
			Reputation:      reputation,
			Rollout:         rollout,
		}, log)
		if err != nil {
			return err
		}
		finalState = snapshot
		summary := snapshot.Summarize()
		elapsed := time.Since(start)
		if firstRegistered == 0 && summary.RegisteredCount > 0 {
			firstRegistered = elapsed
		}
		if firstAdmitted == 0 && summary.EligibleCount > 0 {
			firstAdmitted = elapsed
		}
		if summary.EligibleCount >= summary.DesiredWorkers {
			steadyState = elapsed
			break
		}
		if time.Now().After(deadline) {
			steadyState = elapsed
			break
		}
		time.Sleep(2 * time.Second)
	}

	summary := finalState.Summarize()
	diversitySummary := fleet.EvaluatePlacement(finalState, fleet.PlacementPolicy{Mode: fleet.PlacementModeDiversity})
	throughputSummary := fleet.EvaluatePlacement(finalState, fleet.PlacementPolicy{Mode: fleet.PlacementModeThroughput})
	report := fleetBenchReport{
		Tool:                    *tool,
		Cloud:                   string(kind.Canonical()),
		GeneratedAt:             time.Now().UTC(),
		DeployDuration:          deployDuration,
		FirstRegisteredDuration: firstRegistered,
		FirstAdmittedDuration:   firstAdmitted,
		SteadyStateDuration:     steadyState,
		Placement:               placement.Summary(),
		DesiredWorkers:          summary.DesiredWorkers,
		ControllerCount:         1,
		UniqueIPv4Count:         summary.UniqueIPv4Count,
		IPv6ReadyCount:          summary.IPv6ReadyCount,
		DiversityEligible:       diversitySummary.EligibleCount,
		ThroughputEligible:      throughputSummary.EligibleCount,
		ExcludedByReason:        summary.ExcludedByReason,
		VersionCounts:           summary.VersionCounts,
		RolloutPhase:            summary.RolloutPhase,
		RollbackReason:          summary.RollbackReason,
	}

	if *format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			return err
		}
		return writeBenchReport(*outputPath, report)
	}
	_, _ = fmt.Fprintf(os.Stdout, "Tool:                 %s\n", report.Tool)
	_, _ = fmt.Fprintf(os.Stdout, "Cloud:                %s\n", report.Cloud)
	_, _ = fmt.Fprintf(os.Stdout, "Deploy:               %s\n", report.DeployDuration)
	_, _ = fmt.Fprintf(os.Stdout, "First registered:     %s\n", report.FirstRegisteredDuration)
	_, _ = fmt.Fprintf(os.Stdout, "First admitted:       %s\n", report.FirstAdmittedDuration)
	_, _ = fmt.Fprintf(os.Stdout, "Steady state:         %s\n", report.SteadyStateDuration)
	_, _ = fmt.Fprintf(os.Stdout, "Placement:            %s\n", report.Placement)
	_, _ = fmt.Fprintf(os.Stdout, "Workers:              %d desired\n", report.DesiredWorkers)
	_, _ = fmt.Fprintf(os.Stdout, "IPv4 / IPv6:          %d unique / %d ready\n", report.UniqueIPv4Count, report.IPv6ReadyCount)
	_, _ = fmt.Fprintf(os.Stdout, "Admission capacity:   diversity=%d throughput=%d\n", report.DiversityEligible, report.ThroughputEligible)
	if reasons := fleetSummaryReasons(report.ExcludedByReason); reasons != "" {
		_, _ = fmt.Fprintf(os.Stdout, "Excluded:             %s\n", reasons)
	}
	if report.RolloutPhase != "" {
		_, _ = fmt.Fprintf(os.Stdout, "Rollout:              %s\n", report.RolloutPhase)
	}
	return writeBenchReport(*outputPath, report)
}

func writeBenchReport(path string, report fleetBenchReport) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating benchmark report dir: %w", err)
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling benchmark report: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("writing benchmark report: %w", err)
	}
	return nil
}
