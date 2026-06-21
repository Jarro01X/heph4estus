package scanapp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"heph4estus/internal/cloud"
	"heph4estus/internal/fleet"
	"heph4estus/internal/infra"
	"heph4estus/internal/jobs"
	"heph4estus/internal/logger"
	"heph4estus/internal/modules"
	"heph4estus/internal/operator"
	"heph4estus/internal/scanruntime"
	nmaptool "heph4estus/internal/tools/nmap"
	targetlisttool "heph4estus/internal/tools/targetlist"
	wordlisttool "heph4estus/internal/tools/wordlist"
	"heph4estus/internal/worker"
)

// RunResult summarizes a CLI scan run for command-layer output decisions.
type RunResult struct {
	JobID         string
	Tool          string
	Started       bool
	Reused        bool
	CleanupPolicy string
	ExportDir     string
}

// GenericOptions contains the already parsed and validated generic scan intent.
type GenericOptions struct {
	ToolName        string
	InputFile       string
	WordlistFile    string
	RuntimeTarget   string
	ToolOptions     string
	Chunks          int
	Workers         int
	ComputeMode     string
	Format          string
	OutDir          string
	CloudKind       cloud.Kind
	Placement       fleet.PlacementPolicy
	LifecyclePolicy infra.LifecyclePolicy
	Module          *modules.ModuleDefinition
	PromptFunc      func(string) bool
	Stream          io.Writer
	Output          io.Writer
	Log             logger.Logger
	Statusf         scanruntime.StatusFunc
	ProviderBuilder scanruntime.ProviderBuilder
	FleetWaiter     scanruntime.ProviderNativeFleetWaiter
	Tracker         *operator.Tracker
}

// NmapOptions contains the already parsed and validated nmap scan intent.
type NmapOptions struct {
	InputFile       string
	DefaultOptions  string
	Workers         int
	ComputeMode     string
	Placement       fleet.PlacementPolicy
	Mode            string
	PortChunks      int
	DNSServers      string
	TimingTemplate  string
	JitterMax       int
	NoRDNS          bool
	Format          string
	OutDir          string
	CloudKind       cloud.Kind
	LifecyclePolicy infra.LifecyclePolicy
	PromptFunc      func(string) bool
	Stream          io.Writer
	Output          io.Writer
	Log             logger.Logger
	Statusf         scanruntime.StatusFunc
	ProviderBuilder scanruntime.ProviderBuilder
	FleetWaiter     scanruntime.ProviderNativeFleetWaiter
	Tracker         *operator.Tracker
}

// TargetListOptions executes a planned target-list scan against injected deps.
type TargetListOptions struct {
	ToolName    string
	JobID       string
	InputFile   string
	Preflight   *targetlisttool.Metadata
	Module      *modules.ModuleDefinition
	ToolOptions string
	Workers     int
	ComputeMode string
	Format      string
	Queue       cloud.Queue
	Storage     cloud.Storage
	Compute     cloud.Compute
	Outputs     infra.TerraformOutputs
	Bucket      string
	QueueURL    string
	Tracker     *operator.Tracker
	CloudKind   cloud.Kind
	Placement   fleet.PlacementPolicy
	Output      io.Writer
	Statusf     scanruntime.StatusFunc
	FleetWaiter scanruntime.ProviderNativeFleetWaiter
}

// WordlistOptions executes a planned wordlist scan against injected deps.
type WordlistOptions struct {
	ToolName      string
	JobID         string
	WordlistFile  string
	Preflight     *wordlisttool.Metadata
	RuntimeTarget string
	ToolOptions   string
	Chunks        int
	Workers       int
	ComputeMode   string
	Format        string
	Queue         cloud.Queue
	Storage       cloud.Storage
	Compute       cloud.Compute
	Outputs       infra.TerraformOutputs
	Bucket        string
	QueueURL      string
	Tracker       *operator.Tracker
	CloudKind     cloud.Kind
	Placement     fleet.PlacementPolicy
	Output        io.Writer
	Statusf       scanruntime.StatusFunc
	FleetWaiter   scanruntime.ProviderNativeFleetWaiter
}

