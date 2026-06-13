package main

import (
	"context"
	"fmt"

	"heph4estus/internal/cloud"
	"heph4estus/internal/fleet"
	"heph4estus/internal/infra"
	"heph4estus/internal/logger"
	"heph4estus/internal/scanruntime"
)

var waitForProviderNativeFleetFunc = waitForProviderNativeFleet

func buildRuntimeProvider(ctx context.Context, kind cloud.Kind, outputs map[string]string, log logger.Logger) (cloud.Provider, error) {
	return scanruntime.DefaultProviderBuilder(ctx, kind, outputs, log)
}

func waitForProviderNativeFleet(ctx context.Context, kind cloud.Kind, outputs map[string]string, policy fleet.PlacementPolicy) (int, error) {
	typed := infra.DecodeTerraformOutputs(kind, outputs)
	runtime := typed.Selfhosted
	natsURL := runtime.NATSURL
	if natsURL == "" {
		return 0, fmt.Errorf("terraform outputs missing nats_url")
	}

	desired := runtime.WorkerCount
	if desired <= 0 {
		desired = 1
	}

	mgr, err := fleet.NewNATSFleetManager(fleet.NATSFleetManagerConfig{
		NATSURL:         natsURL,
		DesiredWorkers:  desired,
		ControllerIP:    runtime.ControllerIP,
		GenerationID:    runtime.GenerationID,
		Cloud:           string(kind.Canonical()),
		Placement:       policy,
		ExpectedVersion: runtime.DockerImage,
		RootCAPEM:       runtime.ControllerCAPEM,
		ServerName:      runtime.ControllerHost,
		ClientCertPEM:   runtime.NATSOperatorClientCertPEM,
		ClientKeyPEM:    runtime.NATSOperatorClientKeyPEM,
	}, logger.NewSimpleLogger())
	if err != nil {
		return 0, fmt.Errorf("starting provider-native fleet manager: %w", err)
	}
	defer func() { _ = mgr.Close() }()

	state, err := mgr.WaitForWorkers(ctx, desired)
	if err != nil {
		return 0, fmt.Errorf("waiting for provider-native fleet: %w", err)
	}
	summary := state.Summarize()
	logStatus(
		"Provider-native fleet ready: %d/%d eligible, %d IPv6-ready, %d/%d unique IPv4 [%s]",
		summary.EligibleCount, summary.DesiredWorkers, summary.IPv6ReadyCount, summary.UniqueEligibleIPv4Count, summary.UniqueIPv4Count, policy.Summary(),
	)
	return summary.EligibleCount, nil
}

func fleetWorkerCount(outputs map[string]string) int {
	return infra.OutputInt(outputs, infra.OutputWorkerCount)
}
