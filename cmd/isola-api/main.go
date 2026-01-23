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

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/isola-ai/isola-sb/internal/api/config"
	"github.com/isola-ai/isola-sb/internal/api/handlers"
	"github.com/isola-ai/isola-sb/internal/api/server"
)

func main() {
	cfg := config.Config{}

	flag.StringVar(&cfg.HTTPAddr, "http-addr", getEnvOrDefault("ISOLA_HTTP_ADDR", ":8080"), "HTTP server listen address")
	flag.StringVar(&cfg.MetricsAddr, "metrics-addr", getEnvOrDefault("ISOLA_METRICS_ADDR", "0"), "Metrics server address (0 to disable)")
	flag.StringVar(&cfg.LogLevel, "log-level", getEnvOrDefault("ISOLA_LOG_LEVEL", "info"), "Log level (debug, info, warn, error)")
	flag.BoolVar(&cfg.DevMode, "dev", getEnvOrDefault("ISOLA_DEV_MODE", "") != "", "Enable development mode (console logging, gin debug)")
	flag.Parse()

	logger := initLogger(cfg)
	defer func() { _ = logger.Sync() }()

	if cfg.DevMode {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	handler := handlers.NewHandler(logger)
	router := server.NewRouter(cfg, logger, handler)

	srv := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: router,
	}

	// Graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		logger.Info("starting isola-api server", zap.String("addr", cfg.HTTPAddr))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Fatal("server error", zap.Error(err))
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down server")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), config.ShutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("server shutdown error", zap.Error(err))
	}

	logger.Info("server stopped")
}

func initLogger(cfg config.Config) *zap.Logger {
	var zapCfg zap.Config
	if cfg.DevMode {
		zapCfg = zap.NewDevelopmentConfig()
		zapCfg.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	} else {
		zapCfg = zap.NewProductionConfig()
	}

	level, err := zapcore.ParseLevel(cfg.LogLevel)
	if err != nil {
		level = zapcore.InfoLevel
	}
	zapCfg.Level = zap.NewAtomicLevelAt(level)

	logger, err := zapCfg.Build()
	if err != nil {
		panic(err)
	}
	return logger
}

func getEnvOrDefault(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}
