package scanruntime

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"heph4estus/internal/cloud"
	awscloud "heph4estus/internal/cloud/aws"
	"heph4estus/internal/cloud/factory"
	"heph4estus/internal/fleet"
	"heph4estus/internal/infra"
	"heph4estus/internal/jobs"
	"heph4estus/internal/logger"
	"heph4estus/internal/operator"
	"heph4estus/internal/worker"
)

const (
	SpotThreshold  = 50
	PollInterval   = 2 * time.Second
	EnqueueTimeout = 5 * time.Minute
	LaunchTimeout  = 5 * time.Minute
)

type StatusFunc func(format string, args ...interface{})

type ProviderBuilder func(context.Context, cloud.Kind, map[string]string, logger.Logger) (cloud.Provider, error)
type ProviderNativeFleetWaiter func(context.Context, cloud.Kind, map[string]string, fleet.PlacementPolicy) (int, error)
type EnsureInfraFunc func(context.Context, *infra.ToolConfig, infra.LifecyclePolicy, string, io.Writer, func(string) bool, logger.Logger) (*infra.EnsureResult, error)
type DestroyInfraFunc func(context.Context, *infra.ToolConfig, io.Writer, logger.Logger) error
type ExportJobFunc func(context.Context, cloud.Storage, string, string, string, string) (*operator.ExportResult, error)
type ResultRenderer func(context.Context, cloud.Storage, string, string) error

type SetupOptions struct {
	ToolName        string
	CloudKind       cloud.Kind
	Workers         int
	LifecyclePolicy infra.LifecyclePolicy
	PromptFunc      func(string) bool
	Stream          io.Writer
	Log             logger.Logger
	EnsureInfra     EnsureInfraFunc
}

type Environment struct {
	ToolConfig *infra.ToolConfig
	Outputs    infra.TerraformOutputs
	QueueURL   string
	Bucket     string
	Reused     bool
}

func Setup(ctx context.Context, opts SetupOptions) (*Environment, error) {
	ensureInfra := opts.EnsureInfra
	if ensureInfra == nil {
		ensureInfra = infra.EnsureInfra
	}

	kind := opts.CloudKind.Canonical()
	if kind == "" {
		kind = cloud.DefaultKind
	}

	var (
		toolCfg *infra.ToolConfig
		raw     map[string]string
		reused  bool
	)

	switch {
	case kind.IsProviderNative():
		cfg, err := infra.ResolveToolConfig(opts.ToolName, kind)
		if err != nil {
			return nil, err
		}
		cfg.TerraformVars[infra.OutputWorkerCount] = strconv.Itoa(opts.Workers)
		result, err := ensureInfra(ctx, cfg, opts.LifecyclePolicy, "", opts.Stream, opts.PromptFunc, opts.Log)
		if err != nil {
			return nil, err
		}
		toolCfg = cfg
		raw = result.Outputs
		reused = result.Reused

	case kind.IsSelfhostedFamily():
		cfg := factory.SelfhostedConfigFromEnv()
		if cfg.QueueID == "" || cfg.Bucket == "" {
			return nil, fmt.Errorf("%s requires SELFHOSTED_QUEUE_ID and SELFHOSTED_BUCKET environment variables", kind.Canonical())
		}
		raw = map[string]string{
			infra.OutputSQSQueueURL:  cfg.QueueID,
			infra.OutputS3BucketName: cfg.Bucket,
		}

	default:
		cfg, err := infra.ResolveToolConfig(opts.ToolName)
		if err != nil {
			return nil, err
		}
		result, err := ensureInfra(ctx, cfg, opts.LifecyclePolicy, infra.AWSRegion(), opts.Stream, opts.PromptFunc, opts.Log)
		if err != nil {
			return nil, err
		}
		toolCfg = cfg
		raw = result.Outputs
		reused = result.Reused
	}

	outputs := infra.DecodeTerraformOutputs(kind, raw)
	queueURL, bucket := queueAndBucket(outputs)
	if queueURL == "" || bucket == "" {
		return nil, fmt.Errorf("terraform outputs missing sqs_queue_url or s3_bucket_name")
	}

	return &Environment{
		ToolConfig: toolCfg,
		Outputs:    outputs,
		QueueURL:   queueURL,
		Bucket:     bucket,
		Reused:     reused,
	}, nil
}

