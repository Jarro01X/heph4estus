package main

import (
	"flag"
	"fmt"
	"strconv"
	"strings"

	"heph4estus/internal/cloud"
	"heph4estus/internal/logger"
	naabutool "heph4estus/internal/tools/naabu"
)

func runNaabu(args []string, log logger.Logger) error {
	fs := flag.NewFlagSet("naabu", flag.ContinueOnError)
	inputFile := fs.String("file", "", "Path to file containing targets (required)")
	modeValue := fs.String("mode", string(naabutool.ModeCombined), "Mode: combined or discovery")
	nmapOptions := fs.String("nmap-options", "", "Extra nmap options for combined mode")
	workers := fs.Int("workers", 0, "Number of worker tasks to launch (default: from config or 10)")
	computeMode := fs.String("compute-mode", "", "Compute mode: auto, fargate, or spot (default: from config or auto)")
	placementMode := fs.String("placement", "", "Fleet placement policy: diversity or throughput (default: from config or diversity)")
	maxWorkersPerHost := fs.Int("max-workers-per-host", 0, "Maximum admitted workers per host/public IP (default: from config or policy)")
	minUniqueIPs := fs.Int("min-unique-ips", 0, "Minimum unique public IPv4 addresses required before scan start")
	ipv6Required := fs.Bool("ipv6-required", false, "Require IPv6-validated workers before scan start")
	dualStackRequired := fs.Bool("dual-stack-required", false, "Require workers with both public IPv4 and IPv6-ready public IPv6")
	format := fs.String("format", "text", "Output format: text or json")
	outDir := fs.String("out", "", "Download results/artifacts to this directory after completion")

	noDeploy := fs.Bool("no-deploy", false, "Fail instead of deploying or redeploying infrastructure")
	autoApprove := fs.Bool("auto-approve", false, "Skip deploy confirmation prompts when lifecycle requires deploy")
	destroyAfter := fs.Bool("destroy-after", false, "Destroy infrastructure after the run completes")
	cloudFlag := fs.String("cloud", "", "Cloud provider: "+cloud.SupportedKindsText()+" (default: from config or aws)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	normalizedMode := strings.ToLower(strings.TrimSpace(*modeValue))
	if normalizedMode == "" {
		normalizedMode = string(naabutool.ModeCombined)
	}
	if normalizedMode != string(naabutool.ModeCombined) && normalizedMode != string(naabutool.ModeDiscovery) {
		return fmt.Errorf("--mode must be combined or discovery")
	}
	mode, _ := naabutool.ParseMode(normalizedMode)
	if mode == naabutool.ModeDiscovery && strings.TrimSpace(*nmapOptions) != "" {
		return fmt.Errorf("--nmap-options is only valid with --mode combined")
	}

	return runScan(buildNaabuScanArgs(mode, naabuScanArgsOptions{
		InputFile:         *inputFile,
		NmapOptions:       *nmapOptions,
		Workers:           *workers,
		ComputeMode:       *computeMode,
		PlacementMode:     *placementMode,
		MaxWorkersPerHost: *maxWorkersPerHost,
		MinUniqueIPs:      *minUniqueIPs,
		IPv6Required:      *ipv6Required,
		DualStackRequired: *dualStackRequired,
		Format:            *format,
		OutDir:            *outDir,
		NoDeploy:          *noDeploy,
		AutoApprove:       *autoApprove,
		DestroyAfter:      *destroyAfter,
		Cloud:             *cloudFlag,
	}), log)
}

type naabuScanArgsOptions struct {
	InputFile         string
	NmapOptions       string
	Workers           int
	ComputeMode       string
	PlacementMode     string
	MaxWorkersPerHost int
	MinUniqueIPs      int
	IPv6Required      bool
	DualStackRequired bool
	Format            string
	OutDir            string
	NoDeploy          bool
	AutoApprove       bool
	DestroyAfter      bool
	Cloud             string
}

func buildNaabuScanArgs(mode naabutool.Mode, opts naabuScanArgsOptions) []string {
	scanArgs := []string{"--tool", mode.ModuleName()}
	appendStringFlag := func(name, value string) {
		if strings.TrimSpace(value) != "" {
			scanArgs = append(scanArgs, name, value)
		}
	}
	appendIntFlag := func(name string, value int) {
		if value != 0 {
			scanArgs = append(scanArgs, name, strconv.Itoa(value))
		}
	}
	appendBoolFlag := func(name string, value bool) {
		if value {
			scanArgs = append(scanArgs, name)
		}
	}

	appendStringFlag("--file", opts.InputFile)
	if mode == naabutool.ModeCombined {
		appendStringFlag("--options", opts.NmapOptions)
	}
	appendIntFlag("--workers", opts.Workers)
	appendStringFlag("--compute-mode", opts.ComputeMode)
	appendStringFlag("--placement", opts.PlacementMode)
	appendIntFlag("--max-workers-per-host", opts.MaxWorkersPerHost)
	appendIntFlag("--min-unique-ips", opts.MinUniqueIPs)
	appendBoolFlag("--ipv6-required", opts.IPv6Required)
	appendBoolFlag("--dual-stack-required", opts.DualStackRequired)
	appendStringFlag("--format", opts.Format)
	appendStringFlag("--out", opts.OutDir)
	appendBoolFlag("--no-deploy", opts.NoDeploy)
	appendBoolFlag("--auto-approve", opts.AutoApprove)
	appendBoolFlag("--destroy-after", opts.DestroyAfter)
	appendStringFlag("--cloud", opts.Cloud)

	return scanArgs
}
