package operator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// stubStorage implements cloud.Storage for export tests.
type stubStorage struct {
	objects map[string][]byte // key → data
}

func (s *stubStorage) Upload(context.Context, string, string, []byte) error { return nil }
func (s *stubStorage) Count(context.Context, string, string) (int, error)   { return 0, nil }

func (s *stubStorage) List(_ context.Context, _, prefix string) ([]string, error) {
	var keys []string
	for k := range s.objects {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

func (s *stubStorage) Download(_ context.Context, _, key string) ([]byte, error) {
	data, ok := s.objects[key]
	if !ok {
		return nil, fmt.Errorf("not found: %s", key)
	}
	return data, nil
}

func TestExportJobWritesResultsAndArtifacts(t *testing.T) {
	store := &stubStorage{objects: map[string][]byte{
		"scans/httpx/job-1/results/example.com_123.json": []byte(`{"target":"example.com"}`),
		"scans/httpx/job-1/results/test.com_456.json":    []byte(`{"target":"test.com"}`),
		"scans/httpx/job-1/artifacts/example.com_123.xml": []byte("<xml/>"),
	}}

	outDir := t.TempDir()
	result, err := ExportJob(context.Background(), store, "bucket", "httpx", "job-1", outDir)
	if err != nil {
		t.Fatalf("ExportJob: %v", err)
	}

	if result.ResultCount != 2 {
		t.Errorf("ResultCount = %d, want 2", result.ResultCount)
	}
	if result.ArtifactCount != 1 {
		t.Errorf("ArtifactCount = %d, want 1", result.ArtifactCount)
	}

	expectedDir := filepath.Join(outDir, "httpx", "job-1")
	if result.Dir != expectedDir {
		t.Errorf("Dir = %q, want %q", result.Dir, expectedDir)
	}

	// Verify files exist.
	for _, rel := range []string{
		"results/example.com_123.json",
		"results/test.com_456.json",
		"artifacts/example.com_123.xml",
	} {
		path := filepath.Join(expectedDir, rel)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected file %s to exist: %v", rel, err)
		}
	}

	// Verify content.
	data, err := os.ReadFile(filepath.Join(expectedDir, "results", "example.com_123.json"))
	if err != nil {
		t.Fatalf("reading result file: %v", err)
	}
	if string(data) != `{"target":"example.com"}` {
		t.Errorf("unexpected content: %s", data)
	}
}

func TestExportJobEmptyPrefixes(t *testing.T) {
	store := &stubStorage{objects: map[string][]byte{}}

	outDir := t.TempDir()
	result, err := ExportJob(context.Background(), store, "bucket", "nmap", "job-2", outDir)
	if err != nil {
		t.Fatalf("ExportJob: %v", err)
	}
	if result.ResultCount != 0 {
		t.Errorf("ResultCount = %d, want 0", result.ResultCount)
	}
	if result.ArtifactCount != 0 {
		t.Errorf("ArtifactCount = %d, want 0", result.ArtifactCount)
	}
}

func TestExportJobDownloadFailure(t *testing.T) {
	// Use a storage that lists a key but fails to download it.
	failStore := &downloadFailStorage{
		listKeys: []string{"scans/nmap/job-3/results/bad_123.json"},
	}

	outDir := t.TempDir()
	_, err := ExportJob(context.Background(), failStore, "bucket", "nmap", "job-3", outDir)
	if err == nil {
		t.Fatal("expected error from download failure")
	}
}

type downloadFailStorage struct {
	listKeys []string
}

func (s *downloadFailStorage) Upload(context.Context, string, string, []byte) error { return nil }
func (s *downloadFailStorage) Count(context.Context, string, string) (int, error)   { return 0, nil }

func (s *downloadFailStorage) List(_ context.Context, _, prefix string) ([]string, error) {
	var keys []string
	for _, k := range s.listKeys {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

func (s *downloadFailStorage) Download(context.Context, string, string) ([]byte, error) {
	return nil, fmt.Errorf("simulated download failure")
}

func TestExportJobNestedArtifacts(t *testing.T) {
	store := &stubStorage{objects: map[string][]byte{
		"scans/nmap/job-4/artifacts/10.0.0.1/scan_123.xml": []byte("<xml/>"),
	}}

	outDir := t.TempDir()
	result, err := ExportJob(context.Background(), store, "bucket", "nmap", "job-4", outDir)
	if err != nil {
		t.Fatalf("ExportJob: %v", err)
	}
	if result.ArtifactCount != 1 {
		t.Errorf("ArtifactCount = %d, want 1", result.ArtifactCount)
	}

	path := filepath.Join(outDir, "nmap", "job-4", "artifacts", "10.0.0.1", "scan_123.xml")
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected nested artifact to exist: %v", err)
	}
}
