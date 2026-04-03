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

package bootstrap

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/isola-run/isola/internal/identity"
	signer "github.com/isola-run/isola/internal/identity-signer"
)

// Config holds bootstrap configuration from environment / flags.
type Config struct {
	// SignerURL is the URL of the identity signer service.
	SignerURL string
	// TokenPath is the path to the projected ServiceAccount token.
	TokenPath string
	// PodName from downward API.
	PodName string
	// PodUID from downward API.
	PodUID string
	// Namespace from downward API.
	Namespace string
	// SandboxName from the pod label or env.
	SandboxName string
	// TLSDir is where to write the TLS material (cert, key, CA bundle).
	TLSDir string
	// Logger for structured logging.
	Logger *slog.Logger
}

// Run performs the one-shot certificate bootstrap:
//  1. Read the projected SA token
//  2. Read pod metadata from downward API
//  3. Generate an ECDSA key pair
//  4. Create a CSR with the SPIFFE URI SAN
//  5. Call the identity signer
//  6. Write TLS material to shared volume
func Run(cfg Config) error {
	logger := cfg.Logger

	token, err := os.ReadFile(cfg.TokenPath)
	if err != nil {
		return fmt.Errorf("read projected token: %w", err)
	}
	logger.Info("read projected token", "path", cfg.TokenPath)

	spiffeID := identity.SandboxSPIFFEID(cfg.Namespace, cfg.SandboxName, cfg.PodUID)
	logger.Info("computed SPIFFE ID", "spiffeID", spiffeID)

	key, err := identity.GenerateKey()
	if err != nil {
		return fmt.Errorf("generate key: %w", err)
	}

	csrPEM, err := identity.CreateCSR(key, spiffeID)
	if err != nil {
		return fmt.Errorf("create CSR: %w", err)
	}

	signReq := signer.SignRequest{
		SandboxName: cfg.SandboxName,
		Namespace:   cfg.Namespace,
		PodName:     cfg.PodName,
		PodUID:      cfg.PodUID,
		CSRPEM:      string(csrPEM),
	}

	reqBody, err := json.Marshal(signReq)
	if err != nil {
		return fmt.Errorf("marshal sign request: %w", err)
	}

	signerURL := cfg.SignerURL + "/v1/sign"
	httpReq, err := http.NewRequest(http.MethodPost, signerURL, bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("build HTTP request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+string(token))
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("call identity signer: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("signer returned %d: %s", resp.StatusCode, string(body))
	}

	var signResp signer.SignResponse
	if err := json.NewDecoder(resp.Body).Decode(&signResp); err != nil {
		return fmt.Errorf("decode signer response: %w", err)
	}

	logger.Info("received signed certificate", "identity", signResp.IdentityURI, "notAfter", signResp.NotAfter)

	keyPEM, err := identity.EncodePKCS8Key(key)
	if err != nil {
		return fmt.Errorf("encode private key: %w", err)
	}

	files := map[string][]byte{
		identity.TLSKeyFile:  keyPEM,
		identity.TLSCertFile: []byte(signResp.CertificateChainPEM),
		identity.CABundleFile: []byte(signResp.CABundlePEM),
	}

	for name, data := range files {
		path := filepath.Join(cfg.TLSDir, name)
		if err := os.WriteFile(path, data, 0600); err != nil {
			return fmt.Errorf("write %s: %w", name, err)
		}
		logger.Info("wrote TLS file", "path", path)
	}

	return nil
}
