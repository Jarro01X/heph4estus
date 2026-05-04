package infra

import (
	"fmt"
	"strings"
	"time"

	"heph4estus/internal/cloud"
)

type CertificateRotationComponent string

const (
	CertificateComponentController CertificateRotationComponent = "controller"
	CertificateComponentWorker     CertificateRotationComponent = "worker"
	CertificateComponentCA         CertificateRotationComponent = "ca"
)

var allCertificateRotationComponents = []CertificateRotationComponent{
	CertificateComponentController,
	CertificateComponentWorker,
	CertificateComponentCA,
}

type CertificateRotationPlan struct {
	Tool                          string
	Cloud                         cloud.Kind
	RequestedComponent            string
	Components                    []CertificateRotationComponent
	ControllerSecurityMode        string
	GenerationID                  string
	WorkerCount                   string
	ControllerCAFingerprintSHA256 string
	ControllerCertNotAfter        string
	TLSEnabledServices            []string
	ControllerServices            []string
	WorkerRecycleRequired         bool
	OperatorTrustRefreshRequired  bool
	Actions                       []string
	Verification                  []string
	Warnings                      []string
}

func ParseCertificateRotationComponents(raw string) ([]CertificateRotationComponent, error) {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return nil, fmt.Errorf("--component flag is required (controller, worker, ca, or all)")
	}
	if raw == "all" {
		return append([]CertificateRotationComponent(nil), allCertificateRotationComponents...), nil
	}
	switch raw {
	case string(CertificateComponentController):
		return []CertificateRotationComponent{CertificateComponentController}, nil
	case string(CertificateComponentWorker):
		return []CertificateRotationComponent{CertificateComponentWorker}, nil
	case string(CertificateComponentCA):
		return []CertificateRotationComponent{CertificateComponentCA}, nil
	default:
		return nil, fmt.Errorf("--component must be controller, worker, ca, or all, got %q", raw)
	}
}

func PlanCertificateRotation(kind cloud.Kind, tool, component string, probe ProbeResult) (*CertificateRotationPlan, error) {
	if !kind.IsProviderNative() {
		return nil, fmt.Errorf("certificate rotation only supports provider-native clouds, got %q", kind.Canonical())
	}
	components, err := ParseCertificateRotationComponents(component)
	if err != nil {
		return nil, err
	}
	if probe.Status != StatusReady {
		return nil, certificateRotationProbeError(probe)
	}
	outputs := probe.Outputs
	plan := &CertificateRotationPlan{
		Tool:                          tool,
		Cloud:                         kind.Canonical(),
		RequestedComponent:            strings.TrimSpace(strings.ToLower(component)),
		Components:                    components,
		ControllerSecurityMode:        outputOrUnknown(outputs, "controller_security_mode"),
		GenerationID:                  outputOrUnknown(outputs, "generation_id"),
		WorkerCount:                   outputOrUnknown(outputs, "worker_count"),
		ControllerCAFingerprintSHA256: outputOrUnknown(outputs, "controller_ca_fingerprint_sha256"),
		ControllerCertNotAfter:        outputOrUnknown(outputs, "controller_cert_not_after"),
		TLSEnabledServices:            certificateTLSEnabledServices(outputs),
	}
	for _, component := range components {
		addCertificateComponentPlan(plan, component)
	}
	plan.ControllerServices = dedupStrings(plan.ControllerServices)
	plan.Actions = dedupStrings(plan.Actions)
	plan.Verification = dedupStrings(plan.Verification)
	plan.Warnings = dedupStrings(append(plan.Warnings, certificateRotationWarnings(outputs)...))
	return plan, nil
}

func certificateRotationProbeError(probe ProbeResult) error {
	switch probe.Status {
	case StatusMissing:
		return fmt.Errorf("certificate rotation requires ready infrastructure: no Terraform outputs found")
	case StatusStale:
		return fmt.Errorf("certificate rotation requires current infrastructure outputs; missing keys: %s", strings.Join(probe.MissingKeys, ", "))
	case StatusMismatch:
		if probe.DeployedTool != "" {
			return fmt.Errorf("certificate rotation requires matching infrastructure; deployed tool is %q", probe.DeployedTool)
		}
		return fmt.Errorf("certificate rotation requires matching provider-native infrastructure")
	case StatusError:
		if probe.Err != nil {
			return fmt.Errorf("certificate rotation preflight failed: %w", probe.Err)
		}
		return fmt.Errorf("certificate rotation preflight failed")
	default:
		return fmt.Errorf("certificate rotation requires ready infrastructure, got %s", probe.Status)
	}
}

