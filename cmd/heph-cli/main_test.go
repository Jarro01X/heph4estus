package main

import (
	"strings"
	"testing"

	"heph4estus/internal/logger"
)

func testLogger() logger.Logger {
	return logger.NewSimpleLogger()
}

func TestNoCommand(t *testing.T) {
	err := run([]string{}, testLogger())
	if err == nil {
		t.Fatal("expected error for no command")
	}
	if !strings.Contains(err.Error(), "no command specified") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUnknownCommand(t *testing.T) {
	err := run([]string{"bogus"}, testLogger())
	if err == nil {
		t.Fatal("expected error for unknown command")
	}
	if !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHelpFlag(t *testing.T) {
	for _, flag := range []string{"--help", "-help", "-h"} {
		err := run([]string{flag}, testLogger())
		if err != nil {
			t.Fatalf("help flag %q returned error: %v", flag, err)
		}
	}
}

func TestNmapMissingFile(t *testing.T) {
	err := run([]string{"nmap"}, testLogger())
	if err == nil {
		t.Fatal("expected error for nmap without --file")
	}
	if !strings.Contains(err.Error(), "--file flag is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNmapHelp(t *testing.T) {
	err := run([]string{"nmap", "--help"}, testLogger())
	if err == nil {
		t.Fatal("expected flag.ErrHelp wrapped error")
	}
	if !strings.Contains(err.Error(), "flag: help requested") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestScanMissingTool(t *testing.T) {
	err := run([]string{"scan"}, testLogger())
	if err == nil {
		t.Fatal("expected error for scan without --tool")
	}
	if !strings.Contains(err.Error(), "--tool flag is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestScanPlaceholder(t *testing.T) {
	err := run([]string{"scan", "--tool", "nuclei"}, testLogger())
	if err != nil {
		t.Fatalf("scan placeholder returned error: %v", err)
	}
}

func TestInfraNoSubcommand(t *testing.T) {
	err := run([]string{"infra"}, testLogger())
	if err == nil {
		t.Fatal("expected error for infra without subcommand")
	}
	if !strings.Contains(err.Error(), "requires a subcommand") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInfraDeployPlaceholder(t *testing.T) {
	err := run([]string{"infra", "deploy"}, testLogger())
	if err != nil {
		t.Fatalf("infra deploy returned error: %v", err)
	}
}

func TestInfraDestroyPlaceholder(t *testing.T) {
	err := run([]string{"infra", "destroy"}, testLogger())
	if err != nil {
		t.Fatalf("infra destroy returned error: %v", err)
	}
}

func TestInfraUnknownSubcommand(t *testing.T) {
	err := run([]string{"infra", "bogus"}, testLogger())
	if err == nil {
		t.Fatal("expected error for unknown infra subcommand")
	}
	if !strings.Contains(err.Error(), "unknown subcommand") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStatusPlaceholder(t *testing.T) {
	err := run([]string{"status"}, testLogger())
	if err != nil {
		t.Fatalf("status placeholder returned error: %v", err)
	}
}
