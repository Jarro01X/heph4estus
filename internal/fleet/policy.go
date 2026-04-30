package fleet

import (
	"fmt"
	"sort"
	"strings"
)

// PlacementMode controls how provider-native fleets trade off source-IP
// diversity vs raw throughput.
type PlacementMode string

const (
	PlacementModeDiversity  PlacementMode = "diversity"
	PlacementModeThroughput PlacementMode = "throughput"
)

// PlacementPolicy defines which workers are eligible for a run.
type PlacementPolicy struct {
	Mode              PlacementMode `json:"mode,omitempty"`
	MaxWorkersPerHost int           `json:"max_workers_per_host,omitempty"`
	MinUniqueIPs      int           `json:"min_unique_ips,omitempty"`
	IPv6Required      bool          `json:"ipv6_required,omitempty"`
	DualStackRequired bool          `json:"dual_stack_required,omitempty"`
}

// ExclusionReason describes why a worker was excluded from the admitted fleet.
type ExclusionReason string

const (
	ExclusionReasonNotReady             ExclusionReason = "not_ready"
	ExclusionReasonUnhealthy            ExclusionReason = "unhealthy"
	ExclusionReasonVersionUnknown       ExclusionReason = "version_unknown"
	ExclusionReasonVersionMismatch      ExclusionReason = "version_mismatch"
	ExclusionReasonIPv6NotReady         ExclusionReason = "ipv6_not_ready"
	ExclusionReasonDualStackRequired    ExclusionReason = "dual_stack_required"
	ExclusionReasonPlacementLimit       ExclusionReason = "placement_limit_exceeded"
	ExclusionReasonQuarantinedUnhealthy ExclusionReason = "quarantined_unhealthy"
)

// Normalize fills in implicit defaults without changing operator intent.
func (p PlacementPolicy) Normalize(desiredWorkers int) PlacementPolicy {
	if p.Mode == "" {
		p.Mode = PlacementModeDiversity
	}
	if p.DualStackRequired {
		p.IPv6Required = true
	}
	if p.Mode == PlacementModeDiversity && p.MaxWorkersPerHost == 0 {
		p.MaxWorkersPerHost = 1
	}
	if p.Mode == PlacementModeThroughput && p.MaxWorkersPerHost < 0 {
		p.MaxWorkersPerHost = 0
	}
	if p.MinUniqueIPs < 0 {
		p.MinUniqueIPs = 0
	}
	if p.MaxWorkersPerHost < 0 {
		p.MaxWorkersPerHost = 0
	}
	if desiredWorkers > 0 && p.MinUniqueIPs > desiredWorkers {
		p.MinUniqueIPs = desiredWorkers
	}
	return p
}

// Validate rejects unsupported policy values.
func (p PlacementPolicy) Validate() error {
	switch p.Mode {
	case "", PlacementModeDiversity, PlacementModeThroughput:
	default:
		return fmt.Errorf("placement mode must be diversity or throughput, got %q", p.Mode)
	}
	if p.MaxWorkersPerHost < 0 {
		return fmt.Errorf("max workers per host must be non-negative")
	}
	if p.MinUniqueIPs < 0 {
		return fmt.Errorf("min unique IPs must be non-negative")
	}
	return nil
}

// Summary returns a compact operator-facing description of the policy.
func (p PlacementPolicy) Summary() string {
	p = p.Normalize(0)
	parts := []string{string(p.Mode)}
	if p.MaxWorkersPerHost > 0 {
		parts = append(parts, fmt.Sprintf("max %d/IP", p.MaxWorkersPerHost))
	}
	if p.MinUniqueIPs > 0 {
		parts = append(parts, fmt.Sprintf("min %d unique IPv4", p.MinUniqueIPs))
	}
	if p.DualStackRequired {
		parts = append(parts, "dual-stack required")
	} else if p.IPv6Required {
		parts = append(parts, "IPv6 required")
	}
	return strings.Join(parts, ", ")
}

func summarizeReasonCounts(counts map[string]int) string {
	if len(counts) == 0 {
		return ""
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", k, counts[k]))
	}
	return strings.Join(parts, ", ")
}