// NmapScanOptions executes parsed nmap tasks against injected deps.
type NmapScanOptions struct {
	Tasks       []nmaptool.ScanTask
	Workers     int
	ComputeMode string
	JitterMax   int
	Format      string
	Outputs     infra.TerraformOutputs
	Queue       cloud.Queue
	Storage     cloud.Storage
	Compute     cloud.Compute
	Tracker     *operator.Tracker
	JobID       string
	Placement   fleet.PlacementPolicy
	CloudKind   cloud.Kind
	Output      io.Writer
	Statusf     scanruntime.StatusFunc
	FleetWaiter scanruntime.ProviderNativeFleetWaiter
}

// DefaultTracker returns a tracker backed by the default local job store.
func DefaultTracker() *operator.Tracker {
	store, err := operator.NewJobStore()
	if err != nil {
		return operator.NoopTracker()
	}
	return operator.NewTracker(store)
}

func RunGeneric(ctx context.Context, opts GenericOptions) (RunResult, error) {
	opts.Log = loggerOrDefault(opts.Log)
	opts.Stream = writerOrDefault(opts.Stream, os.Stderr)
	opts.Output = writerOrDefault(opts.Output, os.Stdout)
	opts.Statusf = statusOrNoop(opts.Statusf)
	if opts.Tracker == nil {
		opts.Tracker = DefaultTracker()
	}

	var (
		targetMeta   *targetlisttool.Metadata
		wordlistMeta *wordlisttool.Metadata
		err          error
	)
	if opts.Module != nil && opts.Module.InputType == modules.InputTypeWordlist {
		wordlistMeta, err = PreflightWordlistFile(opts.ToolName, opts.WordlistFile, opts.RuntimeTarget, opts.ToolOptions, opts.Chunks, opts.Workers)
	} else {
		targetMeta, err = PreflightTargetListFile(opts.InputFile, opts.Workers)
	}
	if err != nil {
		return RunResult{}, err
	}

	env, err := scanruntime.Setup(ctx, scanruntime.SetupOptions{
		ToolName:        opts.ToolName,
		CloudKind:       opts.CloudKind,
		Workers:         opts.Workers,
		LifecyclePolicy: opts.LifecyclePolicy,
		PromptFunc:      opts.PromptFunc,
		Stream:          opts.Stream,
		Log:             opts.Log,
	})
	if err != nil {
		return RunResult{}, err
	}

	provider, err := scanruntime.BuildProvider(ctx, scanruntime.ProviderOptions{
		CloudKind:       opts.CloudKind,
		Outputs:         env.Outputs,
		Log:             opts.Log,
		ProviderBuilder: opts.ProviderBuilder,
	})
	if err != nil {
		return RunResult{}, fmt.Errorf("building cloud provider: %w", err)
	}

	jobID := jobs.NewID(opts.ToolName)
	cleanupPolicy := scanruntime.CleanupPolicy(opts.LifecyclePolicy.DestroyAfter)
	_ = scanruntime.CreateJobRecord(scanruntime.JobRecordOptions{
		Tracker:       opts.Tracker,
		JobID:         jobID,
		ToolName:      opts.ToolName,
		Workers:       opts.Workers,
		ComputeMode:   opts.ComputeMode,
		CloudKind:     opts.CloudKind,
		CleanupPolicy: cleanupPolicy,
		Bucket:        env.Bucket,
		Outputs:       env.Outputs,
		Placement:     opts.Placement,
	})

	var started bool
	var scanErr error
	if opts.Module != nil && opts.Module.InputType == modules.InputTypeWordlist {
		started, scanErr = RunWordlistScan(ctx, WordlistOptions{
			ToolName:      opts.ToolName,
			JobID:         jobID,
			WordlistFile:  opts.WordlistFile,
			Preflight:     wordlistMeta,
			RuntimeTarget: opts.RuntimeTarget,
			ToolOptions:   opts.ToolOptions,
			Chunks:        opts.Chunks,
			Workers:       opts.Workers,
			ComputeMode:   opts.ComputeMode,
			Format:        opts.Format,
			Queue:         provider.Queue(),
			Storage:       provider.Storage(),
			Compute:       provider.Compute(),
			Outputs:       env.Outputs,
			Bucket:        env.Bucket,
			QueueURL:      env.QueueURL,
			Tracker:       opts.Tracker,
			CloudKind:     opts.CloudKind,
			Placement:     opts.Placement,
			Output:        opts.Output,
			Statusf:       opts.Statusf,
			FleetWaiter:   opts.FleetWaiter,
		})
	} else {
		started, scanErr = RunTargetListScan(ctx, TargetListOptions{
			ToolName:    opts.ToolName,
			JobID:       jobID,
			InputFile:   opts.InputFile,
			Preflight:   targetMeta,
			Module:      opts.Module,
			ToolOptions: opts.ToolOptions,
			Workers:     opts.Workers,
			ComputeMode: opts.ComputeMode,
			Format:      opts.Format,
			Queue:       provider.Queue(),
			Storage:     provider.Storage(),
			Compute:     provider.Compute(),
			Outputs:     env.Outputs,
			Bucket:      env.Bucket,
			QueueURL:    env.QueueURL,
			Tracker:     opts.Tracker,
			CloudKind:   opts.CloudKind,
			Placement:   opts.Placement,
			Output:      opts.Output,
			Statusf:     opts.Statusf,
			FleetWaiter: opts.FleetWaiter,
		})
	}

	finalized, finalizeErr := scanruntime.Finalize(ctx, scanruntime.FinalizeOptions{
		JobID:        jobID,
		ToolName:     opts.ToolName,
		Tracker:      opts.Tracker,
		Started:      started,
		ScanErr:      scanErr,
		OutDir:       opts.OutDir,
		Storage:      provider.Storage(),
		Bucket:       env.Bucket,
		DestroyAfter: opts.LifecyclePolicy.DestroyAfter,
		CloudKind:    opts.CloudKind,
		ToolConfig:   env.ToolConfig,
		Stream:       opts.Stream,
		Log:          opts.Log,
		Statusf:      opts.Statusf,
	})
	if finalizeErr != nil {
		return RunResult{}, finalizeErr
	}

	return RunResult{
		JobID:         jobID,
		Tool:          opts.ToolName,
		Started:       started,
		Reused:        env.Reused,
		CleanupPolicy: cleanupPolicy,
		ExportDir:     finalized.ExportDir,
	}, scanErr
}

