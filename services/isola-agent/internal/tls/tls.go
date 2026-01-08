// Package tls provides TLS certificate loading from environment variables for the isola-agent.
package tls

import (
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"os"
)

const (
	// EnvTLSCert is the environment variable containing the base64-encoded TLS certificate
	EnvTLSCert = "ISOLA_AGENT_TLS_CERT"
	// EnvTLSKey is the environment variable containing the base64-encoded TLS private key
	EnvTLSKey = "ISOLA_AGENT_TLS_KEY"
)

// LoadCertFromEnv loads a TLS certificate from base64-encoded environment variables.
// Returns the certificate and a boolean indicating if TLS is enabled.
func LoadCertFromEnv() (tls.Certificate, bool, error) {
	certB64 := os.Getenv(EnvTLSCert)
	keyB64 := os.Getenv(EnvTLSKey)

	if certB64 == "" || keyB64 == "" {
		// TLS not configured - run in HTTP mode
		return tls.Certificate{}, false, nil
	}

	certPEM, err := base64.StdEncoding.DecodeString(certB64)
	if err != nil {
		return tls.Certificate{}, false, fmt.Errorf("decoding TLS cert: %w", err)
	}

	keyPEM, err := base64.StdEncoding.DecodeString(keyB64)
	if err != nil {
		return tls.Certificate{}, false, fmt.Errorf("decoding TLS key: %w", err)
	}

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, false, fmt.Errorf("parsing TLS key pair: %w", err)
	}

	return cert, true, nil
}

// NewTLSConfig creates a TLS configuration with the given certificate.
func NewTLSConfig(cert tls.Certificate) *tls.Config {
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
	}
}
