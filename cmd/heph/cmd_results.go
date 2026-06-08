package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"heph4estus/internal/cloud"
	"heph4estus/internal/cloud/factory"
	"heph4estus/internal/jobs"
	"heph4estus/internal/logger"
	"heph4estus/internal/operator"
	resultfmt "heph4estus/internal/results"
	"heph4estus/internal/worker"
)

type resultsDeps struct {
	newJobStore func() (*operator.JobStore, error)
	storageFor  func(context.Context, *operator.JobRecord, cloud.Kind, logger.Logger) (cloud.Storage, error)
}

type resultsContext struct {
	store          *operator.JobStore
	record         *operator.JobRecord
	storage        cloud.Storage
	tool           string
	jobID          string
	bucket         string
	resultPrefix   string
	artifactPrefix string
}

type resultListEntry struct {
	Key    string `json:"key"`
	Target string `json:"target"`
	URI    string `json:"uri"`
}

func runResults(args []string, log logger.Logger) error {
	return runResultsWithDeps(args, log, resultsDeps{
		newJobStore: operator.NewJobStore,
		storageFor:  storageForResults,
	})
}

func runResultsWithDeps(args []string, log logger.Logger, deps resultsDeps) error {
	if len(args) < 1 {
		return fmt.Errorf("results requires a subcommand: list, download, export")
	}
	if log == nil {
		log = logger.NewSimpleLogger()
	}
	if deps.newJobStore == nil {
		deps.newJobStore = operator.NewJobStore
	}
	if deps.storageFor == nil {
		deps.storageFor = storageForResults
	}

	subcommand := args[0]
	subArgs := args[1:]
	switch subcommand {
	case "list":
		return runResultsList(subArgs, log, deps)
	case "download":
		return runResultsDownload(subArgs, log, deps)
	case "export":
		return runResultsExport(subArgs, log, deps)
	default:
		return fmt.Errorf("unknown results subcommand %q: expected list, download, export", subcommand)
	}
}

func runResultsList(args []string, log logger.Logger, deps resultsDeps) error {
	fs := flag.NewFlagSet("results list", flag.ContinueOnError)
	var jobID string
	fs.StringVar(&jobID, "job", "", "Job ID to list results for")
	fs.StringVar(&jobID, "job-id", "", "Job ID to list results for")
	format := fs.String("format", "text", "Output format: text or json")
	cloudFlag := fs.String("cloud", "", "Override cloud provider used to read results")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(jobID) == "" {
		return fmt.Errorf("--job flag is required")
	}
	if *format != "text" && *format != "json" {
		return fmt.Errorf("--format must be text or json")
	}

	ctx, err := loadResultsContext(context.Background(), jobID, *cloudFlag, log, deps)
	if err != nil {
		return err
	}
	entries, err := listResultEntries(context.Background(), ctx.storage, ctx.bucket, ctx.resultPrefix)
	if err != nil {
		return err
	}
	if *format == "json" {
		return outputResultEntriesJSON(entries)
	}
	return outputResultEntriesText(entries)
}

func runResultsDownload(args []string, log logger.Logger, deps resultsDeps) error {
	fs := flag.NewFlagSet("results download", flag.ContinueOnError)
	var jobID string
	fs.StringVar(&jobID, "job", "", "Job ID to download results for")
	fs.StringVar(&jobID, "job-id", "", "Job ID to download results for")
	outDir := fs.String("output", "", "Directory to write results and artifacts into")
	cloudFlag := fs.String("cloud", "", "Override cloud provider used to read results")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(jobID) == "" {
		return fmt.Errorf("--job flag is required")
	}
	if strings.TrimSpace(*outDir) == "" {
		return fmt.Errorf("--output flag is required")
	}

	ctx, err := loadResultsContext(context.Background(), jobID, *cloudFlag, log, deps)
	if err != nil {
		return err
	}
	result, err := operator.ExportJob(context.Background(), ctx.storage, ctx.bucket, ctx.tool, ctx.jobID, *outDir)
	if err != nil {
		return err
	}
	ctx.record.LocalOutputDir = result.Dir
	if err := ctx.store.Update(ctx.record); err != nil {
		return fmt.Errorf("updating job record: %w", err)
	}
	_, _ = fmt.Fprintf(os.Stdout, "Downloaded %d results and %d artifacts to %s\n", result.ResultCount, result.ArtifactCount, result.Dir)
	return nil
}

