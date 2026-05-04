package infra

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"strings"
	"time"
)

const controllerCertValidity = 365 * 24 * time.Hour

type ControllerCertificateMaterial struct {
	CAPEM      string
	CertPEM    string
	KeyPEM     string
	NotAfter   string
	Generation string
	RotatedAt  string
}

type ControllerCAMaterial struct {
	ControllerCertificateMaterial
	CAKeyPEM            string
	CAFingerprintSHA256 string
}

type ControllerCertificateUpdate struct {
	Material ControllerCertificateMaterial
	Services []string
}

func GenerateControllerCertificateMaterial(now time.Time, outputs map[string]string, caPrivateKeyPEM string) (ControllerCertificateMaterial, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()
	generation, err := newCertificateGeneration("cert", now)
	if err != nil {
		return ControllerCertificateMaterial{}, err
	}
	caPEM := normalizeCertificatePEM(outputs["controller_ca_pem"])
	if caPEM == "" {
		return ControllerCertificateMaterial{}, fmt.Errorf("controller_ca_pem output is required for controller certificate rotation")
	}
	return generateControllerServerCertificateMaterial(now, outputs, caPEM, caPrivateKeyPEM, generation)
}

func GenerateControllerCAMaterial(now time.Time, outputs map[string]string) (ControllerCAMaterial, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()
	generation, err := newCertificateGeneration("ca", now)
	if err != nil {
		return ControllerCAMaterial{}, err
	}
	caPEM, caKeyPEM, err := generateControllerCA(now)
	if err != nil {
		return ControllerCAMaterial{}, err
	}
	certMaterial, err := generateControllerServerCertificateMaterial(now, outputs, caPEM, caKeyPEM, generation)
	if err != nil {
		return ControllerCAMaterial{}, err
	}
	return ControllerCAMaterial{
		ControllerCertificateMaterial: certMaterial,
		CAKeyPEM:                      caKeyPEM,
		CAFingerprintSHA256:           sha256PEM(caPEM),
	}, nil
}

func generateControllerServerCertificateMaterial(now time.Time, outputs map[string]string, caPEM, caPrivateKeyPEM, generation string) (ControllerCertificateMaterial, error) {
	caCert, err := parseCertificatePEM(caPEM)
	if err != nil {
		return ControllerCertificateMaterial{}, fmt.Errorf("controller_ca_pem output is invalid: %w", err)
	}
	if err := validateControllerCACertificate(caCert, now); err != nil {
		return ControllerCertificateMaterial{}, err
	}
	caKey, err := parseSignerPrivateKeyPEM(caPrivateKeyPEM)
	if err != nil {
		return ControllerCertificateMaterial{}, fmt.Errorf("controller CA private key is invalid: %w", err)
	}
	if err := verifySignerMatchesCertificate(caKey, caCert); err != nil {
		return ControllerCertificateMaterial{}, err
	}

	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return ControllerCertificateMaterial{}, fmt.Errorf("generating controller server key: %w", err)
	}
	serverKeyDER, err := x509.MarshalECPrivateKey(serverKey)
	if err != nil {
		return ControllerCertificateMaterial{}, fmt.Errorf("encoding controller server key: %w", err)
	}

	serial, err := randomCertificateSerial()
	if err != nil {
		return ControllerCertificateMaterial{}, err
	}
	notAfter := now.Add(controllerCertValidity)
	if caCert.NotAfter.Before(notAfter) {
		notAfter = caCert.NotAfter
	}
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "heph-controller", Organization: []string{"heph4estus"}},
		DNSNames:              []string{"heph-controller", "localhost", "host.docker.internal"},
		IPAddresses:           controllerCertificateIPs(outputs),
		NotBefore:             now.Add(-5 * time.Minute),
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, template, caCert, &serverKey.PublicKey, caKey)
	if err != nil {
		return ControllerCertificateMaterial{}, fmt.Errorf("signing controller server certificate: %w", err)
	}
	return ControllerCertificateMaterial{
		CAPEM:      caPEM,
		CertPEM:    string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})),
		KeyPEM:     string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: serverKeyDER})),
		NotAfter:   notAfter.Format(time.RFC3339),
		Generation: generation,
		RotatedAt:  now.Format(time.RFC3339),
	}, nil
}