func addCertificateComponentPlan(plan *CertificateRotationPlan, component CertificateRotationComponent) {
	switch component {
	case CertificateComponentController:
		plan.ControllerServices = append(plan.ControllerServices, plan.TLSEnabledServices...)
		plan.Actions = append(plan.Actions,
			"generate a replacement controller server certificate signed by the current controller CA",
			"install the replacement server certificate and key on the controller",
			"restart TLS-enabled controller services so they load the replacement certificate",
		)
		plan.Verification = append(plan.Verification,
			"verify NATS, MinIO, and registry TLS endpoints present the replacement server certificate where TLS is enabled",
			"verify workers resume heartbeats after controller service restart",
		)
	case CertificateComponentWorker:
		plan.WorkerRecycleRequired = true
		plan.Actions = append(plan.Actions,
			"refresh worker controller CA trust material",
			"replace or restart workers so system and Docker CA trust are refreshed",
		)
		plan.Verification = append(plan.Verification,
			"verify workers can reconnect to NATS after trust refresh",
			"verify workers can pull from the controller registry after trust refresh",
		)
	case CertificateComponentCA:
		plan.ControllerServices = append(plan.ControllerServices, plan.TLSEnabledServices...)
		plan.WorkerRecycleRequired = true
		plan.OperatorTrustRefreshRequired = true
		plan.Actions = append(plan.Actions,
			"generate a replacement controller CA and controller server certificate chain",
			"install replacement controller TLS material",
			"refresh operator-side controller CA trust metadata",
			"replace or restart workers so the replacement CA is trusted",
		)
		plan.Verification = append(plan.Verification,
			"verify operator clients trust the replacement controller CA",
			"verify workers resume heartbeats after CA trust refresh",
			"verify registry trust uses the replacement controller CA",
		)
	}
}

func certificateTLSEnabledServices(outputs map[string]string) []string {
	services := []string{}
	if outputStringBool(outputs["nats_tls_enabled"]) || strings.HasPrefix(strings.TrimSpace(outputs["nats_url"]), "tls://") {
		services = append(services, "nats")
	}
	if outputStringBool(outputs["minio_tls_enabled"]) || strings.HasPrefix(strings.TrimSpace(outputs["s3_endpoint"]), "https://") {
		services = append(services, "minio")
	}
	if outputStringBool(outputs["registry_tls_enabled"]) || strings.HasPrefix(strings.TrimSpace(outputs["registry_url"]), "https://") {
		services = append(services, "registry")
	}
	return services
}

func certificateRotationWarnings(outputs map[string]string) []string {
	warnings := []string{}
	mode := strings.TrimSpace(outputs["controller_security_mode"])
	if mode == "private-auth" {
		warnings = append(warnings, "controller_security_mode is private-auth; certificate rotation only affects services after TLS mode is enabled")
	}
	if len(certificateTLSEnabledServices(outputs)) == 0 {
		warnings = append(warnings, "no TLS-enabled controller services are present in current outputs")
	}
	if strings.TrimSpace(outputs["controller_ca_fingerprint_sha256"]) == "" {
		warnings = append(warnings, "controller CA fingerprint output is missing")
	}
	rawExpiry := strings.TrimSpace(outputs["controller_cert_not_after"])
	if rawExpiry == "" {
		return append(warnings, "controller certificate expiry output is missing")
	}
	expiresAt, err := time.Parse(time.RFC3339, rawExpiry)
	if err != nil {
		return append(warnings, fmt.Sprintf("controller certificate expiry %q is not RFC3339", rawExpiry))
	}
	now := time.Now().UTC()
	switch {
	case !expiresAt.After(now):
		warnings = append(warnings, fmt.Sprintf("controller certificate expired at %s", expiresAt.UTC().Format(time.RFC3339)))
	case expiresAt.Before(now.Add(30 * 24 * time.Hour)):
		warnings = append(warnings, fmt.Sprintf("controller certificate expires soon at %s", expiresAt.UTC().Format(time.RFC3339)))
	}
	return warnings
}

func outputStringBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1", "yes", "y", "on":
		return true
	default:
		return false
	}
}
