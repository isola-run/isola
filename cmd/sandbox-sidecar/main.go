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
	"github.com/isola-run/isola/internal/logging"
	sandboxsidecar "github.com/isola-run/isola/internal/sandbox-sidecar"
	"github.com/isola-run/isola/internal/sandbox-sidecar/command"
	"github.com/isola-run/isola/internal/sandbox-sidecar/filesystem"
	"github.com/isola-run/isola/internal/sandbox-sidecar/health"
	"github.com/isola-run/isola/internal/sandbox-sidecar/proc"
	"github.com/isola-run/isola/internal/sandbox-sidecar/version"
)

const (
	serverReadHeaderTimeout = 10 * time.Second
	serverReadTimeout       = 60 * time.Second
	serverIdleTimeout       = 120 * time.Second
	// have the sandbox-sidecar's WriteTimeout longer than its client's (api-gateway - 45 seconds).
	serverWriteTimeout = 75 * time.Second
)

type config struct {
	logLevel string
	devMode  bool
}

func main() {
	cfg := config{}

	flag.StringVar(&cfg.logLevel, "log-level", env.GetOrDefault("ISOLA_LOG_LEVEL", "info"), "Log level (debug, info, warn, error)")
	flag.BoolVar(&cfg.devMode, "dev-mode", env.GetOrDefault("ISOLA_DEV_MODE", "") != "", "Enable development mode (text logging)")
	flag.Parse()

	logger := logging.New(logging.Config{
		Level:   cfg.logLevel,
		DevMode: cfg.devMode,
	})

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

	// Injected by the operator from its own ISOLA_VERSION env, which the Helm
	// chart sets to .Chart.AppVersion. "dev" when running outside the chart.
	isolaVersion := env.GetOrDefault(constants.IsolaVersionEnv, "dev")
	if isolaVersion == "dev" {
		logger.Warn("ISOLA_VERSION is not set; /version will report \"dev\". This is expected for local dev runs and unexpected when the sidecar is injected by the operator.")
	}

	humaConfig := huma.DefaultConfig("Isola Sandbox Sidecar API", isolaVersion)
	humaConfig.Info.Description = "Internal API for sandbox operations"
	// the sandbox-sidecar is internal api, so we don't want to expose the docs
	humaConfig.DocsPath = ""
	humaConfig.SchemasPath = ""
	humaConfig.OpenAPIPath = ""
	api := humachi.New(r, humaConfig)

	procFS := &proc.RealProcFS{}
	pidResolver := sandboxsidecar.NewPIDResolver(procFS)

	health.Register(api, health.New())
	version.Register(api, version.New(isolaVersion))

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

	// currently no graceful shutdown, but it might make sense to have a short grace period
	// to allow completing retrieval of sandbox app stdout for example (if in progress)
	logger.Info("starting sandbox-sidecar server", "port", constants.SidecarPort)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("server error", "error", err)
		os.Exit(1)
	}
}
