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
	"crypto/x509"
	"encoding/pem"
	"net/url"
	"testing"
	"time"
)

func TestSandboxSPIFFEID(t *testing.T) {
	id := SandboxSPIFFEID("default", "my-sandbox", "abc-123")
	expected := "spiffe://isola.run/ns/default/sandbox/my-sandbox/pod/abc-123"
	if id != expected {
		t.Errorf("got %q, want %q", id, expected)
	}
}

func TestParseSPIFFEID(t *testing.T) {
	uri, _ := url.Parse("spiffe://isola.run/ns/default/sandbox/my-sandbox/pod/abc-123")
	ns, sb, uid, err := ParseSPIFFEID(uri)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ns != "default" || sb != "my-sandbox" || uid != "abc-123" {
		t.Errorf("got ns=%q sb=%q uid=%q", ns, sb, uid)
	}
}

func TestParseSPIFFEID_InvalidDomain(t *testing.T) {
	uri, _ := url.Parse("spiffe://other.domain/ns/default/sandbox/my-sandbox/pod/abc-123")
	_, _, _, err := ParseSPIFFEID(uri)
	if err == nil {
		t.Error("expected error for wrong trust domain")
	}
}

func TestParseSPIFFEID_InvalidPath(t *testing.T) {
	uri, _ := url.Parse("spiffe://isola.run/invalid/path")
	_, _, _, err := ParseSPIFFEID(uri)
	if err == nil {
		t.Error("expected error for invalid path")
	}
}

func TestGenerateCA(t *testing.T) {
	certPEM, keyPEM, err := GenerateCA(CAConfig{
		CommonName: "Test CA",
		Lifetime:   1 * time.Hour,
	})
	if err != nil {
		t.Fatalf("GenerateCA failed: %v", err)
	}

	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil {
		t.Fatal("failed to decode CA cert PEM")
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		t.Fatalf("parse CA cert: %v", err)
	}
	if !cert.IsCA {
		t.Error("expected CA certificate")
	}
	if cert.Subject.CommonName != "Test CA" {
		t.Errorf("got CN=%q, want %q", cert.Subject.CommonName, "Test CA")
	}

	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		t.Fatal("failed to decode CA key PEM")
	}
}

func TestSignCSR(t *testing.T) {
	caCertPEM, caKeyPEM, err := GenerateCA(CAConfig{
		CommonName: "Test CA",
		Lifetime:   1 * time.Hour,
	})
	if err != nil {
		t.Fatalf("GenerateCA: %v", err)
	}

	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	spiffeID := SandboxSPIFFEID("default", "my-sandbox", "uid-123")
	csrPEM, err := CreateCSR(key, spiffeID)
	if err != nil {
		t.Fatalf("CreateCSR: %v", err)
	}

	certPEM, err := SignCSR(csrPEM, caCertPEM, caKeyPEM, 30*time.Minute)
	if err != nil {
		t.Fatalf("SignCSR: %v", err)
	}

	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil {
		t.Fatal("failed to decode signed cert PEM")
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		t.Fatalf("parse signed cert: %v", err)
	}

	if len(cert.URIs) != 1 {
		t.Fatalf("expected 1 URI SAN, got %d", len(cert.URIs))
	}
	if cert.URIs[0].String() != spiffeID {
		t.Errorf("got URI SAN %q, want %q", cert.URIs[0].String(), spiffeID)
	}
	if cert.IsCA {
		t.Error("leaf cert should not be CA")
	}

	// Verify chain
	caPool := x509.NewCertPool()
	caPool.AppendCertsFromPEM(caCertPEM)
	if _, err := cert.Verify(x509.VerifyOptions{
		Roots:     caPool,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}); err != nil {
		t.Errorf("cert chain verification failed: %v", err)
	}
}

func TestSignCSR_InvalidCSR(t *testing.T) {
	caCertPEM, caKeyPEM, _ := GenerateCA(CAConfig{
		CommonName: "Test CA",
		Lifetime:   1 * time.Hour,
	})

	_, err := SignCSR([]byte("not a PEM"), caCertPEM, caKeyPEM, 30*time.Minute)
	if err == nil {
		t.Error("expected error for invalid CSR")
	}
}
