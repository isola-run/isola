// Package main is the entry point for the isola-api service.
package main

//go:generate go tool oapi-codegen -config oapi-codegen.yaml ../../api/openapi.yaml

import (
	"context"
	"errors"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/isola-ai/isola-sb/internal/api/config"
	"github.com/isola-ai/isola-sb/internal/api/handlers"
	"github.com/isola-ai/isola-sb/internal/api/server"
	"github.com/isola-ai/isola-sb/internal/logging"
)

func main() {
	cfg := config.Config{}

	flag.StringVar(&cfg.HTTPAddr, "http-addr", getEnvOrDefault("ISOLA_HTTP_ADDR", ":8080"), "HTTP server listen address")
	flag.StringVar(&cfg.LogLevel, "log-level", getEnvOrDefault("ISOLA_LOG_LEVEL", "info"), "Log level (debug, info, warn, error)")
	flag.BoolVar(&cfg.DevMode, "dev", getEnvOrDefault("ISOLA_DEV_MODE", "") != "", "Enable development mode (text logging)")
	flag.Parse()

	logger := logging.New(logging.Config{
		Level:   cfg.LogLevel,
		DevMode: cfg.DevMode,
	})

	handler := handlers.NewHandler(logger)
	router := server.NewRouter(logger, handler)

	srv := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: router,
	}

	// Graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		logger.Info("starting isola-api server", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down server")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), config.ShutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("server shutdown error", "error", err)
	}

	logger.Info("server stopped")
}

func getEnvOrDefault(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}
