package main

import (
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/httplog/v2"

	"github.com/isola-ai/isola-sb/internal/constants"
	"github.com/isola-ai/isola-sb/internal/env"
	"github.com/isola-ai/isola-sb/internal/logging"
	"github.com/isola-ai/isola-sb/internal/sandbox-sidecar/handlers"
	"github.com/isola-ai/isola-sb/internal/sandbox-sidecar/proc"
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

	humaConfig := huma.DefaultConfig("Isola Sandbox Sidecar API", "1.0.0")
	humaConfig.Info.Description = "Internal API for sandbox filesystem operations"
	api := humachi.New(r, humaConfig)

	procFS := &proc.RealProcFS{}
	pidResolver := handlers.NewPIDResolver(procFS)

	healthHandlers := handlers.NewHealthHandlers()
	filesystemHandlers := handlers.NewFilesystemHandlers(logger, procFS, pidResolver)
	commandHandlers := handlers.NewCommandHandlers(logger, procFS, pidResolver, &handlers.NsenterCommandBuilder{})

	handlers.RegisterHealthRoutes(api, healthHandlers)
	handlers.RegisterFilesystemRoutes(api, filesystemHandlers)
	handlers.RegisterCommandRoutes(api, commandHandlers)

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", constants.SidecarPort),
		Handler: r,
	}

	// currently no graceful shutdown, but it might make sense to have a short grace period
	// to allow completing retrieval of sandbox app stdout for example (if in progress)
	logger.Info("starting sandbox-sidecar server", "port", constants.SidecarPort)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("server error", "error", err)
		os.Exit(1)
	}
}
