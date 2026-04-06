package jobs

import (
	"strings"
	"testing"
)

func TestParseWordlistEntries(t *testing.T) {
	tests := []struct {
		content string
		want    int
	}{
		{"admin\nlogin\napi\n", 3},
		{"# comment\n\nadmin\nlogin\n", 2},
		{"", 0},
		{"\n\n\n", 0},
		{"admin\n# ignore\nlogin\n", 2},
	}
	for _, tt := range tests {
		got := ParseWordlistEntries(tt.content)
		if len(got) != tt.want {
			t.Errorf("ParseWordlistEntries(%q) = %d, want %d", tt.content, len(got), tt.want)
		}
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

func TestPlanWordlistJobEmptyWordlist(t *testing.T) {
	_, err := PlanWordlistJob("ffuf", "job-123", "https://example.com/FUZZ", "", "# only comments\n\n", 2)
	if err == nil {
		t.Fatal("expected error for empty wordlist")
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
