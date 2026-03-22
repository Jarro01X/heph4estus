package worker

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"heph4estus/internal/cloud"
	"heph4estus/internal/logger"
	"heph4estus/internal/modules"
)

// Executor runs a module command for a given task.
type Executor struct {
	log     logger.Logger
	storage cloud.Storage
	bucket  string
}

// NewExecutor creates a new Executor.
func NewExecutor(log logger.Logger, storage cloud.Storage, bucket string) *Executor {
	return &Executor{
		log:     log,
		storage: storage,
		bucket:  bucket,
	}
}

// Execute runs the module command, handling input/output file management.
// Returns the Result and output file bytes (nil if no output file was produced).
func (e *Executor) Execute(ctx context.Context, mod *modules.ModuleDefinition, task Task) (Result, []byte, error) {
	result := Result{
		ToolName:  mod.Name,
		Target:    task.Target,
		Timestamp: time.Now(),
	}

	tempDir, err := os.MkdirTemp("", "heph-worker-*")
	if err != nil {
		return result, nil, fmt.Errorf("creating temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	inputPath := filepath.Join(tempDir, "input")
	outputPath := filepath.Join(tempDir, "output."+mod.OutputExt)

	// Prepare input file.
	if task.InputKey != "" {
		data, err := e.storage.Download(ctx, e.bucket, task.InputKey)
		if err != nil {
			return result, nil, fmt.Errorf("downloading input %s: %w", task.InputKey, err)
		}
		if err := os.WriteFile(inputPath, data, 0600); err != nil {
			return result, nil, fmt.Errorf("writing input file: %w", err)
		}
	} else if CommandUsesPlaceholder(mod.Command, "input") || CommandUsesPlaceholder(mod.Command, "wordlist") {
		if err := os.WriteFile(inputPath, []byte(task.Target+"\n"), 0600); err != nil {
			return result, nil, fmt.Errorf("writing target to input file: %w", err)
		}
	}

	// Render command template.
	vars := TemplateVars{
		Input:   inputPath,
		Output:  outputPath,
		Target:  task.Target,
		Options: task.Options,
	}
	rendered := RenderCommand(mod.Command, vars)
	e.log.Info("Executing: %s", rendered)

	// Execute with module timeout.
	timeout := mod.TimeoutDuration()
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(execCtx, "sh", "-c", rendered)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}

	// Set module-defined environment variables.
	if len(mod.Env) > 0 {
		cmd.Env = os.Environ()
		for k, v := range mod.Env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}

	output, execErr := cmd.CombinedOutput()
	result.Output = string(output)

	if execErr != nil {
		if execCtx.Err() == context.DeadlineExceeded {
			result.Error = fmt.Sprintf("command timed out after %v", timeout)
		} else {
			result.Error = execErr.Error()
		}
	}

	// Read output file if it exists.
	var outputBytes []byte
	if _, statErr := os.Stat(outputPath); statErr == nil {
		outputBytes, err = os.ReadFile(outputPath)
		if err != nil {
			e.log.Error("Failed to read output file: %v", err)
		}
	}

	return result, outputBytes, nil
}
