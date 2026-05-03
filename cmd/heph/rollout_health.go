package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"heph4estus/internal/cloud"
	"heph4estus/internal/fleetstate"
	"heph4estus/internal/logger"
	"heph4estus/internal/operator"
)

const rolloutOutcomeLookback = 6 * time.Hour

type rolloutOutcomeSummary struct {
	CompletedJobs   int
	FailedJobs      int
	ActiveJobs      int
	MostRecentEvent time.Time
}

func (s rolloutOutcomeSummary) TotalJobs() int {
	return s.CompletedJobs + s.FailedJobs + s.ActiveJobs
}

func (s rolloutOutcomeSummary) String() string {
	return fmt.Sprintf("complete=%d failed=%d active=%d", s.CompletedJobs, s.FailedJobs, s.ActiveJobs)
}

func loadRolloutOutcomeSummary(tool string, kind cloud.Kind, generation, version string, lookback time.Duration) (rolloutOutcomeSummary, error) {
	store, err := operator.NewJobStore()
	if err != nil {
		return rolloutOutcomeSummary{}, err
	}
	ids, err := store.List()
	if err != nil {
		return rolloutOutcomeSummary{}, err
	}
	cutoff := time.Time{}
	if lookback > 0 {
		cutoff = time.Now().UTC().Add(-lookback)
	}
	var summary rolloutOutcomeSummary
	for _, id := range ids {
		rec, err := store.Load(id)
		if err != nil {
			return rolloutOutcomeSummary{}, err
		}
		if rec.ToolName != tool || rec.Cloud != string(kind.Canonical()) {
			continue
		}
		if generation != "" {
			if rec.GenerationID == "" || rec.GenerationID != generation {
				continue
			}
		}
		if version != "" {
			if rec.ExpectedWorkerVersion == "" || rec.ExpectedWorkerVersion != version {
				continue
			}
		}
		ts := rec.UpdatedAt
		if ts.IsZero() {
			ts = rec.CreatedAt
		}
		if !cutoff.IsZero() && ts.Before(cutoff) {
			continue
		}
		if summary.MostRecentEvent.IsZero() || ts.After(summary.MostRecentEvent) {
			summary.MostRecentEvent = ts
		}
		switch rec.Phase {
		case operator.PhaseComplete:
			summary.CompletedJobs++
		case operator.PhaseFailed:
			summary.FailedJobs++
		default:
			summary.ActiveJobs++
		}
	}
	return summary, nil
}

func validateRolloutOutcomeSummary(summary rolloutOutcomeSummary) error {
	if summary.FailedJobs > 0 && summary.CompletedJobs == 0 {
		return fmt.Errorf("recent task failures detected for canary (%s)", summary.String())
	}
	return nil
}

func rollbackCanaryOutcomeFailure(ctx context.Context, fctx *providerFleetContext, rollout *fleetstate.RolloutRecord, summary rolloutOutcomeSummary, log logger.Logger) error {
	reason := fmt.Sprintf("recent task failures detected for canary (%s)", summary.String())
	rollout.Phase = fleetstate.RolloutPhaseRolledBack
	rollout.RollbackReason = reason
	_ = fctx.RolloutStore.Save(rollout)
	_ = replaceWorkerIndexes(ctx, fctx.ToolConfig, fctx.Cloud, rollout.CanaryWorkerIndexes, map[string]string{"docker_image": rollout.PreviousVersion}, log)
	return fmt.Errorf("%s", reason)
}

func outcomeSummaryLine(summary rolloutOutcomeSummary) string {
	if summary.TotalJobs() == 0 {
		return ""
	}
	parts := []string{summary.String()}
	if !summary.MostRecentEvent.IsZero() {
		parts = append(parts, fmt.Sprintf("last=%s", summary.MostRecentEvent.Format(time.RFC3339)))
	}
	return strings.Join(parts, " ")
}
