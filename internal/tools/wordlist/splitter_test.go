package wordlist

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeWordlist(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "words.txt")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write wordlist: %v", err)
	}
	return path
}

func readChunk(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read chunk: %v", err)
	}
	return string(data)
}

func TestSplitFileRejectsEmptyFile(t *testing.T) {
	path := writeWordlist(t, "")

	_, err := SplitFile(path, t.TempDir(), Policy{}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "no entries found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSplitFileRejectsBlankLineOnlyFile(t *testing.T) {
	path := writeWordlist(t, "\n\n\n")

	_, err := SplitFile(path, t.TempDir(), Policy{}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "no entries found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSplitFileTinyFile(t *testing.T) {
	path := writeWordlist(t, "admin\nlogin\n")

	result, err := SplitFile(path, t.TempDir(), Policy{WorkerCount: 1}, func(i int) string {
		return "key"
	})
	if err != nil {
		t.Fatalf("split wordlist: %v", err)
	}
	defer result.Cleanup()

	if result.TotalWords != 2 {
		t.Fatalf("TotalWords = %d, want 2", result.TotalWords)
	}
	if result.TotalSourceBytes != int64(len("admin\nlogin\n")) {
		t.Fatalf("TotalSourceBytes = %d", result.TotalSourceBytes)
	}
	if result.EffectiveChunks != 1 || len(result.Chunks) != 1 {
		t.Fatalf("chunks = %d/%d, want 1", result.EffectiveChunks, len(result.Chunks))
	}
	if got := readChunk(t, result.Chunks[0].Path); got != "admin\nlogin\n" {
		t.Fatalf("chunk data = %q", got)
	}
	if result.Chunks[0].Key != "key" {
		t.Fatalf("chunk key = %q", result.Chunks[0].Key)
	}
}

func TestSplitFileExactSizeSplits(t *testing.T) {
	path := writeWordlist(t, "abc\ndef\n")

	result, err := SplitFile(path, t.TempDir(), Policy{RequestedChunks: 2, TargetChunkSize: 4}, nil)
	if err != nil {
		t.Fatalf("split wordlist: %v", err)
	}
	defer result.Cleanup()

	if len(result.Chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(result.Chunks))
	}
	if got := readChunk(t, result.Chunks[0].Path); got != "abc\n" {
		t.Fatalf("chunk 0 = %q", got)
	}
	if got := readChunk(t, result.Chunks[1].Path); got != "def\n" {
		t.Fatalf("chunk 1 = %q", got)
	}
}

func TestSplitFileUnevenSizeSplits(t *testing.T) {
	path := writeWordlist(t, "aa\nbb\ncc\n")

	result, err := SplitFile(path, t.TempDir(), Policy{RequestedChunks: 2, TargetChunkSize: 5}, nil)
	if err != nil {
		t.Fatalf("split wordlist: %v", err)
	}
	defer result.Cleanup()

	if len(result.Chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(result.Chunks))
	}
	if got := readChunk(t, result.Chunks[0].Path); got != "aa\n" {
		t.Fatalf("chunk 0 = %q", got)
	}
	if got := readChunk(t, result.Chunks[1].Path); got != "bb\ncc\n" {
		t.Fatalf("chunk 1 = %q", got)
	}
}

func TestSplitFileExplicitChunkCount(t *testing.T) {
	path := writeWordlist(t, "a\nb\nc\nd\n")

	result, err := SplitFile(path, t.TempDir(), Policy{RequestedChunks: 3}, nil)
	if err != nil {
		t.Fatalf("split wordlist: %v", err)
	}
	defer result.Cleanup()

	if result.EffectiveChunks != 3 {
		t.Fatalf("EffectiveChunks = %d, want 3", result.EffectiveChunks)
	}
	for i, chunk := range result.Chunks {
		if chunk.Index != i {
			t.Fatalf("chunk index = %d, want %d", chunk.Index, i)
		}
		if chunk.TotalChunks != 3 {
			t.Fatalf("TotalChunks = %d, want 3", chunk.TotalChunks)
		}
	}
}

