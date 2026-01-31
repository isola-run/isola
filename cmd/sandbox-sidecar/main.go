package main

import (
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/httplog/v2"

	"github.com/isola-ai/isola-sb/internal/env"
	"github.com/isola-ai/isola-sb/internal/logging"
	"github.com/isola-ai/isola-sb/internal/sandbox-sidecar/handlers"
	"github.com/isola-ai/isola-sb/internal/sandbox-sidecar/proc"
)

const port = 10032

type config struct {
	logLevel string
	devMode  bool
}

func initChiServer(logger *slog.Logger, cfg config) *http.Server {
	r := chi.NewRouter()

	r.Use(httplog.RequestLogger(httplog.NewLogger("sandbox-sidecar", httplog.Options{
		LogLevel: slog.LevelInfo,
		JSON:     !cfg.devMode,
		Concise:  true,
	})))

	healthHandler := handlers.NewHealthHandler()
	filesystemHandler := handlers.NewFilesystemHandler(logger, &proc.RealProcFS{})

	r.Get("/health", healthHandler.GetHealth)
	r.Get("/healthz", healthHandler.GetHealth)
	r.Post("/filesystem", filesystemHandler.PostFilesystem)

	return &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: r,
	}
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

	srv := initChiServer(logger, cfg)

	// currently no graceful shutdown, but it might make sense to have a short grace period
	// to allow completing retrieval of sandbox app stdout for example (if in progress)
	logger.Info("starting sandbox-sidecar server", "port", port)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("server error", "error", err)
		os.Exit(1)
	}
}
