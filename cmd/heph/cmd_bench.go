package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"heph4estus/internal/bench"
	"heph4estus/internal/cloud"
	"heph4estus/internal/fleet"
	"heph4estus/internal/fleetstate"
	"heph4estus/internal/infra"
	"heph4estus/internal/logger"
	"heph4estus/internal/operator"
)

func runBench(args []string, log logger.Logger) error {
	if len(args) == 0 {
		return fmt.Errorf("bench requires a subcommand: fleet, history, compare")
	}
	switch args[0] {
	case "fleet":
		return runBenchFleet(args[1:], log)
	case "history":
		return runBenchHistory(args[1:], log)
	case "compare":
		return runBenchCompare(args[1:], log)
	default:
		return fmt.Errorf("bench: unknown subcommand %q", args[0])
	}
}

func runBenchFleet(args []string, log logger.Logger) error {
	fs := flag.NewFlagSet("bench fleet", flag.ContinueOnError)
	tool := fs.String("tool", "", "Tool whose provider-native fleet should be benchmarked")
	jobID := fs.String("job-id", "", "Optional job ID to enrich the report with real workload throughput metrics")
	format := fs.String("format", "text", "Output format: text or json")
	outputPath := fs.String("output", "", "Optional path to also write the benchmark report as JSON")
	noSave := fs.Bool("no-save", false, "Do not persist the benchmark run in the local benchmark history store")
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
			RootCAPEM:       ensureResult.Outputs["controller_ca_pem"],
			ServerName:      ensureResult.Outputs["controller_host"],
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
	report := bench.FleetReport{
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
	if strings.TrimSpace(*jobID) != "" {
		if err := applyJobBenchmarkMetrics(mainContext(), &report, *jobID, kind, log); err != nil {
			return err
		}
	}

	var savedPath string
	if !*noSave {
		store, err := bench.NewStore()
		if err != nil {
			return err
		}
		savedPath, err = store.Save(report)
		if err != nil {
			return err
		}
	}

	if *format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			return err
		}
		if err := writeBenchReport(*outputPath, report); err != nil {
			return err
		}
		if savedPath != "" {
			_, _ = fmt.Fprintf(os.Stderr, "Saved benchmark history to %s\n", savedPath)
		}
		return nil
	}
	if err := outputBenchFleetText(report, savedPath); err != nil {
		return err
	}
	return writeBenchReport(*outputPath, report)
}

func runBenchHistory(args []string, log logger.Logger) error {
	fs := flag.NewFlagSet("bench history", flag.ContinueOnError)
	tool := fs.String("tool", "", "Optional tool filter")
	cloudFlag := fs.String("cloud", "", "Optional cloud filter")
	format := fs.String("format", "text", "Output format: text or json")
	limit := fs.Int("limit", 10, "Maximum number of reports to show")
	if err := fs.Parse(args); err != nil {
		return err
	}
	store, err := bench.NewStore()
	if err != nil {
		return err
	}
	cloudValue := ""
	if *cloudFlag != "" {
		opCfg, _ := operator.LoadConfig()
		kind, err := resolveCLICloud(*cloudFlag, opCfg)
		if err != nil {
			return err
		}
		cloudValue = string(kind.Canonical())
	}
	reports, err := store.List(*tool, cloudValue, *limit)
	if err != nil {
		return err
	}
	if *format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(reports)
	}
	if len(reports) == 0 {
		_, _ = fmt.Fprintln(os.Stdout, "No benchmark history.")
		return nil
	}
	for _, report := range reports {
		line := fmt.Sprintf("%s %-8s %-12s deploy=%s admitted=%s steady=%s ipv4=%d ipv6=%d",
			report.GeneratedAt.Format(time.RFC3339),
			report.Cloud,
			report.Tool,
			report.DeployDuration,
			report.FirstAdmittedDuration,
			report.SteadyStateDuration,
			report.UniqueIPv4Count,
			report.IPv6ReadyCount,
		)
		if report.JobID != "" {
			line += fmt.Sprintf(" job=%s done=%d/%d tpm=%.2f", report.JobID, report.CompletedTasks, report.TotalTasks, report.TasksPerMinute)
		}
		_, _ = fmt.Fprintln(os.Stdout, line)
	}
	return nil
}

