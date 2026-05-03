package main

import (
	"strings"
	"testing"
	"time"

	"heph4estus/internal/cloud"
	"heph4estus/internal/operator"
)

func TestLoadRolloutOutcomeSummaryFiltersByVersionAndGeneration(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	store, err := operator.NewJobStore()
	if err != nil {
		t.Fatalf("NewJobStore: %v", err)
	}
	now := time.Now().UTC()
	fixtures := []*operator.JobRecord{
		{
			JobID:                 "ok-1",
			ToolName:              "httpx",
			Cloud:                 "hetzner",
			Phase:                 operator.PhaseComplete,
			CreatedAt:             now.Add(-30 * time.Minute),
			UpdatedAt:             now.Add(-25 * time.Minute),
			GenerationID:          "gen-2",
			ExpectedWorkerVersion: "registry/heph-httpx:2",
		},
		{
			JobID:                 "fail-1",
			ToolName:              "httpx",
			Cloud:                 "hetzner",
			Phase:                 operator.PhaseFailed,
			CreatedAt:             now.Add(-20 * time.Minute),
			UpdatedAt:             now.Add(-15 * time.Minute),
			GenerationID:          "gen-2",
			ExpectedWorkerVersion: "registry/heph-httpx:2",
		},
		{
			JobID:                 "other-version",
			ToolName:              "httpx",
			Cloud:                 "hetzner",
			Phase:                 operator.PhaseFailed,
			CreatedAt:             now.Add(-10 * time.Minute),
			UpdatedAt:             now.Add(-9 * time.Minute),
			GenerationID:          "gen-2",
			ExpectedWorkerVersion: "registry/heph-httpx:3",
		},
		{
			JobID:                 "other-cloud",
			ToolName:              "httpx",
			Cloud:                 "linode",
			Phase:                 operator.PhaseComplete,
			CreatedAt:             now.Add(-10 * time.Minute),
			UpdatedAt:             now.Add(-9 * time.Minute),
			GenerationID:          "gen-2",
			ExpectedWorkerVersion: "registry/heph-httpx:2",
		},
	}
	for _, rec := range fixtures {
		if err := store.Create(rec); err != nil {
			t.Fatalf("Create(%s): %v", rec.JobID, err)
		}
	}
	summary, err := loadRolloutOutcomeSummary("httpx", cloud.KindHetzner, "gen-2", "registry/heph-httpx:2", 2*time.Hour)
	if err != nil {
		t.Fatalf("loadRolloutOutcomeSummary: %v", err)
	}
	if summary.CompletedJobs != 1 || summary.FailedJobs != 1 || summary.ActiveJobs != 0 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
}

func TestValidateRolloutOutcomeSummaryRequiresSuccessWhenFailuresExist(t *testing.T) {
	err := validateRolloutOutcomeSummary(rolloutOutcomeSummary{FailedJobs: 2})
	if err == nil {
		t.Fatal("expected validation failure")
	}
	if !strings.Contains(err.Error(), "recent task failures") {
		t.Fatalf("unexpected error: %v", err)
	}
}