func RunNmap(ctx context.Context, opts NmapOptions) (RunResult, error) {
	opts.Log = loggerOrDefault(opts.Log)
	opts.Stream = writerOrDefault(opts.Stream, os.Stderr)
	opts.Output = writerOrDefault(opts.Output, os.Stdout)
	opts.Statusf = statusOrNoop(opts.Statusf)
	if opts.Tracker == nil {
		opts.Tracker = DefaultTracker()
	}

	content, err := os.ReadFile(opts.InputFile)
	if err != nil {
		return RunResult{}, fmt.Errorf("reading target file: %w", err)
	}

	scanner := nmaptool.NewScanner(opts.Log)
	tasks := scanner.ParseTargetsWithMode(string(content), opts.DefaultOptions, opts.Mode, opts.PortChunks)
	for i := range tasks {
		if opts.NoRDNS {
			tasks[i].Options = "-n " + tasks[i].Options
		}
		if opts.TimingTemplate != "" {
			tasks[i].Options = fmt.Sprintf("-T%s %s", opts.TimingTemplate, tasks[i].Options)
		}
		if opts.DNSServers != "" {
			tasks[i].Options = fmt.Sprintf("--dns-servers %s %s", opts.DNSServers, tasks[i].Options)
		}
	}
	if len(tasks) == 0 {
		return RunResult{}, fmt.Errorf("no targets found in %s", opts.InputFile)
	}

	jobID := jobs.NewID("nmap")
	for i := range tasks {
		tasks[i].JobID = jobID
	}
	if opts.Mode == "target-ports" {
		opts.Statusf("Mode: target-ports — %d target groups, %d total tasks (%d chunks/target) [job %s]", countGroups(tasks), len(tasks), opts.PortChunks, jobID)
	} else {
		opts.Statusf("Parsed %d targets from %s [job %s]", len(tasks), opts.InputFile, jobID)
	}

	env, err := scanruntime.Setup(ctx, scanruntime.SetupOptions{
		ToolName:        "nmap",
		CloudKind:       opts.CloudKind,
		Workers:         opts.Workers,
		LifecyclePolicy: opts.LifecyclePolicy,
		PromptFunc:      opts.PromptFunc,
		Stream:          opts.Stream,
		Log:             opts.Log,
	})
	if err != nil {
		return RunResult{}, err
	}

	cleanupPolicy := scanruntime.CleanupPolicy(opts.LifecyclePolicy.DestroyAfter)
	_ = scanruntime.CreateJobRecord(scanruntime.JobRecordOptions{
		Tracker:       opts.Tracker,
		JobID:         jobID,
		ToolName:      "nmap",
		TotalTasks:    len(tasks),
		Workers:       opts.Workers,
		ComputeMode:   opts.ComputeMode,
		CloudKind:     opts.CloudKind,
		CleanupPolicy: cleanupPolicy,
		Bucket:        env.Bucket,
		Outputs:       env.Outputs,
		Placement:     opts.Placement,
	})

	var (
		provider cloud.Provider
		scanErr  error
		started  bool
	)
	provider, err = scanruntime.BuildProvider(ctx, scanruntime.ProviderOptions{
		CloudKind:       opts.CloudKind,
		Outputs:         env.Outputs,
		Log:             opts.Log,
		ProviderBuilder: opts.ProviderBuilder,
	})
	if err != nil {
		scanErr = fmt.Errorf("building cloud provider: %w", err)
	} else {
		started, scanErr = RunNmapScan(ctx, NmapScanOptions{
			Tasks:       tasks,
			Workers:     opts.Workers,
			ComputeMode: opts.ComputeMode,
			JitterMax:   opts.JitterMax,
			Format:      opts.Format,
			Outputs:     env.Outputs,
			Queue:       provider.Queue(),
			Storage:     provider.Storage(),
			Compute:     provider.Compute(),
			Tracker:     opts.Tracker,
			JobID:       jobID,
			Placement:   opts.Placement,
			CloudKind:   opts.CloudKind,
			Output:      opts.Output,
			Statusf:     opts.Statusf,
			FleetWaiter: opts.FleetWaiter,
		})
	}

	var storage cloud.Storage
	if provider != nil {
		storage = provider.Storage()
	}
	finalized, finalizeErr := scanruntime.Finalize(ctx, scanruntime.FinalizeOptions{
		JobID:        jobID,
		ToolName:     "nmap",
		Tracker:      opts.Tracker,
		Started:      started,
		ScanErr:      scanErr,
		OutDir:       opts.OutDir,
		Storage:      storage,
		Bucket:       env.Bucket,
		DestroyAfter: opts.LifecyclePolicy.DestroyAfter,
		CloudKind:    opts.CloudKind,
		ToolConfig:   env.ToolConfig,
		Stream:       opts.Stream,
		Log:          opts.Log,
		Statusf:      opts.Statusf,
	})
	if finalizeErr != nil {
		return RunResult{}, finalizeErr
	}

	return RunResult{
		JobID:         jobID,
		Tool:          "nmap",
		Started:       started,
		Reused:        env.Reused,
		CleanupPolicy: cleanupPolicy,
		ExportDir:     finalized.ExportDir,
	}, scanErr
}

