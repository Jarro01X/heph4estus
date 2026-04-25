package infra

import (
	"context"
	"fmt"

	"heph4estus/internal/cloud"
)

// InfraStatus classifies the current state of deployed infrastructure.
type InfraStatus int

const (
	// StatusReady means matching infra is deployed with all required outputs.
	StatusReady InfraStatus = iota
	// StatusMissing means no infrastructure has been deployed yet.
	StatusMissing
	// StatusStale means outputs exist but required keys are absent.
	StatusStale
	// StatusMismatch means infra is deployed for a different tool.
	StatusMismatch
	// StatusError means Terraform probing failed due to a real error.
	StatusError
)

func (s InfraStatus) String() string {
	switch s {
	case StatusReady:
		return "ready"
	case StatusMissing:
		return "missing"
	case StatusStale:
		return "stale"
	case StatusMismatch:
		return "mismatch"
	case StatusError:
		return "error"
	default:
		return "unknown"
	}
}

// ProbeResult holds the classified state of existing infrastructure.
type ProbeResult struct {
	Status       InfraStatus
	Outputs      map[string]string // nil when Status is Missing or Error
	DeployedTool string            // the tool_name from outputs, if any
	MissingKeys  []string          // required keys that are absent
	Err          error             // the underlying error when Status is Error
}

