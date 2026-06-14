package statuscore

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"heph4estus/internal/cloud"
	awscloud "heph4estus/internal/cloud/aws"
	"heph4estus/internal/jobs"
	"heph4estus/internal/operator"
	"heph4estus/internal/tui/core"
)

const (
	SpotThreshold    = 50
	CounterThreshold = 10_000
	PollInterval     = 2 * time.Second
)

type Phase int

const (
	PhaseUploading Phase = iota
	PhaseEnqueuing
	PhaseLaunching
	PhaseScanning
	PhaseExporting
	PhaseDestroying
	PhaseComplete
)

type RateSample struct {
	Time  time.Time
	Count int
}

type WorkerLauncher interface {
	LaunchWorkers(ctx context.Context, opts cloud.ContainerOpts) (string, error)
	LaunchSpotWorkers(ctx context.Context, opts cloud.SpotOpts) ([]string, error)
}

type ProgressTracker interface {
	CountResults(ctx context.Context, bucket, prefix string) (int, error)
}

type LaunchOptions struct {
	Infra         core.InfraOutputs
	Launcher      WorkerLauncher
	ContainerName string
	ToolName      string
	WorkerEnv     map[string]string
}

type LaunchResult struct {
	Launched    int
	Total       int
	Err         error
	Spot        bool
	InstanceIDs []string
}

type ExportResult struct {
	Dir   string
	Count int
	Err   error
}

type DestroyResult struct {
	Err error
}

type ProgressResult struct {
	Completed int
	Err       error
}

type CompletionAction int

const (
	CompletionContinue CompletionAction = iota
	CompletionNavigate
	CompletionExport
	CompletionDestroy
)

type CompletionResult struct {
	Action  CompletionAction
	Warning string
}

func UseSpot(infra core.InfraOutputs) bool {
	if infra.Cloud.IsSelfhostedFamily() {
		return false
	}
	switch infra.ComputeMode {
	case "spot":
		return true
	case "fargate":
		return false
	default:
		return infra.WorkerCount >= SpotThreshold
	}
}

func DefaultWorkerEnv(infra core.InfraOutputs, toolName string) map[string]string {
	toolName = resolveToolName(infra, toolName)
	return map[string]string{
		"QUEUE_URL":          infra.SQSQueueURL,
		"S3_BUCKET":          infra.S3BucketName,
		"TOOL_NAME":          toolName,
		"JITTER_MAX_SECONDS": strconv.Itoa(infra.JitterMaxSeconds),
	}
}

func Launch(ctx context.Context, opts LaunchOptions) LaunchResult {
	infra := opts.Infra
	total := infra.WorkerCount
	toolName := resolveToolName(infra, opts.ToolName)
	if UseSpot(infra) {
		return launchSpot(ctx, opts)
	}
	if infra.Cloud.IsProviderNative() {
		launched := infra.FleetWorkerCount
		if launched <= 0 {
			launched = infra.WorkerCount
		}
		if launched <= 0 {
			launched = 1
		}
		return LaunchResult{Launched: launched, Total: launched}
	}

	env := opts.WorkerEnv
	if env == nil {
		env = DefaultWorkerEnv(infra, toolName)
	}
	containerName := opts.ContainerName
	if containerName == "" {
		containerName = fmt.Sprintf("%s-worker", toolName)
	}
	_, err := opts.Launcher.LaunchWorkers(ctx, cloud.ContainerOpts{
		Cluster:        infra.ECSClusterName,
		TaskDefinition: infra.TaskDefinitionARN,
		ContainerName:  containerName,
		Subnets:        infra.SubnetIDs,
		SecurityGroups: []string{infra.SecurityGroupID},
		Env:            env,
		Count:          infra.WorkerCount,
	})
	return LaunchResult{Launched: infra.WorkerCount, Total: total, Err: err}
}

func launchSpot(ctx context.Context, opts LaunchOptions) LaunchResult {
	infra := opts.Infra
	total := infra.WorkerCount
	toolName := resolveToolName(infra, opts.ToolName)
	imageTag := strings.TrimSpace(infra.ImageTag)
	if imageTag == "" {
		return LaunchResult{Total: total, Err: fmt.Errorf("terraform outputs missing image_tag"), Spot: true}
	}
	env := opts.WorkerEnv
	if env == nil {
		env = DefaultWorkerEnv(infra, toolName)
	}
	userData := awscloud.GenerateUserData(awscloud.UserDataOpts{
		ECRRepoURL: infra.ECRRepoURL,
		ImageTag:   imageTag,
		Region:     RegionFromECR(infra.ECRRepoURL),
		EnvVars:    env,
	})
	ids, err := opts.Launcher.LaunchSpotWorkers(ctx, cloud.SpotOpts{
		AMI:             infra.AMIID,
		Count:           infra.WorkerCount,
		SecurityGroups:  []string{infra.SecurityGroupID},
		SubnetIDs:       infra.SubnetIDs,
		InstanceProfile: infra.InstanceProfileARN,
		UserData:        userData,
		Tags: map[string]string{
			"Project": "heph4estus",
			"Tool":    toolName,
		},
	})
	return LaunchResult{Launched: len(ids), Total: total, Err: err, Spot: true, InstanceIDs: ids}
}

func resolveToolName(infra core.InfraOutputs, toolName string) string {
	if toolName != "" {
		return toolName
	}
	return infra.ToolName
}

func ShouldExport(infra core.InfraOutputs, storage cloud.Storage) bool {
	return infra.CleanupPolicy == "destroy-after" && infra.OutputDir != "" && storage != nil
}

