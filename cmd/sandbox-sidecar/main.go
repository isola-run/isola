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

	"github.com/isola-ai/isola-sb/internal/constants"
	"github.com/isola-ai/isola-sb/internal/env"
	"github.com/isola-ai/isola-sb/internal/logging"
	sandboxsidecar "github.com/isola-ai/isola-sb/internal/sandbox-sidecar"
	"github.com/isola-ai/isola-sb/internal/sandbox-sidecar/command"
	"github.com/isola-ai/isola-sb/internal/sandbox-sidecar/filesystem"
	"github.com/isola-ai/isola-sb/internal/sandbox-sidecar/health"
	"github.com/isola-ai/isola-sb/internal/sandbox-sidecar/proc"
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
	flag.BoolVar(&cfg.devMode, "dev", env.GetOrDefault("ISOLA_DEV_MODE", "") != "", "Enable development mode (text logging)")
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
	filesystem.Register(api, filesystem.New(logger, procFS, pidResolver))
	command.Register(api, command.New(logger, procFS, pidResolver, &command.NsenterCommandBuilder{}))

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