func TestSplitFileAutoChunkCountFromSizeAndWorkers(t *testing.T) {
	path := writeWordlist(t, "a\nb\nc\nd\n")

	result, err := SplitFile(path, t.TempDir(), Policy{WorkerCount: 3, TargetChunkSize: 4}, nil)
	if err != nil {
		t.Fatalf("split wordlist: %v", err)
	}
	defer result.Cleanup()

	if result.EffectiveChunks != 3 {
		t.Fatalf("EffectiveChunks = %d, want 3", result.EffectiveChunks)
	}
}

func TestSplitFileNoEmptyChunks(t *testing.T) {
	path := writeWordlist(t, "a\nb\n")

	result, err := SplitFile(path, t.TempDir(), Policy{RequestedChunks: 10}, nil)
	if err != nil {
		t.Fatalf("split wordlist: %v", err)
	}
	defer result.Cleanup()

	if result.EffectiveChunks != 2 {
		t.Fatalf("EffectiveChunks = %d, want 2", result.EffectiveChunks)
	}
	for i, chunk := range result.Chunks {
		if chunk.WordCount == 0 {
			t.Fatalf("chunk %d is empty", i)
		}
	}
}

func TestSplitFilePreservesRawEntries(t *testing.T) {
	path := writeWordlist(t, "#comment\n admin\ntrailing \n\n")

	result, err := SplitFile(path, t.TempDir(), Policy{}, nil)
	if err != nil {
		t.Fatalf("split wordlist: %v", err)
	}
	defer result.Cleanup()

	if got := readChunk(t, result.Chunks[0].Path); got != "#comment\n admin\ntrailing \n" {
		t.Fatalf("chunk data = %q", got)
	}
}

func TestSplitFileOverlongLineError(t *testing.T) {
	path := writeWordlist(t, strings.Repeat("a", 17)+"\n")

	_, err := SplitFile(path, t.TempDir(), Policy{ScannerMaxTokenSize: 16}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "scanner max token size 16 bytes") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSplitFileRejectsUnsafeExplicitChunkSize(t *testing.T) {
	path := writeWordlist(t, "a\n")
	if err := os.Truncate(path, 21); err != nil {
		t.Fatalf("truncate wordlist: %v", err)
	}

	_, err := SplitFile(path, t.TempDir(), Policy{RequestedChunks: 2, MaxChunkSize: 10}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "increase --chunks") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSplitFileRejectsChunkThatWouldExceedMaxSize(t *testing.T) {
	path := writeWordlist(t, "aaaaaaaa\nbbbbbbbb\nc\n")
	tempDir := t.TempDir()

	_, err := SplitFile(path, tempDir, Policy{
		RequestedChunks:     2,
		MaxChunkSize:        10,
		ScannerMaxTokenSize: 64,
	}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "would exceed max safe chunk size") {
		t.Fatalf("unexpected error: %v", err)
	}
	entries, readErr := os.ReadDir(tempDir)
	if readErr != nil {
		t.Fatalf("read temp dir: %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("expected temp dir cleanup after split failure, found %d files", len(entries))
	}
}

func TestSplitFileCleansOpenChunkOnScannerError(t *testing.T) {
	path := writeWordlist(t, "ok\n"+strings.Repeat("x", 20)+"\n")
	tempDir := t.TempDir()

	_, err := SplitFile(path, tempDir, Policy{ScannerMaxTokenSize: 16}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "scanner max token size") {
		t.Fatalf("unexpected error: %v", err)
	}
	entries, readErr := os.ReadDir(tempDir)
	if readErr != nil {
		t.Fatalf("read temp dir: %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("expected open chunk cleanup after scanner error, found %d files", len(entries))
	}
}

func TestSplitFileCleanupRemovesTempFiles(t *testing.T) {
	path := writeWordlist(t, "a\nb\n")

	result, err := SplitFile(path, t.TempDir(), Policy{RequestedChunks: 2}, nil)
	if err != nil {
		t.Fatalf("split wordlist: %v", err)
	}
	paths := make([]string, len(result.Chunks))
	for i, chunk := range result.Chunks {
		paths[i] = chunk.Path
		if _, err := os.Stat(chunk.Path); err != nil {
			t.Fatalf("chunk should exist before cleanup: %v", err)
		}
	}

	if err := result.Cleanup(); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	for _, path := range paths {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("expected %s to be removed, stat err=%v", path, err)
		}
	}
}
