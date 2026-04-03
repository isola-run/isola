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

package main

import (
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/isola-run/isola/internal/env"
	signer "github.com/isola-run/isola/internal/identity-signer"
	"github.com/isola-run/isola/internal/identity"
	"github.com/isola-run/isola/internal/logging"
)

const (
	serverReadHeaderTimeout = 10 * time.Second
	serverReadTimeout       = 30 * time.Second
	serverWriteTimeout      = 30 * time.Second
	serverIdleTimeout       = 120 * time.Second

	defaultCertLifetime = 24 * time.Hour
	defaultCALifetime   = 365 * 24 * time.Hour
)

type config struct {
	httpPort         int
	logLevel         string
	devMode          bool
	certLifetime     time.Duration
	sandboxNamespace string
	caCertPath       string
	caKeyPath        string
}

func main() {
	cfg := config{}

	flag.IntVar(&cfg.httpPort, "http-port", env.GetOrDefaultInt("ISOLA_SIGNER_HTTP_PORT", 8443), "HTTP server port")
	flag.StringVar(&cfg.logLevel, "log-level", env.GetOrDefault("ISOLA_LOG_LEVEL", "info"), "Log level (debug, info, warn, error)")
	flag.BoolVar(&cfg.devMode, "dev-mode", env.GetOrDefault("ISOLA_DEV_MODE", "") != "", "Enable development mode (text logging)")
	flag.DurationVar(&cfg.certLifetime, "cert-lifetime", defaultCertLifetime, "Lifetime for issued leaf certificates")
	flag.StringVar(&cfg.sandboxNamespace, "sandbox-namespace", os.Getenv("ISOLA_SANDBOX_NAMESPACE"), "Namespace where sandboxes are created (required)")
	flag.StringVar(&cfg.caCertPath, "ca-cert", os.Getenv("ISOLA_CA_CERT_PATH"), "Path to CA certificate PEM file (auto-generated if empty)")
	flag.StringVar(&cfg.caKeyPath, "ca-key", os.Getenv("ISOLA_CA_KEY_PATH"), "Path to CA private key PEM file (auto-generated if empty)")
	flag.Parse()

	logger := logging.New(logging.Config{
		Level:   cfg.logLevel,
		DevMode: cfg.devMode,
	})

	if cfg.sandboxNamespace == "" {
		logger.Error("--sandbox-namespace or ISOLA_SANDBOX_NAMESPACE is required")
		os.Exit(1)
	}

	var caCertPEM, caKeyPEM []byte
	var err error

	if cfg.caCertPath != "" && cfg.caKeyPath != "" {
		caCertPEM, err = os.ReadFile(cfg.caCertPath)
		if err != nil {
			logger.Error("failed to read CA cert", "error", err)
			os.Exit(1)
		}
		caKeyPEM, err = os.ReadFile(cfg.caKeyPath)
		if err != nil {
			logger.Error("failed to read CA key", "error", err)
			os.Exit(1)
		}
		logger.Info("loaded CA from files", "cert", cfg.caCertPath, "key", cfg.caKeyPath)
	} else {
		caCertPEM, caKeyPEM, err = identity.GenerateCA(identity.CAConfig{
			CommonName: "Isola Identity CA",
			Lifetime:   defaultCALifetime,
		})
		if err != nil {
			logger.Error("failed to generate CA", "error", err)
			os.Exit(1)
		}
		logger.Info("generated ephemeral CA (will be lost on restart)")
	}

	restConfig, err := rest.InClusterConfig()
	if err != nil {
		logger.Error("failed to get in-cluster config", "error", err)
		os.Exit(1)
	}
	kubeClient, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		logger.Error("failed to create kubernetes client", "error", err)
		os.Exit(1)
	}

	s := signer.New(signer.Config{
		Logger:           logger,
		KubeClient:       kubeClient,
		CACertPEM:        caCertPEM,
		CAKeyPEM:         caKeyPEM,
		CertLifetime:     cfg.certLifetime,
		SandboxNamespace: cfg.sandboxNamespace,
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/sign", s.HandleSign)
	mux.HandleFunc("/healthz", s.HandleHealth)
	// Serve CA bundle so the gateway can fetch it
	mux.HandleFunc("/v1/ca-bundle", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-pem-file")
		_, _ = w.Write(s.CACertPEM())
	})

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.httpPort),
		Handler:           mux,
		ReadHeaderTimeout: serverReadHeaderTimeout,
		ReadTimeout:       serverReadTimeout,
		WriteTimeout:      serverWriteTimeout,
		IdleTimeout:       serverIdleTimeout,
	}

	logger.Info("starting identity-signer server", "port", cfg.httpPort)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("server error", "error", err)
		os.Exit(1)
	}
}
