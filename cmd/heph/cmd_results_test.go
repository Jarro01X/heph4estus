package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"heph4estus/internal/cloud"
	"heph4estus/internal/jobs"
	"heph4estus/internal/logger"
	"heph4estus/internal/operator"
	"heph4estus/internal/worker"
)

type resultsTestStorage struct {
	objects     map[string][]byte
	listErr     error
	downloadErr error
}

func (s *resultsTestStorage) Upload(context.Context, string, string, []byte) error { return nil }
func (s *resultsTestStorage) Count(context.Context, string, string) (int, error)   { return 0, nil }

func (s *resultsTestStorage) List(_ context.Context, _, prefix string) ([]string, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	var keys []string
	for key := range s.objects {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	return keys, nil
}

func (s *resultsTestStorage) Download(_ context.Context, _, key string) ([]byte, error) {
	if s.downloadErr != nil {
		return nil, s.downloadErr
	}
	data, ok := s.objects[key]
	if !ok {
		return nil, fmt.Errorf("not found: %s", key)
	}
	return data, nil
}

func TestRunResultsRequiresSubcommand(t *testing.T) {
	err := runResultsWithDeps([]string{}, testLogger(), resultsDeps{})
	if err == nil {
		t.Fatal("expected error without results subcommand")
	}
	if !strings.Contains(err.Error(), "results requires a subcommand") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunResultsListRequiresJob(t *testing.T) {
	err := runResultsWithDeps([]string{"list"}, testLogger(), resultsDeps{})
	if err == nil {
		t.Fatal("expected missing job error")
	}
	if !strings.Contains(err.Error(), "--job flag is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunResultsInvalidFormats(t *testing.T) {
	tests := [][]string{
		{"list", "--job", "job-1", "--format", "yaml"},
		{"export", "--job", "job-1", "--format", "xml"},
	}
	for _, args := range tests {
		err := runResultsWithDeps(args, testLogger(), resultsDeps{})
		if err == nil {
			t.Fatalf("expected invalid format error for %v", args)
		}
		if !strings.Contains(err.Error(), "--format must be") {
			t.Fatalf("unexpected error for %v: %v", args, err)
		}
	}
}

func TestRunResultsInvalidExportView(t *testing.T) {
	err := runResultsWithDeps([]string{"export", "--job", "job-1", "--view", "raw"}, testLogger(), resultsDeps{})
	if err == nil {
		t.Fatal("expected invalid view error")
	}
	if !strings.Contains(err.Error(), "--view must be records or findings") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunResultsDownloadRequiresOutput(t *testing.T) {
	err := runResultsWithDeps([]string{"download", "--job", "job-1"}, testLogger(), resultsDeps{})
	if err == nil {
		t.Fatal("expected missing output error")
	}
	if !strings.Contains(err.Error(), "--output flag is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunResultsMissingJobRecord(t *testing.T) {
	store := operator.NewJobStoreAt(t.TempDir())
	deps := testResultsDeps(store, &resultsTestStorage{})
	err := runResultsWithDeps([]string{"list", "--job", "missing"}, testLogger(), deps)
	if err == nil {
		t.Fatal("expected missing job record error")
	}
	if !strings.Contains(err.Error(), "job record not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunResultsListText(t *testing.T) {
	store := operator.NewJobStoreAt(t.TempDir())
	createResultsJob(t, store, "job-1", "httpx")
	storage := &resultsTestStorage{objects: resultObjects(t, "httpx", "job-1")}
	deps := testResultsDeps(store, storage)

	out, err := captureStdout(func() error {
		return runResultsWithDeps([]string{"list", "--job", "job-1"}, testLogger(), deps)
	})
	if err != nil {
		t.Fatalf("runResults list: %v", err)
	}
	if !strings.Contains(out, "TARGET") || !strings.Contains(out, "example.com") || !strings.Contains(out, "test.com") {
		t.Fatalf("unexpected text output:\n%s", out)
	}
	if strings.Index(out, "example.com") > strings.Index(out, "test.com") {
		t.Fatalf("expected sorted output, got:\n%s", out)
	}
}

func TestRunResultsListJSON(t *testing.T) {
	store := operator.NewJobStoreAt(t.TempDir())
	createResultsJob(t, store, "job-1", "httpx")
	storage := &resultsTestStorage{objects: resultObjects(t, "httpx", "job-1")}
	deps := testResultsDeps(store, storage)

	out, err := captureStdout(func() error {
		return runResultsWithDeps([]string{"list", "--job-id", "job-1", "--format", "json"}, testLogger(), deps)
	})
	if err != nil {
		t.Fatalf("runResults list json: %v", err)
	}
	var entries []resultListEntry
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("decode list output: %v\n%s", err, out)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	if entries[0].Target != "example.com" {
		t.Fatalf("first target = %q, want example.com", entries[0].Target)
	}
	if !strings.HasPrefix(entries[0].URI, "s3://bucket/") {
		t.Fatalf("unexpected URI: %q", entries[0].URI)
	}
}

func TestRunResultsDownload(t *testing.T) {
	store := operator.NewJobStoreAt(t.TempDir())
	createResultsJob(t, store, "job-1", "httpx")
	storage := &resultsTestStorage{objects: resultObjects(t, "httpx", "job-1")}
	outDir := t.TempDir()
	deps := testResultsDeps(store, storage)

	out, err := captureStdout(func() error {
		return runResultsWithDeps([]string{"download", "--job", "job-1", "--output", outDir}, testLogger(), deps)
	})
	if err != nil {
		t.Fatalf("runResults download: %v", err)
	}
	if !strings.Contains(out, "Downloaded 3 results and 1 artifacts") {
		t.Fatalf("unexpected download output:\n%s", out)
	}
	wantPath := filepath.Join(outDir, "httpx", "job-1", "results", "example.com_100.json")
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("expected downloaded result at %s: %v", wantPath, err)
	}
	rec, err := store.Load("job-1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if rec.LocalOutputDir != filepath.Join(outDir, "httpx", "job-1") {
		t.Fatalf("LocalOutputDir = %q", rec.LocalOutputDir)
	}
}

func TestRunResultsExportJSON(t *testing.T) {
	store := operator.NewJobStoreAt(t.TempDir())
	createResultsJob(t, store, "job-1", "httpx")
	deps := testResultsDeps(store, &resultsTestStorage{objects: resultObjects(t, "httpx", "job-1")})

	out, err := captureStdout(func() error {
		return runResultsWithDeps([]string{"export", "--job", "job-1", "--format", "json"}, testLogger(), deps)
	})
	if err != nil {
		t.Fatalf("runResults export json: %v", err)
	}
	var results []worker.Result
	if err := json.Unmarshal([]byte(out), &results); err != nil {
		t.Fatalf("decode export json: %v\n%s", err, out)
	}
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
	if results[0].Target != "example.com" {
		t.Fatalf("first target = %q, want example.com", results[0].Target)
	}
}

func TestRunResultsExportJSONL(t *testing.T) {
	store := operator.NewJobStoreAt(t.TempDir())
	createResultsJob(t, store, "job-1", "httpx")
	deps := testResultsDeps(store, &resultsTestStorage{objects: resultObjects(t, "httpx", "job-1")})

	out, err := captureStdout(func() error {
		return runResultsWithDeps([]string{"export", "--job", "job-1", "--format", "jsonl"}, testLogger(), deps)
	})
	if err != nil {
		t.Fatalf("runResults export jsonl: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("jsonl lines = %d, want 2\n%s", len(lines), out)
	}
	var first worker.Result
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("decode first jsonl line: %v", err)
	}
	if first.Target != "example.com" {
		t.Fatalf("first target = %q, want example.com", first.Target)
	}
}

func TestRunResultsExportCSV(t *testing.T) {
	store := operator.NewJobStoreAt(t.TempDir())
	createResultsJob(t, store, "job-1", "httpx")
	deps := testResultsDeps(store, &resultsTestStorage{objects: resultObjects(t, "httpx", "job-1")})

	out, err := captureStdout(func() error {
		return runResultsWithDeps([]string{"export", "--job", "job-1", "--format", "csv"}, testLogger(), deps)
	})
	if err != nil {
		t.Fatalf("runResults export csv: %v", err)
	}
	rows, err := csv.NewReader(strings.NewReader(out)).ReadAll()
	if err != nil {
		t.Fatalf("decode csv: %v\n%s", err, out)
	}
	if len(rows) != 3 {
		t.Fatalf("csv rows = %d, want 3", len(rows))
	}
	if rows[0][0] != "key" || rows[0][4] != "status" {
		t.Fatalf("unexpected header: %#v", rows[0])
	}
	if rows[1][3] != "example.com" || rows[1][4] != "ok" {
		t.Fatalf("unexpected first row: %#v", rows[1])
	}
	if rows[2][3] != "test.com" || rows[2][4] != "error" {
		t.Fatalf("unexpected second row: %#v", rows[2])
	}
}

func TestRunResultsExportFindingsJSON(t *testing.T) {
	store := operator.NewJobStoreAt(t.TempDir())
	createResultsJob(t, store, "job-1", "nmap")
	resultKey := jobs.ResultKey("nmap", "job-1", "example.com", "", 0, 0, 100, "json")
	artifactKey := jobs.ArtifactKey("nmap", "job-1", "example.com", "", 0, 0, 100, "xml")
	storage := &resultsTestStorage{objects: map[string][]byte{
		resultKey:          mustResultJSONWithOutput(t, "nmap", "job-1", "example.com", artifactKey, "", ""),
		artifactKey:        []byte(`<nmaprun><host><address addr="192.0.2.10" addrtype="ipv4"/><hostnames><hostname name="example.com"/></hostnames><ports><port protocol="tcp" portid="443"><state state="open"/><service name="https" product="nginx" version="1.25"/></port><port protocol="tcp" portid="25"><state state="filtered"/></port></ports></host></nmaprun>`),
		resultKey + ".bak": []byte(`ignored`),
	}}
	deps := testResultsDeps(store, storage)

	out, err := captureStdout(func() error {
		return runResultsWithDeps([]string{"export", "--job", "job-1", "--view", "findings", "--format", "json"}, testLogger(), deps)
	})
	if err != nil {
		t.Fatalf("runResults export findings json: %v", err)
	}
	var records []map[string]any
	if err := json.Unmarshal([]byte(out), &records); err != nil {
		t.Fatalf("decode findings json: %v\n%s", err, out)
	}
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}
	record := records[0]
	if record["tool"] != "nmap" || record["target"] != "example.com" || record["service"] != "https" {
		t.Fatalf("unexpected record: %#v", record)
	}
	if record["port"] != float64(443) || record["artifact_key"] != artifactKey || record["source_key"] != resultKey {
		t.Fatalf("unexpected metadata: %#v", record)
	}
}

func TestRunResultsExportFindingsCSV(t *testing.T) {
	store := operator.NewJobStoreAt(t.TempDir())
	createResultsJob(t, store, "job-1", "naabu")
	resultKey := jobs.ResultKey("naabu", "job-1", "example.com", "", 0, 0, 100, "json")
	artifactKey := jobs.ArtifactKey("naabu", "job-1", "example.com", "", 0, 0, 100, "jsonl")
	storage := &resultsTestStorage{objects: map[string][]byte{
		resultKey:   mustResultJSONWithOutput(t, "naabu", "job-1", "example.com", artifactKey, "", ""),
		artifactKey: []byte(`{"host":"example.com","ip":"192.0.2.10","port":443,"tls":true,"cdn":true,"cdn-name":"cloudflare"}` + "\n"),
	}}
	deps := testResultsDeps(store, storage)

	out, err := captureStdout(func() error {
		return runResultsWithDeps([]string{"export", "--job", "job-1", "--view", "findings", "--format", "csv"}, testLogger(), deps)
	})
	if err != nil {
		t.Fatalf("runResults export findings csv: %v", err)
	}
	rows, err := csv.NewReader(strings.NewReader(out)).ReadAll()
	if err != nil {
		t.Fatalf("decode findings csv: %v\n%s", err, out)
	}
	if len(rows) != 2 {
		t.Fatalf("csv rows = %d, want 2", len(rows))
	}
	if rows[0][0] != "tool" || rows[0][6] != "port" || rows[0][12] != "cdn_name" {
		t.Fatalf("unexpected header: %#v", rows[0])
	}
	if rows[1][0] != "naabu" || rows[1][6] != "443" || rows[1][10] != "true" || rows[1][12] != "cloudflare" {
		t.Fatalf("unexpected row: %#v", rows[1])
	}
}

func TestRunResultsExportFindingsUnsupportedTool(t *testing.T) {
	store := operator.NewJobStoreAt(t.TempDir())
	createResultsJob(t, store, "job-1", "httpx")
	deps := testResultsDeps(store, &resultsTestStorage{objects: resultObjects(t, "httpx", "job-1")})

	_, err := captureStdout(func() error {
		return runResultsWithDeps([]string{"export", "--job", "job-1", "--view", "findings"}, testLogger(), deps)
	})
	if err == nil {
		t.Fatal("expected unsupported findings tool error")
	}
	if !strings.Contains(err.Error(), "findings export is not available for tool") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunResultsExportFindingsMissingArtifactData(t *testing.T) {
	store := operator.NewJobStoreAt(t.TempDir())
	createResultsJob(t, store, "job-1", "nmap")
	resultKey := jobs.ResultKey("nmap", "job-1", "example.com", "", 0, 0, 100, "json")
	deps := testResultsDeps(store, &resultsTestStorage{objects: map[string][]byte{
		resultKey: mustResultJSONWithOutput(t, "nmap", "job-1", "example.com", "", "", ""),
	}})

	_, err := captureStdout(func() error {
		return runResultsWithDeps([]string{"export", "--job", "job-1", "--view", "findings"}, testLogger(), deps)
	})
	if err == nil {
		t.Fatal("expected missing artifact data error")
	}
	if !strings.Contains(err.Error(), "no artifact output_key or inline output") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunResultsExportMalformedJSON(t *testing.T) {
	store := operator.NewJobStoreAt(t.TempDir())
	createResultsJob(t, store, "job-1", "httpx")
	prefix := jobs.ResultPrefix("httpx", "job-1")
	deps := testResultsDeps(store, &resultsTestStorage{
		objects: map[string][]byte{prefix + "bad_100.json": []byte(`{bad json}`)},
	})

	_, err := captureStdout(func() error {
		return runResultsWithDeps([]string{"export", "--job", "job-1", "--format", "jsonl"}, testLogger(), deps)
	})
	if err == nil {
		t.Fatal("expected malformed JSON error")
	}
	if !strings.Contains(err.Error(), "parsing") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunResultsListStorageError(t *testing.T) {
	store := operator.NewJobStoreAt(t.TempDir())
	createResultsJob(t, store, "job-1", "httpx")
	deps := testResultsDeps(store, &resultsTestStorage{listErr: fmt.Errorf("list failed")})

	_, err := captureStdout(func() error {
		return runResultsWithDeps([]string{"list", "--job", "job-1"}, testLogger(), deps)
	})
	if err == nil {
		t.Fatal("expected storage list error")
	}
	if !strings.Contains(err.Error(), "listing results") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStorageForResultsUsesStoredSelfhostedConfig(t *testing.T) {
	rec := &operator.JobRecord{
		JobID:       "job-1",
		ToolName:    "httpx",
		Bucket:      "bucket",
		S3Endpoint:  "http://127.0.0.1:9000",
		S3Region:    "us-east-1",
		S3AccessKey: "access",
		S3SecretKey: "secret",
		S3PathStyle: true,
	}
	if !recHasStoredS3Config(rec) {
		t.Fatal("expected record to have stored S3 config")
	}
	outputs := jobRecordOutputs(rec)
	if outputs["s3_endpoint"] != rec.S3Endpoint {
		t.Fatalf("s3_endpoint = %q", outputs["s3_endpoint"])
	}
	if outputs["s3_path_style"] != "true" {
		t.Fatalf("s3_path_style = %q", outputs["s3_path_style"])
	}
}

func testResultsDeps(store *operator.JobStore, storage cloud.Storage) resultsDeps {
	return resultsDeps{
		newJobStore: func() (*operator.JobStore, error) {
			return store, nil
		},
		storageFor: func(context.Context, *operator.JobRecord, cloud.Kind, logger.Logger) (cloud.Storage, error) {
			return storage, nil
		},
	}
}

func createResultsJob(t *testing.T, store *operator.JobStore, jobID, tool string) {
	t.Helper()
	if err := store.Create(&operator.JobRecord{
		JobID:     jobID,
		ToolName:  tool,
		Phase:     operator.PhaseComplete,
		CreatedAt: time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC),
		Cloud:     "aws",
		Bucket:    "bucket",
	}); err != nil {
		t.Fatalf("Create job: %v", err)
	}
}

func resultObjects(t *testing.T, tool, jobID string) map[string][]byte {
	t.Helper()
	prefix := jobs.ResultPrefix(tool, jobID)
	artifactPrefix := jobs.ArtifactPrefix(tool, jobID)
	return map[string][]byte{
		prefix + "test.com_200.json":             mustResultJSON(t, tool, jobID, "test.com", "boom"),
		prefix + "example.com_100.json":          mustResultJSON(t, tool, jobID, "example.com", ""),
		prefix + "notes.txt":                     []byte("not a result"),
		artifactPrefix + "example.com_100.jsonl": []byte(`{"url":"https://example.com"}` + "\n"),
	}
}

func mustResultJSON(t *testing.T, tool, jobID, target, resultErr string) []byte {
	t.Helper()
	return mustResultJSONWithOutput(t, tool, jobID, target, jobs.ArtifactKey(tool, jobID, target, "", 0, 0, 100, "jsonl"), "", resultErr)
}

func mustResultJSONWithOutput(t *testing.T, tool, jobID, target, outputKey, output, resultErr string) []byte {
	t.Helper()
	data, err := json.Marshal(worker.Result{
		ToolName:    tool,
		JobID:       jobID,
		Target:      target,
		Output:      output,
		OutputKey:   outputKey,
		Error:       resultErr,
		Timestamp:   time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC),
		ChunkIdx:    0,
		TotalChunks: 0,
	})
	if err != nil {
		t.Fatalf("Marshal result: %v", err)
	}
	return data
}

func captureStdout(fn func() error) (string, error) {
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		return "", err
	}
	os.Stdout = w
	runErr := fn()
	_ = w.Close()
	os.Stdout = old
	data, readErr := io.ReadAll(r)
	if readErr != nil {
		return "", readErr
	}
	return string(data), runErr
}