func runResultsExport(args []string, log logger.Logger, deps resultsDeps) error {
	fs := flag.NewFlagSet("results export", flag.ContinueOnError)
	var jobID string
	fs.StringVar(&jobID, "job", "", "Job ID to export results for")
	fs.StringVar(&jobID, "job-id", "", "Job ID to export results for")
	format := fs.String("format", "jsonl", "Output format: json, jsonl, or csv")
	view := fs.String("view", "records", "Export view: records or findings")
	cloudFlag := fs.String("cloud", "", "Override cloud provider used to read results")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(jobID) == "" {
		return fmt.Errorf("--job flag is required")
	}
	exportFormat, err := resultfmt.ParseFormat(*format)
	if err != nil {
		return fmt.Errorf("--%s", err.Error())
	}
	viewValue := strings.ToLower(strings.TrimSpace(*view))
	if viewValue != "records" && viewValue != "findings" {
		return fmt.Errorf("--view must be records or findings")
	}

	ctx, err := loadResultsContext(context.Background(), jobID, *cloudFlag, log, deps)
	if err != nil {
		return err
	}
	results, err := loadWorkerResults(context.Background(), ctx.storage, ctx.bucket, ctx.resultPrefix)
	if err != nil {
		return err
	}
	if viewValue == "findings" {
		return outputWorkerResultFindings(context.Background(), ctx.storage, ctx.bucket, ctx.tool, ctx.jobID, results, exportFormat)
	}

	switch *format {
	case "json":
		return outputWorkerResultsJSON(results)
	case "jsonl":
		return outputWorkerResultsJSONL(results)
	case "csv":
		return outputWorkerResultsCSV(results)
	default:
		return fmt.Errorf("--format must be json, jsonl, or csv")
	}
}

func loadResultsContext(ctx context.Context, jobID, cloudOverride string, log logger.Logger, deps resultsDeps) (*resultsContext, error) {
	store, err := deps.newJobStore()
	if err != nil {
		return nil, fmt.Errorf("opening job store: %w", err)
	}
	rec, err := store.Load(jobID)
	if err != nil {
		return nil, fmt.Errorf("%w — run 'heph results' only for jobs started on this machine", err)
	}

	tool := strings.TrimSpace(rec.ToolName)
	if tool == "" {
		return nil, fmt.Errorf("job record %s is missing tool_name", jobID)
	}
	bucket := strings.TrimSpace(rec.Bucket)
	if bucket == "" {
		return nil, fmt.Errorf("job record %s is missing bucket", jobID)
	}

	resultPrefix := rec.ResultPrefix
	if resultPrefix == "" {
		resultPrefix = jobs.ResultPrefix(tool, jobID)
	}
	artifactPrefix := rec.ArtifactPrefix
	if artifactPrefix == "" {
		artifactPrefix = jobs.ArtifactPrefix(tool, jobID)
	}

	opCfg, _ := operator.LoadConfig()
	effectiveCloud := cloudOverride
	if effectiveCloud == "" {
		effectiveCloud = rec.Cloud
	}
	cloudKind, err := resolveCLICloud(effectiveCloud, opCfg)
	if err != nil {
		return nil, err
	}

	storage, err := deps.storageFor(ctx, rec, cloudKind, log)
	if err != nil {
		return nil, err
	}
	return &resultsContext{
		store:          store,
		record:         rec,
		storage:        storage,
		tool:           tool,
		jobID:          jobID,
		bucket:         bucket,
		resultPrefix:   resultPrefix,
		artifactPrefix: artifactPrefix,
	}, nil
}