type ProviderOptions struct {
	CloudKind       cloud.Kind
	Outputs         infra.TerraformOutputs
	Log             logger.Logger
	ProviderBuilder ProviderBuilder
}

func BuildProvider(ctx context.Context, opts ProviderOptions) (cloud.Provider, error) {
	builder := opts.ProviderBuilder
	if builder == nil {
		builder = DefaultProviderBuilder
	}
	return builder(ctx, opts.CloudKind, opts.Outputs.ToMap(), opts.Log)
}

func DefaultProviderBuilder(ctx context.Context, kind cloud.Kind, outputs map[string]string, log logger.Logger) (cloud.Provider, error) {
	if kind.IsProviderNative() && outputs != nil {
		typed := infra.DecodeTerraformOutputs(kind, outputs)
		return factory.Build(factory.Config{
			Kind:       kind,
			Selfhosted: factory.SelfhostedConfigFromTypedOutputs(typed.Selfhosted),
			Logger:     log,
		})
	}
	return factory.BuildForKind(ctx, kind, log)
}

type JobRecordOptions struct {
	Tracker       *operator.Tracker
	JobID         string
	ToolName      string
	Phase         operator.Phase
	TotalTasks    int
	Workers       int
	ComputeMode   string
	CloudKind     cloud.Kind
	CleanupPolicy string
	Bucket        string
	Outputs       infra.TerraformOutputs
	Placement     fleet.PlacementPolicy
}

func CreateJobRecord(opts JobRecordOptions) error {
	if opts.Tracker == nil {
		return nil
	}
	phase := opts.Phase
	if phase == "" {
		phase = operator.PhaseEnqueuing
	}
	record := &operator.JobRecord{
		JobID:                 opts.JobID,
		ToolName:              opts.ToolName,
		Phase:                 phase,
		TotalTasks:            opts.TotalTasks,
		WorkerCount:           opts.Workers,
		ComputeMode:           opts.ComputeMode,
		Cloud:                 string(opts.CloudKind),
		CleanupPolicy:         opts.CleanupPolicy,
		Bucket:                opts.Bucket,
		S3Endpoint:            firstNonEmpty(opts.Outputs.Selfhosted.S3Endpoint, opts.Outputs.AWS.S3Endpoint),
		S3Region:              firstNonEmpty(opts.Outputs.Selfhosted.S3Region, opts.Outputs.AWS.S3Region),
		S3AccessKey:           firstNonEmpty(opts.Outputs.Selfhosted.S3AccessKey, opts.Outputs.AWS.S3AccessKey),
		S3SecretKey:           firstNonEmpty(opts.Outputs.Selfhosted.S3SecretKey, opts.Outputs.AWS.S3SecretKey),
		S3PathStyle:           opts.Outputs.Selfhosted.S3PathStyle || opts.Outputs.AWS.S3PathStyle,
		Placement:             opts.Placement,
		ExpectedWorkerVersion: firstNonEmpty(opts.Outputs.Selfhosted.DockerImage, opts.Outputs.AWS.DockerImage),
		NATSUrl:               opts.Outputs.Selfhosted.NATSURL,
		ControllerIP:          opts.Outputs.Selfhosted.ControllerIP,
		GenerationID:          opts.Outputs.Selfhosted.GenerationID,
		ControllerCAPEM:       opts.Outputs.Selfhosted.ControllerCAPEM,
		ControllerHost:        opts.Outputs.Selfhosted.ControllerHost,
		NATSClientCertPEM:     opts.Outputs.Selfhosted.NATSOperatorClientCertPEM,
		NATSClientKeyPEM:      opts.Outputs.Selfhosted.NATSOperatorClientKeyPEM,
	}
	return opts.Tracker.Create(record)
}

