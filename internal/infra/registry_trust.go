package infra

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"heph4estus/internal/cloud"
)

const (
	defaultDockerCertsDir = "/etc/docker/certs.d"
	dockerCertsDirEnv     = "HEPH_DOCKER_CERTS_DIR"
)

// RegistryTrustConfig describes the operator-side Docker registry CA trust
// needed when provider-native controllers expose the registry over TLS.
type RegistryTrustConfig struct {
	RegistryURL     string
	ControllerCAPEM string
	TrustDir        string
	DockerCertsDir  string
}

// RegistryTrustResult reports whether registry trust was required and where
// the local and Docker daemon CA files are expected to live.
type RegistryTrustResult struct {
	Required     bool
	RegistryHost string
	LocalCAPath  string
	DockerCAPath string
	Trusted      bool
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
	registryURL := strings.TrimSpace(cfg.RegistryURL)
	caPEM := normalizeCertificatePEM(cfg.ControllerCAPEM)
	if registryURL == "" || !strings.HasPrefix(strings.ToLower(registryURL), "https://") || caPEM == "" {
		return &RegistryTrustResult{}, nil
	}
	if err := validateCertificatePEM(caPEM); err != nil {
		return nil, fmt.Errorf("controller registry CA PEM is invalid: %w", err)
	}

	registryHost := dockerRegistryHost(registryURL)
	if registryHost == "" {
		return nil, fmt.Errorf("registry_url %q does not include a registry host", registryURL)
	}

	trustDir := strings.TrimSpace(cfg.TrustDir)
	if trustDir == "" {
		var err error
		trustDir, err = defaultRegistryTrustDir()
		if err != nil {
			return nil, err
		}
	}
	localCAPath := filepath.Join(trustDir, "registry", sanitizeRegistryHost(registryHost), "ca.crt")
	if err := os.MkdirAll(filepath.Dir(localCAPath), 0o700); err != nil {
		return nil, fmt.Errorf("create registry trust cache: %w", err)
	}
	if err := os.WriteFile(localCAPath, []byte(caPEM), 0o644); err != nil {
		return nil, fmt.Errorf("write registry CA trust cache: %w", err)
	}

	dockerCertsDir := strings.TrimSpace(cfg.DockerCertsDir)
	if dockerCertsDir == "" {
		dockerCertsDir = strings.TrimSpace(os.Getenv(dockerCertsDirEnv))
	}
	if dockerCertsDir == "" {
		dockerCertsDir = defaultDockerCertsDir
	}

	result := &RegistryTrustResult{
		Required:     true,
		RegistryHost: registryHost,
		LocalCAPath:  localCAPath,
		DockerCAPath: filepath.Join(dockerCertsDir, registryHost, "ca.crt"),
	}

	dockerCA, err := os.ReadFile(result.DockerCAPath)
	if err != nil {
		if os.IsNotExist(err) {
			return result, registryTrustMissingError(result)
		}
		return result, fmt.Errorf("read Docker registry CA trust at %s: %w\n\n%s", result.DockerCAPath, err, registryTrustInstallInstructions(result))
	}
	if normalizeCertificatePEM(string(dockerCA)) != caPEM {
		return result, fmt.Errorf("Docker registry CA trust at %s does not match controller CA saved at %s\n\n%s", result.DockerCAPath, result.LocalCAPath, registryTrustInstallInstructions(result))
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

func validateCertificatePEM(certPEM string) error {
	rest := []byte(certPEM)
	for {
		block, remaining := pem.Decode(rest)
		if block == nil {
			return fmt.Errorf("no certificate PEM block found")
		}
		if block.Type == "CERTIFICATE" {
			if _, err := x509.ParseCertificate(block.Bytes); err != nil {
				return err
			}
			return nil
		}
		rest = remaining
	}
}