func ControllerCertificateTerraformVars(material ControllerCertificateMaterial) map[string]string {
	return map[string]string{
		"controller_ca_pem_override":         material.CAPEM,
		"controller_cert_pem_override":       material.CertPEM,
		"controller_key_pem_override":        material.KeyPEM,
		"controller_cert_not_after_override": material.NotAfter,
		"controller_cert_generation":         material.Generation,
		"controller_cert_rotated_at":         material.RotatedAt,
	}
}

func ControllerCATerraformVars(material ControllerCAMaterial) map[string]string {
	vars := ControllerCertificateTerraformVars(material.ControllerCertificateMaterial)
	vars["controller_ca_key_pem_override"] = material.CAKeyPEM
	return vars
}

func UpdateControllerCertificate(ctx context.Context, runner RemoteCommandRunner, host string, update ControllerCertificateUpdate) error {
	if runner == nil {
		return fmt.Errorf("remote runner is required")
	}
	if err := validateControllerCertificateMaterial(update.Material); err != nil {
		return err
	}
	return runner.Run(ctx, host, controllerCertificateUpdateCommand(update))
}

// CertificateTLSEnabledServices returns the controller services whose current
// outputs indicate that TLS material is in use.
func CertificateTLSEnabledServices(outputs map[string]string) []string {
	return certificateTLSEnabledServices(outputs)
}

func validateControllerCertificateMaterial(material ControllerCertificateMaterial) error {
	if normalizeCertificatePEM(material.CAPEM) == "" {
		return fmt.Errorf("controller CA PEM is required")
	}
	if _, err := parseCertificatePEM(material.CAPEM); err != nil {
		return fmt.Errorf("controller CA PEM is invalid: %w", err)
	}
	if normalizeCertificatePEM(material.CertPEM) == "" {
		return fmt.Errorf("controller certificate PEM is required")
	}
	if _, err := parseCertificatePEM(material.CertPEM); err != nil {
		return fmt.Errorf("controller certificate PEM is invalid: %w", err)
	}
	if _, err := parseSignerPrivateKeyPEM(material.KeyPEM); err != nil {
		return fmt.Errorf("controller private key PEM is invalid: %w", err)
	}
	if strings.TrimSpace(material.Generation) == "" {
		return fmt.Errorf("controller certificate generation is required")
	}
	return nil
}

func validateControllerCACertificate(cert *x509.Certificate, now time.Time) error {
	if cert == nil {
		return fmt.Errorf("controller CA certificate is required")
	}
	if !cert.IsCA {
		return fmt.Errorf("controller_ca_pem output is not a CA certificate")
	}
	if cert.KeyUsage != 0 && cert.KeyUsage&x509.KeyUsageCertSign == 0 {
		return fmt.Errorf("controller_ca_pem output cannot sign certificates")
	}
	if !cert.NotAfter.After(now) {
		return fmt.Errorf("controller CA certificate expired at %s", cert.NotAfter.UTC().Format(time.RFC3339))
	}
	return nil
}

func generateControllerCA(now time.Time) (string, string, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("generating controller CA key: %w", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return "", "", fmt.Errorf("encoding controller CA key: %w", err)
	}
	serial, err := randomCertificateSerial()
	if err != nil {
		return "", "", err
	}
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "heph4estus controller CA", Organization: []string{"heph4estus"}},
		NotBefore:             now.Add(-5 * time.Minute),
		NotAfter:              now.Add(controllerCertValidity),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return "", "", fmt.Errorf("signing controller CA certificate: %w", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})),
		string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})), nil
}

