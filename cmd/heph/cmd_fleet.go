package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"heph4estus/internal/cloud"
	"heph4estus/internal/fleet"
	"heph4estus/internal/fleetstate"
	"heph4estus/internal/logger"
	"heph4estus/internal/operator"
)

func runFleet(args []string, log logger.Logger) error {
	if len(args) == 0 {
		return fmt.Errorf("fleet requires a subcommand: status, reconcile, reputation, quarantine, unquarantine, rollout, rollback")
	}
	switch args[0] {
	case "status":
		return runFleetStatus(args[1:], log)
	case "reconcile":
		return runFleetReconcile(args[1:], log)
	case "reputation":
		return runFleetReputation(args[1:], log)
	case "quarantine":
		return runFleetQuarantine(args[1:], log)
	case "unquarantine":
		return runFleetUnquarantine(args[1:], log)
	case "rollout":
		return runFleetRollout(args[1:], log)
	case "rollback":
		return runFleetRollback(args[1:], log)
	default:
		return fmt.Errorf("fleet: unknown subcommand %q", args[0])
	}
}

type fleetStatusOutput struct {
	Tool            string                        `json:"tool"`
	Cloud           string                        `json:"cloud"`
	Placement       string                        `json:"placement"`
	ExpectedVersion string                        `json:"expected_version,omitempty"`
	ControllerIP    string                        `json:"controller_ip,omitempty"`
	GenerationID    string                        `json:"generation_id,omitempty"`
	Summary         fleet.FleetSummary            `json:"summary"`
	Rollout         *fleetstate.RolloutRecord     `json:"rollout,omitempty"`
	Reputation      []fleetstate.ReputationRecord `json:"reputation,omitempty"`
}

func runFleetStatus(args []string, log logger.Logger) error {
	fs := flag.NewFlagSet("fleet status", flag.ContinueOnError)
	tool := fs.String("tool", "", "Tool whose provider-native fleet to inspect")
	format := fs.String("format", "text", "Output format: text or json")
	cloudFlag := fs.String("cloud", "", "Cloud provider: "+cloud.SupportedKindsText()+" (required)")
	placementMode := fs.String("placement", "", "Placement policy: diversity or throughput")
	maxWorkersPerHost := fs.Int("max-workers-per-host", 0, "Maximum admitted workers per host/public IP")
	minUniqueIPs := fs.Int("min-unique-ips", 0, "Minimum unique public IPv4 addresses")
	ipv6Required := fs.Bool("ipv6-required", false, "Require IPv6-validated workers")
	dualStackRequired := fs.Bool("dual-stack-required", false, "Require dual-stack workers")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tool == "" {
		return fmt.Errorf("--tool flag is required")
	}
	opCfg, _ := operator.LoadConfig()
	kind, err := resolveCLICloud(*cloudFlag, opCfg)
	if err != nil {
		return err
	}
	placement, err := operator.ResolvePlacementPolicy(fleet.PlacementPolicy{
		Mode:              fleet.PlacementMode(*placementMode),
		MaxWorkersPerHost: *maxWorkersPerHost,
		MinUniqueIPs:      *minUniqueIPs,
		IPv6Required:      *ipv6Required,
		DualStackRequired: *dualStackRequired,
	}, opCfg, 0)
	if err != nil {
		return err
	}
	fctx, err := loadProviderFleetContext(mainContext(), *tool, kind, placement, true, log)
	if err != nil {
		return err
	}
	out := fleetStatusOutput{
		Tool:            *tool,
		Cloud:           string(kind.Canonical()),
		Placement:       fctx.Placement.Summary(),
		ExpectedVersion: fctx.ExpectedVersion,
		ControllerIP:    fctx.Outputs["controller_ip"],
		GenerationID:    fctx.Outputs["generation_id"],
		Summary:         fctx.Snapshot.Summarize(),
		Rollout:         fctx.Rollout,
		Reputation:      fctx.Reputation,
	}
	if *format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}
	return outputFleetStatusText(out)
}

