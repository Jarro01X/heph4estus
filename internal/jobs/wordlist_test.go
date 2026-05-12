package jobs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func cleanupWordlistPlan(t *testing.T, plan *WordlistPlan) {
	t.Helper()
	if err := plan.Cleanup(); err != nil {
		t.Fatalf("cleanup wordlist plan: %v", err)
	}
}

func TestParseWordlistEntries(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []string
	}{
		{
			name:    "plain entries",
			content: "admin\nlogin\napi\n",
			want:    []string{"admin", "login", "api"},
		},
		{
			name:    "preserves hash prefixes and spaces",
			content: "#comment\n admin\ntrailing \n  both  \n",
			want:    []string{"#comment", " admin", "trailing ", "  both  "},
		},
		{
			name:    "skips blank lines only",
			content: "\n\nadmin\n\nlogin\n",
			want:    []string{"admin", "login"},
		},
		{
			name:    "empty content",
			content: "",
			want:    nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseWordlistEntries(tt.content)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ParseWordlistEntries(%q) = %#v, want %#v", tt.content, got, tt.want)
			}
		})
	}
}

func TestChunkEntries(t *testing.T) {
	entries := []string{"a", "b", "c", "d", "e"}

	// 2 chunks: round-robin distribution.
	chunks := ChunkEntries(entries, 2)
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	// a,c,e in chunk 0; b,d in chunk 1
	if len(chunks[0]) != 3 || len(chunks[1]) != 2 {
		t.Fatalf("unexpected chunk sizes: %d, %d", len(chunks[0]), len(chunks[1]))
	}
	if chunks[0][0] != "a" || chunks[0][1] != "c" || chunks[0][2] != "e" {
		t.Errorf("chunk[0] = %v, want [a c e]", chunks[0])
	}
	if chunks[1][0] != "b" || chunks[1][1] != "d" {
		t.Errorf("chunk[1] = %v, want [b d]", chunks[1])
	}
}

func TestChunkEntriesClampedToEntries(t *testing.T) {
	entries := []string{"a", "b"}
	chunks := ChunkEntries(entries, 10)
	if len(chunks) != 2 {
		t.Fatalf("expected chunks clamped to 2, got %d", len(chunks))
	}
}

func TestChunkEntriesNoEmpty(t *testing.T) {
	entries := []string{"a", "b", "c"}
	chunks := ChunkEntries(entries, 3)
	for i, c := range chunks {
		if len(c) == 0 {
			t.Errorf("chunk[%d] is empty", i)
		}
	}
}

func TestChunkEntriesZeroChunks(t *testing.T) {
	entries := []string{"a", "b"}
	chunks := ChunkEntries(entries, 0)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk for 0 requested, got %d", len(chunks))
	}
}

func TestPlanWordlistJob(t *testing.T) {
	plan, err := PlanWordlistJob("ffuf", "job-123", "https://example.com/FUZZ", "-ac", "admin\nlogin\napi\ntest\n", 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plan.TotalWords != 4 {
		t.Fatalf("expected 4 words, got %d", plan.TotalWords)
	}
	if len(plan.Tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(plan.Tasks))
	}
	if len(plan.ChunkData) != 2 {
		t.Fatalf("expected 2 chunk data entries, got %d", len(plan.ChunkData))
	}

	task := plan.Tasks[0]
	if task.ToolName != "ffuf" {
		t.Errorf("task.ToolName = %q, want ffuf", task.ToolName)
	}
	if task.JobID != "job-123" {
		t.Errorf("task.JobID = %q, want job-123", task.JobID)
	}
	if task.Target != "https://example.com/FUZZ" {
		t.Errorf("task.Target = %q", task.Target)
	}
	if task.InputKey == "" {
		t.Error("task.InputKey should be set")
	}
	if task.Options != "-ac" {
		t.Errorf("task.Options = %q, want -ac", task.Options)
	}
	if task.GroupID == "" {
		t.Error("task.GroupID should be set")
	}
	if task.ChunkIdx != 0 {
		t.Errorf("task.ChunkIdx = %d, want 0", task.ChunkIdx)
	}
	if task.TotalChunks != 2 {
		t.Errorf("task.TotalChunks = %d, want 2", task.TotalChunks)
	}
}