func runBenchCompare(args []string, log logger.Logger) error {
	fs := flag.NewFlagSet("bench compare", flag.ContinueOnError)
	tool := fs.String("tool", "", "Tool filter when comparing from stored history")
	cloudFlag := fs.String("cloud", "", "Cloud filter when comparing from stored history")
	format := fs.String("format", "text", "Output format: text or json")
	baselinePath := fs.String("baseline", "", "Optional path to a baseline benchmark report JSON")
	candidatePath := fs.String("candidate", "", "Optional path to a candidate benchmark report JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	store, err := bench.NewStore()
	if err != nil {
		return err
	}
	var comparison bench.FleetComparison
	switch {
	case *baselinePath != "" || *candidatePath != "":
		if strings.TrimSpace(*baselinePath) == "" || strings.TrimSpace(*candidatePath) == "" {
			return fmt.Errorf("--baseline and --candidate must be provided together")
		}
		baseline, err := store.Load(*baselinePath)
		if err != nil {
			return err
		}
		candidate, err := store.Load(*candidatePath)
		if err != nil {
			return err
		}
		comparison = bench.CompareFleetReports(*baseline, *candidate)
	default:
		cloudValue := ""
		if *cloudFlag != "" {
			opCfg, _ := operator.LoadConfig()
			kind, err := resolveCLICloud(*cloudFlag, opCfg)
			if err != nil {
				return err
			}
			cloudValue = string(kind.Canonical())
		}
		reports, err := store.List(*tool, cloudValue, 2)
		if err != nil {
			return err
		}
		if len(reports) < 2 {
			return fmt.Errorf("need at least two benchmark reports to compare")
		}
		comparison = bench.CompareFleetReports(reports[1], reports[0])
	}
	if *format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(comparison)
	}
	return outputBenchComparisonText(comparison)
}

func outputBenchFleetText(report bench.FleetReport, savedPath string) error {
	if _, err := fmt.Fprintf(os.Stdout, "Tool:                 %s\n", report.Tool); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(os.Stdout, "Cloud:                %s\n", report.Cloud); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(os.Stdout, "Deploy:               %s\n", report.DeployDuration); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(os.Stdout, "First registered:     %s\n", report.FirstRegisteredDuration); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(os.Stdout, "First admitted:       %s\n", report.FirstAdmittedDuration); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(os.Stdout, "Steady state:         %s\n", report.SteadyStateDuration); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(os.Stdout, "Placement:            %s\n", report.Placement); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(os.Stdout, "Workers:              %d desired\n", report.DesiredWorkers); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(os.Stdout, "IPv4 / IPv6:          %d unique / %d ready\n", report.UniqueIPv4Count, report.IPv6ReadyCount); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(os.Stdout, "Admission capacity:   diversity=%d throughput=%d\n", report.DiversityEligible, report.ThroughputEligible); err != nil {
		return err
	}
	if reasons := fleetSummaryReasons(report.ExcludedByReason); reasons != "" {
		if _, err := fmt.Fprintf(os.Stdout, "Excluded:             %s\n", reasons); err != nil {
			return err
		}
	}
	if report.RolloutPhase != "" {
		if _, err := fmt.Fprintf(os.Stdout, "Rollout:              %s\n", report.RolloutPhase); err != nil {
			return err
		}
	}
	if report.JobID != "" {
		if _, err := fmt.Fprintf(os.Stdout, "Job:                  %s (%s)\n", report.JobID, report.JobPhase); err != nil {
			return err
		}
		if report.TotalTasks > 0 {
			if _, err := fmt.Fprintf(os.Stdout, "Completion:           %d / %d (%.1f%%)\n", report.CompletedTasks, report.TotalTasks, report.CompletionPercent); err != nil {
				return err
			}
		}
		if report.ActiveRuntime > 0 {
			if _, err := fmt.Fprintf(os.Stdout, "Active runtime:       %s\n", report.ActiveRuntime); err != nil {
				return err
			}
		}
		if report.TasksPerMinute > 0 {
			if _, err := fmt.Fprintf(os.Stdout, "Tasks/minute:         %.2f\n", report.TasksPerMinute); err != nil {
				return err
			}
		}
	}
	if savedPath != "" {
		if _, err := fmt.Fprintf(os.Stdout, "Saved:                %s\n", savedPath); err != nil {
			return err
		}
	}
	return nil
}

func outputBenchComparisonText(comparison bench.FleetComparison) error {
	if _, err := fmt.Fprintf(os.Stdout, "Baseline:   %s %s/%s\n",
		comparison.Baseline.GeneratedAt.Format(time.RFC3339),
		comparison.Baseline.Cloud,
		comparison.Baseline.Tool,
	); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(os.Stdout, "Candidate:  %s %s/%s\n\n",
		comparison.Candidate.GeneratedAt.Format(time.RFC3339),
		comparison.Candidate.Cloud,
		comparison.Candidate.Tool,
	); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(os.Stdout, "Deploy:     %s\n", comparison.Delta.DeployDuration); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(os.Stdout, "Registered: %s\n", comparison.Delta.FirstRegisteredDuration); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(os.Stdout, "Admitted:   %s\n", comparison.Delta.FirstAdmittedDuration); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(os.Stdout, "Steady:     %s\n", comparison.Delta.SteadyStateDuration); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(os.Stdout, "IPv4:       %+d\n", comparison.Delta.UniqueIPv4Count); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(os.Stdout, "IPv6:       %+d\n", comparison.Delta.IPv6ReadyCount); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(os.Stdout, "Diversity:  %+d\n", comparison.Delta.DiversityEligible); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(os.Stdout, "Throughput: %+d\n", comparison.Delta.ThroughputEligible); err != nil {
		return err
	}
	if comparison.Baseline.JobID != "" || comparison.Candidate.JobID != "" || comparison.Delta.CompletedTasks != 0 || comparison.Delta.TasksPerMinute != 0 {
		if _, err := fmt.Fprintf(os.Stdout, "Completed:  %+d\n", comparison.Delta.CompletedTasks); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(os.Stdout, "Runtime:    %s\n", comparison.Delta.ActiveRuntime); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(os.Stdout, "Tasks/min:  %+.2f\n", comparison.Delta.TasksPerMinute); err != nil {
			return err
		}
	}
	return nil
}