func RunTargetListScan(ctx context.Context, opts TargetListOptions) (bool, error) {
	opts.Output = writerOrDefault(opts.Output, io.Discard)
	opts.Statusf = statusOrNoop(opts.Statusf)
	opts.Tracker = trackerOrNoop(opts.Tracker)

	fileBacked := targetListUsesFileInput(opts.Module)
	var (
		tempDir string
		err     error
	)
	if fileBacked {
		if err := targetlisttool.CleanupStaleTempDirs(targetlisttool.DefaultStaleTempAge); err != nil {
			opts.Statusf("Warning: failed to clean stale target-list temp dirs: %v", err)
		}
		tempDir, err = os.MkdirTemp("", "heph-targetlist-*")
		if err != nil {
			return false, fmt.Errorf("creating target-list temp dir: %w", err)
		}
		defer func() {
			if err := os.RemoveAll(tempDir); err != nil {
				opts.Statusf("Warning: failed to remove target-list temp dir %s: %v", tempDir, err)
			}
		}()
	}

	plan, err := jobs.PlanTargetListFile(opts.ToolName, opts.JobID, opts.ToolOptions, opts.InputFile, tempDir, opts.Workers, fileBacked)
	if err != nil {
		return false, fmt.Errorf("planning target-list job: %w", err)
	}
	defer func() {
		if err := plan.Cleanup(); err != nil {
			opts.Statusf("Warning: failed to clean temporary target-list chunks: %v", err)
		}
	}()

	sourceBytes := plan.TotalSourceBytes
	if opts.Preflight != nil && opts.Preflight.TotalSourceBytes > 0 {
		sourceBytes = opts.Preflight.TotalSourceBytes
	}
	if plan.FileBacked {
		opts.Statusf("Parsed %d targets from %s (%s); chunks effective=%d target=%s max=%s [job %s]",
			plan.TotalTargets,
			opts.InputFile,
			formatByteSize(sourceBytes),
			plan.EffectiveChunks,
			formatByteSize(plan.TargetChunkSize),
			formatByteSize(plan.MaxChunkSize),
			opts.JobID,
		)
	} else {
		opts.Statusf("Parsed %d targets from %s [job %s]", plan.TotalTargets, opts.InputFile, opts.JobID)
	}

	if plan.FileBacked {
		_ = opts.Tracker.UpdatePhase(opts.JobID, operator.PhaseUploading)
		opts.Statusf("Uploading %d target-list chunks to s3://%s/...", plan.EffectiveChunks, opts.Bucket)
		uploadCtx, uploadCancel := context.WithTimeout(ctx, scanruntime.EnqueueTimeout)
		defer uploadCancel()
		if err := jobs.UploadTargetListChunks(uploadCtx, opts.Storage, opts.Bucket, plan); err != nil {
			return false, fmt.Errorf("uploading target-list chunks: %w", err)
		}
	}

	if plan.FileBacked {
		if err := plan.Cleanup(); err != nil {
			opts.Statusf("Warning: failed to clean temporary target-list chunks: %v", err)
		}
	}

	if store := opts.Tracker.Store(); store != nil {
		if rec, loadErr := store.Load(opts.JobID); loadErr == nil {
			rec.TotalTasks = len(plan.Tasks)
			rec.TotalTargets = plan.TotalTargets
			_ = store.Update(rec)
		}
	}

	unitLabel := "targets"
	if plan.FileBacked {
		unitLabel = "chunks"
	}
	return scanruntime.ExecuteQueuedScan(ctx, scanruntime.ExecuteOptions{
		ToolName:      opts.ToolName,
		JobID:         opts.JobID,
		Tasks:         plan.Tasks,
		EnqueueLabel:  "target tasks",
		Workers:       opts.Workers,
		ComputeMode:   opts.ComputeMode,
		Queue:         opts.Queue,
		Storage:       opts.Storage,
		Compute:       opts.Compute,
		Outputs:       opts.Outputs,
		QueueURL:      opts.QueueURL,
		Bucket:        opts.Bucket,
		CloudKind:     opts.CloudKind,
		Placement:     opts.Placement,
		Tracker:       opts.Tracker,
		ProgressLabel: unitLabel,
		CompleteLabel: unitLabel,
		RenderResults: func(renderCtx context.Context, renderStorage cloud.Storage, renderBucket, prefix string) error {
			return outputGenericResults(renderCtx, renderStorage, renderBucket, prefix, opts.Format, opts.Output, opts.Statusf)
		},
		Statusf:     opts.Statusf,
		FleetWaiter: opts.FleetWaiter,
	})
}

