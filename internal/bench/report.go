package bench

import "time"

// FleetReport captures a single provider-native fleet benchmark run.
type FleetReport struct {
	Tool                    string         `json:"tool"`
	Cloud                   string         `json:"cloud"`
	GeneratedAt             time.Time      `json:"generated_at"`
	DeployDuration          time.Duration  `json:"deploy_duration"`
	FirstRegisteredDuration time.Duration  `json:"first_registered_duration"`
	FirstAdmittedDuration   time.Duration  `json:"first_admitted_duration"`
	SteadyStateDuration     time.Duration  `json:"steady_state_duration"`
	Placement               string         `json:"placement"`
	DesiredWorkers          int            `json:"desired_workers"`
	ControllerCount         int            `json:"controller_count"`
	UniqueIPv4Count         int            `json:"unique_ipv4_count"`
	IPv6ReadyCount          int            `json:"ipv6_ready_count"`
	DiversityEligible       int            `json:"diversity_eligible"`
	ThroughputEligible      int            `json:"throughput_eligible"`
	JobID                   string         `json:"job_id,omitempty"`
	JobPhase                string         `json:"job_phase,omitempty"`
	CompletedTasks          int            `json:"completed_tasks,omitempty"`
	TotalTasks              int            `json:"total_tasks,omitempty"`
	CompletionPercent       float64        `json:"completion_percent,omitempty"`
	ActiveRuntime           time.Duration  `json:"active_runtime,omitempty"`
	TasksPerMinute          float64        `json:"tasks_per_minute,omitempty"`
	ExcludedByReason        map[string]int `json:"excluded_by_reason,omitempty"`
	VersionCounts           map[string]int `json:"version_counts,omitempty"`
	RolloutPhase            string         `json:"rollout_phase,omitempty"`
	RollbackReason          string         `json:"rollback_reason,omitempty"`
}

// FleetComparison describes the delta between two benchmark runs.
type FleetComparison struct {
	Baseline  FleetReport          `json:"baseline"`
	Candidate FleetReport          `json:"candidate"`
	Delta     FleetComparisonDelta `json:"delta"`
}

type FleetComparisonDelta struct {
	DeployDuration          time.Duration `json:"deploy_duration"`
	FirstRegisteredDuration time.Duration `json:"first_registered_duration"`
	FirstAdmittedDuration   time.Duration `json:"first_admitted_duration"`
	SteadyStateDuration     time.Duration `json:"steady_state_duration"`
	DesiredWorkers          int           `json:"desired_workers"`
	ControllerCount         int           `json:"controller_count"`
	UniqueIPv4Count         int           `json:"unique_ipv4_count"`
	IPv6ReadyCount          int           `json:"ipv6_ready_count"`
	DiversityEligible       int           `json:"diversity_eligible"`
	ThroughputEligible      int           `json:"throughput_eligible"`
	CompletedTasks          int           `json:"completed_tasks"`
	TotalTasks              int           `json:"total_tasks"`
	CompletionPercent       float64       `json:"completion_percent"`
	ActiveRuntime           time.Duration `json:"active_runtime"`
	TasksPerMinute          float64       `json:"tasks_per_minute"`
}

// CompareFleetReports computes the candidate-minus-baseline delta.
func CompareFleetReports(baseline, candidate FleetReport) FleetComparison {
	return FleetComparison{
		Baseline:  baseline,
		Candidate: candidate,
		Delta: FleetComparisonDelta{
			DeployDuration:          candidate.DeployDuration - baseline.DeployDuration,
			FirstRegisteredDuration: candidate.FirstRegisteredDuration - baseline.FirstRegisteredDuration,
			FirstAdmittedDuration:   candidate.FirstAdmittedDuration - baseline.FirstAdmittedDuration,
			SteadyStateDuration:     candidate.SteadyStateDuration - baseline.SteadyStateDuration,
			DesiredWorkers:          candidate.DesiredWorkers - baseline.DesiredWorkers,
			ControllerCount:         candidate.ControllerCount - baseline.ControllerCount,
			UniqueIPv4Count:         candidate.UniqueIPv4Count - baseline.UniqueIPv4Count,
			IPv6ReadyCount:          candidate.IPv6ReadyCount - baseline.IPv6ReadyCount,
			DiversityEligible:       candidate.DiversityEligible - baseline.DiversityEligible,
			ThroughputEligible:      candidate.ThroughputEligible - baseline.ThroughputEligible,
			CompletedTasks:          candidate.CompletedTasks - baseline.CompletedTasks,
			TotalTasks:              candidate.TotalTasks - baseline.TotalTasks,
			CompletionPercent:       candidate.CompletionPercent - baseline.CompletionPercent,
			ActiveRuntime:           candidate.ActiveRuntime - baseline.ActiveRuntime,
			TasksPerMinute:          candidate.TasksPerMinute - baseline.TasksPerMinute,
		},
	}
}