func controllerCertificateUpdateCommand(update ControllerCertificateUpdate) string {
	material := update.Material
	lines := []string{
		"set -eu",
		"install -d -m 0755 /data/tls",
		"backup_dir=" + shellQuote("/data/tls/backups/"+material.Generation),
		`install -d -m 0700 "$backup_dir"`,
		`[ ! -f /data/tls/ca.crt ] || cp /data/tls/ca.crt "$backup_dir/ca.crt"`,
		`[ ! -f /data/tls/public.crt ] || cp /data/tls/public.crt "$backup_dir/public.crt"`,
		`[ ! -f /data/tls/private.key ] || cp /data/tls/private.key "$backup_dir/private.key"`,
		`tmpdir=$(mktemp -d)`,
		`trap 'rm -rf "$tmpdir"' EXIT`,
		`cat > "$tmpdir/ca.crt" <<'HEPH_CONTROLLER_CA'`,
		normalizePEM(material.CAPEM),
		"HEPH_CONTROLLER_CA",
		`cat > "$tmpdir/public.crt" <<'HEPH_CONTROLLER_CERT'`,
		normalizePEM(material.CertPEM),
		"HEPH_CONTROLLER_CERT",
		`cat > "$tmpdir/private.key" <<'HEPH_CONTROLLER_KEY'`,
		normalizePEM(material.KeyPEM),
		"HEPH_CONTROLLER_KEY",
		`install -m 0644 "$tmpdir/ca.crt" /data/tls/ca.crt`,
		`install -m 0644 "$tmpdir/public.crt" /data/tls/public.crt`,
		`install -m 0600 "$tmpdir/private.key" /data/tls/private.key`,
	}
	for _, service := range sanitizedControllerServices(update.Services) {
		lines = append(lines, "systemctl restart "+service)
	}
	return strings.Join(lines, "\n")
}

func sanitizedControllerServices(services []string) []string {
	allowed := map[string]struct{}{
		"nats":     {},
		"minio":    {},
		"registry": {},
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(services))
	for _, service := range services {
		service = strings.ToLower(strings.TrimSpace(service))
		if _, ok := allowed[service]; !ok {
			continue
		}
		if _, ok := seen[service]; ok {
			continue
		}
		seen[service] = struct{}{}
		out = append(out, service)
	}
	return out
}

func controllerCertificateIPs(outputs map[string]string) []net.IP {
	ips := []net.IP{net.ParseIP("127.0.0.1")}
	if ip := net.ParseIP(strings.TrimSpace(outputs["controller_ip"])); ip != nil && !ip.IsUnspecified() {
		ips = append(ips, ip)
	}
	return ips
}

func parseSignerPrivateKeyPEM(keyPEM string) (crypto.Signer, error) {
	rest := []byte(strings.TrimSpace(keyPEM))
	for {
		block, remaining := pem.Decode(rest)
		if block == nil {
			return nil, fmt.Errorf("no private key PEM block found")
		}
		switch block.Type {
		case "EC PRIVATE KEY":
			key, err := x509.ParseECPrivateKey(block.Bytes)
			if err != nil {
				return nil, err
			}
			return key, nil
		case "RSA PRIVATE KEY":
			key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
			if err != nil {
				return nil, err
			}
			return key, nil
		case "PRIVATE KEY":
			key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
			if err != nil {
				return nil, err
			}
			signer, ok := key.(crypto.Signer)
			if !ok {
				return nil, fmt.Errorf("private key type %T does not implement crypto.Signer", key)
			}
			return signer, nil
		}
		rest = remaining
	}
}

func verifySignerMatchesCertificate(signer crypto.Signer, cert *x509.Certificate) error {
	signerPublic, err := x509.MarshalPKIXPublicKey(signer.Public())
	if err != nil {
		return fmt.Errorf("encoding CA private key public key: %w", err)
	}
	certPublic, err := x509.MarshalPKIXPublicKey(cert.PublicKey)
	if err != nil {
		return fmt.Errorf("encoding CA certificate public key: %w", err)
	}
	if string(signerPublic) != string(certPublic) {
		return fmt.Errorf("controller CA private key does not match controller_ca_pem output")
	}
	return nil
}

func randomCertificateSerial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return nil, fmt.Errorf("generating certificate serial: %w", err)
	}
	if serial.Sign() == 0 {
		return big.NewInt(1), nil
	}
	return serial, nil
}

func newCertificateGeneration(prefix string, now time.Time) (string, error) {
	suffix, err := randomHex(4)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%s-%s", prefix, now.Format("20060102t150405z"), suffix), nil
}

var _ crypto.Signer = (*ecdsa.PrivateKey)(nil)
var _ crypto.Signer = (*rsa.PrivateKey)(nil)
