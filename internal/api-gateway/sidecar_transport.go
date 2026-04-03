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
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"net/url"

	sandboxv1alpha1 "github.com/isola-run/isola/api/v1alpha1"
	"github.com/isola-run/isola/internal/constants"
	"github.com/isola-run/isola/internal/identity"
)

// SidecarTransport provides methods for building sidecar URLs and creating
// HTTP clients that verify the sidecar's mTLS identity.
type SidecarTransport struct {
	caBundlePEM []byte
	port        int
}

// NewSidecarTransport creates a transport that verifies sidecar identity using
// the Isola CA bundle. If caBundlePEM is nil, the transport falls back to plain HTTP.
func NewSidecarTransport(caBundlePEM []byte) *SidecarTransport {
	return &SidecarTransport{
		caBundlePEM: caBundlePEM,
		port:        constants.SidecarPort,
	}
}

// TLSEnabled returns true if TLS verification is configured.
func (t *SidecarTransport) TLSEnabled() bool {
	return len(t.caBundlePEM) > 0
}

// Scheme returns "https" if TLS is enabled, "http" otherwise.
func (t *SidecarTransport) Scheme() string {
	if t.TLSEnabled() {
		return "https"
	}
	return "http"
}

// SidecarBaseURL builds the base URL for a sandbox sidecar.
func (t *SidecarTransport) SidecarBaseURL(sb *sandboxv1alpha1.Sandbox) string {
	return fmt.Sprintf("%s://%s:%d", t.Scheme(), sb.Status.PodIP, t.port)
}

// HTTPClient creates an HTTP client that verifies the sidecar presents a certificate
// with the expected SPIFFE identity for the given sandbox. If TLS is not enabled,
// returns a plain HTTP client.
func (t *SidecarTransport) HTTPClient(sb *sandboxv1alpha1.Sandbox) (*http.Client, error) {
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse // sidecar should never redirect
		},
	}

	if !t.TLSEnabled() {
		return client, nil
	}

	expectedSPIFFEID := identity.SandboxSPIFFEID(sb.Namespace, sb.Name, sb.Status.PodUID)
	expectedURI, err := url.Parse(expectedSPIFFEID)
	if err != nil {
		return nil, fmt.Errorf("parse expected SPIFFE ID: %w", err)
	}

	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(t.caBundlePEM) {
		return nil, fmt.Errorf("failed to parse CA bundle")
	}

	client.Transport = &http.Transport{
		TLSClientConfig: &tls.Config{
			RootCAs:    caPool,
			MinVersion: tls.VersionTLS12,
			// Skip hostname verification since we use IP addresses, but verify via
			// custom callback that the peer cert contains the expected SPIFFE URI SAN.
			InsecureSkipVerify: true,
			VerifyConnection: func(cs tls.ConnectionState) error {
				// Verify the certificate chain against our CA
				opts := x509.VerifyOptions{
					Roots:         caPool,
					CurrentTime:   cs.PeerCertificates[0].NotBefore,
					Intermediates: x509.NewCertPool(),
					KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
				}
				for _, cert := range cs.PeerCertificates[1:] {
					opts.Intermediates.AddCert(cert)
				}
				if _, err := cs.PeerCertificates[0].Verify(opts); err != nil {
					return fmt.Errorf("certificate chain verification failed: %w", err)
				}

				// Verify the exact SPIFFE identity
				for _, uri := range cs.PeerCertificates[0].URIs {
					if uri.String() == expectedURI.String() {
						return nil
					}
				}
				return fmt.Errorf("peer certificate does not contain expected SPIFFE ID %s", expectedSPIFFEID)
			},
		},
	}

	return client, nil
}