func RunWordlistScan(ctx context.Context, opts WordlistOptions) (bool, error) {
	opts.Output = writerOrDefault(opts.Output, io.Discard)
	opts.Statusf = statusOrNoop(opts.Statusf)
	opts.Tracker = trackerOrNoop(opts.Tracker)

	if err := wordlisttool.CleanupStaleTempDirs(wordlisttool.DefaultStaleTempAge); err != nil {
		opts.Statusf("Warning: failed to clean stale wordlist temp dirs: %v", err)
	}

	tempDir, err := os.MkdirTemp("", "heph-wordlist-*")
	if err != nil {
		return false, fmt.Errorf("creating wordlist temp dir: %w", err)
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			opts.Statusf("Warning: failed to remove wordlist temp dir %s: %v", tempDir, err)
		}
	}()

	plan, err := jobs.PlanWordlistFile(opts.ToolName, opts.JobID, opts.RuntimeTarget, opts.ToolOptions, opts.WordlistFile, tempDir, opts.Chunks, opts.Workers)
	if err != nil {
		return false, fmt.Errorf("planning wordlist job: %w", err)
	}
	defer func() {
		if err := plan.Cleanup(); err != nil {
			opts.Statusf("Warning: failed to clean temporary wordlist chunks: %v", err)
		}
	}()

	requested := "auto"
	if opts.Chunks > 0 {
		requested = strconv.Itoa(opts.Chunks)
	}
	sourceBytes := plan.TotalSourceBytes
	if opts.Preflight != nil && opts.Preflight.TotalSourceBytes > 0 {
		sourceBytes = opts.Preflight.TotalSourceBytes
	}
	opts.Statusf("Parsed %d entries from %s (%s); chunks requested=%s effective=%d target=%s max=%s [job %s]",
		plan.TotalWords,
		opts.WordlistFile,
		formatByteSize(sourceBytes),
		requested,
		plan.EffectiveChunks,
		formatByteSize(plan.TargetChunkSize),
		formatByteSize(plan.MaxChunkSize),
		opts.JobID,
	)
	if opts.RuntimeTarget != "" {
		opts.Statusf("Target: %s", opts.RuntimeTarget)
	}

	_ = opts.Tracker.UpdatePhase(opts.JobID, operator.PhaseUploading)
	if store := opts.Tracker.Store(); store != nil {
		if rec, loadErr := store.Load(opts.JobID); loadErr == nil {
			rec.TotalTasks = plan.EffectiveChunks
			rec.TotalWords = plan.TotalWords
			rec.RuntimeTarget = opts.RuntimeTarget
			_ = store.Update(rec)
		}
	}

	opts.Statusf("Uploading %d chunks to s3://%s/...", plan.EffectiveChunks, opts.Bucket)
	uploadCtx, uploadCancel := context.WithTimeout(ctx, scanruntime.EnqueueTimeout)
	defer uploadCancel()
	if err := jobs.UploadChunks(uploadCtx, opts.Storage, opts.Bucket, plan); err != nil {
		return false, fmt.Errorf("uploading wordlist chunks: %w", err)
	}

	if err := plan.Cleanup(); err != nil {
		opts.Statusf("Warning: failed to clean temporary wordlist chunks: %v", err)
	}

	return scanruntime.ExecuteQueuedScan(ctx, scanruntime.ExecuteOptions{
		ToolName:      opts.ToolName,
		JobID:         opts.JobID,
		Tasks:         plan.Tasks,
		EnqueueLabel:  "chunk tasks",
		Workers:       opts.Workers,
		ComputeMode:   opts.ComputeMode,
		Queue:         opts.Queue,
		Storage:       opts.Storage,
		Compute:       opts.Compute,
		Outputs:       opts.Outputs,
		QueueURL:      opts.QueueURL,
		Bucket:        opts.Bucket,
		CloudKind:     opts.CloudKind,
		Placement:     opts.Placement,
		Tracker:       opts.Tracker,
		ProgressLabel: "chunks",
		CompleteLabel: "chunks",
		RenderResults: func(renderCtx context.Context, renderStorage cloud.Storage, renderBucket, prefix string) error {
			return outputGenericResults(renderCtx, renderStorage, renderBucket, prefix, opts.Format, opts.Output, opts.Statusf)
		},
		Statusf:     opts.Statusf,
		FleetWaiter: opts.FleetWaiter,
	})
}

