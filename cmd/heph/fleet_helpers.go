package main

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"heph4estus/internal/cloud"
	"heph4estus/internal/fleet"
	"heph4estus/internal/fleetstate"
	"heph4estus/internal/infra"
	"heph4estus/internal/logger"
	"heph4estus/internal/operator"
)

type providerFleetContext struct {
	ToolConfig      *infra.ToolConfig
	Outputs         map[string]string
	Placement       fleet.PlacementPolicy
	ExpectedVersion string
	ReputationStore *fleetstate.ReputationStore
	RolloutStore    *fleetstate.RolloutStore
	Reputation      []fleetstate.ReputationRecord
	Rollout         *fleetstate.RolloutRecord
	Snapshot        *fleet.FleetState
	Cloud           cloud.Kind
}

func loadProviderFleetContext(ctx context.Context, tool string, kind cloud.Kind, placement fleet.PlacementPolicy, includeAllGenerations bool, log logger.Logger) (*providerFleetContext, error) {
	if !kind.IsProviderNative() {
		return nil, fmt.Errorf("fleet commands require a provider-native cloud, got %q", kind.Canonical())
	}
	cfg, err := infra.ResolveToolConfig(tool, kind)
	if err != nil {
		return nil, err
	}
	tf := infra.NewTerraformClient(log)
	outputs, err := tf.ReadOutputs(ctx, cfg.TerraformDir)
	if err != nil {
		return nil, err
	}
	reputationStore, err := fleetstate.NewReputationStore()
	if err != nil {
		return nil, err
	}
	rolloutStore, err := fleetstate.NewRolloutStore()
	if err != nil {
		return nil, err
	}
	reputation, err := reputationStore.List(string(kind.Canonical()))
	if err != nil {
		return nil, err
	}
	rollout, err := rolloutStore.Load(string(kind.Canonical()), tool)
	if err != nil {
		return nil, err
	}

	expectedVersion := outputs["docker_image"]
	if rollout != nil && rollout.TargetVersion != "" {
		expectedVersion = rollout.TargetVersion
	}
	if placement.Mode == "" {
		opCfg, _ := operator.LoadConfig()
		placement, err = operator.ResolvePlacementPolicy(placement, opCfg, fleetWorkerCount(outputs))
		if err != nil {
			return nil, err
		}
	}

	generationID := outputs["generation_id"]
	if includeAllGenerations {
		generationID = ""
	}
	snapshot, err := fleet.QueryFleetSnapshot(ctx, fleet.NATSFleetManagerConfig{
		NATSURL:         outputs["nats_url"],
		DesiredWorkers:  fleetWorkerCount(outputs),
		ControllerIP:    outputs["controller_ip"],
		GenerationID:    generationID,
		Cloud:           string(kind.Canonical()),
		Placement:       placement,
		ExpectedVersion: expectedVersion,
		Reputation:      reputation,
		Rollout:         rollout,
		RootCAPEM:       outputs["controller_ca_pem"],
		ServerName:      outputs["controller_host"],
		ClientCertPEM:   outputs["nats_operator_client_cert_pem"],
		ClientKeyPEM:    outputs["nats_operator_client_key_pem"],
	}, log)
	if err != nil {
		return nil, err
	}

	return &providerFleetContext{
		ToolConfig:      cfg,
		Outputs:         outputs,
		Placement:       placement,
		ExpectedVersion: expectedVersion,
		ReputationStore: reputationStore,
		RolloutStore:    rolloutStore,
		Reputation:      reputation,
		Rollout:         rollout,
		Snapshot:        snapshot,
		Cloud:           kind,
	}, nil
}

func workerIndexFromID(workerID string) (int, error) {
	const prefix = "heph-worker-"
	if !strings.HasPrefix(workerID, prefix) {
		return 0, fmt.Errorf("worker %q does not match heph-worker-N format", workerID)
	}
	return strconv.Atoi(strings.TrimPrefix(workerID, prefix))
}

