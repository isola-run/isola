// Package agent provides a TLS client for communicating with sandbox agents.
// It verifies that the agent's certificate is signed by the trusted CA and
// contains the expected sandbox UUID, preventing stale IP reuse attacks.
package agent

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

const (
	// SPIFFEIDPrefix is the expected prefix for sandbox SPIFFE IDs in certificates
	SPIFFEIDPrefix = "spiffe://isola.run/sandbox/"

	// DefaultAgentPort is the TLS port used by sandbox agents
	DefaultAgentPort = 8443
)

// Client provides HTTP clients for communicating with sandbox agents over TLS.
// It periodically reloads the CA bundle to support CA rotation.
type Client struct {
	caBundlePath string
	mu           sync.RWMutex
	caCertPool   *x509.CertPool
	lastModTime  time.Time
}

// NewClient creates a new agent client that verifies certificates against the CA bundle.
func NewClient(caBundlePath string) (*Client, error) {
	c := &Client{caBundlePath: caBundlePath}
	if err := c.loadCABundle(); err != nil {
		return nil, err
	}
	go c.pollCABundle()
	return c, nil
}

func (c *Client) loadCABundle() error {
	bundlePEM, err := os.ReadFile(c.caBundlePath)
	if err != nil {
		return fmt.Errorf("reading CA bundle from %s: %w", c.caBundlePath, err)
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(bundlePEM) {
		return fmt.Errorf("no valid certificates found in CA bundle")
	}

	info, err := os.Stat(c.caBundlePath)
	if err != nil {
		return fmt.Errorf("stat CA bundle: %w", err)
	}

	c.mu.Lock()
	c.caCertPool = pool
	c.lastModTime = info.ModTime()
	c.mu.Unlock()

	log.Printf("Loaded CA bundle from %s", c.caBundlePath)
	return nil
}

func (c *Client) pollCABundle() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		info, err := os.Stat(c.caBundlePath)
		if err != nil {
			continue
		}

		c.mu.RLock()
		lastMod := c.lastModTime
		c.mu.RUnlock()

		if info.ModTime().After(lastMod) {
			if err := c.loadCABundle(); err != nil {
				log.Printf("Failed to reload CA bundle: %v", err)
			}
		}
	}
}

func (c *Client) getCertPool() *x509.CertPool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.caCertPool
}

// HTTPClient returns an HTTP client configured to verify the agent's certificate
// contains the expected sandbox UUID. The certificate must be signed by the trusted CA
// and contain the sandbox UUID in its URI SAN as a SPIFFE ID.
func (c *Client) HTTPClient(expectedUUID string) *http.Client {
	expectedSPIFFE := SPIFFEIDPrefix + expectedUUID

	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS13,
				RootCAs:    c.getCertPool(),
				VerifyPeerCertificate: func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
					return verifySandboxUUID(rawCerts, expectedSPIFFE, expectedUUID)
				},
			},
		},
	}
}

// verifySandboxUUID checks that the certificate contains the expected sandbox UUID.
func verifySandboxUUID(rawCerts [][]byte, expectedSPIFFE, expectedUUID string) error {
	if len(rawCerts) == 0 {
		return fmt.Errorf("no certificate presented by agent")
	}

	cert, err := x509.ParseCertificate(rawCerts[0])
	if err != nil {
		return fmt.Errorf("parsing agent certificate: %w", err)
	}

	// Check URI SANs for SPIFFE ID
	for _, uri := range cert.URIs {
		if uri.String() == expectedSPIFFE {
			return nil
		}
	}

	// Fallback: check Common Name contains UUID
	if cert.Subject.CommonName == "sandbox-"+expectedUUID {
		return nil
	}

	return fmt.Errorf("agent certificate does not contain expected sandbox UUID %s (CN=%s, URIs=%v)",
		expectedUUID, cert.Subject.CommonName, cert.URIs)
}

// Port returns the default agent TLS port.
func Port() int {
	return DefaultAgentPort
}