func TestPlanWordlistJobPreservesRawEntries(t *testing.T) {
	plan, err := PlanWordlistJob("ffuf", "job-123", "https://example.com/FUZZ", "", "#comment\n admin\ntrailing \n", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := string(plan.ChunkData[0]); got != "#comment\n admin\ntrailing \n" {
		t.Fatalf("chunk data = %q, want raw wordlist preserved", got)
	}
}

func TestPlanWordlistJobEmptyWordlist(t *testing.T) {
	_, err := PlanWordlistJob("ffuf", "job-123", "https://example.com/FUZZ", "", "\n\n", 2)
	if err == nil {
		t.Fatal("expected error for empty wordlist")
	}
}

func TestPlanWordlistFileCreatesCompatibleTasks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "words.txt")
	content := "admin\nlogin\napi\ntest\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write wordlist: %v", err)
	}

	plan, err := PlanWordlistFile("ffuf", "job-123", "https://example.com/FUZZ", "-ac", path, t.TempDir(), 2, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer cleanupWordlistPlan(t, plan)

	if plan.TotalWords != 4 {
		t.Fatalf("TotalWords = %d, want 4", plan.TotalWords)
	}
	if plan.TotalSourceBytes != int64(len(content)) {
		t.Fatalf("TotalSourceBytes = %d, want %d", plan.TotalSourceBytes, len(content))
	}
	if plan.EffectiveChunks != 2 {
		t.Fatalf("EffectiveChunks = %d, want 2", plan.EffectiveChunks)
	}
	if len(plan.ChunkData) != 0 {
		t.Fatalf("file-based plan should not keep ChunkData, got %d entries", len(plan.ChunkData))
	}
	if len(plan.ChunkFiles) != 2 || len(plan.Tasks) != 2 {
		t.Fatalf("chunks/tasks = %d/%d, want 2/2", len(plan.ChunkFiles), len(plan.Tasks))
	}

	task := plan.Tasks[0]
	if task.ToolName != "ffuf" || task.JobID != "job-123" {
		t.Fatalf("unexpected task identity: %#v", task)
	}
	if task.InputKey != "scans/ffuf/job-123/inputs/chunk_0.txt" {
		t.Fatalf("InputKey = %q", task.InputKey)
	}
	if task.ChunkIdx != 0 || task.TotalChunks != 2 {
		t.Fatalf("chunk metadata = %d/%d, want 0/2", task.ChunkIdx, task.TotalChunks)
	}
	if task.GroupID != SafeTargetStem("https://example.com/FUZZ") {
		t.Fatalf("GroupID = %q", task.GroupID)
	}
	if plan.ChunkFiles[0].Key != task.InputKey {
		t.Fatalf("chunk key = %q, task key = %q", plan.ChunkFiles[0].Key, task.InputKey)
	}
	if plan.ChunkFiles[0].ByteSize == 0 || plan.ChunkFiles[0].WordCount == 0 {
		t.Fatalf("chunk metadata not populated: %#v", plan.ChunkFiles[0])
	}
}

func TestPlanWordlistFileInputKeysRemainStable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "words.txt")
	if err := os.WriteFile(path, []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatalf("write wordlist: %v", err)
	}

	plan, err := PlanWordlistFile("gobuster", "job-abc", "example.com", "", path, t.TempDir(), 3, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer cleanupWordlistPlan(t, plan)

	for i, task := range plan.Tasks {
		want := InputKey("gobuster", "job-abc", i)
		if task.InputKey != want {
			t.Fatalf("task %d InputKey = %q, want %q", i, task.InputKey, want)
		}
	}
}

type uploadRecord struct {
	key  string
	size int
}

type recordingStorage struct {
	uploads       []uploadRecord
	failKey       string
	maxUploadSize int
}

func (s *recordingStorage) Upload(_ context.Context, _, key string, data []byte) error {
	if key == s.failKey {
		return errors.New("upload failed")
	}
	s.uploads = append(s.uploads, uploadRecord{key: key, size: len(data)})
	if len(data) > s.maxUploadSize {
		s.maxUploadSize = len(data)
	}
	return nil
}

func (s *recordingStorage) Download(context.Context, string, string) ([]byte, error) { return nil, nil }
func (s *recordingStorage) List(context.Context, string, string) ([]string, error)   { return nil, nil }
func (s *recordingStorage) Count(context.Context, string, string) (int, error)       { return 0, nil }

