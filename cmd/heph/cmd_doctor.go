package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"heph4estus/internal/doctor"
	"heph4estus/internal/logger"
)

func runDoctor(args []string, log logger.Logger) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	format := fs.String("format", "text", "Output format: text or json")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *format != "text" && *format != "json" {
		return fmt.Errorf("--format must be text or json")
	}

	ctx := mainContext()
	results := doctor.RunAll(ctx, doctor.DefaultDeps())

	if *format == "json" {
		return outputDoctorJSON(results)
	}
	return outputDoctorText(results)
}

func outputDoctorJSON(results []doctor.CheckResult) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(results)
}

func outputDoctorText(results []doctor.CheckResult) error {
	passCount, warnCount, failCount := 0, 0, 0

	for _, r := range results {
		var icon string
		switch r.Status {
		case doctor.StatusPass:
			icon = "[PASS]"
			passCount++
		case doctor.StatusWarn:
			icon = "[WARN]"
			warnCount++
		case doctor.StatusFail:
			icon = "[FAIL]"
			failCount++
		}
		_, _ = fmt.Fprintf(os.Stdout, "  %s %s\n", icon, r.Summary)
		if r.Fix != "" {
			_, _ = fmt.Fprintf(os.Stdout, "         -> %s\n", r.Fix)
		}
	}

	_, _ = fmt.Fprintln(os.Stdout)
	_, _ = fmt.Fprintf(os.Stdout, "Results: %d passed, %d warnings, %d failed\n", passCount, warnCount, failCount)

	if failCount > 0 {
		return exitError{code: 1}
	}
	return nil
}

// exitError signals a non-zero exit without printing an additional error message.
type exitError struct{ code int }

func (e exitError) Error() string { return fmt.Sprintf("exit status %d", e.code) }
