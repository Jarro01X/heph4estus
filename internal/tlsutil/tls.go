package tlsutil

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strings"
)

// ClientConfig builds a TLS client config that trusts the system roots plus an
// optional controller CA supplied as PEM content or a file path.
func ClientConfig(caPEM, caFile string) (*tls.Config, error) {
	return ClientConfigWithServerName(caPEM, caFile, "")
}

// ClientConfigWithServerName is ClientConfig plus an explicit TLS server name
// override for callers that connect by IP while validating a stable DNS SAN.
func ClientConfigWithServerName(caPEM, caFile, serverName string) (*tls.Config, error) {
	return ClientConfigWithIdentity(caPEM, caFile, serverName, "", "", "", "")
}

// ClientConfigWithIdentity is ClientConfigWithServerName plus an optional
// client certificate/key pair for mTLS endpoints.
func ClientConfigWithIdentity(caPEM, caFile, serverName, certPEM, keyPEM, certFile, keyFile string) (*tls.Config, error) {
	pool, err := RootCAPool(caPEM, caFile)
	if err != nil {
		return nil, err
	}
	certs, err := clientCertificates(certPEM, keyPEM, certFile, keyFile)
	if err != nil {
		return nil, err
	}
	if pool == nil && len(certs) == 0 {
		return nil, nil
	}
	return &tls.Config{
		MinVersion:   tls.VersionTLS12,
		RootCAs:      pool,
		ServerName:   strings.TrimSpace(serverName),
		Certificates: certs,
	}, nil
}

// RootCAPool returns nil when no custom CA was supplied. Callers can then use
// the default platform trust behavior.
func RootCAPool(caPEM, caFile string) (*x509.CertPool, error) {
	pemBlocks := strings.TrimSpace(caPEM)
	filePath := strings.TrimSpace(caFile)
	if pemBlocks == "" && filePath == "" {
		return nil, nil
	}

	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}

	if pemBlocks != "" {
		if ok := pool.AppendCertsFromPEM([]byte(pemBlocks)); !ok {
			return nil, fmt.Errorf("parsing controller CA PEM")
		}
	}
	if filePath != "" {
		data, err := os.ReadFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("reading controller CA file: %w", err)
		}
		if ok := pool.AppendCertsFromPEM(data); !ok {
			return nil, fmt.Errorf("parsing controller CA file %q", filePath)
		}
	}
	return pool, nil
}

func clientCertificates(certPEM, keyPEM, certFile, keyFile string) ([]tls.Certificate, error) {
	certPEM = strings.TrimSpace(certPEM)
	keyPEM = strings.TrimSpace(keyPEM)
	certFile = strings.TrimSpace(certFile)
	keyFile = strings.TrimSpace(keyFile)
	switch {
	case certPEM != "" || keyPEM != "":
		if certPEM == "" || keyPEM == "" {
			return nil, fmt.Errorf("client certificate PEM and key PEM must both be set")
		}
		cert, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
		if err != nil {
			return nil, fmt.Errorf("parsing client certificate PEM: %w", err)
		}
		return []tls.Certificate{cert}, nil
	case certFile != "" || keyFile != "":
		if certFile == "" || keyFile == "" {
			return nil, fmt.Errorf("client certificate file and key file must both be set")
		}
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, fmt.Errorf("loading client certificate files: %w", err)
		}
		return []tls.Certificate{cert}, nil
	default:
		return nil, nil
	}
}
