package infra

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"
)

func TestGenerateControllerCertificateMaterial(t *testing.T) {
	caPEM, caKeyPEM := testCAKeyPairPEM(t)
	now := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)

	material, err := GenerateControllerCertificateMaterial(now, map[string]string{
		"controller_ca_pem": caPEM,
		"controller_ip":     "203.0.113.10",
	}, caKeyPEM)
	if err != nil {
		t.Fatalf("GenerateControllerCertificateMaterial: %v", err)
	}
	if material.CAPEM != caPEM {
		t.Fatal("material CA PEM should preserve the current controller CA")
	}
	if !strings.HasPrefix(material.Generation, "cert-20260504t120000z-") {
		t.Fatalf("Generation = %q", material.Generation)
	}
	if material.RotatedAt != "2026-05-04T12:00:00Z" {
		t.Fatalf("RotatedAt = %q", material.RotatedAt)
	}
	if material.NotAfter != "2027-05-04T12:00:00Z" {
		t.Fatalf("NotAfter = %q", material.NotAfter)
	}

	cert, err := parseCertificatePEM(material.CertPEM)
	if err != nil {
		t.Fatalf("parse generated cert: %v", err)
	}
	if err := cert.CheckSignatureFrom(mustParseCert(t, caPEM)); err != nil {
		t.Fatalf("generated cert was not signed by CA: %v", err)
	}
	if !containsDNSName(cert.DNSNames, "heph-controller") || !containsDNSName(cert.DNSNames, "localhost") {
		t.Fatalf("missing expected DNS SANs: %v", cert.DNSNames)
	}
	if !containsIPAddress(cert.IPAddresses, "203.0.113.10") {
		t.Fatalf("missing controller IP SAN: %v", cert.IPAddresses)
	}
	if _, err := parseSignerPrivateKeyPEM(material.KeyPEM); err != nil {
		t.Fatalf("generated private key invalid: %v", err)
	}
}

func TestGenerateControllerCAMaterial(t *testing.T) {
	now := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	material, err := GenerateControllerCAMaterial(now, map[string]string{"controller_ip": "203.0.113.10"})
	if err != nil {
		t.Fatalf("GenerateControllerCAMaterial: %v", err)
	}
	if !strings.HasPrefix(material.Generation, "ca-20260504t120000z-") {
		t.Fatalf("Generation = %q", material.Generation)
	}
	if material.CAFingerprintSHA256 != sha256PEM(material.CAPEM) {
		t.Fatalf("fingerprint = %q, want %q", material.CAFingerprintSHA256, sha256PEM(material.CAPEM))
	}
	if _, err := parseSignerPrivateKeyPEM(material.CAKeyPEM); err != nil {
		t.Fatalf("CA private key invalid: %v", err)
	}
	caCert := mustParseCert(t, material.CAPEM)
	if !caCert.IsCA {
		t.Fatal("generated CA is not marked as a CA")
	}
	serverCert := mustParseCert(t, material.CertPEM)
	if err := serverCert.CheckSignatureFrom(caCert); err != nil {
		t.Fatalf("server cert was not signed by rotated CA: %v", err)
	}
}

