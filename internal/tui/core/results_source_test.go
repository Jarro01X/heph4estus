package core

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// mockStorage implements cloud.Storage for S3ResultsSource tests.
type mockStorage struct {
	keys    []string
	listErr error
	data    map[string][]byte
	dlErr   error
}

func (s *mockStorage) Upload(_ context.Context, _, _ string, _ []byte) error { return nil }
func (s *mockStorage) Download(_ context.Context, _, key string) ([]byte, error) {
	if s.dlErr != nil {
		return nil, s.dlErr
	}
	if d, ok := s.data[key]; ok {
		return d, nil
	}
	return nil, fmt.Errorf("not found: %s", key)
}
func (s *mockStorage) List(_ context.Context, _, _ string) ([]string, error) {
	return s.keys, s.listErr
}
func (s *mockStorage) Count(_ context.Context, _, _ string) (int, error) {
	return len(s.keys), nil
}

func TestS3ResultsSource_ListKeys(t *testing.T) {
	keys := []string{"key1.json", "key2.json"}
	s := &S3ResultsSource{
		Storage: &mockStorage{keys: keys},
		Bucket:  "test-bucket",
		Prefix:  "scans/nmap/job/results/",
	}
	got, err := s.ListKeys(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(got))
	}
}

func TestS3ResultsSource_Download(t *testing.T) {
	data := []byte(`{"target":"example.com"}`)
	s := &S3ResultsSource{
		Storage: &mockStorage{data: map[string][]byte{"key1.json": data}},
		Bucket:  "test-bucket",
		Prefix:  "scans/nmap/job/results/",
	}
	got, err := s.Download(context.Background(), "key1.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("expected %q, got %q", data, got)
	}
}

func TestLocalResultsSource_ListKeys(t *testing.T) {
	dir := t.TempDir()
	resultsDir := filepath.Join(dir, "results")
	if err := os.MkdirAll(resultsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Write two result files.
	for _, name := range []string{"target1_1000.json", "target2_1001.json"} {
		data, _ := json.Marshal(map[string]string{"target": name})
		if err := os.WriteFile(filepath.Join(resultsDir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	src := &LocalResultsSource{ResultsDir: resultsDir}
	keys, err := src.ListKeys(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(keys))
	}
}

func TestLocalResultsSource_Download(t *testing.T) {
	dir := t.TempDir()
	resultsDir := filepath.Join(dir, "results")
	if err := os.MkdirAll(resultsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	content := []byte(`{"target":"192.168.1.1","output":"open ports"}`)
	if err := os.WriteFile(filepath.Join(resultsDir, "192.168.1.1_1000.json"), content, 0o644); err != nil {
		t.Fatal(err)
	}

	src := &LocalResultsSource{ResultsDir: resultsDir}
	got, err := src.Download(context.Background(), "192.168.1.1_1000.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("expected %q, got %q", content, got)
	}
}

func TestLocalResultsSource_EmptyDir(t *testing.T) {
	src := &LocalResultsSource{ResultsDir: filepath.Join(t.TempDir(), "nonexistent")}
	keys, err := src.ListKeys(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(keys) != 0 {
		t.Fatalf("expected 0 keys for nonexistent dir, got %d", len(keys))
	}
}

func TestLocalResultsSource_NestedFiles(t *testing.T) {
	dir := t.TempDir()
	resultsDir := filepath.Join(dir, "results")
	nestedDir := filepath.Join(resultsDir, "group1")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(nestedDir, "chunk0.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	src := &LocalResultsSource{ResultsDir: resultsDir}
	keys, err := src.ListKeys(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(keys))
	}
	if keys[0] != "group1/chunk0.json" {
		t.Fatalf("expected 'group1/chunk0.json', got %q", keys[0])
	}

	// Download nested key.
	data, err := src.Download(context.Background(), "group1/chunk0.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != "{}" {
		t.Fatalf("expected '{}', got %q", data)
	}
}

func TestTerraformDestroyer(t *testing.T) {
	called := false
	var gotWorkDir string
	d := &TerraformDestroyer{
		DestroyFunc: func(_ context.Context, workDir string) error {
			called = true
			gotWorkDir = workDir
			return nil
		},
		WorkDir: "/tmp/tf",
	}

	err := d.Destroy(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("expected DestroyFunc to be called")
	}
	if gotWorkDir != "/tmp/tf" {
		t.Fatalf("expected workDir '/tmp/tf', got %q", gotWorkDir)
	}
}

func TestTerraformDestroyer_Error(t *testing.T) {
	d := &TerraformDestroyer{
		DestroyFunc: func(_ context.Context, _ string) error {
			return fmt.Errorf("terraform error")
		},
		WorkDir: "/tmp/tf",
	}

	err := d.Destroy(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "terraform error" {
		t.Fatalf("expected 'terraform error', got %q", err.Error())
	}
}