func outputFleetStatusText(out fleetStatusOutput) error {
	_, _ = fmt.Fprintf(os.Stdout, "Tool:        %s\n", out.Tool)
	_, _ = fmt.Fprintf(os.Stdout, "Cloud:       %s\n", out.Cloud)
	if out.ControllerIP != "" {
		_, _ = fmt.Fprintf(os.Stdout, "Controller:  %s\n", out.ControllerIP)
	}
	if out.GenerationID != "" {
		_, _ = fmt.Fprintf(os.Stdout, "Generation:  %s\n", out.GenerationID)
	}
	if out.ExpectedVersion != "" {
		_, _ = fmt.Fprintf(os.Stdout, "Version:     %s\n", out.ExpectedVersion)
	}
	if out.Placement != "" {
		_, _ = fmt.Fprintf(os.Stdout, "Placement:   %s\n", out.Placement)
	}
	_, _ = fmt.Fprintf(os.Stdout, "Workers:     %d registered, %d healthy, %d ready, %d eligible\n",
		out.Summary.RegisteredCount, out.Summary.HealthyCount, out.Summary.ReadyCount, out.Summary.EligibleCount)
	_, _ = fmt.Fprintf(os.Stdout, "IPv4:        %d total unique, %d eligible unique\n", out.Summary.UniqueIPv4Count, out.Summary.UniqueEligibleIPv4Count)
	_, _ = fmt.Fprintf(os.Stdout, "IPv6:        %d ready\n", out.Summary.IPv6ReadyCount)
	if reasons := fleetSummaryReasons(out.Summary.ExcludedByReason); reasons != "" {
		_, _ = fmt.Fprintf(os.Stdout, "Excluded:    %s\n", reasons)
	}
	if out.Rollout != nil {
		_, _ = fmt.Fprintf(os.Stdout, "\nRollout:\n")
		_, _ = fmt.Fprintf(os.Stdout, "  Phase:      %s\n", out.Rollout.Phase)
		if out.Rollout.TargetVersion != "" {
			_, _ = fmt.Fprintf(os.Stdout, "  Target:     %s\n", out.Rollout.TargetVersion)
		}
		if out.Rollout.PreviousVersion != "" {
			_, _ = fmt.Fprintf(os.Stdout, "  Previous:   %s\n", out.Rollout.PreviousVersion)
		}
		if len(out.Rollout.CanaryWorkerIndexes) > 0 {
			_, _ = fmt.Fprintf(os.Stdout, "  Canary:     %v\n", out.Rollout.CanaryWorkerIndexes)
		}
		if len(out.Rollout.PromotedWorkerIndexes) > 0 {
			_, _ = fmt.Fprintf(os.Stdout, "  Promoted:   %v\n", out.Rollout.PromotedWorkerIndexes)
		}
		if len(out.Rollout.DrainingWorkerIndexes) > 0 {
			_, _ = fmt.Fprintf(os.Stdout, "  Draining:   %v\n", out.Rollout.DrainingWorkerIndexes)
		}
		if out.Rollout.RollbackReason != "" {
			_, _ = fmt.Fprintf(os.Stdout, "  Rollback:   %s\n", out.Rollout.RollbackReason)
		}
	}
	return nil
}

func runFleetReputation(args []string, log logger.Logger) error {
	if len(args) > 0 && args[0] == "list" {
		args = args[1:]
	} else if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("fleet reputation only supports the list subcommand")
	}
	fs := flag.NewFlagSet("fleet reputation", flag.ContinueOnError)
	cloudFlag := fs.String("cloud", "", "Cloud provider: "+cloud.SupportedKindsText()+" (required)")
	format := fs.String("format", "text", "Output format: text or json")
	if err := fs.Parse(args); err != nil {
		return err
	}
	opCfg, _ := operator.LoadConfig()
	kind, err := resolveCLICloud(*cloudFlag, opCfg)
	if err != nil {
		return err
	}
	store, err := fleetstate.NewReputationStore()
	if err != nil {
		return err
	}
	records, err := store.List(string(kind.Canonical()))
	if err != nil {
		return err
	}
	if *format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(records)
	}
	if len(records) == 0 {
		_, _ = fmt.Fprintln(os.Stdout, "No reputation records.")
		return nil
	}
	for _, rec := range records {
		_, _ = fmt.Fprintf(os.Stdout, "%-16s %-14s %-20s %s\n", rec.PublicIPv4, rec.State, rec.Reason, rec.UpdatedAt.Format(time.RFC3339))
	}
	return nil
}