type ExecuteOptions struct {
	ToolName      string
	JobID         string
	Tasks         []worker.Task
	EnqueueLabel  string
	Workers       int
	ComputeMode   string
	Queue         cloud.Queue
	Storage       cloud.Storage
	Compute       cloud.Compute
	Outputs       infra.TerraformOutputs
	QueueURL      string
	Bucket        string
	CloudKind     cloud.Kind
	Placement     fleet.PlacementPolicy
	Tracker       *operator.Tracker
	WorkerEnv     map[string]string
	ContainerName string
	ProgressLabel string
	CompleteLabel string
	RenderResults ResultRenderer
	Statusf       StatusFunc
	FleetWaiter   ProviderNativeFleetWaiter
}

func ExecuteQueuedScan(ctx context.Context, opts ExecuteOptions) (bool, error) {
	queueURL := opts.QueueURL
	bucket := opts.Bucket
	if queueURL == "" || bucket == "" {
		return false, fmt.Errorf("terraform outputs missing sqs_queue_url or s3_bucket_name")
	}

	label := opts.EnqueueLabel
	if label == "" {
		label = "tasks"
	}
	statusf := normalizeStatus(opts.Statusf)
	if opts.Tracker != nil {
		_ = opts.Tracker.UpdatePhase(opts.JobID, operator.PhaseEnqueuing)
	}
	statusf("Enqueueing %d %s...", len(opts.Tasks), label)
	enqueueCtx, enqueueCancel := context.WithTimeout(ctx, EnqueueTimeout)
	defer enqueueCancel()

	result, err := jobs.EnqueueTasks(enqueueCtx, opts.Queue, queueURL, opts.Tasks, jobs.EnqueueOptions{})
	if err != nil {
		return false, fmt.Errorf("enqueueing %s: %w", label, err)
	}
	statusf("Enqueued %d %s", result.SentTasks, label)

	if opts.Tracker != nil {
		_ = opts.Tracker.UpdatePhase(opts.JobID, operator.PhaseLaunching)
	}
	if err := LaunchWorkers(ctx, LaunchOptions{
		ToolName:      opts.ToolName,
		Workers:       opts.Workers,
		ComputeMode:   opts.ComputeMode,
		Compute:       opts.Compute,
		Outputs:       opts.Outputs,
		QueueURL:      queueURL,
		Bucket:        bucket,
		CloudKind:     opts.CloudKind,
		Placement:     opts.Placement,
		WorkerEnv:     opts.WorkerEnv,
		ContainerName: opts.ContainerName,
		Statusf:       statusf,
		FleetWaiter:   opts.FleetWaiter,
	}); err != nil {
		return false, err
	}

	if opts.Tracker != nil {
		_ = opts.Tracker.UpdatePhase(opts.JobID, operator.PhaseScanning)
	}
	return true, PollAndRender(ctx, PollOptions{
		Storage:       opts.Storage,
		Bucket:        bucket,
		ToolName:      opts.ToolName,
		JobID:         opts.JobID,
		TotalTasks:    len(opts.Tasks),
		ProgressLabel: opts.ProgressLabel,
		CompleteLabel: opts.CompleteLabel,
		RenderResults: opts.RenderResults,
		Statusf:       statusf,
	})
}

type LaunchOptions struct {
	ToolName      string
	Workers       int
	ComputeMode   string
	Compute       cloud.Compute
	Outputs       infra.TerraformOutputs
	QueueURL      string
	Bucket        string
	CloudKind     cloud.Kind
	Placement     fleet.PlacementPolicy
	WorkerEnv     map[string]string
	ContainerName string
	Statusf       StatusFunc
	FleetWaiter   ProviderNativeFleetWaiter
}

