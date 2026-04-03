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

package signer

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/isola-run/isola/internal/identity"
)

// SignRequest is the JSON body sent by the bootstrap init container.
type SignRequest struct {
	SandboxName string `json:"sandboxName"`
	Namespace   string `json:"namespace"`
	PodName     string `json:"podName"`
	PodUID      string `json:"podUID"`
	CSRPEM      string `json:"csrPEM"`
}

// SignResponse is returned on successful signing.
type SignResponse struct {
	CertificateChainPEM string `json:"certificateChainPEM"`
	CABundlePEM         string `json:"caBundlePEM"`
	IdentityURI         string `json:"identityURI"`
	NotAfter            string `json:"notAfter"`
}

// Signer handles certificate signing requests from sandbox bootstrap containers.
type Signer struct {
	logger           *slog.Logger
	kubeClient       kubernetes.Interface
	caCertPEM        []byte
	caKeyPEM         []byte
	certLifetime     time.Duration
	sandboxNamespace string
}

// Config holds signer configuration.
type Config struct {
	Logger           *slog.Logger
	KubeClient       kubernetes.Interface
	CACertPEM        []byte
	CAKeyPEM         []byte
	CertLifetime     time.Duration
	SandboxNamespace string
}

// New creates a new Signer.
func New(cfg Config) *Signer {
	return &Signer{
		logger:           cfg.Logger,
		kubeClient:       cfg.KubeClient,
		caCertPEM:        cfg.CACertPEM,
		caKeyPEM:         cfg.CAKeyPEM,
		certLifetime:     cfg.CertLifetime,
		sandboxNamespace: cfg.SandboxNamespace,
	}
}

// CACertPEM returns the CA certificate PEM for serving to bootstrap clients.
func (s *Signer) CACertPEM() []byte {
	return s.caCertPEM
}

// HandleSign processes a signing request.
func (s *Signer) HandleSign(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract bearer token
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		http.Error(w, "missing bearer token", http.StatusUnauthorized)
		return
	}
	token := strings.TrimPrefix(authHeader, "Bearer ")

	var req SignRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.SandboxName == "" || req.Namespace == "" || req.PodName == "" || req.PodUID == "" || req.CSRPEM == "" {
		http.Error(w, "missing required fields", http.StatusBadRequest)
		return
	}

	if req.Namespace != s.sandboxNamespace {
		s.logger.Warn("namespace mismatch", "requested", req.Namespace, "expected", s.sandboxNamespace)
		http.Error(w, "namespace not allowed", http.StatusForbidden)
		return
	}

	// Validate the projected ServiceAccount token via TokenReview
	if err := s.validateToken(r.Context(), token, req); err != nil {
		s.logger.Warn("token validation failed", "error", err, "podName", req.PodName)
		http.Error(w, "token validation failed", http.StatusForbidden)
		return
	}

	// Build the expected SPIFFE identity
	spiffeID := identity.SandboxSPIFFEID(req.Namespace, req.SandboxName, req.PodUID)

	// Sign the CSR
	certPEM, err := identity.SignCSR([]byte(req.CSRPEM), s.caCertPEM, s.caKeyPEM, s.certLifetime)
	if err != nil {
		s.logger.Error("failed to sign CSR", "error", err, "sandboxName", req.SandboxName)
		http.Error(w, "signing failed", http.StatusInternalServerError)
		return
	}

	notAfter := time.Now().Add(s.certLifetime).UTC().Format(time.RFC3339)

	resp := SignResponse{
		CertificateChainPEM: string(certPEM),
		CABundlePEM:         string(s.caCertPEM),
		IdentityURI:         spiffeID,
		NotAfter:            notAfter,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.logger.Error("failed to encode response", "error", err)
	}

	s.logger.Info("signed certificate", "sandboxName", req.SandboxName, "podUID", req.PodUID, "spiffeID", spiffeID)
}

// HandleHealth returns a simple health check response.
func (s *Signer) HandleHealth(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func (s *Signer) validateToken(ctx context.Context, token string, req SignRequest) error {
	review := &authenticationv1.TokenReview{
		Spec: authenticationv1.TokenReviewSpec{
			Token:     token,
			Audiences: []string{identity.TokenAudience},
		},
	}

	result, err := s.kubeClient.AuthenticationV1().TokenReviews().Create(ctx, review, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("TokenReview API call failed: %w", err)
	}

	if !result.Status.Authenticated {
		return fmt.Errorf("token not authenticated: %s", result.Status.Error)
	}

	// Verify the token is bound to the expected pod
	ref := result.Status.User.Extra
	if ref == nil {
		return fmt.Errorf("token has no pod binding")
	}

	podNames := ref["authentication.kubernetes.io/pod-name"]
	podUIDs := ref["authentication.kubernetes.io/pod-uid"]

	if len(podNames) == 0 || podNames[0] != req.PodName {
		return fmt.Errorf("token pod name mismatch: got %v, expected %s", podNames, req.PodName)
	}
	if len(podUIDs) == 0 || podUIDs[0] != req.PodUID {
		return fmt.Errorf("token pod UID mismatch: got %v, expected %s", podUIDs, req.PodUID)
	}

	return nil
}