func runFleetQuarantine(args []string, log logger.Logger) error {
	fs := flag.NewFlagSet("fleet quarantine", flag.ContinueOnError)
	cloudFlag := fs.String("cloud", "", "Cloud provider: "+cloud.SupportedKindsText()+" (required)")
	ip := fs.String("ip", "", "Public IPv4 to quarantine")
	reason := fs.String("reason", "", "Why the IP is being quarantined")
	stateFlag := fs.String("state", string(fleetstate.ReputationStateQuarantined), "Reputation state: cooling_down, quarantined, or retired")
	durationFlag := fs.String("duration", "24h", "Cooldown/quarantine duration (ignored for retired)")
	notes := fs.String("notes", "", "Optional operator notes")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *ip == "" {
		return fmt.Errorf("--ip flag is required")
	}
	opCfg, _ := operator.LoadConfig()
	kind, err := resolveCLICloud(*cloudFlag, opCfg)
	if err != nil {
		return err
	}
	duration, err := parseDurationFlag(*durationFlag, 24*time.Hour)
	if err != nil {
		return err
	}
	store, err := fleetstate.NewReputationStore()
	if err != nil {
		return err
	}
	state := fleetstate.ReputationState(strings.TrimSpace(*stateFlag))
	if err := store.SetState(string(kind.Canonical()), *ip, *reason, *notes, state, duration); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(os.Stdout, "Set %s to %s\n", *ip, state); err != nil {
		return err
	}
	return nil
}

func runFleetUnquarantine(args []string, log logger.Logger) error {
	fs := flag.NewFlagSet("fleet unquarantine", flag.ContinueOnError)
	cloudFlag := fs.String("cloud", "", "Cloud provider: "+cloud.SupportedKindsText()+" (required)")
	ip := fs.String("ip", "", "Public IPv4 to clear")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *ip == "" {
		return fmt.Errorf("--ip flag is required")
	}
	opCfg, _ := operator.LoadConfig()
	kind, err := resolveCLICloud(*cloudFlag, opCfg)
	if err != nil {
		return err
	}
	store, err := fleetstate.NewReputationStore()
	if err != nil {
		return err
	}
	if err := store.Clear(string(kind.Canonical()), *ip); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(os.Stdout, "Cleared reputation state for %s\n", *ip); err != nil {
		return err
	}
	return nil
}

func runFleetReconcile(args []string, log logger.Logger) error {
	fs := flag.NewFlagSet("fleet reconcile", flag.ContinueOnError)
	tool := fs.String("tool", "", "Tool whose provider-native fleet to reconcile")
	cloudFlag := fs.String("cloud", "", "Cloud provider: "+cloud.SupportedKindsText()+" (required)")
	timeoutFlag := fs.String("timeout", "10m", "How long to wait for repaired workers")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tool == "" {
		return fmt.Errorf("--tool flag is required")
	}
	opCfg, _ := operator.LoadConfig()
	kind, err := resolveCLICloud(*cloudFlag, opCfg)
	if err != nil {
		return err
	}
	timeout, err := parseDurationFlag(*timeoutFlag, 10*time.Minute)
	if err != nil {
		return err
	}
	fctx, err := loadProviderFleetContext(mainContext(), *tool, kind, fleet.PlacementPolicy{}, true, log)
	if err != nil {
		return err
	}
	indexes := repairCandidateIndexes(fctx.Snapshot)
	if len(indexes) == 0 {
		if _, err := fmt.Fprintln(os.Stdout, "No repair candidates found."); err != nil {
			return err
		}
		return nil
	}
	if err := replaceWorkerIndexes(mainContext(), fctx.ToolConfig, kind, indexes, nil, log); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(mainContext(), timeout)
	defer cancel()
	state, err := waitForFleetSnapshot(ctx, kind, fctx.Outputs, fctx.Placement, fctx.ExpectedVersion, fctx.Reputation, fctx.Rollout, fleetWorkerCount(fctx.Outputs), false, log)
	if err != nil {
		return err
	}
	summary := state.Summarize()
	if _, err := fmt.Fprintf(os.Stdout, "Reconciled fleet: %d eligible, %d healthy, %d unique IPv4\n", summary.EligibleCount, summary.HealthyCount, summary.UniqueEligibleIPv4Count); err != nil {
		return err
	}
	return nil
}

func runFleetRollout(args []string, log logger.Logger) error {
	if len(args) == 0 {
		return fmt.Errorf("fleet rollout requires a subcommand: status, start, promote")
	}
	switch args[0] {
	case "status":
		return runFleetRolloutStatus(args[1:], log)
	case "start":
		return runFleetRolloutStart(args[1:], log)
	case "promote":
		return runFleetRolloutPromote(args[1:], log)
	default:
		return fmt.Errorf("fleet rollout: unknown subcommand %q", args[0])
	}
}