func RunNmapScan(ctx context.Context, opts NmapScanOptions) (bool, error) {
	opts.Output = writerOrDefault(opts.Output, io.Discard)
	opts.Statusf = statusOrNoop(opts.Statusf)
	opts.Tracker = trackerOrNoop(opts.Tracker)

	genericTasks := make([]worker.Task, len(opts.Tasks))
	for i, task := range opts.Tasks {
		genericTasks[i] = worker.Task{
			ToolName:    "nmap",
			JobID:       task.JobID,
			Target:      task.Target,
			Options:     task.Options,
			GroupID:     task.GroupID,
			ChunkIdx:    task.ChunkIdx,
			TotalChunks: task.TotalChunks,
		}
	}

	queueURL, bucket := queueAndBucket(opts.Outputs)
	if opts.CloudKind == "" {
		opts.CloudKind = cloud.KindAWS
	}
	return scanruntime.ExecuteQueuedScan(ctx, scanruntime.ExecuteOptions{
		ToolName:     "nmap",
		JobID:        opts.JobID,
		Tasks:        genericTasks,
		EnqueueLabel: "targets",
		Workers:      opts.Workers,
		ComputeMode:  opts.ComputeMode,
		Queue:        opts.Queue,
		Storage:      opts.Storage,
		Compute:      opts.Compute,
		Outputs:      opts.Outputs,
		QueueURL:     queueURL,
		Bucket:       bucket,
		CloudKind:    opts.CloudKind,
		Placement:    opts.Placement,
		Tracker:      opts.Tracker,
		WorkerEnv: map[string]string{
			"QUEUE_URL":          queueURL,
			"S3_BUCKET":          bucket,
			"JITTER_MAX_SECONDS": strconv.Itoa(opts.JitterMax),
			"TOOL_NAME":          "nmap",
		},
		ContainerName: "nmap-worker",
		CompleteLabel: "targets",
		RenderResults: func(renderCtx context.Context, renderStorage cloud.Storage, renderBucket, prefix string) error {
			return outputNmapResults(renderCtx, renderStorage, renderBucket, prefix, opts.Format, opts.Output, opts.Statusf)
		},
		Statusf:     opts.Statusf,
		FleetWaiter: opts.FleetWaiter,
	})
}

