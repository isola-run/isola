package main

import (
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/gin-contrib/requestid"
	"github.com/gin-gonic/gin"
	sloggin "github.com/samber/slog-gin"

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

func initGinServer(logger *slog.Logger, cfg config) (*http.Server, error) {
	if !cfg.devMode {
		// default is debug mode
		// in tests we might want to pass env GIN_MODE=test
		// hence we only explicitly set it if devMode is false (otherwise use GIN_MODE or default to debug)
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()
	// no use of c.ClientIP() and thus no need to configure trusted proxies
	// misconfiguring trusted proxies is a no-no: https://gin-gonic.com/en/docs/deployment/
	if err := r.SetTrustedProxies(nil); err != nil {
		logger.Error("unable to set trusted proxies", "error", err)
		return nil, fmt.Errorf("set trusted proxies: %w", err)
	}
	// first middleware is sloggin to ensure logging is available for all other middlewares
	r.Use(sloggin.NewWithConfig(logger, sloggin.Config{
		WithRequestID: true,
	}))
	r.Use(requestid.New())
	r.Use(gin.Recovery())

	handler := handlers.NewHandler(logger, &proc.RealProcFS{})
	r.GET("/health", handler.GetHealth)
	r.GET("/healthz", handler.GetHealth)
	r.POST("/files/upload", handler.PostUpload)

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: r,
	}

	return srv, nil
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

	srv, err := initGinServer(logger, cfg)
	if err != nil {
		logger.Error("unable to create gin server", "error", err)
		os.Exit(1)
	}

	// currently no graceful shutdown, but it might make sense to have a short grace period
	// to allow completing retrieval of sandbox app stdout for example (if in progress)
	logger.Info("starting sandbox-sidecar server", "port", port)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("server error", "error", err)
		os.Exit(1)
	}
}
