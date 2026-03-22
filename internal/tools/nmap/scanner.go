package nmap

import (
	"context"
	"encoding/json"
	"fmt"
	"heph4estus/internal/logger"
	"os/exec"
	"strings"
	"time"
)

// Scanner is responsible for running nmap scans
type Scanner struct {
	logger logger.Logger
}

// NewScanner creates a new scanner
func NewScanner(logger logger.Logger) *Scanner {
	return &Scanner{
		logger: logger,
	}
}

// RunScan runs an nmap scan for the given task
func (s *Scanner) RunScan(task ScanTask) ScanResult {
	s.logger.Info("Running nmap scan for target: %s with options: %s", task.Target, task.Options)

	args := append([]string{task.Target}, strings.Fields(task.Options)...)
	s.logger.Info("Executing command: nmap %s", strings.Join(args, " "))

	// Create a context with a timeout (5 minutes)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Create the command with the context
	cmd := exec.CommandContext(ctx, "nmap", args...)
	output, err := cmd.CombinedOutput()

	result := ScanResult{
		Target:    task.Target,
		Output:    string(output),
		Timestamp: time.Now(),
	}

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			s.logger.Error("Scan timed out after 5 minutes for target: %s", task.Target)
			result.Error = "scan timed out after 5 minutes"
		} else {
			result.Error = err.Error()
			s.logger.Error("Scan error: %v", err)
		}
	} else {
		s.logger.Info("Scan completed successfully")
	}

	return result
}

// ParseTargets parses targets from a file content
func (s *Scanner) ParseTargets(content string, defaultOptions string) []ScanTask {
	lines := strings.Split(strings.TrimSpace(content), "\n")
	targets := make([]ScanTask, 0, len(lines))

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) == 0 {
			continue
		}

		target := ScanTask{
			Target:  parts[0],
			Options: defaultOptions,
		}
		if len(parts) > 1 {
			target.Options = strings.Join(parts[1:], " ")
		}
		targets = append(targets, target)
	}

	return targets
}

// ParseTargetsWithMode parses targets with support for port-splitting distribution.
// In "target-only" mode (or empty), it delegates to ParseTargets.
// In "target-ports" mode, each target's port range is split into portChunks chunks,
// producing one ScanTask per chunk.
func (s *Scanner) ParseTargetsWithMode(content, defaultOptions, mode string, portChunks int) []ScanTask {
	if mode == "" || mode == "target-only" {
		return s.ParseTargets(content, defaultOptions)
	}

	lines := strings.Split(strings.TrimSpace(content), "\n")
	var tasks []ScanTask
	lineNum := 0

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lineNum++

		parts := strings.Fields(line)
		if len(parts) == 0 {
			continue
		}

		target := parts[0]
		options := defaultOptions
		if len(parts) > 1 {
			options = strings.Join(parts[1:], " ")
		}

		portSpec, remainingOptions, found := ExtractPortFlag(options)
		if !found {
			portSpec = "1-65535"
			remainingOptions = options
		}

		ports, err := ParsePortSpec(portSpec)
		if err != nil {
			if s.logger != nil {
				s.logger.Error("Invalid port spec %q for target %s, emitting unsplit: %v", portSpec, target, err)
			}
			tasks = append(tasks, ScanTask{Target: target, Options: options})
			continue
		}

		chunks := SplitPorts(ports, portChunks)
		groupID := fmt.Sprintf("%s_line%d", target, lineNum)

		for i, chunk := range chunks {
			chunkOptions := strings.TrimSpace(remainingOptions + " -p " + FormatPortSpec(chunk))
			tasks = append(tasks, ScanTask{
				Target:      target,
				Options:     chunkOptions,
				GroupID:     groupID,
				ChunkIdx:    i,
				TotalChunks: len(chunks),
			})
		}
	}

	return tasks
}

// FormatResult formats a scan result as JSON
func (s *Scanner) FormatResult(result ScanResult) ([]byte, error) {
	return json.Marshal(result)
}
