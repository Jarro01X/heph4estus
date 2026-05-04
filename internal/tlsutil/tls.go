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
	pool, err := RootCAPool(caPEM, caFile)
	if err != nil {
		return nil, err
	}
	if pool == nil {
		return nil, nil
	}
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    pool,
		ServerName: strings.TrimSpace(serverName),
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