func CompleteScan(infra core.InfraOutputs, storage cloud.Storage) CompletionResult {
	if ShouldExport(infra, storage) {
		return CompletionResult{Action: CompletionExport}
	}
	if infra.CleanupPolicy == "destroy-after" {
		if infra.Cloud.IsSelfhostedFamily() && !infra.Cloud.IsProviderNative() {
			return CompletionResult{
				Action:  CompletionNavigate,
				Warning: "destroy-after skipped: selfhosted does not support auto-destroy",
			}
		}
		if infra.OutputDir == "" {
			return CompletionResult{
				Action:  CompletionNavigate,
				Warning: "destroy-after skipped: no output directory configured",
			}
		}
	}
	return CompletionResult{Action: CompletionNavigate}
}

func CompleteExport(infra *core.InfraOutputs, destroyer core.Destroyer, result ExportResult) CompletionResult {
	if result.Err != nil {
		return CompletionResult{
			Action:  CompletionNavigate,
			Warning: fmt.Sprintf("destroy-after skipped: export failed (%v)", result.Err),
		}
	}
	infra.Exported = true
	infra.ExportDir = result.Dir
	if infra.Cloud.IsSelfhostedFamily() && !infra.Cloud.IsProviderNative() {
		return CompletionResult{
			Action:  CompletionNavigate,
			Warning: "destroy-after skipped: selfhosted does not support auto-destroy",
		}
	}
	if destroyer != nil {
		return CompletionResult{Action: CompletionDestroy}
	}
	return CompletionResult{
		Action:  CompletionNavigate,
		Warning: "destroy-after skipped: no terraform directory",
	}
}

func CompleteDestroy(infra *core.InfraOutputs, result DestroyResult) string {
	if result.Err != nil {
		infra.DestroyErr = result.Err.Error()
		return fmt.Sprintf("destroy failed: %v", result.Err)
	}
	infra.Destroyed = true
	return ""
}

func ExportResults(ctx context.Context, storage cloud.Storage, infra core.InfraOutputs, toolName string) ExportResult {
	if toolName == "" {
		toolName = infra.ToolName
	}
	result, err := operator.ExportJob(ctx, storage, infra.S3BucketName, toolName, infra.JobID, infra.OutputDir)
	if err != nil {
		return ExportResult{Err: err}
	}
	return ExportResult{Dir: result.Dir, Count: result.ResultCount + result.ArtifactCount}
}

func RunAutoDestroy(ctx context.Context, destroyer core.Destroyer) DestroyResult {
	return DestroyResult{Err: destroyer.Destroy(ctx)}
}

func PollProgress(ctx context.Context, tracker ProgressTracker, infra core.InfraOutputs, toolName string) ProgressResult {
	if toolName == "" {
		toolName = infra.ToolName
	}
	count, err := tracker.CountResults(ctx, infra.S3BucketName, jobs.ResultPrefix(toolName, infra.JobID))
	return ProgressResult{Completed: count, Err: err}
}

func NavigateToResults(target core.ViewID, infra core.InfraOutputs) tea.Cmd {
	return func() tea.Msg {
		return core.NavigateWithDataMsg{Target: target, Data: infra}
	}
}

func TrackPhase(tracker *operator.Tracker, jobID string, phase operator.Phase) {
	if tracker != nil && jobID != "" {
		_ = tracker.UpdatePhase(jobID, phase)
	}
}

func TrackFail(tracker *operator.Tracker, jobID string, err error) {
	if tracker != nil && jobID != "" {
		_ = tracker.Fail(jobID, err)
	}
}

func LifecycleSummary(infra core.InfraOutputs, labelStyle lipgloss.Style) string {
	var b strings.Builder
	infraLabel := "freshly deployed"
	if infra.Reused {
		infraLabel = "reused"
	}
	fmt.Fprintf(&b, "  %s%s\n", labelStyle.Render("Infra:"), infraLabel)
	if infra.CleanupPolicy != "" {
		fmt.Fprintf(&b, "  %s%s\n", labelStyle.Render("Cleanup:"), infra.CleanupPolicy)
	}
	if infra.Placement.Summary() != "" {
		fmt.Fprintf(&b, "  %s%s\n", labelStyle.Render("Placement:"), infra.Placement.Summary())
	}
	return b.String()
}

func WarningText(warning string) string {
	if warning == "" {
		return ""
	}
	return "\n  " + core.MutedStyle.Render(warning) + "\n"
}

func UpdateRateSamples(samples []RateSample, completed int, now time.Time) []RateSample {
	samples = append(samples, RateSample{Time: now, Count: completed})
	cutoff := now.Add(-30 * time.Second)
	for len(samples) > 1 && samples[0].Time.Before(cutoff) {
		samples = samples[1:]
	}
	return samples
}

func CalcRateETA(samples []RateSample, total, completed int) (targetsPerMin float64, remaining time.Duration) {
	if len(samples) < 2 {
		return 0, 0
	}
	first := samples[0]
	last := samples[len(samples)-1]
	dt := last.Time.Sub(first.Time).Minutes()
	if dt <= 0 {
		return 0, 0
	}
	dc := float64(last.Count - first.Count)
	rate := dc / dt
	if rate <= 0 {
		return 0, 0
	}
	left := float64(total - completed)
	eta := time.Duration(left/rate*60) * time.Second
	return rate, eta
}

func ProgressBar(current, total, width int) string {
	if total <= 0 {
		return strings.Repeat("░", width)
	}
	filled := min(current*width/total, width)
	return "[" + strings.Repeat("█", filled) + strings.Repeat("░", width-filled) + "]"
}

func RegionFromECR(url string) string {
	parts := strings.Split(url, ".")
	for i, p := range parts {
		if p == "ecr" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return "us-east-1"
}
