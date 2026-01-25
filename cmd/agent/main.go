// Package main is the entry point for the isola-agent service.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"

	"github.com/isola-ai/isola-sb/internal/agent/handlers"
	"github.com/isola-ai/isola-sb/internal/logging"
)

// Environment variable keys
const (
	EnvHTTPHost = "ISOLA_HTTP_HOST"
	EnvHTTPPort = "ISOLA_HTTP_PORT"
	EnvLogLevel = "ISOLA_LOG_LEVEL"
	EnvDevMode  = "ISOLA_DEV_MODE"
)

// Default values
const (
	DefaultHTTPHost = "0.0.0.0"
	DefaultHTTPPort = "8080"
)

func main() {
	host := getEnv(EnvHTTPHost, DefaultHTTPHost)
	port := getEnv(EnvHTTPPort, DefaultHTTPPort)
	logLevel := getEnv(EnvLogLevel, "info")
	devMode := os.Getenv(EnvDevMode) != ""

	logger := logging.New(logging.Config{
		Level:   logLevel,
		DevMode: devMode,
	})

	handler, err := handlers.NewHandler(logger)
	if err != nil {
		logger.Error("failed to initialize handler", "error", err)
		os.Exit(1)
	}

	r := chi.NewRouter()
	r.Use(chimiddleware.Recoverer)
	r.Use(chimiddleware.Logger)
	handler.RegisterRoutes(r)

	addr := fmt.Sprintf("%s:%s", host, port)
	srv := &http.Server{
		Addr:    addr,
		Handler: r,
	}

	// Graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		logger.Info("starting isola-agent server", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down server")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("server shutdown error", "error", err)
	}

	logger.Info("server stopped")
}

func getEnv(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}
