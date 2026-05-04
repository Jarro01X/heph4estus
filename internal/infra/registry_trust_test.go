package infra

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"heph4estus/internal/cloud"
)

func TestEnsureProviderRegistryTrustNoopForAWS(t *testing.T) {
	result, err := EnsureProviderRegistryTrust(cloud.KindAWS, map[string]string{
		"registry_url":         "https://10.0.1.5:5000",
		"controller_ca_pem":    testCAPEM(t, "controller"),
		"registry_tls_enabled": "true",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Required {
		t.Fatal("AWS should not require selfhosted registry trust")
	}
}

func TestEnsureRegistryTrustNoopForHTTP(t *testing.T) {
	result, err := EnsureRegistryTrust(RegistryTrustConfig{
		RegistryURL:     "http://10.0.1.5:5000",
		ControllerCAPEM: testCAPEM(t, "controller"),
		TrustDir:        t.TempDir(),
		DockerCertsDir:  t.TempDir(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Required {
		t.Fatal("HTTP registry should not require Docker CA trust")
	}
}

func TestEnsureRegistryTrustNoopWithoutCAPEM(t *testing.T) {
	result, err := EnsureRegistryTrust(RegistryTrustConfig{
		RegistryURL:    "https://10.0.1.5:5000",
		TrustDir:       t.TempDir(),
		DockerCertsDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Required {
		t.Fatal("missing CA PEM should not force Docker CA trust")
	}
}

func TestEnsureRegistryTrustSavesCAAndErrorsWhenDockerTrustMissing(t *testing.T) {
	caPEM := testCAPEM(t, "controller")
	trustDir := t.TempDir()
	dockerCertsDir := t.TempDir()

	result, err := EnsureRegistryTrust(RegistryTrustConfig{
		RegistryURL:     "https://10.0.1.5:5000",
		ControllerCAPEM: caPEM,
		TrustDir:        trustDir,
		DockerCertsDir:  dockerCertsDir,
	})
	if err == nil {
		t.Fatal("expected missing Docker trust error")
	}
	if result == nil || !result.Required {
		t.Fatal("expected required trust result")
	}
	if result.RegistryHost != "10.0.1.5:5000" {
		t.Fatalf("registry host = %q, want 10.0.1.5:5000", result.RegistryHost)
	}
	localCA, readErr := os.ReadFile(result.LocalCAPath)
	if readErr != nil {
		t.Fatalf("expected local CA file: %v", readErr)
	}
	if string(localCA) != caPEM {
		t.Fatal("local CA file did not contain controller CA")
	}
	msg := err.Error()
	if !strings.Contains(msg, "Docker daemon trust is not installed") {
		t.Fatalf("expected actionable trust error, got %q", msg)
	}
	if !strings.Contains(msg, result.LocalCAPath) || !strings.Contains(msg, result.DockerCAPath) {
		t.Fatalf("expected error to include install paths, got %q", msg)
	}
}

func TestEnsureRegistryTrustPassesWhenDockerTrustMatches(t *testing.T) {
	caPEM := testCAPEM(t, "controller")
	trustDir := t.TempDir()
	dockerCertsDir := t.TempDir()
	dockerCAPath := filepath.Join(dockerCertsDir, "heph-controller:5000", "ca.crt")
	if err := os.MkdirAll(filepath.Dir(dockerCAPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dockerCAPath, []byte(caPEM), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := EnsureRegistryTrust(RegistryTrustConfig{
		RegistryURL:     "https://heph-controller:5000",
		ControllerCAPEM: caPEM,
		TrustDir:        trustDir,
		DockerCertsDir:  dockerCertsDir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Required || !result.Trusted {
		t.Fatalf("expected trusted result, got %+v", result)
	}
	if result.DockerCAPath != dockerCAPath {
		t.Fatalf("DockerCAPath = %q, want %q", result.DockerCAPath, dockerCAPath)
	}
}

func TestEnsureRegistryTrustFailsWhenDockerTrustDiffers(t *testing.T) {
	trustDir := t.TempDir()
	dockerCertsDir := t.TempDir()
	dockerCAPath := filepath.Join(dockerCertsDir, "10.0.1.5:5000", "ca.crt")
	if err := os.MkdirAll(filepath.Dir(dockerCAPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dockerCAPath, []byte(testCAPEM(t, "old-controller")), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := EnsureRegistryTrust(RegistryTrustConfig{
		RegistryURL:     "https://10.0.1.5:5000",
		ControllerCAPEM: testCAPEM(t, "new-controller"),
		TrustDir:        trustDir,
		DockerCertsDir:  dockerCertsDir,
	})
	if err == nil {
		t.Fatal("expected mismatched Docker trust error")
	}
	if result == nil || !result.Required || result.Trusted {
		t.Fatalf("expected required untrusted result, got %+v", result)
	}
	if !strings.Contains(err.Error(), "does not match controller CA") {
		t.Fatalf("expected mismatch error, got %q", err.Error())
	}
}

func TestEnsureRegistryTrustRejectsInvalidCAPEM(t *testing.T) {
	_, err := EnsureRegistryTrust(RegistryTrustConfig{
		RegistryURL:     "https://10.0.1.5:5000",
		ControllerCAPEM: "not a certificate",
		TrustDir:        t.TempDir(),
		DockerCertsDir:  t.TempDir(),
	})
	if err == nil {
		t.Fatal("expected invalid CA error")
	}
	if !strings.Contains(err.Error(), "controller registry CA PEM is invalid") {
		t.Fatalf("expected invalid CA error, got %q", err.Error())
	}
}

func testCAPEM(t *testing.T, commonName string) string {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: commonName},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, pub, priv)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}