func runFleetRolloutStatus(args []string, log logger.Logger) error {
	fs := flag.NewFlagSet("fleet rollout status", flag.ContinueOnError)
	tool := fs.String("tool", "", "Tool whose rollout state to inspect")
	cloudFlag := fs.String("cloud", "", "Cloud provider: "+cloud.SupportedKindsText()+" (required)")
	format := fs.String("format", "text", "Output format: text or json")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tool == "" {
		return fmt.Errorf("--tool flag is required")
	}
	opCfg, _ := operator.LoadConfig()
	kind, err := resolveCLICloud(*cloudFlag, opCfg)
	if err != nil {
		return err
	}
	store, err := fleetstate.NewRolloutStore()
	if err != nil {
		return err
	}
	rollout, err := store.Load(string(kind.Canonical()), *tool)
	if err != nil {
		return err
	}
	if *format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(rollout)
	}
	if rollout == nil {
		if _, err := fmt.Fprintln(os.Stdout, "No rollout state recorded."); err != nil {
			return err
		}
		return nil
	}
	_, _ = fmt.Fprintf(os.Stdout, "Phase:      %s\n", rollout.Phase)
	_, _ = fmt.Fprintf(os.Stdout, "Target:     %s\n", rollout.TargetVersion)
	if rollout.PreviousVersion != "" {
		_, _ = fmt.Fprintf(os.Stdout, "Previous:   %s\n", rollout.PreviousVersion)
	}
	if len(rollout.CanaryWorkerIndexes) > 0 {
		_, _ = fmt.Fprintf(os.Stdout, "Canary:     %v\n", rollout.CanaryWorkerIndexes)
	}
	if len(rollout.PromotedWorkerIndexes) > 0 {
		_, _ = fmt.Fprintf(os.Stdout, "Promoted:   %v\n", rollout.PromotedWorkerIndexes)
	}
	if rollout.RollbackReason != "" {
		_, _ = fmt.Fprintf(os.Stdout, "Rollback:   %s\n", rollout.RollbackReason)
	}
	return nil
}

func runFleetRolloutStart(args []string, log logger.Logger) error {
	fs := flag.NewFlagSet("fleet rollout start", flag.ContinueOnError)
	tool := fs.String("tool", "", "Tool whose provider-native fleet to roll forward")
	cloudFlag := fs.String("cloud", "", "Cloud provider: "+cloud.SupportedKindsText()+" (required)")
	canaryCount := fs.Int("canary-count", 0, "Number of workers to replace in the canary wave")
	autoPromote := fs.Bool("auto-promote", true, "Promote the rollout after a successful canary")
	timeoutFlag := fs.String("timeout", "10m", "How long to wait for canary/promotion health")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tool == "" {
		return fmt.Errorf("--tool flag is required")
	}
	opCfg, _ := operator.LoadConfig()
	kind, err := resolveCLICloud(*cloudFlag, opCfg)
	if err != nil {
		return err
	}
	timeout, err := parseDurationFlag(*timeoutFlag, 10*time.Minute)
	if err != nil {
		return err
	}
	fctx, err := loadProviderFleetContext(mainContext(), *tool, kind, fleet.PlacementPolicy{}, true, log)
	if err != nil {
		return err
	}
	targetVersion := fctx.Outputs["docker_image"]
	if targetVersion == "" {
		return fmt.Errorf("terraform outputs missing docker_image")
	}
	outdated, err := outdatedWorkerIndexes(fctx.Snapshot, targetVersion)
	if err != nil {
		return err
	}
	if len(outdated) == 0 {
		rollout := &fleetstate.RolloutRecord{
			ToolName:          *tool,
			Cloud:             string(kind.Canonical()),
			Phase:             fleetstate.RolloutPhaseStable,
			DesiredGeneration: fctx.Outputs["generation_id"],
			ActiveGeneration:  fctx.Outputs["generation_id"],
			TargetVersion:     targetVersion,
		}
		if err := fctx.RolloutStore.Save(rollout); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(os.Stdout, "Fleet already matches the target version."); err != nil {
			return err
		}
		return nil
	}
	previousVersion := choosePreviousVersion(fctx.Snapshot, targetVersion)
	if previousVersion == "" {
		return fmt.Errorf("could not determine previous worker version for rollback")
	}
	if *canaryCount <= 0 {
		*canaryCount = defaultCanaryCount(fleetWorkerCount(fctx.Outputs))
	}
	if *canaryCount > len(outdated) {
		*canaryCount = len(outdated)
	}
	canaryIndexes := append([]int(nil), outdated[:*canaryCount]...)
	remaining := append([]int(nil), outdated[*canaryCount:]...)
	rollout := &fleetstate.RolloutRecord{
		ToolName:              *tool,
		Cloud:                 string(kind.Canonical()),
		Phase:                 fleetstate.RolloutPhaseCanary,
		DesiredGeneration:     fctx.Outputs["generation_id"],
		ActiveGeneration:      fctx.Outputs["generation_id"],
		CanaryGeneration:      fctx.Outputs["generation_id"],
		TargetVersion:         targetVersion,
		PreviousVersion:       previousVersion,
		CanaryCount:           *canaryCount,
		CanaryWorkerIndexes:   canaryIndexes,
		CanaryWorkerIDs:       rolloutCanaryIDs(canaryIndexes),
		DrainingWorkerIndexes: remaining,
	}
	if err := fctx.RolloutStore.Save(rollout); err != nil {
		return err
	}
	if err := replaceWorkerIndexes(mainContext(), fctx.ToolConfig, kind, canaryIndexes, nil, log); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(mainContext(), timeout)
	defer cancel()
	if _, err := waitForFleetSnapshot(ctx, kind, fctx.Outputs, fctx.Placement, targetVersion, fctx.Reputation, rollout, len(canaryIndexes), true, log); err != nil {
		rollout.Phase = fleetstate.RolloutPhaseRolledBack
		rollout.RollbackReason = err.Error()
		_ = fctx.RolloutStore.Save(rollout)
		_ = replaceWorkerIndexes(mainContext(), fctx.ToolConfig, kind, canaryIndexes, map[string]string{"docker_image": previousVersion}, log)
		return fmt.Errorf("canary failed and rollback was attempted: %w", err)
	}
	if !*autoPromote {
		fmt.Fprintf(os.Stdout, "Canary healthy for %v. Run `heph fleet rollout promote --tool %s --cloud %s` to continue.\n", canaryIndexes, *tool, kind.Canonical())
		return nil
	}
	return promoteRollout(mainContext(), fctx, rollout, remaining, timeout, log)
}