func PreflightTargetListFile(path string, workers int) (*targetlisttool.Metadata, error) {
	meta, err := targetlisttool.InspectFile(path, targetlisttool.Policy{WorkerCount: workers})
	if err != nil {
		return nil, fmt.Errorf("validating target file: %w", err)
	}
	return meta, nil
}

func PreflightWordlistFile(_, path, _, _ string, chunks, workers int) (*wordlisttool.Metadata, error) {
	meta, err := wordlisttool.InspectFile(path, wordlisttool.Policy{
		RequestedChunks: chunks,
		WorkerCount:     workers,
	})
	if err != nil {
		return nil, fmt.Errorf("validating wordlist file: %w", err)
	}
	return meta, nil
}

func queueAndBucket(outputs infra.TerraformOutputs) (string, string) {
	queueURL := outputs.AWS.SQSQueueURL
	if queueURL == "" {
		queueURL = outputs.Selfhosted.QueueURL
	}
	bucket := outputs.AWS.S3BucketName
	if bucket == "" {
		bucket = outputs.Selfhosted.S3BucketName
	}
	return queueURL, bucket
}

func formatByteSize(n int64) string {
	const mib = 1024 * 1024
	if n%mib == 0 && n >= mib {
		return fmt.Sprintf("%d MiB", n/mib)
	}
	return fmt.Sprintf("%d bytes", n)
}

