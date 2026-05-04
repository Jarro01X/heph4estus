package tlsutil

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"
)

func TestRootCAPoolEmpty(t *testing.T) {
	pool, err := RootCAPool("", "")
	if err != nil {
		t.Fatalf("RootCAPool: %v", err)
	}
	if pool != nil {
		t.Fatal("expected nil pool without custom CA")
	}
}

func TestRootCAPoolFromPEM(t *testing.T) {
	pool, err := RootCAPool(testCAPEM(t), "")
	if err != nil {
		t.Fatalf("RootCAPool: %v", err)
	}
	if pool == nil {
		t.Fatal("expected cert pool")
	}
}

func TestRootCAPoolInvalidPEM(t *testing.T) {
	if _, err := RootCAPool("not a cert", ""); err == nil {
		t.Fatal("expected invalid PEM error")
	}
}

func TestClientConfigWithServerName(t *testing.T) {
	cfg, err := ClientConfigWithServerName(testCAPEM(t), "", "heph-controller")
	if err != nil {
		t.Fatalf("ClientConfigWithServerName: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected TLS config")
	}
	if cfg.ServerName != "heph-controller" {
		t.Fatalf("ServerName = %q", cfg.ServerName)
	}
}

func testCAPEM(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "heph4estus test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}