func LaunchWorkers(ctx context.Context, opts LaunchOptions) error {
	statusf := normalizeStatus(opts.Statusf)
	statusf("Launching %d workers (mode: %s)...", opts.Workers, opts.ComputeMode)
	launchCtx, launchCancel := context.WithTimeout(ctx, LaunchTimeout)
	defer launchCancel()

	kind := opts.CloudKind.Canonical()
	if kind.IsProviderNative() {
		if opts.FleetWaiter == nil {
			return fmt.Errorf("provider-native fleet waiter is required")
		}
		ready, err := opts.FleetWaiter(launchCtx, kind, opts.Outputs.ToMap(), opts.Placement)
		if err != nil {
			return err
		}
		statusf("Using provider-native %s fleet (%d eligible workers, policy: %s)", kind.Canonical(), ready, opts.Placement.Summary())
		return nil
	}

	workerEnv := opts.WorkerEnv
	if workerEnv == nil {
		workerEnv = map[string]string{
			"QUEUE_URL": opts.QueueURL,
			"S3_BUCKET": opts.Bucket,
			"TOOL_NAME": opts.ToolName,
		}
	}
	containerName := opts.ContainerName
	if containerName == "" {
		containerName = fmt.Sprintf("%s-worker", opts.ToolName)
	}

	useSpot := !kind.IsSelfhostedFamily() && ResolveComputeMode(opts.ComputeMode, opts.Workers)
	if useSpot {
		awsOut := opts.Outputs.AWS
		if strings.TrimSpace(awsOut.ImageTag) == "" {
			return fmt.Errorf("terraform outputs missing image_tag")
		}
		userData := awscloud.GenerateUserData(awscloud.UserDataOpts{
			ECRRepoURL: awsOut.ECRRepoURL,
			ImageTag:   awsOut.ImageTag,
			Region:     RegionFromECR(awsOut.ECRRepoURL),
			EnvVars:    workerEnv,
		})
		ids, err := opts.Compute.RunSpotInstances(launchCtx, cloud.SpotOpts{
			AMI:             awsOut.AMIID,
			Count:           opts.Workers,
			SecurityGroups:  []string{awsOut.SecurityGroupID},
			SubnetIDs:       awsOut.SubnetIDs,
			InstanceProfile: awsOut.InstanceProfileARN,
			UserData:        userData,
			Tags: map[string]string{
				"Project": "heph4estus",
				"Tool":    opts.ToolName,
			},
		})
		if err != nil {
			return fmt.Errorf("launching spot instances: %w", err)
		}
		statusf("Launched %d spot instances", len(ids))
		return nil
	}

	awsOut := opts.Outputs.AWS
	_, err := opts.Compute.RunContainer(launchCtx, cloud.ContainerOpts{
		Cluster:        awsOut.ECSClusterName,
		TaskDefinition: awsOut.TaskDefinitionARN,
		ContainerName:  containerName,
		Subnets:        awsOut.SubnetIDs,
		SecurityGroups: []string{awsOut.SecurityGroupID},
		Env:            workerEnv,
		Count:          opts.Workers,
	})
	if err != nil {
		return fmt.Errorf("launching workers: %w", err)
	}
	statusf("Launched %d workers", opts.Workers)
	return nil
}

type PollOptions struct {
	Storage       cloud.Storage
	Bucket        string
	ToolName      string
	JobID         string
	TotalTasks    int
	ProgressLabel string
	CompleteLabel string
	RenderResults ResultRenderer
	Statusf       StatusFunc
}

func PollAndRender(ctx context.Context, opts PollOptions) error {
	statusf := normalizeStatus(opts.Statusf)
	statusf("Scanning...")
	startTime := time.Now()
	scanPrefix := jobs.ResultPrefix(opts.ToolName, opts.JobID)

	for {
		count, err := opts.Storage.Count(ctx, opts.Bucket, scanPrefix)
		if err != nil {
			statusf("Warning: progress check failed: %v", err)
		} else {
			elapsed := time.Since(startTime).Truncate(time.Second)
			pct := float64(count) / float64(opts.TotalTasks) * 100
			if opts.ProgressLabel != "" {
				statusf("Progress: %d/%d %s (%.1f%%) — elapsed %s", count, opts.TotalTasks, opts.ProgressLabel, pct, elapsed)
			} else {
				statusf("Progress: %d/%d (%.1f%%) — elapsed %s", count, opts.TotalTasks, pct, elapsed)
			}
			if count >= opts.TotalTasks {
				break
			}
		}
		time.Sleep(PollInterval)
	}

	elapsed := time.Since(startTime).Truncate(time.Second)
	if opts.CompleteLabel != "" {
		statusf("Scan complete: %d %s in %s", opts.TotalTasks, opts.CompleteLabel, elapsed)
	} else {
		statusf("Scan complete: %d tasks in %s", opts.TotalTasks, elapsed)
	}
	if opts.RenderResults == nil {
		return nil
	}
	return opts.RenderResults(ctx, opts.Storage, opts.Bucket, scanPrefix)
}

