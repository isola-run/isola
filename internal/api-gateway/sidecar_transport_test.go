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

package apigateway

import (
	"crypto/ecdsa"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	sandboxv1alpha1 "github.com/isola-run/isola/api/v1alpha1"
	"github.com/isola-run/isola/internal/identity"
)

func TestSidecarTransport_NoTLS(t *testing.T) {
	tr := NewSidecarTransport(nil)
	if tr.TLSEnabled() {
		t.Error("expected TLS disabled with nil CA bundle")
	}
	if tr.Scheme() != "http" {
		t.Errorf("expected http scheme, got %s", tr.Scheme())
	}

	sb := &sandboxv1alpha1.Sandbox{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-sb",
			Namespace: "default",
			UID:       types.UID("uid-123"),
		},
		Status: sandboxv1alpha1.SandboxStatus{
			PodIP:  "10.0.0.1",
			PodUID: "uid-123",
		},
	}

	client, err := tr.HTTPClient(sb)
	if err != nil {
		t.Fatalf("HTTPClient failed: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestSidecarTransport_WithTLS(t *testing.T) {
	caCertPEM, caKeyPEM, err := identity.GenerateCA(identity.CAConfig{
		CommonName: "Test CA",
		Lifetime:   1 * time.Hour,
	})
	if err != nil {
		t.Fatalf("GenerateCA: %v", err)
	}

	tr := NewSidecarTransport(caCertPEM)
	if !tr.TLSEnabled() {
		t.Error("expected TLS enabled")
	}
	if tr.Scheme() != "https" {
		t.Errorf("expected https scheme, got %s", tr.Scheme())
	}

	// Generate a server cert with the expected SPIFFE identity
	serverKey, err := identity.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	spiffeID := identity.SandboxSPIFFEID("default", "test-sb", "uid-123")
	csrPEM, err := identity.CreateCSR(serverKey, spiffeID)
	if err != nil {
		t.Fatalf("CreateCSR: %v", err)
	}
	serverCertPEM, err := identity.SignCSR(csrPEM, caCertPEM, caKeyPEM, 30*time.Minute)
	if err != nil {
		t.Fatalf("SignCSR: %v", err)
	}

	serverCert, err := tls.X509KeyPair(serverCertPEM, mustEncodePKCS8(t, serverKey))
	if err != nil {
		t.Fatalf("X509KeyPair: %v", err)
	}

	// Start a test HTTPS server with the server cert
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	server.TLS = &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		MinVersion:   tls.VersionTLS12,
	}
	server.StartTLS()
	defer server.Close()

	sb := &sandboxv1alpha1.Sandbox{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-sb",
			Namespace: "default",
		},
		Status: sandboxv1alpha1.SandboxStatus{
			PodIP:  "127.0.0.1",
			PodUID: "uid-123",
		},
	}

	client, err := tr.HTTPClient(sb)
	if err != nil {
		t.Fatalf("HTTPClient: %v", err)
	}

	// Make request to the test server
	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestSidecarTransport_RejectsMismatchedIdentity(t *testing.T) {
	caCertPEM, caKeyPEM, err := identity.GenerateCA(identity.CAConfig{
		CommonName: "Test CA",
		Lifetime:   1 * time.Hour,
	})
	if err != nil {
		t.Fatalf("GenerateCA: %v", err)
	}

	tr := NewSidecarTransport(caCertPEM)

	// Generate server cert with a DIFFERENT pod UID
	serverKey, err := identity.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	wrongSpiffeID := identity.SandboxSPIFFEID("default", "test-sb", "wrong-uid")
	csrPEM, err := identity.CreateCSR(serverKey, wrongSpiffeID)
	if err != nil {
		t.Fatalf("CreateCSR: %v", err)
	}
	serverCertPEM, err := identity.SignCSR(csrPEM, caCertPEM, caKeyPEM, 30*time.Minute)
	if err != nil {
		t.Fatalf("SignCSR: %v", err)
	}

	serverCert, err := tls.X509KeyPair(serverCertPEM, mustEncodePKCS8(t, serverKey))
	if err != nil {
		t.Fatalf("X509KeyPair: %v", err)
	}

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	server.TLS = &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		MinVersion:   tls.VersionTLS12,
	}
	server.StartTLS()
	defer server.Close()

	sb := &sandboxv1alpha1.Sandbox{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-sb",
			Namespace: "default",
		},
		Status: sandboxv1alpha1.SandboxStatus{
			PodIP:  "127.0.0.1",
			PodUID: "correct-uid", // Different from the cert
		},
	}

	client, err := tr.HTTPClient(sb)
	if err != nil {
		t.Fatalf("HTTPClient: %v", err)
	}

	_, err = client.Get(server.URL)
	if err == nil {
		t.Error("expected TLS verification to fail for mismatched SPIFFE ID")
	}
}

