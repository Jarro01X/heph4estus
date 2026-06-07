package targetlist

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTargets(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "targets.txt")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write targets: %v", err)
	}
	return path
}

func cleanupSplitResult(t *testing.T, result *Result) {
	t.Helper()
	if err := result.Cleanup(); err != nil {
		t.Fatalf("cleanup split result: %v", err)
	}
}

func TestInspectFileRejectsDirectory(t *testing.T) {
	_, err := InspectFile(t.TempDir(), Policy{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "directory") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInspectFileRejectsEmptyEffectiveTargets(t *testing.T) {
	path := writeTargets(t, "\n# comment\n  # also comment\n\n")
	_, err := InspectFile(path, Policy{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "no targets found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInspectFileCountsTrimmedTargets(t *testing.T) {
	path := writeTargets(t, " example.com \n# comment\n10.0.0.1\n\n")
	meta, err := InspectFile(path, Policy{WorkerCount: 4})
	if err != nil {
		t.Fatalf("inspect target file: %v", err)
	}
	if meta.TotalTargets != 2 {
		t.Fatalf("TotalTargets = %d, want 2", meta.TotalTargets)
	}
	if meta.EffectiveChunks != 2 {
		t.Fatalf("EffectiveChunks = %d, want 2", meta.EffectiveChunks)
	}
}

func TestSplitFileNormalizesAndSplitsOnLineBoundaries(t *testing.T) {
	path := writeTargets(t, " example.com \n10.0.0.1\n# skipped\n10.0.0.2\n")
	result, err := SplitFile(path, t.TempDir(), Policy{RequestedChunks: 2}, func(i int) string {
		return "chunk-key"
	})
	if err != nil {
		t.Fatalf("split target file: %v", err)
	}
	defer cleanupSplitResult(t, result)

	if result.EffectiveChunks != 2 {
		t.Fatalf("EffectiveChunks = %d, want 2", result.EffectiveChunks)
	}
	var combined string
	for _, chunk := range result.Chunks {
		data, err := os.ReadFile(chunk.Path)
		if err != nil {
			t.Fatalf("read chunk: %v", err)
		}
		combined += string(data)
		if chunk.TargetCount == 0 {
			t.Fatal("chunk should not be empty")
		}
	}
	if combined != "example.com\n10.0.0.1\n10.0.0.2\n" {
		t.Fatalf("combined chunks = %q", combined)
	}
}

func TestSplitFileRejectsOverlongLine(t *testing.T) {
	path := writeTargets(t, strings.Repeat("a", 32)+"\n")
	_, err := SplitFile(path, t.TempDir(), Policy{ScannerMaxTokenSize: 16}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "target line exceeds") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStreamTargetsPreservesCurrentParseSemantics(t *testing.T) {
	path := writeTargets(t, " example.com \n# skipped\n\n10.0.0.1\n")
	var got []string
	count, err := StreamTargets(path, 0, func(_ int, target string) error {
		got = append(got, target)
		return nil
	})
	if err != nil {
		t.Fatalf("stream targets: %v", err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}
	want := []string{"example.com", "10.0.0.1"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("targets = %v, want %v", got, want)
	}
}
