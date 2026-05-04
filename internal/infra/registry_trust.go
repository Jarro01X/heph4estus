package infra

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"heph4estus/internal/cloud"
)

const (
	defaultDockerCertsDir = "/etc/docker/certs.d"
	dockerCertsDirEnv     = "HEPH_DOCKER_CERTS_DIR"
)

// RegistryTrustConfig describes the operator-side Docker registry CA trust
// needed when provider-native controllers expose the registry over TLS.
type RegistryTrustConfig struct {
	RegistryURL                    string
	ControllerCAPEM                string
	ControllerCAFingerprintSHA256  string
	ControllerCertNotAfter         string
	TrustDir                       string
	DockerCertsDir                 string
	RequireControllerTrustMetadata bool
}

// RegistryTrustResult reports whether registry trust was required and where
// the local and Docker daemon CA files are expected to live.
type RegistryTrustResult struct {
	Required          bool
	RegistryHost      string
	LocalCAPath       string
	DockerCAPath      string
	FingerprintSHA256 string
	CertNotAfter      string
	Trusted           bool
	Installed         bool
	Instructions      string
}

// RegistryTrustInstallConfig configures explicit operator-side Docker trust
// installation. Unlike deploy preflight checks, install requires complete
// controller CA metadata and may write to Docker's registry trust directory.
type RegistryTrustInstallConfig struct {
	RegistryTrustConfig
	DryRun bool
}

// EnsureProviderRegistryTrust checks Docker daemon trust for provider-native
// TLS registries. AWS and non-TLS registry outputs are no-ops.
func EnsureProviderRegistryTrust(kind cloud.Kind, outputs map[string]string) (*RegistryTrustResult, error) {
	if !kind.IsSelfhostedFamily() {
		return &RegistryTrustResult{}, nil
	}
	return EnsureRegistryTrust(RegistryTrustConfig{
		RegistryURL:     outputs["registry_url"],
		ControllerCAPEM: outputs["controller_ca_pem"],
	})
}

// EnsureRegistryTrust saves the controller CA into Heph's local trust cache and
// verifies that Docker daemon registry trust is installed for HTTPS registries.
func EnsureRegistryTrust(cfg RegistryTrustConfig) (*RegistryTrustResult, error) {
	result, caPEM, err := planRegistryTrust(cfg)
	if err != nil {
		return nil, err
	}
	if !result.Required {
		return result, nil
	}
	if err := writeRegistryCACache(result.LocalCAPath, caPEM); err != nil {
		return nil, err
	}
	return verifyDockerRegistryTrust(result, caPEM)
}

// InstallRegistryTrust installs a controller registry CA into Docker daemon
// trust after validating the provider outputs. Callers own confirmation before
// invoking this function.
func InstallRegistryTrust(cfg RegistryTrustInstallConfig) (*RegistryTrustResult, error) {
	base := cfg.RegistryTrustConfig
	base.RequireControllerTrustMetadata = true
	result, caPEM, err := planRegistryTrust(base)
	if err != nil {
		return nil, err
	}
	if !result.Required {
		return nil, fmt.Errorf("registry_url %q is not an HTTPS registry endpoint", base.RegistryURL)
	}
	if cfg.DryRun {
		return result, nil
	}
	if err := writeRegistryCACache(result.LocalCAPath, caPEM); err != nil {
		return nil, err
	}

	dockerCA, err := os.ReadFile(result.DockerCAPath)
	if err == nil && normalizeCertificatePEM(string(dockerCA)) == caPEM {
		result.Trusted = true
		return result, nil
	}
	if err != nil && !os.IsNotExist(err) {
		return result, fmt.Errorf("read Docker registry CA trust at %s: %w\n\n%s", result.DockerCAPath, err, result.Instructions)
	}

	if err := os.MkdirAll(filepath.Dir(result.DockerCAPath), 0o755); err != nil {
		return result, fmt.Errorf("create Docker registry trust directory: %w\n\n%s", err, result.Instructions)
	}
	if err := os.WriteFile(result.DockerCAPath, []byte(caPEM), 0o644); err != nil {
		return result, fmt.Errorf("install Docker registry CA trust at %s: %w\n\n%s", result.DockerCAPath, err, result.Instructions)
	}
	result.Installed = true
	result.Trusted = true
	return result, nil
}