func TestSidecarTransport_RejectsUntrustedCA(t *testing.T) {
	caCertPEM, _, err := identity.GenerateCA(identity.CAConfig{
		CommonName: "Trusted CA",
		Lifetime:   1 * time.Hour,
	})
	if err != nil {
		t.Fatalf("GenerateCA: %v", err)
	}

	// Different CA for signing
	_, otherCAKeyPEM, err := identity.GenerateCA(identity.CAConfig{
		CommonName: "Other CA",
		Lifetime:   1 * time.Hour,
	})
	if err != nil {
		t.Fatalf("GenerateCA: %v", err)
	}
	otherCACertPEM, _, err := identity.GenerateCA(identity.CAConfig{
		CommonName: "Other CA",
		Lifetime:   1 * time.Hour,
	})
	if err != nil {
		t.Fatalf("GenerateCA: %v", err)
	}

	tr := NewSidecarTransport(caCertPEM) // trust only the first CA

	serverKey, err := identity.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	spiffeID := identity.SandboxSPIFFEID("default", "test-sb", "uid-123")
	csrPEM, err := identity.CreateCSR(serverKey, spiffeID)
	if err != nil {
		t.Fatalf("CreateCSR: %v", err)
	}
	// Sign with the UNTRUSTED CA
	serverCertPEM, err := identity.SignCSR(csrPEM, otherCACertPEM, otherCAKeyPEM, 30*time.Minute)
	if err != nil {
		t.Fatalf("SignCSR: %v", err)
	}

	serverCert, err := tls.X509KeyPair(serverCertPEM, mustEncodePKCS8(t, serverKey))
	if err != nil {
		t.Fatalf("X509KeyPair: %v", err)
	}

	// Need to add the untrusted CA to the server's pool so the server can serve it
	untrustedPool := x509.NewCertPool()
	untrustedPool.AppendCertsFromPEM(otherCACertPEM)

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	server.TLS = &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		MinVersion:   tls.VersionTLS12,
	}
	server.StartTLS()
	defer server.Close()

	sb := &sandboxv1alpha1.Sandbox{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-sb",
			Namespace: "default",
		},
		Status: sandboxv1alpha1.SandboxStatus{
			PodIP:  "127.0.0.1",
			PodUID: "uid-123",
		},
	}

	client, err := tr.HTTPClient(sb)
	if err != nil {
		t.Fatalf("HTTPClient: %v", err)
	}

	_, err = client.Get(server.URL)
	if err == nil {
		t.Error("expected TLS verification to fail for untrusted CA")
	}
}

func TestSidecarBaseURL(t *testing.T) {
	tr := NewSidecarTransport(nil)
	sb := &sandboxv1alpha1.Sandbox{
		Status: sandboxv1alpha1.SandboxStatus{PodIP: "10.0.0.1"},
	}
	url := tr.SidecarBaseURL(sb)
	expected := fmt.Sprintf("http://10.0.0.1:%d", tr.port)
	if url != expected {
		t.Errorf("got %q, want %q", url, expected)
	}

	tr2 := NewSidecarTransport([]byte("some-ca"))
	url2 := tr2.SidecarBaseURL(sb)
	expected2 := fmt.Sprintf("https://10.0.0.1:%d", tr2.port)
	if url2 != expected2 {
		t.Errorf("got %q, want %q", url2, expected2)
	}
}

func mustEncodePKCS8(t *testing.T, key interface{}) []byte {
	t.Helper()
	ecKey, ok := key.(*ecdsa.PrivateKey)
	if !ok {
		t.Fatal("expected *ecdsa.PrivateKey")
	}
	pem, err := identity.EncodePKCS8Key(ecKey)
	if err != nil {
		t.Fatalf("EncodePKCS8Key: %v", err)
	}
	return pem
}