func runFleetRolloutPromote(args []string, log logger.Logger) error {
	fs := flag.NewFlagSet("fleet rollout promote", flag.ContinueOnError)
	tool := fs.String("tool", "", "Tool whose canary rollout to promote")
	cloudFlag := fs.String("cloud", "", "Cloud provider: "+cloud.SupportedKindsText()+" (required)")
	timeoutFlag := fs.String("timeout", "10m", "How long to wait for promotion health")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tool == "" {
		return fmt.Errorf("--tool flag is required")
	}
	opCfg, _ := operator.LoadConfig()
	kind, err := resolveCLICloud(*cloudFlag, opCfg)
	if err != nil {
		return err
	}
	timeout, err := parseDurationFlag(*timeoutFlag, 10*time.Minute)
	if err != nil {
		return err
	}
	fctx, err := loadProviderFleetContext(mainContext(), *tool, kind, fleet.PlacementPolicy{}, true, log)
	if err != nil {
		return err
	}
	if fctx.Rollout == nil || fctx.Rollout.Phase != fleetstate.RolloutPhaseCanary {
		return fmt.Errorf("no canary rollout is active for %s/%s", kind.Canonical(), *tool)
	}
	remaining, err := outdatedWorkerIndexes(fctx.Snapshot, fctx.Rollout.TargetVersion)
	if err != nil {
		return err
	}
	remaining = subtractIndexes(remaining, fctx.Rollout.CanaryWorkerIndexes)
	return promoteRollout(mainContext(), fctx, fctx.Rollout, remaining, timeout, log)
}

