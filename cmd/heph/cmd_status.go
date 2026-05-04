package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"time"

	"heph4estus/internal/cloud"
	"heph4estus/internal/cloud/factory"
	"heph4estus/internal/fleet"
	"heph4estus/internal/fleetstate"
	"heph4estus/internal/logger"
	"heph4estus/internal/operator"
)

// statusSnapshot is the structured output of heph status.
type statusSnapshot struct {
	JobID          string         `json:"job_id"`
	Tool           string         `json:"tool"`
	Phase          operator.Phase `json:"phase"`
	Cloud          string         `json:"cloud,omitempty"`
	Bucket         string         `json:"bucket,omitempty"`
	Progress       statusProgress `json:"progress"`
	Elapsed        string         `json:"elapsed"`
	CleanupPolicy  string         `json:"cleanup_policy,omitempty"`
	ResultPrefix   string         `json:"result_prefix,omitempty"`
	ArtifactPrefix string         `json:"artifact_prefix,omitempty"`
	LocalOutputDir string         `json:"local_output_dir,omitempty"`
	LastError      string         `json:"last_error,omitempty"`
	Fleet          *statusFleet   `json:"fleet,omitempty"`
}

type statusProgress struct {
	Completed int     `json:"completed"`
	Total     int     `json:"total"`
	Percent   float64 `json:"percent"`
}

// statusFleet holds fleet-level observability data for provider-native runs.
type statusFleet struct {
	ControllerIP            string         `json:"controller_ip,omitempty"`
	GenerationID            string         `json:"generation_id,omitempty"`
	CanaryGeneration        string         `json:"canary_generation,omitempty"`
	ExpectedWorkerVersion   string         `json:"expected_worker_version,omitempty"`
	Placement               string         `json:"placement,omitempty"`
	RolloutPhase            string         `json:"rollout_phase,omitempty"`
	RollbackReason          string         `json:"rollback_reason,omitempty"`
	DesiredWorkers          int            `json:"desired_workers"`
	RegisteredCount         int            `json:"registered"`
	HealthyCount            int            `json:"healthy"`
	ReadyCount              int            `json:"ready"`
	EligibleCount           int            `json:"eligible"`
	ExcludedCount           int            `json:"excluded"`
	QuarantinedCount        int            `json:"quarantined"`
	UniqueIPv4Count         int            `json:"unique_ipv4"`
	UniqueEligibleIPv4Count int            `json:"unique_eligible_ipv4"`
	IPv6ReadyCount          int            `json:"ipv6_ready"`
	CanaryWorkerCount       int            `json:"canary_workers,omitempty"`
	PromotedWorkerCount     int            `json:"promoted_workers,omitempty"`
	DrainingWorkerCount     int            `json:"draining_workers,omitempty"`
	ExcludedByReason        map[string]int `json:"excluded_by_reason,omitempty"`
}