func TestGenerateControllerCertificateMaterialRejectsMismatchedCAKey(t *testing.T) {
	caPEM, _ := testCAKeyPairPEM(t)
	_, wrongKeyPEM := testCAKeyPairPEM(t)

	_, err := GenerateControllerCertificateMaterial(time.Now(), map[string]string{"controller_ca_pem": caPEM}, wrongKeyPEM)
	if err == nil {
		t.Fatal("expected mismatched CA key error")
	}
	if !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestControllerCertificateTerraformVars(t *testing.T) {
	vars := ControllerCertificateTerraformVars(ControllerCertificateMaterial{
		CAPEM:      "ca",
		CertPEM:    "cert",
		KeyPEM:     "key",
		NotAfter:   "2027-05-04T12:00:00Z",
		Generation: "cert-gen",
		RotatedAt:  "2026-05-04T12:00:00Z",
	})
	for _, key := range []string{
		"controller_ca_pem_override",
		"controller_cert_pem_override",
		"controller_key_pem_override",
		"controller_cert_not_after_override",
		"controller_cert_generation",
		"controller_cert_rotated_at",
	} {
		if strings.TrimSpace(vars[key]) == "" {
			t.Fatalf("expected var %q in %v", key, vars)
		}
	}
}

func TestControllerCATerraformVarsIncludesCAKey(t *testing.T) {
	vars := ControllerCATerraformVars(ControllerCAMaterial{
		ControllerCertificateMaterial: ControllerCertificateMaterial{
			CAPEM:      "ca",
			CertPEM:    "cert",
			KeyPEM:     "key",
			NotAfter:   "2027-05-04T12:00:00Z",
			Generation: "ca-gen",
			RotatedAt:  "2026-05-04T12:00:00Z",
		},
		CAKeyPEM: "ca-key",
	})
	if vars["controller_ca_key_pem_override"] != "ca-key" {
		t.Fatalf("controller_ca_key_pem_override = %q", vars["controller_ca_key_pem_override"])
	}
	if vars["controller_ca_pem_override"] != "ca" || vars["controller_cert_pem_override"] != "cert" {
		t.Fatalf("missing certificate vars: %v", vars)
	}
}

func TestUpdateControllerCertificateCommand(t *testing.T) {
	caPEM, caKeyPEM := testCAKeyPairPEM(t)
	material, err := GenerateControllerCertificateMaterial(time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC), map[string]string{"controller_ca_pem": caPEM}, caKeyPEM)
	if err != nil {
		t.Fatal(err)
	}
	runner := &captureRemoteRunner{}
	err = UpdateControllerCertificate(context.Background(), runner, "198.51.100.10", ControllerCertificateUpdate{
		Material: material,
		Services: []string{"nats", "minio", "registry", "nats", "bogus"},
	})
	if err != nil {
		t.Fatalf("UpdateControllerCertificate: %v", err)
	}
	if runner.host != "198.51.100.10" {
		t.Fatalf("host = %q", runner.host)
	}
	for _, want := range []string{
		"install -m 0644 \"$tmpdir/ca.crt\" /data/tls/ca.crt",
		"install -m 0644 \"$tmpdir/public.crt\" /data/tls/public.crt",
		"install -m 0600 \"$tmpdir/private.key\" /data/tls/private.key",
		"systemctl restart nats",
		"systemctl restart minio",
		"systemctl restart registry",
	} {
		if !strings.Contains(runner.command, want) {
			t.Fatalf("command missing %q:\n%s", want, runner.command)
		}
	}
	if strings.Count(runner.command, "systemctl restart nats") != 1 {
		t.Fatalf("expected one nats restart:\n%s", runner.command)
	}
	if strings.Contains(runner.command, "bogus") {
		t.Fatalf("unexpected service in command:\n%s", runner.command)
	}
}

type captureRemoteRunner struct {
	host    string
	command string
}

func (r *captureRemoteRunner) Run(_ context.Context, host string, remoteCmd string) error {
	r.host = host
	r.command = remoteCmd
	return nil
}

func testCAKeyPairPEM(t *testing.T) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: "test controller CA", Organization: []string{"heph4estus"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(2 * 365 * 24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}))
}

func mustParseCert(t *testing.T, certPEM string) *x509.Certificate {
	t.Helper()
	cert, err := parseCertificatePEM(certPEM)
	if err != nil {
		t.Fatal(err)
	}
	return cert
}

func containsDNSName(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}

func containsIPAddress(ips []net.IP, want string) bool {
	target := net.ParseIP(want)
	for _, ip := range ips {
		if ip.Equal(target) {
			return true
		}
	}
	return false
}