type FinalizeOptions struct {
	JobID        string
	ToolName     string
	Tracker      *operator.Tracker
	Started      bool
	ScanErr      error
	OutDir       string
	Storage      cloud.Storage
	Bucket       string
	DestroyAfter bool
	CloudKind    cloud.Kind
	ToolConfig   *infra.ToolConfig
	Stream       io.Writer
	Log          logger.Logger
	Statusf      StatusFunc
	ExportJob    ExportJobFunc
	DestroyInfra DestroyInfraFunc
}

type FinalizeResult struct {
	ExportDir string
}

func Finalize(ctx context.Context, opts FinalizeOptions) (FinalizeResult, error) {
	if opts.Tracker != nil {
		if opts.ScanErr != nil {
			_ = opts.Tracker.Fail(opts.JobID, opts.ScanErr)
		} else if opts.Started {
			_ = opts.Tracker.Complete(opts.JobID)
		}
	}

	statusf := normalizeStatus(opts.Statusf)
	exportJob := opts.ExportJob
	if exportJob == nil {
		exportJob = operator.ExportJob
	}
	destroyInfra := opts.DestroyInfra
	if destroyInfra == nil {
		destroyInfra = infra.RunDestroy
	}

	var result FinalizeResult
	if opts.OutDir != "" && opts.ScanErr == nil && opts.Started {
		statusf("Exporting results to %s...", opts.OutDir)
		exportResult, err := exportJob(ctx, opts.Storage, opts.Bucket, opts.ToolName, opts.JobID, opts.OutDir)
		if err != nil {
			return result, fmt.Errorf("export failed: %w", err)
		}
		result.ExportDir = exportResult.Dir
		statusf("Exported %d results, %d artifacts to %s", exportResult.ResultCount, exportResult.ArtifactCount, exportResult.Dir)
		updateJobRecord(opts.Tracker, opts.JobID, func(rec *operator.JobRecord) {
			rec.LocalOutputDir = exportResult.Dir
		})
	}

	if opts.DestroyAfter && opts.Started {
		kind := opts.CloudKind.Canonical()
		if kind.IsSelfhostedFamily() && !kind.IsProviderNative() {
			statusf("Skipping destroy: %s does not support auto-destroy", kind.Canonical())
		} else if opts.ToolConfig != nil {
			statusf("Destroying infrastructure (--destroy-after)...")
			if err := destroyInfra(ctx, opts.ToolConfig, opts.Stream, opts.Log); err != nil {
				if opts.ScanErr != nil {
					return result, fmt.Errorf("scan failed: %w; additionally, destroy failed: %v", opts.ScanErr, err)
				}
				return result, fmt.Errorf("scan completed but destroy failed: %w", err)
			}
		}
	}
	return result, nil
}

func CleanupPolicy(destroyAfter bool) string {
	if destroyAfter {
		return "destroy-after"
	}
	return "reuse"
}

func ResolveComputeMode(mode string, workers int) bool {
	switch mode {
	case "spot":
		return true
	case "fargate":
		return false
	default:
		return workers >= SpotThreshold
	}
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

func SplitOutputList(s string) []string {
	s = strings.Trim(s, "[]")
	parts := strings.Split(s, " ")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

func queueAndBucket(outputs infra.TerraformOutputs) (string, string) {
	queueURL := firstNonEmpty(outputs.AWS.SQSQueueURL, outputs.Selfhosted.QueueURL)
	bucket := firstNonEmpty(outputs.AWS.S3BucketName, outputs.Selfhosted.S3BucketName)
	return queueURL, bucket
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func updateJobRecord(tracker *operator.Tracker, jobID string, mutate func(*operator.JobRecord)) {
	if tracker == nil || mutate == nil {
		return
	}
	store := tracker.Store()
	if store == nil {
		return
	}
	rec, err := store.Load(jobID)
	if err != nil {
		return
	}
	mutate(rec)
	_ = store.Update(rec)
}

func normalizeStatus(statusf StatusFunc) StatusFunc {
	if statusf != nil {
		return statusf
	}
	return func(string, ...interface{}) {}
}
