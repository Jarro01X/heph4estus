package infra

import (
	"bytes"
	"context"
	"io"
	"os/exec"
)

// CommandResult holds the output of a command execution.
type CommandResult struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// CommandExecutor runs a command with the given working directory, output stream, and arguments.
// The first element of args is the binary name; remaining elements are its arguments.
type CommandExecutor func(ctx context.Context, dir string, stream io.Writer, args ...string) (*CommandResult, error)

// DefaultExecutor runs commands via os/exec.
func DefaultExecutor(ctx context.Context, dir string, stream io.Writer, args ...string) (*CommandResult, error) {
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	if dir != "" {
		cmd.Dir = dir
	}

	var stdoutBuf, stderrBuf bytes.Buffer

	if stream != nil {
		cmd.Stdout = io.MultiWriter(&stdoutBuf, stream)
		cmd.Stderr = io.MultiWriter(&stderrBuf, stream)
	} else {
		cmd.Stdout = &stdoutBuf
		cmd.Stderr = &stderrBuf
	}

	err := cmd.Run()

	exitCode := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		exitCode = exitErr.ExitCode()
	}

	return &CommandResult{
		Stdout:   stdoutBuf.Bytes(),
		Stderr:   stderrBuf.Bytes(),
		ExitCode: exitCode,
	}, err
}