func replaceWorkerIndexes(ctx context.Context, cfg *infra.ToolConfig, kind cloud.Kind, indexes []int, varsOverride map[string]string, stream logger.Logger) error {
	if len(indexes) == 0 {
		return nil
	}
	dedup := make(map[int]struct{}, len(indexes))
	for _, idx := range indexes {
		dedup[idx] = struct{}{}
	}
	indexes = indexes[:0]
	for idx := range dedup {
		indexes = append(indexes, idx)
	}
	sort.Ints(indexes)

	addrs := make([]string, 0, len(indexes))
	for _, idx := range indexes {
		addr, err := infra.WorkerResourceAddress(kind, idx)
		if err != nil {
			return err
		}
		addrs = append(addrs, addr)
	}
	vars := copyTerraformVars(cfg.TerraformVars)
	for k, v := range varsOverride {
		vars[k] = v
	}
	if err := infra.ValidateProviderNativeTerraformVars(kind, vars); err != nil {
		return err
	}
	tf := infra.NewTerraformClient(stream)
	return tf.ApplyReplace(ctx, cfg.TerraformDir, vars, addrs, nil)
}

func copyTerraformVars(src map[string]string) map[string]string {
	if src == nil {
		return map[string]string{}
	}
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func waitForFleetSnapshot(ctx context.Context, kind cloud.Kind, outputs map[string]string, placement fleet.PlacementPolicy, expectedVersion string, reputation []fleetstate.ReputationRecord, rollout *fleetstate.RolloutRecord, minReady int, includeAllGenerations bool, log logger.Logger) (*fleet.FleetState, error) {
	generationID := outputs["generation_id"]
	if includeAllGenerations {
		generationID = ""
	}
	mgr, err := fleet.NewNATSFleetManager(fleet.NATSFleetManagerConfig{
		NATSURL:         outputs["nats_url"],
		DesiredWorkers:  fleetWorkerCount(outputs),
		ControllerIP:    outputs["controller_ip"],
		GenerationID:    generationID,
		Cloud:           string(kind.Canonical()),
		Placement:       placement,
		ExpectedVersion: expectedVersion,
		Reputation:      reputation,
		Rollout:         rollout,
		RootCAPEM:       outputs["controller_ca_pem"],
		ServerName:      outputs["controller_host"],
		ClientCertPEM:   outputs["nats_operator_client_cert_pem"],
		ClientKeyPEM:    outputs["nats_operator_client_key_pem"],
	}, log)
	if err != nil {
		return nil, err
	}
	defer func() { _ = mgr.Close() }()
	return mgr.WaitForWorkers(ctx, minReady)
}

func choosePreviousVersion(snapshot *fleet.FleetState, target string) string {
	if snapshot == nil {
		return ""
	}
	counts := snapshot.Summarize().VersionCounts
	type versionCount struct {
		version string
		count   int
	}
	var candidates []versionCount
	for version, count := range counts {
		if version == "" || version == "unknown" || version == target {
			continue
		}
		candidates = append(candidates, versionCount{version: version, count: count})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].count != candidates[j].count {
			return candidates[i].count > candidates[j].count
		}
		return candidates[i].version < candidates[j].version
	})
	if len(candidates) == 0 {
		return ""
	}
	return candidates[0].version
}

func outdatedWorkerIndexes(snapshot *fleet.FleetState, targetVersion string) ([]int, error) {
	var indexes []int
	if snapshot == nil {
		return indexes, nil
	}
	for _, worker := range snapshot.Workers {
		if worker.Version == targetVersion {
			continue
		}
		idx, err := workerIndexFromID(worker.ID)
		if err != nil {
			return nil, err
		}
		indexes = append(indexes, idx)
	}
	sort.Ints(indexes)
	return indexes, nil
}

func rolloutCanaryIDs(indexes []int) []string {
	ids := make([]string, 0, len(indexes))
	for _, idx := range indexes {
		ids = append(ids, fmt.Sprintf("heph-worker-%d", idx))
	}
	return ids
}

func defaultCanaryCount(desiredWorkers int) int {
	switch {
	case desiredWorkers <= 1:
		return 1
	case desiredWorkers <= 4:
		return 1
	default:
		if desiredWorkers/4 < 2 {
			return 2
		}
		return desiredWorkers / 4
	}
}

func parseDurationFlag(raw string, fallback time.Duration) (time.Duration, error) {
	if strings.TrimSpace(raw) == "" {
		return fallback, nil
	}
	return time.ParseDuration(raw)
}
