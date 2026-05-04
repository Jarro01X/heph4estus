package infra

import (
	"fmt"
	"strings"

	"heph4estus/internal/cloud"
)

type CredentialRotationComponent string

const (
	CredentialComponentNATS     CredentialRotationComponent = "nats"
	CredentialComponentMinIO    CredentialRotationComponent = "minio"
	CredentialComponentRegistry CredentialRotationComponent = "registry"
)

var allCredentialRotationComponents = []CredentialRotationComponent{
	CredentialComponentNATS,
	CredentialComponentMinIO,
	CredentialComponentRegistry,
}

type CredentialRotationPlan struct {
	Tool                         string
	Cloud                        cloud.Kind
	RequestedComponent           string
	Components                   []CredentialRotationComponent
	ControllerServices           []string
	OperatorOutputKeys           []string
	WorkerRecycleRequired        bool
	WorkerCount                  string
	CredentialScopeVersion       string
	NATSCredentialGeneration     string
	NATSCredentialRotatedAt      string
	MinIOCredentialGeneration    string
	MinIOCredentialRotatedAt     string
	RegistryCredentialGeneration string
	RegistryCredentialRotatedAt  string
	ControllerSecurityMode       string
	GenerationID                 string
	Actions                      []string
	Verification                 []string
}

func ParseCredentialRotationComponents(raw string) ([]CredentialRotationComponent, error) {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return nil, fmt.Errorf("--component flag is required (nats, minio, registry, or all)")
	}
	if raw == "all" {
		return append([]CredentialRotationComponent(nil), allCredentialRotationComponents...), nil
	}
	switch raw {
	case string(CredentialComponentNATS):
		return []CredentialRotationComponent{CredentialComponentNATS}, nil
	case string(CredentialComponentMinIO):
		return []CredentialRotationComponent{CredentialComponentMinIO}, nil
	case string(CredentialComponentRegistry):
		return []CredentialRotationComponent{CredentialComponentRegistry}, nil
	default:
		return nil, fmt.Errorf("--component must be nats, minio, registry, or all, got %q", raw)
	}
}

func PlanCredentialRotation(kind cloud.Kind, tool, component string, probe ProbeResult) (*CredentialRotationPlan, error) {
	if !kind.IsProviderNative() {
		return nil, fmt.Errorf("credential rotation only supports provider-native clouds, got %q", kind.Canonical())
	}
	components, err := ParseCredentialRotationComponents(component)
	if err != nil {
		return nil, err
	}
	if probe.Status != StatusReady {
		return nil, credentialRotationProbeError(probe)
	}
	outputs := probe.Outputs
	plan := &CredentialRotationPlan{
		Tool:                         tool,
		Cloud:                        kind.Canonical(),
		RequestedComponent:           strings.TrimSpace(strings.ToLower(component)),
		Components:                   components,
		WorkerRecycleRequired:        true,
		WorkerCount:                  outputOrUnknown(outputs, "worker_count"),
		CredentialScopeVersion:       outputOrUnknown(outputs, "credential_scope_version"),
		NATSCredentialGeneration:     outputOrUnknown(outputs, "nats_credential_generation"),
		NATSCredentialRotatedAt:      outputOrUnknown(outputs, "nats_credential_rotated_at"),
		MinIOCredentialGeneration:    outputOrUnknown(outputs, "minio_credential_generation"),
		MinIOCredentialRotatedAt:     outputOrUnknown(outputs, "minio_credential_rotated_at"),
		RegistryCredentialGeneration: outputOrUnknown(outputs, "registry_credential_generation"),
		RegistryCredentialRotatedAt:  outputOrUnknown(outputs, "registry_credential_rotated_at"),
		ControllerSecurityMode:       outputOrUnknown(outputs, "controller_security_mode"),
		GenerationID:                 outputOrUnknown(outputs, "generation_id"),
	}
	for _, component := range components {
		addCredentialComponentPlan(plan, component)
	}
	plan.ControllerServices = dedupStrings(plan.ControllerServices)
	plan.OperatorOutputKeys = dedupStrings(plan.OperatorOutputKeys)
	plan.Actions = dedupStrings(plan.Actions)
	plan.Verification = dedupStrings(plan.Verification)
	return plan, nil
}

func credentialRotationProbeError(probe ProbeResult) error {
	switch probe.Status {
	case StatusMissing:
		return fmt.Errorf("credential rotation requires ready infrastructure: no Terraform outputs found")
	case StatusStale:
		return fmt.Errorf("credential rotation requires current infrastructure outputs; missing keys: %s", strings.Join(probe.MissingKeys, ", "))
	case StatusMismatch:
		if probe.DeployedTool != "" {
			return fmt.Errorf("credential rotation requires matching infrastructure; deployed tool is %q", probe.DeployedTool)
		}
		return fmt.Errorf("credential rotation requires matching provider-native infrastructure")
	case StatusError:
		if probe.Err != nil {
			return fmt.Errorf("credential rotation preflight failed: %w", probe.Err)
		}
		return fmt.Errorf("credential rotation preflight failed")
	default:
		return fmt.Errorf("credential rotation requires ready infrastructure, got %s", probe.Status)
	}
}

func addCredentialComponentPlan(plan *CredentialRotationPlan, component CredentialRotationComponent) {
	switch component {
	case CredentialComponentNATS:
		plan.ControllerServices = append(plan.ControllerServices, "nats")
		plan.OperatorOutputKeys = append(plan.OperatorOutputKeys, "nats_url", "nats_user", "nats_password", "nats_operator_user", "nats_operator_password")
		plan.Actions = append(plan.Actions,
			"generate new NATS operator and worker credentials",
			"update controller NATS auth configuration",
			"restart the controller NATS service",
			"replace or restart workers so worker NATS credentials are refreshed",
		)
		plan.Verification = append(plan.Verification,
			"verify the operator can connect to NATS and inspect the stream",
			"verify workers resume heartbeats after reconcile",
		)
	case CredentialComponentMinIO:
		plan.ControllerServices = append(plan.ControllerServices, "minio")
		plan.OperatorOutputKeys = append(plan.OperatorOutputKeys, "s3_access_key", "s3_secret_key", "s3_operator_access_key", "s3_operator_secret_key")
		plan.Actions = append(plan.Actions,
			"generate new MinIO operator and worker credentials",
			"update MinIO users and bucket-scoped policies without deleting objects",
			"replace or restart workers so worker S3 credentials are refreshed",
		)
		plan.Verification = append(plan.Verification,
			"verify operator S3 list/read access",
			"verify worker S3 write access with a small object operation",
		)
	case CredentialComponentRegistry:
		plan.ControllerServices = append(plan.ControllerServices, "registry")
		plan.OperatorOutputKeys = append(plan.OperatorOutputKeys, "registry_username", "registry_password", "registry_publisher_username", "registry_publisher_password")
		plan.Actions = append(plan.Actions,
			"generate new registry publisher and worker credentials",
			"update controller registry htpasswd data",
			"restart the controller registry service",
			"replace or restart workers so registry pull credentials are refreshed",
		)
		plan.Verification = append(plan.Verification,
			"verify operator Docker login and image push",
			"verify worker registry pull readiness after reconcile",
		)
	}
}

func outputOrUnknown(outputs map[string]string, key string) string {
	if outputs == nil || strings.TrimSpace(outputs[key]) == "" {
		return "unknown"
	}
	return outputs[key]
}

func dedupStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