func planRegistryTrust(cfg RegistryTrustConfig) (*RegistryTrustResult, string, error) {
	registryURL := strings.TrimSpace(cfg.RegistryURL)
	caPEM := normalizeCertificatePEM(cfg.ControllerCAPEM)
	if registryURL == "" {
		if cfg.RequireControllerTrustMetadata {
			return nil, "", fmt.Errorf("registry_url output is required for registry trust install")
		}
		return &RegistryTrustResult{}, "", nil
	}
	if !strings.HasPrefix(strings.ToLower(registryURL), "https://") {
		return &RegistryTrustResult{}, "", nil
	}
	if caPEM == "" {
		if cfg.RequireControllerTrustMetadata {
			return nil, "", fmt.Errorf("controller_ca_pem output is required for registry trust install")
		}
		return &RegistryTrustResult{}, "", nil
	}
	if _, err := parseCertificatePEM(caPEM); err != nil {
		return nil, "", fmt.Errorf("controller registry CA PEM is invalid: %w", err)
	}
	fingerprint := sha256PEM(caPEM)
	if expected := strings.TrimSpace(cfg.ControllerCAFingerprintSHA256); expected != "" {
		if normalizeFingerprint(expected) != fingerprint {
			return nil, "", fmt.Errorf("controller CA fingerprint mismatch: output sha256:%s does not match CA PEM sha256:%s", normalizeFingerprint(expected), fingerprint)
		}
	} else if cfg.RequireControllerTrustMetadata {
		return nil, "", fmt.Errorf("controller_ca_fingerprint_sha256 output is required for registry trust install")
	}
	certNotAfter := ""
	if raw := strings.TrimSpace(cfg.ControllerCertNotAfter); raw != "" {
		expiresAt, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return nil, "", fmt.Errorf("controller_cert_not_after output %q is not RFC3339: %w", raw, err)
		}
		if !expiresAt.After(time.Now().UTC()) {
			return nil, "", fmt.Errorf("controller certificate expired at %s", expiresAt.Format(time.RFC3339))
		}
		certNotAfter = expiresAt.Format(time.RFC3339)
	} else if cfg.RequireControllerTrustMetadata {
		return nil, "", fmt.Errorf("controller_cert_not_after output is required for registry trust install")
	}

	registryHost := dockerRegistryHost(registryURL)
	if registryHost == "" {
		return nil, "", fmt.Errorf("registry_url %q does not include a registry host", registryURL)
	}

	trustDir := strings.TrimSpace(cfg.TrustDir)
	if trustDir == "" {
		var err error
		trustDir, err = defaultRegistryTrustDir()
		if err != nil {
			return nil, "", err
		}
	}
	localCAPath := filepath.Join(trustDir, "registry", sanitizeRegistryHost(registryHost), "ca.crt")

	dockerCertsDir := strings.TrimSpace(cfg.DockerCertsDir)
	if dockerCertsDir == "" {
		dockerCertsDir = strings.TrimSpace(os.Getenv(dockerCertsDirEnv))
	}
	if dockerCertsDir == "" {
		dockerCertsDir = defaultDockerCertsDir
	}

	result := &RegistryTrustResult{
		Required:          true,
		RegistryHost:      registryHost,
		LocalCAPath:       localCAPath,
		DockerCAPath:      filepath.Join(dockerCertsDir, registryHost, "ca.crt"),
		FingerprintSHA256: fingerprint,
		CertNotAfter:      certNotAfter,
	}
	result.Instructions = registryTrustInstallInstructions(result)
	return result, caPEM, nil
}

func writeRegistryCACache(localCAPath, caPEM string) error {
	if err := os.MkdirAll(filepath.Dir(localCAPath), 0o700); err != nil {
		return fmt.Errorf("create registry trust cache: %w", err)
	}
	if err := os.WriteFile(localCAPath, []byte(caPEM), 0o644); err != nil {
		return fmt.Errorf("write registry CA trust cache: %w", err)
	}
	return nil
}

func verifyDockerRegistryTrust(result *RegistryTrustResult, caPEM string) (*RegistryTrustResult, error) {
	dockerCA, err := os.ReadFile(result.DockerCAPath)
	if err != nil {
		if os.IsNotExist(err) {
			return result, registryTrustMissingError(result)
		}
		return result, fmt.Errorf("read Docker registry CA trust at %s: %w\n\n%s", result.DockerCAPath, err, registryTrustInstallInstructions(result))
	}
	if normalizeCertificatePEM(string(dockerCA)) != caPEM {
		return result, fmt.Errorf("docker registry CA trust at %s does not match controller CA saved at %s\n\n%s", result.DockerCAPath, result.LocalCAPath, registryTrustInstallInstructions(result))
	}

	result.Trusted = true
	return result, nil
}

func defaultRegistryTrustDir() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config directory for registry trust cache: %w", err)
	}
	return filepath.Join(configDir, "heph4estus", "trust"), nil
}

func registryTrustMissingError(result *RegistryTrustResult) error {
	return fmt.Errorf("controller registry %s uses TLS with a private CA, but Docker daemon trust is not installed\n\nCA saved to %s.\n\n%s", result.RegistryHost, result.LocalCAPath, registryTrustInstallInstructions(result))
}

func registryTrustInstallInstructions(result *RegistryTrustResult) string {
	dockerCADir := filepath.Dir(result.DockerCAPath)
	return fmt.Sprintf("Install it explicitly:\n  sudo mkdir -p %s\n  sudo cp %s %s\n  sudo systemctl restart docker\n\nThen rerun the deploy.", dockerCADir, result.LocalCAPath, result.DockerCAPath)
}

func sanitizeRegistryHost(host string) string {
	replacer := strings.NewReplacer(":", "_", "/", "_", "\\", "_", "[", "", "]", "")
	return replacer.Replace(host)
}

func normalizeCertificatePEM(certPEM string) string {
	certPEM = strings.TrimSpace(certPEM)
	if certPEM == "" {
		return ""
	}
	return certPEM + "\n"
}

func parseCertificatePEM(certPEM string) (*x509.Certificate, error) {
	rest := []byte(certPEM)
	for {
		block, remaining := pem.Decode(rest)
		if block == nil {
			return nil, fmt.Errorf("no certificate PEM block found")
		}
		if block.Type == "CERTIFICATE" {
			return x509.ParseCertificate(block.Bytes)
		}
		rest = remaining
	}
}

func sha256PEM(certPEM string) string {
	sum := sha256.Sum256([]byte(certPEM))
	return hex.EncodeToString(sum[:])
}

func normalizeFingerprint(fingerprint string) string {
	fingerprint = strings.TrimSpace(strings.ToLower(fingerprint))
	fingerprint = strings.TrimPrefix(fingerprint, "sha256:")
	return strings.ReplaceAll(fingerprint, ":", "")
}