func runStatus(args []string, log logger.Logger) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	jobID := fs.String("job-id", "", "Job ID to query (required)")
	format := fs.String("format", "text", "Output format: text or json")
	cloudFlag := fs.String("cloud", "", "Override the cloud provider used to query live progress (default: job record or aws)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *jobID == "" {
		return fmt.Errorf("--job-id flag is required; usage: heph status --job-id <id> [--format text|json]")
	}
	if *format != "text" && *format != "json" {
		return fmt.Errorf("--format must be text or json")
	}

	// Load the job record directly from the store.
	store, err := operator.NewJobStore()
	if err != nil {
		return fmt.Errorf("opening job store: %w", err)
	}

	rec, err := store.Load(*jobID)
	if err != nil {
		return fmt.Errorf("%w — run 'heph status' only for jobs started on this machine", err)
	}

	// Resolve effective cloud: explicit flag overrides the value persisted in
	// the job record, which in turn overrides the operator default.
	opCfg, _ := operator.LoadConfig()
	effectiveCloud := *cloudFlag
	if effectiveCloud == "" {
		effectiveCloud = rec.Cloud
	}
	cloudKind, err := resolveCLICloud(effectiveCloud, opCfg)
	if err != nil {
		return err
	}

	ctx := context.Background()

	// Query live cloud progress if the job has a bucket and result prefix.
	completed := 0
	if rec.Bucket != "" && rec.ResultPrefix != "" && !isTerminalPhase(rec.Phase) {
		count, cloudErr := countResults(ctx, rec.Bucket, rec.ResultPrefix, cloudKind, log)
		if cloudErr != nil {
			log.Error("Warning: could not query live progress: %v", cloudErr)
		} else {
			completed = count
		}
	}

	snap := buildSnapshot(rec, completed)

	// Query fleet state for provider-native runs.
	if cloudKind.IsProviderNative() && !isTerminalPhase(rec.Phase) {
		natsURL := rec.NATSUrl
		if natsURL != "" {
			canonicalCloud := string(cloudKind.Canonical())
			var (
				reputation []fleetstate.ReputationRecord
				rollout    *fleetstate.RolloutRecord
			)
			if store, err := fleetstate.NewReputationStore(); err == nil {
				reputation, _ = store.List(canonicalCloud)
			}
			if store, err := fleetstate.NewRolloutStore(); err == nil {
				rollout, _ = store.Load(canonicalCloud, rec.ToolName)
			}
			generationID := rec.GenerationID
			if rollout != nil {
				generationID = ""
			}
			fleetSnap, err := fleet.QueryFleetSnapshot(ctx, fleet.NATSFleetManagerConfig{
				NATSURL:         natsURL,
				DesiredWorkers:  rec.WorkerCount,
				ControllerIP:    rec.ControllerIP,
				GenerationID:    generationID,
				Cloud:           canonicalCloud,
				Placement:       rec.Placement,
				ExpectedVersion: rec.ExpectedWorkerVersion,
				Reputation:      reputation,
				Rollout:         rollout,
				RootCAPEM:       rec.ControllerCAPEM,
				ServerName:      rec.ControllerHost,
			}, log)
			if err != nil {
				log.Error("Warning: could not query fleet state: %v", err)
			} else {
				summary := fleetSnap.Summarize()
				snap.Fleet = &statusFleet{
					ControllerIP:            fleetSnap.ControllerIP,
					GenerationID:            fleetSnap.GenerationID,
					CanaryGeneration:        rolloutCanaryGeneration(rollout),
					ExpectedWorkerVersion:   rec.ExpectedWorkerVersion,
					Placement:               rec.Placement.Summary(),
					RolloutPhase:            summary.RolloutPhase,
					RollbackReason:          summary.RollbackReason,
					DesiredWorkers:          summary.DesiredWorkers,
					RegisteredCount:         summary.RegisteredCount,
					HealthyCount:            summary.HealthyCount,
					ReadyCount:              summary.ReadyCount,
					EligibleCount:           summary.EligibleCount,
					ExcludedCount:           summary.ExcludedCount,
					QuarantinedCount:        summary.QuarantinedCount,
					UniqueIPv4Count:         summary.UniqueIPv4Count,
					UniqueEligibleIPv4Count: summary.UniqueEligibleIPv4Count,
					IPv6ReadyCount:          summary.IPv6ReadyCount,
					CanaryWorkerCount:       rolloutWorkerCount(rollout, "canary"),
					PromotedWorkerCount:     rolloutWorkerCount(rollout, "promoted"),
					DrainingWorkerCount:     rolloutWorkerCount(rollout, "draining"),
					ExcludedByReason:        summary.ExcludedByReason,
				}
			}
		}
	}

	if *format == "json" {
		return outputStatusJSON(snap)
	}
	return outputStatusText(snap)
}

func isTerminalPhase(p operator.Phase) bool {
	return p == operator.PhaseComplete || p == operator.PhaseFailed
}

