package worker

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"heph4estus/internal/modules"
)

type mockStorage struct {
	data      map[string][]byte
	uploadErr error
}

func (s *mockStorage) Download(ctx context.Context, bucket, key string) ([]byte, error) {
	if data, ok := s.data[key]; ok {
		return data, nil
	}
	return nil, os.ErrNotExist
}

func (s *mockStorage) Upload(ctx context.Context, bucket, key string, data []byte) error {
	return s.uploadErr
}

func (s *mockStorage) List(ctx context.Context, bucket, prefix string) ([]string, error) {
	return nil, nil
}

func (s *mockStorage) Count(ctx context.Context, bucket, prefix string) (int, error) {
	return 0, nil
}

type mockLogger struct{}

func (l *mockLogger) Info(format string, args ...interface{})  {}
func (l *mockLogger) Error(format string, args ...interface{}) {}
func (l *mockLogger) Fatal(format string, args ...interface{}) {}

func TestExecute_SimpleCommand(t *testing.T) {
	mod := &modules.ModuleDefinition{
		Name:          "test",
		Command:       "echo hello > {{output}}",
		InputType:     "target_list",
		OutputExt:     "txt",
		InstallCmd:    "true",
		DefaultCPU:    256,
		DefaultMemory: 512,
		Timeout:       "1m",
	}

	executor := NewExecutor(&mockLogger{}, &mockStorage{data: map[string][]byte{}}, "test-bucket")
	task := Task{ToolName: "test", Target: "example.com"}

	result, outputBytes, err := executor.Execute(context.Background(), mod, task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected result error: %s", result.Error)
	}
	if !strings.Contains(string(outputBytes), "hello") {
		t.Fatalf("expected output to contain 'hello', got %q", string(outputBytes))
	}
}

func TestExecute_Timeout(t *testing.T) {
	mod := &modules.ModuleDefinition{
		Name:          "slow",
		Command:       "sleep 10",
		InputType:     "target_list",
		OutputExt:     "txt",
		InstallCmd:    "true",
		DefaultCPU:    256,
		DefaultMemory: 512,
		Timeout:       "100ms",
	}

	executor := NewExecutor(&mockLogger{}, &mockStorage{data: map[string][]byte{}}, "test-bucket")
	task := Task{ToolName: "slow", Target: "example.com"}

	start := time.Now()
	result, _, err := executor.Execute(context.Background(), mod, task)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Error, "command timed out") {
		t.Fatalf("expected timeout error, got %q", result.Error)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("timeout took too long: %v", elapsed)
	}
}

func TestExecute_InputFromTarget(t *testing.T) {
	mod := &modules.ModuleDefinition{
		Name:          "reader",
		Command:       "cat {{input}} > {{output}}",
		InputType:     "target_list",
		OutputExt:     "txt",
		InstallCmd:    "true",
		DefaultCPU:    256,
		DefaultMemory: 512,
		Timeout:       "1m",
	}

	executor := NewExecutor(&mockLogger{}, &mockStorage{data: map[string][]byte{}}, "test-bucket")
	task := Task{ToolName: "reader", Target: "192.168.1.1"}

	result, outputBytes, err := executor.Execute(context.Background(), mod, task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected result error: %s", result.Error)
	}
	if !strings.Contains(string(outputBytes), "192.168.1.1") {
		t.Fatalf("expected output to contain target, got %q", string(outputBytes))
	}
}

func TestExecute_InputFromS3(t *testing.T) {
	storage := &mockStorage{
		data: map[string][]byte{
			"inputs/targets.txt": []byte("10.0.0.1\n10.0.0.2\n"),
		},
	}

	mod := &modules.ModuleDefinition{
		Name:          "reader",
		Command:       "cat {{input}} > {{output}}",
		InputType:     "target_list",
		OutputExt:     "txt",
		InstallCmd:    "true",
		DefaultCPU:    256,
		DefaultMemory: 512,
		Timeout:       "1m",
	}

	executor := NewExecutor(&mockLogger{}, storage, "test-bucket")
	task := Task{ToolName: "reader", Target: "10.0.0.1", InputKey: "inputs/targets.txt"}

	result, outputBytes, err := executor.Execute(context.Background(), mod, task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected result error: %s", result.Error)
	}
	if !strings.Contains(string(outputBytes), "10.0.0.1") || !strings.Contains(string(outputBytes), "10.0.0.2") {
		t.Fatalf("expected output to contain both targets, got %q", string(outputBytes))
	}
}