// Probe inspects existing Terraform outputs and classifies the infrastructure
// state relative to the requested tool. The kind parameter selects the
// required-output set so that AWS and selfhosted infrastructure are evaluated
// against the correct contract.
func Probe(ctx context.Context, tf *TerraformClient, kind cloud.Kind, terraformDir, requestedTool string) ProbeResult {
	outputs, err := tf.ReadOutputs(ctx, terraformDir)
	if err != nil {
		// Distinguish "no state" from real Terraform failures.
		// terraform output returns an empty map (not an error) when state exists
		// but has no outputs. An error here means terraform itself failed.
		return ProbeResult{
			Status: StatusError,
			Err:    fmt.Errorf("terraform probe failed: %w", err),
		}
	}

	// Empty outputs means no infrastructure deployed yet.
	if len(outputs) == 0 {
		return ProbeResult{Status: StatusMissing}
	}

	// Check for tool mismatch.
	deployedTool := outputs["tool_name"]
	if deployedTool != "" && deployedTool != requestedTool {
		return ProbeResult{
			Status:       StatusMismatch,
			Outputs:      outputs,
			DeployedTool: deployedTool,
		}
	}

	// For provider-native clouds, verify the deployed cloud matches. This
	// prevents silently reusing Hetzner infra when Vultr was requested.
	if kind.IsProviderNative() {
		deployedCloud := outputs["cloud"]
		if deployedCloud != "" && cloud.Kind(deployedCloud).Canonical() != kind.Canonical() {
			return ProbeResult{
				Status:       StatusMismatch,
				Outputs:      outputs,
				DeployedTool: deployedTool,
			}
		}
	}

	// Check for missing required keys.
	var missing []string
	for _, key := range RequiredOutputKeysForCloud(kind) {
		if outputs[key] == "" {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		return ProbeResult{
			Status:       StatusStale,
			Outputs:      outputs,
			DeployedTool: deployedTool,
			MissingKeys:  missing,
		}
	}

	return ProbeResult{
		Status:       StatusReady,
		Outputs:      outputs,
		DeployedTool: deployedTool,
	}
}

// LifecyclePolicy captures operator intent for how to handle infrastructure state.
type LifecyclePolicy struct {
	NoDeploy     bool // fail instead of deploying
	AutoApprove  bool // skip interactive prompts
	DestroyAfter bool // tear down infra after the run
}

// Decision is the action the lifecycle system recommends.
type Decision int

const (
	// DecisionReuse means existing infra matches and can be used directly.
	DecisionReuse Decision = iota
	// DecisionDeploy means infra must be deployed or redeployed.
	DecisionDeploy
	// DecisionBlock means the operation should not proceed.
	DecisionBlock
)

func (d Decision) String() string {
	switch d {
	case DecisionReuse:
		return "reuse"
	case DecisionDeploy:
		return "deploy"
	case DecisionBlock:
		return "block"
	default:
		return "unknown"
	}
}

// Reason explains why a particular lifecycle decision was made.
type Reason int

const (
	ReasonInfraReady        Reason = iota // matching infra exists
	ReasonInfraMissing                    // no infra deployed
	ReasonInfraStale                      // outputs incomplete
	ReasonToolMismatch                    // different tool deployed
	ReasonProbeError                      // terraform failed
	ReasonBlockedByPolicy                 // --no-deploy prevents action
)

func (r Reason) String() string {
	switch r {
	case ReasonInfraReady:
		return "infrastructure matches requested tool"
	case ReasonInfraMissing:
		return "no infrastructure deployed"
	case ReasonInfraStale:
		return "infrastructure outputs are incomplete"
	case ReasonToolMismatch:
		return "infrastructure deployed for a different tool"
	case ReasonProbeError:
		return "failed to probe infrastructure state"
	case ReasonBlockedByPolicy:
		return "deploy blocked by --no-deploy flag"
	default:
		return "unknown reason"
	}
}

// LifecycleResult is the output of the lifecycle decision function.
type LifecycleResult struct {
	Decision Decision
	Reason   Reason
	Probe    ProbeResult
	Message  string // human-readable explanation
}

// Decide takes a probe result and a policy and returns a lifecycle decision.
func Decide(probe ProbeResult, policy LifecyclePolicy) LifecycleResult {
	switch probe.Status {
	case StatusReady:
		return LifecycleResult{
			Decision: DecisionReuse,
			Reason:   ReasonInfraReady,
			Probe:    probe,
			Message:  fmt.Sprintf("reusing existing %s infrastructure", probe.DeployedTool),
		}

	case StatusMissing:
		if policy.NoDeploy {
			return LifecycleResult{
				Decision: DecisionBlock,
				Reason:   ReasonBlockedByPolicy,
				Probe:    probe,
				Message:  "no infrastructure deployed and --no-deploy is set",
			}
		}
		return LifecycleResult{
			Decision: DecisionDeploy,
			Reason:   ReasonInfraMissing,
			Probe:    probe,
			Message:  "deploying infrastructure (none exists)",
		}

	case StatusStale:
		if policy.NoDeploy {
			return LifecycleResult{
				Decision: DecisionBlock,
				Reason:   ReasonBlockedByPolicy,
				Probe:    probe,
				Message:  fmt.Sprintf("infrastructure is stale (missing: %v) and --no-deploy is set", probe.MissingKeys),
			}
		}
		return LifecycleResult{
			Decision: DecisionDeploy,
			Reason:   ReasonInfraStale,
			Probe:    probe,
			Message:  fmt.Sprintf("redeploying infrastructure (missing outputs: %v)", probe.MissingKeys),
		}

	case StatusMismatch:
		if policy.NoDeploy {
			return LifecycleResult{
				Decision: DecisionBlock,
				Reason:   ReasonBlockedByPolicy,
				Probe:    probe,
				Message:  fmt.Sprintf("infrastructure deployed for %q, not the requested tool, and --no-deploy is set", probe.DeployedTool),
			}
		}
		return LifecycleResult{
			Decision: DecisionDeploy,
			Reason:   ReasonToolMismatch,
			Probe:    probe,
			Message:  fmt.Sprintf("redeploying infrastructure (currently deployed for %q)", probe.DeployedTool),
		}

	case StatusError:
		return LifecycleResult{
			Decision: DecisionBlock,
			Reason:   ReasonProbeError,
			Probe:    probe,
			Message:  fmt.Sprintf("cannot determine infrastructure state: %v", probe.Err),
		}

	default:
		return LifecycleResult{
			Decision: DecisionBlock,
			Reason:   ReasonProbeError,
			Probe:    probe,
			Message:  "unknown infrastructure state",
		}
	}
}