func buildSnapshot(rec *operator.JobRecord, liveCompleted int) statusSnapshot {
	elapsed := time.Since(rec.CreatedAt).Truncate(time.Second)
	if isTerminalPhase(rec.Phase) && !rec.UpdatedAt.IsZero() {
		elapsed = rec.UpdatedAt.Sub(rec.CreatedAt).Truncate(time.Second)
	}

	total := rec.TotalTasks
	completed := liveCompleted

	// Infer phase from live data.
	phase := rec.Phase
	if phase == operator.PhaseComplete && total > 0 {
		completed = total
	}
	if !isTerminalPhase(phase) {
		if completed > 0 && completed < total {
			phase = operator.PhaseScanning
		} else if completed >= total && total > 0 {
			phase = operator.PhaseComplete
		}
	}
	if total > 0 && completed > total {
		completed = total
	}

	pct := 0.0
	if total > 0 {
		pct = math.Round(float64(completed)/float64(total)*1000) / 10
	}

	return statusSnapshot{
		JobID:          rec.JobID,
		Tool:           rec.ToolName,
		Phase:          phase,
		Cloud:          rec.Cloud,
		Bucket:         rec.Bucket,
		Progress:       statusProgress{Completed: completed, Total: total, Percent: pct},
		Elapsed:        elapsed.String(),
		CleanupPolicy:  rec.CleanupPolicy,
		ResultPrefix:   rec.ResultPrefix,
		ArtifactPrefix: rec.ArtifactPrefix,
		LocalOutputDir: rec.LocalOutputDir,
		LastError:      rec.LastError,
	}
}

func outputStatusJSON(snap statusSnapshot) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(snap)
}

func outputStatusText(snap statusSnapshot) error {
	_, _ = fmt.Fprintf(os.Stdout, "Job:       %s\n", snap.JobID)
	_, _ = fmt.Fprintf(os.Stdout, "Tool:      %s\n", snap.Tool)
	_, _ = fmt.Fprintf(os.Stdout, "Phase:     %s\n", snap.Phase)
	if snap.Cloud != "" {
		_, _ = fmt.Fprintf(os.Stdout, "Cloud:     %s\n", snap.Cloud)
	}
	_, _ = fmt.Fprintf(os.Stdout, "Progress:  %d / %d  (%.1f%%)\n", snap.Progress.Completed, snap.Progress.Total, snap.Progress.Percent)
	_, _ = fmt.Fprintf(os.Stdout, "Elapsed:   %s\n", snap.Elapsed)

	if snap.Fleet != nil {
		_, _ = fmt.Fprintf(os.Stdout, "\nFleet:\n")
		if snap.Fleet.ControllerIP != "" {
			_, _ = fmt.Fprintf(os.Stdout, "  Controller:  %s\n", snap.Fleet.ControllerIP)
		}
		if snap.Fleet.GenerationID != "" {
			_, _ = fmt.Fprintf(os.Stdout, "  Generation:  %s\n", snap.Fleet.GenerationID)
		}
		if snap.Fleet.ExpectedWorkerVersion != "" {
			_, _ = fmt.Fprintf(os.Stdout, "  Version:     %s\n", snap.Fleet.ExpectedWorkerVersion)
		}
		if snap.Fleet.Placement != "" {
			_, _ = fmt.Fprintf(os.Stdout, "  Placement:   %s\n", snap.Fleet.Placement)
		}
		if snap.Fleet.RolloutPhase != "" {
			_, _ = fmt.Fprintf(os.Stdout, "  Rollout:     %s\n", snap.Fleet.RolloutPhase)
		}
		if snap.Fleet.CanaryGeneration != "" {
			_, _ = fmt.Fprintf(os.Stdout, "  Canary Gen:  %s\n", snap.Fleet.CanaryGeneration)
		}
		_, _ = fmt.Fprintf(os.Stdout, "  Workers:     %d/%d desired, %d healthy, %d ready, %d eligible\n",
			snap.Fleet.RegisteredCount, snap.Fleet.DesiredWorkers,
			snap.Fleet.HealthyCount, snap.Fleet.ReadyCount, snap.Fleet.EligibleCount)
		if snap.Fleet.CanaryWorkerCount > 0 || snap.Fleet.PromotedWorkerCount > 0 || snap.Fleet.DrainingWorkerCount > 0 {
			_, _ = fmt.Fprintf(os.Stdout, "  Waves:       canary=%d promoted=%d draining=%d\n",
				snap.Fleet.CanaryWorkerCount, snap.Fleet.PromotedWorkerCount, snap.Fleet.DrainingWorkerCount)
		}
		if snap.Fleet.ExcludedCount > 0 {
			_, _ = fmt.Fprintf(os.Stdout, "  Excluded:    %d total, %d quarantined\n", snap.Fleet.ExcludedCount, snap.Fleet.QuarantinedCount)
			if reasons := summarizeStatusReasons(snap.Fleet.ExcludedByReason); reasons != "" {
				_, _ = fmt.Fprintf(os.Stdout, "  Reasons:     %s\n", reasons)
			}
		}
		if snap.Fleet.RollbackReason != "" {
			_, _ = fmt.Fprintf(os.Stdout, "  Rollback:    %s\n", snap.Fleet.RollbackReason)
		}
		_, _ = fmt.Fprintf(os.Stdout, "  IPv4:        %d unique\n", snap.Fleet.UniqueIPv4Count)
		if snap.Fleet.UniqueEligibleIPv4Count > 0 {
			_, _ = fmt.Fprintf(os.Stdout, "  IPv4 Ready:  %d unique eligible\n", snap.Fleet.UniqueEligibleIPv4Count)
		}
		_, _ = fmt.Fprintf(os.Stdout, "  IPv6:        %d ready\n", snap.Fleet.IPv6ReadyCount)
		_, _ = fmt.Fprintln(os.Stdout)
	}

	if snap.CleanupPolicy != "" {
		_, _ = fmt.Fprintf(os.Stdout, "Cleanup:   %s\n", snap.CleanupPolicy)
	}
	if snap.ResultPrefix != "" {
		_, _ = fmt.Fprintf(os.Stdout, "Results:   %s\n", s3PrefixURI(snap.Bucket, snap.ResultPrefix))
	}
	if snap.ArtifactPrefix != "" {
		_, _ = fmt.Fprintf(os.Stdout, "Artifacts: %s\n", s3PrefixURI(snap.Bucket, snap.ArtifactPrefix))
	}
	if snap.LocalOutputDir != "" {
		_, _ = fmt.Fprintf(os.Stdout, "Local:     %s\n", snap.LocalOutputDir)
	}
	if snap.LastError != "" {
		_, _ = fmt.Fprintf(os.Stdout, "Error:     %s\n", snap.LastError)
	}
	return nil
}