func promoteRollout(ctx context.Context, fctx *providerFleetContext, rollout *fleetstate.RolloutRecord, remaining []int, timeout time.Duration, log logger.Logger) error {
	rollout.Phase = fleetstate.RolloutPhasePromoting
	if err := fctx.RolloutStore.Save(rollout); err != nil {
		return err
	}
	if len(remaining) > 0 {
		if err := replaceWorkerIndexes(ctx, fctx.ToolConfig, fctx.Cloud, remaining, nil, log); err != nil {
			return err
		}
	}
	rollout.PromotedWorkerIndexes = append(append([]int(nil), rollout.CanaryWorkerIndexes...), remaining...)
	rollout.DrainingWorkerIndexes = nil
	rollout.Phase = fleetstate.RolloutPhaseDrainingOld
	if err := fctx.RolloutStore.Save(rollout); err != nil {
		return err
	}

	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if _, err := waitForFleetSnapshot(waitCtx, fctx.Cloud, fctx.Outputs, fctx.Placement, rollout.TargetVersion, fctx.Reputation, rollout, fleetWorkerCount(fctx.Outputs), true, log); err != nil {
		rollout.Phase = fleetstate.RolloutPhaseRolledBack
		rollout.RollbackReason = err.Error()
		_ = fctx.RolloutStore.Save(rollout)
		_ = replaceWorkerIndexes(ctx, fctx.ToolConfig, fctx.Cloud, rollout.PromotedWorkerIndexes, map[string]string{"docker_image": rollout.PreviousVersion}, log)
		return fmt.Errorf("promotion failed and rollback was attempted: %w", err)
	}

	rollout.Phase = fleetstate.RolloutPhaseStable
	rollout.ActiveGeneration = rollout.DesiredGeneration
	rollout.CanaryWorkerIndexes = nil
	rollout.CanaryWorkerIDs = nil
	rollout.DrainingWorkerIndexes = nil
	rollout.RollbackReason = ""
	if err := fctx.RolloutStore.Save(rollout); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "Rollout promoted to %s\n", rollout.TargetVersion)
	return nil
}

func runFleetRollback(args []string, log logger.Logger) error {
	fs := flag.NewFlagSet("fleet rollback", flag.ContinueOnError)
	tool := fs.String("tool", "", "Tool whose rollout should be rolled back")
	cloudFlag := fs.String("cloud", "", "Cloud provider: "+cloud.SupportedKindsText()+" (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tool == "" {
		return fmt.Errorf("--tool flag is required")
	}
	opCfg, _ := operator.LoadConfig()
	kind, err := resolveCLICloud(*cloudFlag, opCfg)
	if err != nil {
		return err
	}
	fctx, err := loadProviderFleetContext(mainContext(), *tool, kind, fleet.PlacementPolicy{}, true, log)
	if err != nil {
		return err
	}
	if fctx.Rollout == nil {
		return fmt.Errorf("no rollout record found for %s/%s", kind.Canonical(), *tool)
	}
	if fctx.Rollout.PreviousVersion == "" {
		return fmt.Errorf("rollout has no previous version to roll back to")
	}
	changed := append([]int(nil), fctx.Rollout.PromotedWorkerIndexes...)
	changed = append(changed, fctx.Rollout.CanaryWorkerIndexes...)
	if len(changed) == 0 {
		return fmt.Errorf("rollout has no changed workers to roll back")
	}
	if err := replaceWorkerIndexes(mainContext(), fctx.ToolConfig, kind, changed, map[string]string{"docker_image": fctx.Rollout.PreviousVersion}, log); err != nil {
		return err
	}
	fctx.Rollout.Phase = fleetstate.RolloutPhaseRolledBack
	fctx.Rollout.RollbackReason = "operator requested rollback"
	if err := fctx.RolloutStore.Save(fctx.Rollout); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "Rolled back %s to %s\n", *tool, fctx.Rollout.PreviousVersion)
	return nil
}

func repairCandidateIndexes(snapshot *fleet.FleetState) []int {
	candidates := make(map[int]struct{})
	for _, worker := range snapshot.Workers {
		switch worker.ExcludedReason {
		case "reputation_cooling_down", "reputation_quarantined", "reputation_retired", string(fleet.ExclusionReasonUnhealthy), string(fleet.ExclusionReasonQuarantinedUnhealthy):
			idx, err := workerIndexFromID(worker.ID)
			if err == nil {
				candidates[idx] = struct{}{}
			}
		}
	}
	indexes := make([]int, 0, len(candidates))
	for idx := range candidates {
		indexes = append(indexes, idx)
	}
	sort.Ints(indexes)
	return indexes
}

func subtractIndexes(all, remove []int) []int {
	removed := make(map[int]struct{}, len(remove))
	for _, idx := range remove {
		removed[idx] = struct{}{}
	}
	out := make([]int, 0, len(all))
	for _, idx := range all {
		if _, ok := removed[idx]; ok {
			continue
		}
		out = append(out, idx)
	}
	return out
}