func TestUploadChunksReadsFileChunksOneAtATime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "words.txt")
	if err := os.WriteFile(path, []byte("a\nb\nc\nd\n"), 0o644); err != nil {
		t.Fatalf("write wordlist: %v", err)
	}
	plan, err := PlanWordlistFile("ffuf", "job-123", "https://example.com/FUZZ", "", path, t.TempDir(), 2, 1)
	if err != nil {
		t.Fatalf("plan wordlist file: %v", err)
	}
	defer cleanupWordlistPlan(t, plan)

	storage := &recordingStorage{}
	if err := UploadChunks(context.Background(), storage, "bucket", plan); err != nil {
		t.Fatalf("upload chunks: %v", err)
	}
	if len(storage.uploads) != 2 {
		t.Fatalf("uploads = %d, want 2", len(storage.uploads))
	}
	for i, upload := range storage.uploads {
		if upload.size != int(plan.ChunkFiles[i].ByteSize) {
			t.Fatalf("upload %d size = %d, want %d", i, upload.size, plan.ChunkFiles[i].ByteSize)
		}
		if upload.size > int(plan.MaxChunkSize) {
			t.Fatalf("upload %d exceeded max chunk size: %d", i, upload.size)
		}
	}
}

func TestUploadChunksFailureIncludesChunkIndexAndKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "words.txt")
	if err := os.WriteFile(path, []byte("a\nb\n"), 0o644); err != nil {
		t.Fatalf("write wordlist: %v", err)
	}
	plan, err := PlanWordlistFile("ffuf", "job-123", "https://example.com/FUZZ", "", path, t.TempDir(), 2, 1)
	if err != nil {
		t.Fatalf("plan wordlist file: %v", err)
	}
	defer cleanupWordlistPlan(t, plan)

	failKey := InputKey("ffuf", "job-123", 1)
	err = UploadChunks(context.Background(), &recordingStorage{failKey: failKey}, "bucket", plan)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "chunk 1") || !strings.Contains(err.Error(), failKey) {
		t.Fatalf("error should include chunk index and key, got %v", err)
	}
}

func TestUploadChunksRejectsOversizedChunkBeforeRead(t *testing.T) {
	plan := &WordlistPlan{
		MaxChunkSize: 10,
		ChunkFiles: []WordlistChunk{{
			Path:     filepath.Join(t.TempDir(), "missing.txt"),
			Key:      "scans/ffuf/job-123/inputs/chunk_0.txt",
			ByteSize: 11,
			Index:    0,
		}},
	}

	err := UploadChunks(context.Background(), &recordingStorage{}, "bucket", plan)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "above max safe chunk size") {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(err.Error(), "reading chunk") {
		t.Fatalf("expected max-size error before reading file, got %v", err)
	}
}

func TestInputPrefix(t *testing.T) {
	got := InputPrefix("ffuf", "job-123")
	want := "scans/ffuf/job-123/inputs/"
	if got != want {
		t.Fatalf("InputPrefix() = %q, want %q", got, want)
	}
}

func TestInputKey(t *testing.T) {
	got := InputKey("ffuf", "job-123", 2)
	want := "scans/ffuf/job-123/inputs/chunk_2.txt"
	if got != want {
		t.Fatalf("InputKey() = %q, want %q", got, want)
	}
}

func TestSafeTargetStem(t *testing.T) {
	if got := SafeTargetStem("example.com"); got != "example.com" {
		t.Fatalf("SafeTargetStem(example.com) = %q, want example.com", got)
	}
	if got := SafeTargetStem("10.0.0.1"); got != "10.0.0.1" {
		t.Fatalf("SafeTargetStem(10.0.0.1) = %q, want 10.0.0.1", got)
	}

	urlA := "https://example.com/a-b"
	urlB := "https://example.com/a/b"
	stemA := SafeTargetStem(urlA)
	stemB := SafeTargetStem(urlB)

	if strings.Contains(stemA, "/") || strings.Contains(stemA, ":") {
		t.Fatalf("unsafe URL stem should be sanitized, got %q", stemA)
	}
	if strings.Contains(stemB, "/") || strings.Contains(stemB, ":") {
		t.Fatalf("unsafe URL stem should be sanitized, got %q", stemB)
	}
	if stemA == "https---example.com-a-b" {
		t.Fatalf("unsafe URL stem should be disambiguated with a hash, got %q", stemA)
	}
	if stemA == stemB {
		t.Fatalf("distinct unsafe targets should not share a stem: %q", stemA)
	}
	if SafeTargetStem(urlA) != stemA {
		t.Fatal("SafeTargetStem should be deterministic for the same input")
	}
}