func outputGenericResults(ctx context.Context, storage cloud.Storage, bucket, prefix, format string, output io.Writer, statusf scanruntime.StatusFunc) error {
	keys, err := storage.List(ctx, bucket, prefix)
	if err != nil {
		return fmt.Errorf("listing results: %w", err)
	}

	if format == "json" {
		encoder := json.NewEncoder(output)
		for _, key := range keys {
			if !strings.HasSuffix(key, ".json") {
				continue
			}
			data, err := storage.Download(ctx, bucket, key)
			if err != nil {
				statusf("Warning: failed to download %s: %v", key, err)
				continue
			}
			var result worker.Result
			if err := json.Unmarshal(data, &result); err != nil {
				statusf("Warning: failed to parse %s: %v", key, err)
				continue
			}
			if err := encoder.Encode(result); err != nil {
				return fmt.Errorf("encoding result: %w", err)
			}
		}
	} else {
		if err := writeOutputf(output, "\n%-40s %-10s %s\n", "TARGET", "CHUNK", "STATUS"); err != nil {
			return err
		}
		if err := writeOutputln(output, strings.Repeat("─", 60)); err != nil {
			return err
		}
		var failures int
		for _, key := range keys {
			if !strings.HasSuffix(key, ".json") {
				continue
			}
			target := jobs.TargetFromKey(key)
			status := "OK"
			chunkLabel := ""
			data, err := storage.Download(ctx, bucket, key)
			if err != nil {
				status = "???"
			} else {
				var result worker.Result
				if err := json.Unmarshal(data, &result); err == nil {
					if result.Target != "" {
						target = result.Target
					}
					if result.TotalChunks > 0 {
						chunkLabel = fmt.Sprintf("%d/%d", result.ChunkIdx+1, result.TotalChunks)
					}
					if result.Error != "" {
						status = "ERROR"
						failures++
					}
				}
			}
			if err := writeOutputf(output, "%-40s %-10s %s\n", target, chunkLabel, status); err != nil {
				return err
			}
		}
		if err := writeOutputf(output, "\n%d results written to s3://%s/%s", len(keys), bucket, prefix); err != nil {
			return err
		}
		if failures > 0 {
			if err := writeOutputf(output, " (%d failed)", failures); err != nil {
				return err
			}
		}
		if err := writeOutputln(output); err != nil {
			return err
		}
	}
	return nil
}

func outputNmapResults(ctx context.Context, storage cloud.Storage, bucket, prefix, format string, output io.Writer, statusf scanruntime.StatusFunc) error {
	keys, err := storage.List(ctx, bucket, prefix)
	if err != nil {
		return fmt.Errorf("listing results: %w", err)
	}

	if format == "json" {
		encoder := json.NewEncoder(output)
		for _, key := range keys {
			if !strings.HasSuffix(key, ".json") {
				continue
			}
			data, err := storage.Download(ctx, bucket, key)
			if err != nil {
				statusf("Warning: failed to download %s: %v", key, err)
				continue
			}
			var result worker.Result
			if err := json.Unmarshal(data, &result); err != nil {
				statusf("Warning: failed to parse %s: %v", key, err)
				continue
			}
			if err := encoder.Encode(result); err != nil {
				return fmt.Errorf("encoding result: %w", err)
			}
		}
	} else {
		if err := writeOutputf(output, "\n%-40s %s\n", "TARGET", "STATUS"); err != nil {
			return err
		}
		if err := writeOutputln(output, strings.Repeat("─", 50)); err != nil {
			return err
		}
		for _, key := range keys {
			if !strings.HasSuffix(key, ".json") {
				continue
			}
			target := jobs.TargetFromKey(key)
			if err := writeOutputf(output, "%-40s %s\n", target, "done"); err != nil {
				return err
			}
		}
		if err := writeOutputf(output, "\n%d results written to s3://%s/%s\n", len(keys), bucket, prefix); err != nil {
			return err
		}
	}
	return nil
}

func writeOutputf(output io.Writer, format string, args ...interface{}) error {
	_, err := fmt.Fprintf(output, format, args...)
	return err
}

func writeOutputln(output io.Writer, args ...interface{}) error {
	_, err := fmt.Fprintln(output, args...)
	return err
}

func targetListUsesFileInput(mod *modules.ModuleDefinition) bool {
	return mod != nil && mod.NeedsInput() && !mod.NeedsTarget()
}

func countGroups(tasks []nmaptool.ScanTask) int {
	seen := make(map[string]bool)
	for _, task := range tasks {
		if task.GroupID != "" {
			seen[task.GroupID] = true
		}
	}
	return len(seen)
}

func statusOrNoop(statusf scanruntime.StatusFunc) scanruntime.StatusFunc {
	if statusf != nil {
		return statusf
	}
	return func(string, ...interface{}) {}
}

func writerOrDefault(writer, fallback io.Writer) io.Writer {
	if writer != nil {
		return writer
	}
	return fallback
}

func loggerOrDefault(log logger.Logger) logger.Logger {
	if log != nil {
		return log
	}
	return logger.NewSimpleLogger()
}

func trackerOrNoop(tracker *operator.Tracker) *operator.Tracker {
	if tracker != nil {
		return tracker
	}
	return operator.NoopTracker()
}