func applyJobBenchmarkMetrics(ctx context.Context, report *bench.FleetReport, jobID string, kind cloud.Kind, log logger.Logger) error {
	store, err := operator.NewJobStore()
	if err != nil {
		return err
	}
	rec, err := store.Load(jobID)
	if err != nil {
		return err
	}
	if report.Tool != "" && rec.ToolName != "" && report.Tool != rec.ToolName {
		return fmt.Errorf("job %s belongs to tool %s, not %s", rec.JobID, rec.ToolName, report.Tool)
	}
	if report.Cloud != "" && rec.Cloud != "" && report.Cloud != rec.Cloud {
		return fmt.Errorf("job %s belongs to cloud %s, not %s", rec.JobID, rec.Cloud, report.Cloud)
	}
	completed, err := benchmarkCompletedTasks(ctx, rec, kind, log)
	if err != nil {
		return err
	}
	report.JobID = rec.JobID
	report.JobPhase = string(rec.Phase)
	report.TotalTasks = rec.TotalTasks
	report.CompletedTasks = completed
	report.ActiveRuntime = benchmarkJobRuntime(rec, time.Now().UTC())
	if report.Placement == "" && rec.Placement.Mode != "" {
		report.Placement = rec.Placement.Summary()
	}
	if rec.TotalTasks > 0 {
		completion := float64(completed) / float64(rec.TotalTasks) * 100
		if completion > 100 {
			completion = 100
		}
		report.CompletionPercent = roundBenchFloat(completion, 1)
	}
	if report.ActiveRuntime > 0 {
		report.TasksPerMinute = roundBenchFloat(float64(completed)/report.ActiveRuntime.Minutes(), 2)
	}
	return nil
}

func benchmarkCompletedTasks(ctx context.Context, rec *operator.JobRecord, kind cloud.Kind, log logger.Logger) (int, error) {
	if rec == nil {
		return 0, fmt.Errorf("job record is required")
	}
	if rec.Phase == operator.PhaseComplete && rec.TotalTasks > 0 {
		return rec.TotalTasks, nil
	}
	if strings.TrimSpace(rec.Bucket) == "" || strings.TrimSpace(rec.ResultPrefix) == "" {
		if isTerminalPhase(rec.Phase) {
			return rec.TotalTasks, nil
		}
		return 0, fmt.Errorf("job %s has no result prefix for live throughput benchmarking", rec.JobID)
	}
	provider, err := buildBenchmarkProvider(ctx, rec, kind, log)
	if err != nil {
		if rec.Phase == operator.PhaseComplete && rec.TotalTasks > 0 {
			return rec.TotalTasks, nil
		}
		return 0, err
	}
	count, err := provider.Storage().Count(ctx, rec.Bucket, rec.ResultPrefix)
	if err != nil {
		if rec.Phase == operator.PhaseComplete && rec.TotalTasks > 0 {
			return rec.TotalTasks, nil
		}
		return 0, err
	}
	if rec.TotalTasks > 0 && count > rec.TotalTasks {
		count = rec.TotalTasks
	}
	return count, nil
}

func buildBenchmarkProvider(ctx context.Context, rec *operator.JobRecord, kind cloud.Kind, log logger.Logger) (cloud.Provider, error) {
	if kind.IsProviderNative() {
		cfg, err := infra.ResolveToolConfig(rec.ToolName, kind)
		if err != nil {
			return nil, err
		}
		outputs, err := infra.NewTerraformClient(log).ReadOutputs(ctx, cfg.TerraformDir)
		if err != nil {
			return nil, err
		}
		return buildRuntimeProvider(ctx, kind, outputs, log)
	}
	return buildRuntimeProvider(ctx, kind, nil, log)
}

func benchmarkJobRuntime(rec *operator.JobRecord, now time.Time) time.Duration {
	if rec == nil {
		return 0
	}
	start := rec.StartedAt
	if start.IsZero() {
		start = rec.CreatedAt
	}
	if start.IsZero() {
		return 0
	}
	end := now.UTC()
	if isTerminalPhase(rec.Phase) && !rec.UpdatedAt.IsZero() {
		end = rec.UpdatedAt
	}
	if end.Before(start) {
		return 0
	}
	return end.Sub(start).Truncate(time.Second)
}

func roundBenchFloat(v float64, decimals int) float64 {
	if decimals < 0 {
		return v
	}
	factor := math.Pow10(decimals)
	return math.Round(v*factor) / factor
}

func writeBenchReport(path string, report bench.FleetReport) error {
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