func storageForResults(ctx context.Context, rec *operator.JobRecord, cloudKind cloud.Kind, log logger.Logger) (cloud.Storage, error) {
	if recHasStoredS3Config(rec) && cloudKind.IsSelfhostedFamily() {
		provider, err := factory.Build(factory.Config{
			Kind:       cloudKind.Canonical(),
			Selfhosted: factory.SelfhostedConfigFromOutputs(jobRecordOutputs(rec)),
			Logger:     log,
		})
		if err != nil {
			return nil, fmt.Errorf("building cloud provider: %w", err)
		}
		return provider.Storage(), nil
	}

	provider, err := buildRuntimeProvider(ctx, cloudKind, nil, log)
	if err != nil {
		return nil, fmt.Errorf("building cloud provider: %w", err)
	}
	return provider.Storage(), nil
}

func recHasStoredS3Config(rec *operator.JobRecord) bool {
	return rec != nil && rec.S3Endpoint != "" && rec.S3AccessKey != "" && rec.S3SecretKey != ""
}

func jobRecordOutputs(rec *operator.JobRecord) map[string]string {
	return map[string]string{
		"s3_endpoint":                   rec.S3Endpoint,
		"s3_region":                     rec.S3Region,
		"s3_access_key":                 rec.S3AccessKey,
		"s3_secret_key":                 rec.S3SecretKey,
		"s3_path_style":                 strconv.FormatBool(rec.S3PathStyle),
		"sqs_queue_url":                 "",
		"s3_bucket_name":                rec.Bucket,
		"controller_ca_pem":             rec.ControllerCAPEM,
		"controller_host":               rec.ControllerHost,
		"nats_operator_client_cert_pem": rec.NATSClientCertPEM,
		"nats_operator_client_key_pem":  rec.NATSClientKeyPEM,
	}
}

func listResultEntries(ctx context.Context, storage cloud.Storage, bucket, prefix string) ([]resultListEntry, error) {
	keys, err := storage.List(ctx, bucket, prefix)
	if err != nil {
		return nil, fmt.Errorf("listing results: %w", err)
	}
	keys = filterResultKeys(keys)
	entries := make([]resultListEntry, 0, len(keys))
	for _, key := range keys {
		entries = append(entries, resultListEntry{
			Key:    key,
			Target: jobs.TargetFromKey(key),
			URI:    s3ObjectURI(bucket, key),
		})
	}
	return entries, nil
}

type keyedWorkerResult struct {
	Key    string
	Result worker.Result
}

func loadWorkerResults(ctx context.Context, storage cloud.Storage, bucket, prefix string) ([]keyedWorkerResult, error) {
	keys, err := storage.List(ctx, bucket, prefix)
	if err != nil {
		return nil, fmt.Errorf("listing results: %w", err)
	}
	keys = filterResultKeys(keys)

	results := make([]keyedWorkerResult, 0, len(keys))
	for _, key := range keys {
		data, err := storage.Download(ctx, bucket, key)
		if err != nil {
			return nil, fmt.Errorf("downloading %s: %w", key, err)
		}
		var result worker.Result
		if err := json.Unmarshal(data, &result); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", key, err)
		}
		results = append(results, keyedWorkerResult{Key: key, Result: result})
	}
	return results, nil
}

func filterResultKeys(keys []string) []string {
	filtered := make([]string, 0, len(keys))
	for _, key := range keys {
		if strings.HasSuffix(key, ".json") {
			filtered = append(filtered, key)
		}
	}
	sort.Strings(filtered)
	return filtered
}

func outputResultEntriesJSON(entries []resultListEntry) error {
	enc := json.NewEncoder(os.Stdout)
	return enc.Encode(entries)
}

func outputResultEntriesText(entries []resultListEntry) error {
	if len(entries) == 0 {
		_, _ = fmt.Fprintln(os.Stdout, "No results found.")
		return nil
	}
	_, _ = fmt.Fprintf(os.Stdout, "%-40s %s\n", "TARGET", "KEY")
	_, _ = fmt.Fprintf(os.Stdout, "%s\n", strings.Repeat("-", 80))
	for _, entry := range entries {
		_, _ = fmt.Fprintf(os.Stdout, "%-40s %s\n", entry.Target, entry.Key)
	}
	return nil
}

