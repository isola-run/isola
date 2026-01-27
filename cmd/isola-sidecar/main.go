package main

import (
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/httplog/v2"

	"github.com/isola-ai/isola-sb/internal/logging"
	"github.com/isola-ai/isola-sb/internal/sidecar/handlers"
)

const port = 10032

type config struct {
	logLevel string
	devMode  bool
}

func main() {
	cfg := config{}

	flag.StringVar(&cfg.logLevel, "log-level", getEnvOrDefault("ISOLA_LOG_LEVEL", "info"), "Log level (debug, info, warn, error)")
	flag.BoolVar(&cfg.devMode, "dev", getEnvOrDefault("ISOLA_DEV_MODE", "") != "", "Enable development mode (text logging)")
	flag.Parse()

	logger := logging.New(logging.Config{
		Level:   cfg.logLevel,
		DevMode: cfg.devMode,
	})

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(httplog.RequestLogger(httplog.NewLogger("isola-sidecar", httplog.Options{
		LogLevel: slog.LevelInfo,
		JSON:     !cfg.devMode,
	})))

	handler := handlers.NewHandler(logger)
	handler.RegisterRoutes(r)

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: r,
	}
	// currently no graceful shutdown, but it might make sense to have a short grace period
	// to allow completing retrieval of sandbox app stdout for example (if in progress)
	logger.Info("starting isola-sidecar server", "port", port)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("server error", "error", err)
		os.Exit(1)
	}
}

func getEnvOrDefault(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}
