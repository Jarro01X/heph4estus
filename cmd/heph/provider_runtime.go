package main

import (
	"context"
	"fmt"
	"strconv"

	"heph4estus/internal/cloud"
	"heph4estus/internal/cloud/factory"
	"heph4estus/internal/fleet"
	"heph4estus/internal/logger"
)

var waitForProviderNativeFleetFunc = waitForProviderNativeFleet

func buildRuntimeProvider(ctx context.Context, kind cloud.Kind, outputs map[string]string, log logger.Logger) (cloud.Provider, error) {
	if kind.IsProviderNative() && outputs != nil {
		return factory.Build(factory.Config{
			Kind:       kind,
			Selfhosted: factory.SelfhostedConfigFromOutputs(outputs),
			Logger:     log,
		})
	}
	return factory.BuildForKind(ctx, kind, log)
}

func waitForProviderNativeFleet(ctx context.Context, kind cloud.Kind, outputs map[string]string, policy fleet.PlacementPolicy) (int, error) {
	natsURL := outputs["nats_url"]
	if natsURL == "" {
		return 0, fmt.Errorf("terraform outputs missing nats_url")
	}

	desired := fleetWorkerCount(outputs)
	if desired <= 0 {
		desired = 1
	}

	mgr, err := fleet.NewNATSFleetManager(fleet.NATSFleetManagerConfig{
		NATSURL:         natsURL,
		DesiredWorkers:  desired,
		ControllerIP:    outputs["controller_ip"],
		GenerationID:    outputs["generation_id"],
		Cloud:           string(kind.Canonical()),
		Placement:       policy,
		ExpectedVersion: outputs["docker_image"],
		RootCAPEM:       outputs["controller_ca_pem"],
		ServerName:      outputs["controller_host"],
		ClientCertPEM:   outputs["nats_operator_client_cert_pem"],
		ClientKeyPEM:    outputs["nats_operator_client_key_pem"],
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
	if outputs == nil {
		return 0
	}
	raw := outputs["worker_count"]
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}
