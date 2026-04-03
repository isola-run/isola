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
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/httplog/v2"

	"github.com/isola-run/isola/internal/constants"
	"github.com/isola-run/isola/internal/env"
	"github.com/isola-run/isola/internal/identity"
	"github.com/isola-run/isola/internal/logging"
	sandboxsidecar "github.com/isola-run/isola/internal/sandbox-sidecar"
	"github.com/isola-run/isola/internal/sandbox-sidecar/bootstrap"
	"github.com/isola-run/isola/internal/sandbox-sidecar/command"
	"github.com/isola-run/isola/internal/sandbox-sidecar/filesystem"
	"github.com/isola-run/isola/internal/sandbox-sidecar/health"
	"github.com/isola-run/isola/internal/sandbox-sidecar/proc"
)

const (
	serverReadHeaderTimeout = 10 * time.Second
	serverReadTimeout       = 60 * time.Second
	serverIdleTimeout       = 120 * time.Second
	// have the sandbox-sidecar's WriteTimeout longer than its client's (api-gateway - 45 seconds).
	serverWriteTimeout = 75 * time.Second
)

type config struct {
	logLevel          string
	devMode           bool
	bootstrapCertOnly bool
	signerURL         string
	tokenPath         string
	podName           string
	podUID            string
	namespace         string
	sandboxName       string
	tlsDir            string
}

func main() {
	cfg := config{}

	flag.StringVar(&cfg.logLevel, "log-level", env.GetOrDefault("ISOLA_LOG_LEVEL", "info"), "Log level (debug, info, warn, error)")
	flag.BoolVar(&cfg.devMode, "dev-mode", env.GetOrDefault("ISOLA_DEV_MODE", "") != "", "Enable development mode (text logging)")
	flag.BoolVar(&cfg.bootstrapCertOnly, "bootstrap-cert-only", false, "Run in bootstrap mode: obtain a certificate and exit")
	flag.StringVar(&cfg.signerURL, "signer-url", os.Getenv("ISOLA_SIGNER_URL"), "URL of the identity signer service")
	flag.StringVar(&cfg.tokenPath, "token-path", os.Getenv("ISOLA_TOKEN_PATH"), "Path to projected ServiceAccount token")
	flag.StringVar(&cfg.podName, "pod-name", os.Getenv("ISOLA_POD_NAME"), "Pod name (from downward API)")
	flag.StringVar(&cfg.podUID, "pod-uid", os.Getenv("ISOLA_POD_UID"), "Pod UID (from downward API)")
	flag.StringVar(&cfg.namespace, "namespace", os.Getenv("ISOLA_NAMESPACE"), "Pod namespace (from downward API)")
	flag.StringVar(&cfg.sandboxName, "sandbox-name", os.Getenv("ISOLA_SANDBOX_NAME"), "Sandbox name")
	flag.StringVar(&cfg.tlsDir, "tls-dir", env.GetOrDefault("ISOLA_TLS_DIR", "/etc/isola/tls"), "Directory for TLS material")
	flag.Parse()

	logger := logging.New(logging.Config{
		Level:   cfg.logLevel,
		DevMode: cfg.devMode,
	})

	if cfg.bootstrapCertOnly {
		runBootstrap(cfg, logger)
		return
	}

	runServer(cfg, logger)
}

func runBootstrap(cfg config, logger *slog.Logger) {
	if err := bootstrap.Run(bootstrap.Config{
		SignerURL:   cfg.signerURL,
		TokenPath:   cfg.tokenPath,
		PodName:     cfg.podName,
		PodUID:      cfg.podUID,
		Namespace:   cfg.namespace,
		SandboxName: cfg.sandboxName,
		TLSDir:      cfg.tlsDir,
		Logger:      logger,
	}); err != nil {
		logger.Error("bootstrap failed", "error", err)
		os.Exit(1)
	}
	logger.Info("bootstrap complete")
}

func runServer(cfg config, logger *slog.Logger) {
	r := chi.NewRouter()
	// httplog.RequestLogger automatically includes chi's RequestID and Recoverer middleware
	r.Use(httplog.RequestLogger(&httplog.Logger{
		Logger: logger,
		Options: httplog.Options{
			LogLevel: slog.LevelInfo,
			JSON:     !cfg.devMode,
			Concise:  true,
		},
	}))

	humaConfig := huma.DefaultConfig("Isola Sandbox Sidecar API", "0.1.0")
	humaConfig.Info.Description = "Internal API for sandbox operations"
	// the sandbox-sidecar is internal api, so we don't want to expose the docs
	humaConfig.DocsPath = ""
	humaConfig.SchemasPath = ""
	humaConfig.OpenAPIPath = ""
	api := humachi.New(r, humaConfig)

	procFS := &proc.RealProcFS{}
	pidResolver := sandboxsidecar.NewPIDResolver(procFS)

	health.Register(api, health.New())

	v1 := huma.NewGroup(api, "/v1")
	filesystem.Register(v1, filesystem.New(logger, procFS, pidResolver))
	command.Register(v1, command.New(logger, procFS, pidResolver, &command.ChrootCommandBuilder{}))

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", constants.SidecarPort),
		Handler:           r,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
		ReadHeaderTimeout: serverReadHeaderTimeout,
		ReadTimeout:       serverReadTimeout,
		WriteTimeout:      serverWriteTimeout,
		IdleTimeout:       serverIdleTimeout,
	}

	certFile := cfg.tlsDir + "/" + identity.TLSCertFile
	keyFile := cfg.tlsDir + "/" + identity.TLSKeyFile

	if _, err := os.Stat(certFile); err == nil {
		logger.Info("TLS material found, starting HTTPS server", "port", constants.SidecarPort)
		srv.TLSConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
		if err := srv.ListenAndServeTLS(certFile, keyFile); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}
	} else {
		logger.Info("no TLS material found, starting HTTP server", "port", constants.SidecarPort)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}
	}
}