// countResults queries storage for the current result count using the
// provider family recorded in the job record.
func countResults(ctx context.Context, bucket, prefix string, cloudKind cloud.Kind, log logger.Logger) (int, error) {
	provider, err := factory.BuildForKind(ctx, cloudKind, log)
	if err != nil {
		return 0, fmt.Errorf("building cloud provider: %w", err)
	}
	return provider.Storage().Count(ctx, bucket, prefix)
}

func s3PrefixURI(bucket, prefix string) string {
	if prefix == "" {
		return ""
	}
	if bucket == "" {
		return prefix
	}
	return fmt.Sprintf("s3://%s/%s", bucket, prefix)
}

func summarizeStatusReasons(counts map[string]int) string {
	return fleetSummaryReasons(counts)
}

func fleetSummaryReasons(counts map[string]int) string {
	if len(counts) == 0 {
		return ""
	}
	reasons := make([]string, 0, len(counts))
	for reason := range counts {
		reasons = append(reasons, reason)
	}
	sort.Strings(reasons)
	parts := make([]string, 0, len(reasons))
	for _, reason := range reasons {
		count := counts[reason]
		parts = append(parts, fmt.Sprintf("%s=%d", reason, count))
	}
	return strings.Join(parts, ", ")
}

func rolloutCanaryGeneration(rollout *fleetstate.RolloutRecord) string {
	if rollout == nil {
		return ""
	}
	return rollout.CanaryGeneration
}

func rolloutWorkerCount(rollout *fleetstate.RolloutRecord, bucket string) int {
	if rollout == nil {
		return 0
	}
	switch bucket {
	case "canary":
		return len(rollout.CanaryWorkerIndexes)
	case "promoted":
		return len(rollout.PromotedWorkerIndexes)
	case "draining":
		return len(rollout.DrainingWorkerIndexes)
	default:
		return 0
	}
}