func outputWorkerResultsJSON(results []keyedWorkerResult) error {
	out := make([]worker.Result, 0, len(results))
	for _, result := range results {
		out = append(out, result.Result)
	}
	enc := json.NewEncoder(os.Stdout)
	return enc.Encode(out)
}

func outputWorkerResultsJSONL(results []keyedWorkerResult) error {
	enc := json.NewEncoder(os.Stdout)
	for _, result := range results {
		if err := enc.Encode(result.Result); err != nil {
			return err
		}
	}
	return nil
}

func outputWorkerResultsCSV(results []keyedWorkerResult) error {
	w := csv.NewWriter(os.Stdout)
	if err := w.Write([]string{"key", "tool_name", "job_id", "target", "status", "error", "output_key", "timestamp", "group_id", "chunk_idx", "total_chunks"}); err != nil {
		return err
	}
	for _, keyed := range results {
		result := keyed.Result
		status := "ok"
		if result.Error != "" {
			status = "error"
		}
		if err := w.Write([]string{
			keyed.Key,
			result.ToolName,
			result.JobID,
			result.Target,
			status,
			result.Error,
			result.OutputKey,
			result.Timestamp.Format("2006-01-02T15:04:05.999999999Z07:00"),
			result.GroupID,
			strconv.Itoa(result.ChunkIdx),
			strconv.Itoa(result.TotalChunks),
		}); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

func outputWorkerResultFindings(ctx context.Context, storage cloud.Storage, bucket, tool, jobID string, results []keyedWorkerResult, format resultfmt.Format) error {
	if _, ok := resultfmt.FormatterForTool(tool); !ok {
		return fmt.Errorf("findings export is not available for tool %q; use --view records", tool)
	}

	records := make([]resultfmt.Record, 0)
	for _, keyed := range results {
		result := keyed.Result
		toolName := firstNonEmptyString(result.ToolName, tool)
		formatter, ok := resultfmt.FormatterForTool(toolName)
		if !ok {
			return fmt.Errorf("findings export is not available for tool %q in %s; use --view records", toolName, keyed.Key)
		}
		data, artifactKey, err := resultArtifactData(ctx, storage, bucket, keyed)
		if err != nil {
			return err
		}
		input := resultfmt.ArtifactInput{
			ToolName:    toolName,
			JobID:       firstNonEmptyString(result.JobID, jobID),
			Target:      result.Target,
			SourceKey:   keyed.Key,
			ArtifactKey: artifactKey,
			Data:        data,
		}
		formatted, err := formatter.Records(input)
		if err != nil {
			return fmt.Errorf("formatting %s: %w", keyed.Key, err)
		}
		records = append(records, formatted...)
	}

	out, err := resultfmt.RenderRecords(records, format)
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(out)
	return err
}

func resultArtifactData(ctx context.Context, storage cloud.Storage, bucket string, keyed keyedWorkerResult) ([]byte, string, error) {
	outputKey := strings.TrimSpace(keyed.Result.OutputKey)
	if outputKey != "" {
		data, err := storage.Download(ctx, bucket, outputKey)
		if err != nil {
			return nil, outputKey, fmt.Errorf("downloading artifact %s for %s: %w", outputKey, keyed.Key, err)
		}
		return data, outputKey, nil
	}
	if strings.TrimSpace(keyed.Result.Output) != "" {
		return []byte(keyed.Result.Output), "", nil
	}
	return nil, "", fmt.Errorf("result %s has no artifact output_key or inline output for findings export", keyed.Key)
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func s3ObjectURI(bucket, key string) string {
	if bucket == "" {
		return key
	}
	return fmt.Sprintf("s3://%s/%s", bucket, strings.TrimPrefix(key, "/"))
}
