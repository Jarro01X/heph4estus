package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"time"

	"heph4estus/internal/cloud"
	"heph4estus/internal/cloud/factory"
	"heph4estus/internal/logger"
	"heph4estus/internal/operator"
)

// statusSnapshot is the structured output of heph status.
type statusSnapshot struct {
	JobID          string         `json:"job_id"`
	Tool           string         `json:"tool"`
	Phase          operator.Phase `json:"phase"`
	Bucket         string         `json:"bucket,omitempty"`
	Progress       statusProgress `json:"progress"`
	Elapsed        string         `json:"elapsed"`
	CleanupPolicy  string         `json:"cleanup_policy,omitempty"`
	ResultPrefix   string         `json:"result_prefix,omitempty"`
	ArtifactPrefix string         `json:"artifact_prefix,omitempty"`
	LocalOutputDir string         `json:"local_output_dir,omitempty"`
	LastError      string         `json:"last_error,omitempty"`
}

type statusProgress struct {
	Completed int     `json:"completed"`
	Total     int     `json:"total"`
	Percent   float64 `json:"percent"`
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

	// Query live cloud progress if the job has a bucket and result prefix.
	completed := 0
	if rec.Bucket != "" && rec.ResultPrefix != "" && !isTerminalPhase(rec.Phase) {
		ctx := context.Background()
		count, cloudErr := countResults(ctx, rec.Bucket, rec.ResultPrefix, cloudKind, log)
		if cloudErr != nil {
			log.Error("Warning: could not query live progress: %v", cloudErr)
		} else {
			completed = count
		}
	}

	snap := buildSnapshot(rec, completed)

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
	_, _ = fmt.Fprintf(os.Stdout, "Progress:  %d / %d  (%.1f%%)\n", snap.Progress.Completed, snap.Progress.Total, snap.Progress.Percent)
	_, _ = fmt.Fprintf(os.Stdout, "Elapsed:   %s\n", snap.Elapsed)

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
