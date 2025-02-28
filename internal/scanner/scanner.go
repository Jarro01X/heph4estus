package scanner

import (
	"context"
	"encoding/json"
	"nmap-scanner/internal/logger"
	"nmap-scanner/internal/models"
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
func (s *Scanner) RunScan(task models.ScanTask) models.ScanResult {
	s.logger.Info("Running nmap scan for target: %s with options: %s", task.Target, task.Options)

	args := append([]string{task.Target}, strings.Fields(task.Options)...)
	s.logger.Info("Executing command: nmap %s", strings.Join(args, " "))

	// Create a context with a timeout (5 minutes)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Create the command with the context
	cmd := exec.CommandContext(ctx, "nmap", args...)
	output, err := cmd.CombinedOutput()

	result := models.ScanResult{
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
func (s *Scanner) ParseTargets(content string, defaultOptions string) []models.ScanTask {
	lines := strings.Split(strings.TrimSpace(content), "\n")
	targets := make([]models.ScanTask, 0, len(lines))

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) == 0 {
			continue
		}

		target := models.ScanTask{
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

// FormatResult formats a scan result as JSON
func (s *Scanner) FormatResult(result models.ScanResult) ([]byte, error) {
	return json.Marshal(result)
}
