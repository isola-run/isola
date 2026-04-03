// Copyright The Isola Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package identity

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/url"
	"time"
)

const (
	// SPIFFETrustDomain is the trust domain for Isola sandbox identities.
	SPIFFETrustDomain = "isola.run"

	// TokenAudience is the expected audience for projected ServiceAccount tokens
	// presented to the identity signer.
	TokenAudience = "isola-identity-signer"

	// TLSCertFile is the filename for the TLS certificate in the shared volume.
	TLSCertFile = "tls.crt"
	// TLSKeyFile is the filename for the TLS private key in the shared volume.
	TLSKeyFile = "tls.key"
	// CABundleFile is the filename for the CA bundle in the shared volume.
	CABundleFile = "ca.crt"
)

// SandboxSPIFFEID builds the SPIFFE URI SAN for a sandbox pod instance.
func SandboxSPIFFEID(namespace, sandboxName, podUID string) string {
	return fmt.Sprintf("spiffe://%s/ns/%s/sandbox/%s/pod/%s",
		SPIFFETrustDomain, namespace, sandboxName, podUID)
}

// ParseSPIFFEID extracts namespace, sandboxName, and podUID from a SPIFFE URI.
func ParseSPIFFEID(uri *url.URL) (namespace, sandboxName, podUID string, err error) {
	if uri.Scheme != "spiffe" || uri.Host != SPIFFETrustDomain {
		return "", "", "", fmt.Errorf("invalid SPIFFE trust domain: %s", uri.String())
	}
	// Path: /ns/<namespace>/sandbox/<sandboxName>/pod/<podUID>
	var ns, sb, pod string
	_, err = fmt.Sscanf(uri.Path, "/ns/%s/sandbox/%s/pod/%s", &ns, &sb, &pod)
	if err != nil {
		// Manual parse since Sscanf doesn't handle slashes well
		parts := splitPath(uri.Path)
		if len(parts) != 6 || parts[0] != "ns" || parts[2] != "sandbox" || parts[4] != "pod" {
			return "", "", "", fmt.Errorf("invalid SPIFFE ID path: %s", uri.Path)
		}
		return parts[1], parts[3], parts[5], nil
	}
	return ns, sb, pod, nil
}

func splitPath(path string) []string {
	var parts []string
	for _, p := range split(path, '/') {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}

func split(s string, sep byte) []string {
	var result []string
	start := 0
	for i := range len(s) {
		if s[i] == sep {
			result = append(result, s[start:i])
			start = i + 1
		}
	}
	result = append(result, s[start:])
	return result
}

// GenerateKey creates a new ECDSA P-256 private key.
func GenerateKey() (*ecdsa.PrivateKey, error) {
	return ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
}

// CreateCSR generates a PKCS#10 certificate signing request with a SPIFFE URI SAN.
func CreateCSR(key *ecdsa.PrivateKey, spiffeID string) ([]byte, error) {
	uri, err := url.Parse(spiffeID)
	if err != nil {
		return nil, fmt.Errorf("parse SPIFFE ID: %w", err)
	}
	template := &x509.CertificateRequest{
		Subject: pkix.Name{},
		URIs:    []*url.URL{uri},
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, template, key)
	if err != nil {
		return nil, fmt.Errorf("create CSR: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER}), nil
}

// EncodePKCS8Key encodes an ECDSA private key in PKCS#8 PEM format.
func EncodePKCS8Key(key *ecdsa.PrivateKey) ([]byte, error) {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), nil
}

// CAConfig holds parameters for generating a self-signed CA certificate.
type CAConfig struct {
	CommonName string
	Lifetime   time.Duration
}

// GenerateCA creates a self-signed CA certificate and private key.
func GenerateCA(cfg CAConfig) (certPEM, keyPEM []byte, err error) {
	key, err := GenerateKey()
	if err != nil {
		return nil, nil, fmt.Errorf("generate CA key: %w", err)
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, fmt.Errorf("generate serial: %w", err)
	}

	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName: cfg.CommonName,
		},
		NotBefore:             now.Add(-5 * time.Minute),
		NotAfter:              now.Add(cfg.Lifetime),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            0,
		MaxPathLenZero:        true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, fmt.Errorf("create CA cert: %w", err)
	}

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM, err = EncodePKCS8Key(key)
	if err != nil {
		return nil, nil, err
	}
	return certPEM, keyPEM, nil
}

// SignCSR signs a PEM-encoded CSR using the provided CA certificate and key,
// producing a short-lived leaf certificate.
func SignCSR(csrPEM, caCertPEM, caKeyPEM []byte, lifetime time.Duration) ([]byte, error) {
	csrBlock, _ := pem.Decode(csrPEM)
	if csrBlock == nil {
		return nil, fmt.Errorf("failed to decode CSR PEM")
	}
	csr, err := x509.ParseCertificateRequest(csrBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse CSR: %w", err)
	}
	if err := csr.CheckSignature(); err != nil {
		return nil, fmt.Errorf("CSR signature invalid: %w", err)
	}

	caCertBlock, _ := pem.Decode(caCertPEM)
	if caCertBlock == nil {
		return nil, fmt.Errorf("failed to decode CA cert PEM")
	}
	caCert, err := x509.ParseCertificate(caCertBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse CA cert: %w", err)
	}

	caKeyBlock, _ := pem.Decode(caKeyPEM)
	if caKeyBlock == nil {
		return nil, fmt.Errorf("failed to decode CA key PEM")
	}
	caKeyRaw, err := x509.ParsePKCS8PrivateKey(caKeyBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse CA key: %w", err)
	}
	caKey, ok := caKeyRaw.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("CA key is not ECDSA")
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("generate serial: %w", err)
	}

	now := time.Now()
	template := &x509.Certificate{
		SerialNumber:          serialNumber,
		Subject:               csr.Subject,
		URIs:                  csr.URIs,
		NotBefore:             now.Add(-5 * time.Minute),
		NotAfter:              now.Add(lifetime),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, caCert, csr.PublicKey, caKey)
	if err != nil {
		return nil, fmt.Errorf("sign certificate: %w", err)
	}

	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}), nil
}