func TestExecute_NoOutputFile(t *testing.T) {
	mod := &modules.ModuleDefinition{
		Name:          "noout",
		Command:       "echo 'inline output'",
		InputType:     "target_list",
		OutputExt:     "txt",
		InstallCmd:    "true",
		DefaultCPU:    256,
		DefaultMemory: 512,
		Timeout:       "1m",
	}

	executor := NewExecutor(&mockLogger{}, &mockStorage{data: map[string][]byte{}}, "test-bucket")
	task := Task{ToolName: "noout", Target: "example.com"}

	result, outputBytes, err := executor.Execute(context.Background(), mod, task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected result error: %s", result.Error)
	}
	if outputBytes != nil {
		t.Fatalf("expected nil output bytes when no output file, got %d bytes", len(outputBytes))
	}
	if !strings.Contains(result.Output, "inline output") {
		t.Fatalf("expected stdout capture, got %q", result.Output)
	}
}

func TestExecute_CommandFailure(t *testing.T) {
	mod := &modules.ModuleDefinition{
		Name:          "fail",
		Command:       "exit 1",
		InputType:     "target_list",
		OutputExt:     "txt",
		InstallCmd:    "true",
		DefaultCPU:    256,
		DefaultMemory: 512,
		Timeout:       "1m",
	}

	executor := NewExecutor(&mockLogger{}, &mockStorage{data: map[string][]byte{}}, "test-bucket")
	task := Task{ToolName: "fail", Target: "example.com"}

	result, _, err := executor.Execute(context.Background(), mod, task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error == "" {
		t.Fatal("expected non-empty result error for failed command")
	}
}

func TestExecute_EnvVars(t *testing.T) {
	mod := &modules.ModuleDefinition{
		Name:          "envtest",
		Command:       "sh -c 'echo $TEST_VAR > {{output}}'",
		InputType:     "target_list",
		OutputExt:     "txt",
		InstallCmd:    "true",
		DefaultCPU:    256,
		DefaultMemory: 512,
		Timeout:       "1m",
		Env:           map[string]string{"TEST_VAR": "hello_from_env"},
	}

	executor := NewExecutor(&mockLogger{}, &mockStorage{data: map[string][]byte{}}, "test-bucket")
	task := Task{ToolName: "envtest", Target: "example.com"}

	result, outputBytes, err := executor.Execute(context.Background(), mod, task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected result error: %s", result.Error)
	}
	if !strings.Contains(string(outputBytes), "hello_from_env") {
		t.Fatalf("expected env var in output, got %q", string(outputBytes))
	}
}

func TestExecute_TempDirCleanup(t *testing.T) {
	mod := &modules.ModuleDefinition{
		Name:          "cleanup",
		Command:       "pwd",
		InputType:     "target_list",
		OutputExt:     "txt",
		InstallCmd:    "true",
		DefaultCPU:    256,
		DefaultMemory: 512,
		Timeout:       "1m",
	}

	executor := NewExecutor(&mockLogger{}, &mockStorage{data: map[string][]byte{}}, "test-bucket")
	task := Task{ToolName: "cleanup", Target: "example.com"}

	_, _, _ = executor.Execute(context.Background(), mod, task)

	// Verify temp dirs are cleaned up by checking no heph-worker dirs remain.
	matches, _ := filepath.Glob(os.TempDir() + "/heph-worker-*")
	for _, m := range matches {
		t.Errorf("temp dir not cleaned up: %s", m)
	}
}
