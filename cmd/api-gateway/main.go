package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/httplog/v2"
	gonanoid "github.com/matoous/go-nanoid/v2"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	sandboxv1alpha1 "github.com/isola-ai/isola-sb/api/v1alpha1"
	"github.com/isola-ai/isola-sb/internal/api-gateway/handlers"
	"github.com/isola-ai/isola-sb/internal/env"
	"github.com/isola-ai/isola-sb/internal/logging"
)

const shutdownTimeout = 30 * time.Second

const (
	sandboxNameLength = 22
	letterAlphabet    = "abcdefghijklmnopqrstuvwxyz"
	fullAlphabet      = "abcdefghijklmnopqrstuvwxyz0123456789"
)

// GenerateSandboxName creates a unique sandbox name suitable for Kubernetes DNS-1123 labels.
func GenerateSandboxName() (string, error) {
	first, err := gonanoid.Generate(letterAlphabet, 1)
	if err != nil {
		return "", fmt.Errorf("generate first char: %w", err)
	}

	rest, err := gonanoid.Generate(fullAlphabet, sandboxNameLength-1)
	if err != nil {
		return "", fmt.Errorf("generate remaining chars: %w", err)
	}

	return first + rest, nil
}

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(sandboxv1alpha1.AddToScheme(scheme))
}

type config struct {
	httpPort         int
	metricsPort      int
	logLevel         string
	devMode          bool
	sandboxNamespace string
}

func initControllerRuntime(ctx context.Context, logger *slog.Logger, cfg config) (ctrl.Manager, error) {
	if cfg.sandboxNamespace == "" {
		logger.Error("sandbox namespace is required")
		return nil, errors.New("sandbox namespace is required")
	}

	metricsBindAddress := "0"
	if cfg.metricsPort > 0 {
		metricsBindAddress = fmt.Sprintf(":%d", cfg.metricsPort)
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:  scheme,
		Metrics: metricsserver.Options{BindAddress: metricsBindAddress},
		Cache: cache.Options{
			DefaultNamespaces: map[string]cache.Config{
				cfg.sandboxNamespace: {},
			},
			ByObject: map[client.Object]cache.ByObject{
				&sandboxv1alpha1.Sandbox{}:         {},
				&sandboxv1alpha1.SandboxTemplate{}: {},
				&sandboxv1alpha1.RootfsSnapshot{}:  {},
			},
		},
	})
	if err != nil {
		logger.Error("unable to create manager", "error", err)
		return nil, err
	}

	go func() {
		logger.Info("starting controller-runtime manager")
		if err := mgr.Start(ctx); err != nil {
			logger.Error("manager error", "error", err)
		}
	}()

	if !mgr.GetCache().WaitForCacheSync(ctx) {
		logger.Error("cache sync failed")
		return nil, errors.New("cache sync failed")
	}
	logger.Info("cache synced")

	return mgr, nil
}

func main() {
	cfg := config{}

	flag.IntVar(&cfg.httpPort, "http-port", env.GetOrDefaultInt("ISOLA_HTTP_PORT", 8080), "HTTP server port")
	flag.IntVar(&cfg.metricsPort, "metrics-port", env.GetOrDefaultInt("ISOLA_METRICS_PORT", 0), "Metrics server port (0 to disable)")
	flag.StringVar(&cfg.logLevel, "log-level", env.GetOrDefault("ISOLA_LOG_LEVEL", "info"), "Log level (debug, info, warn, error)")
	flag.BoolVar(&cfg.devMode, "dev", env.GetOrDefault("ISOLA_DEV_MODE", "") != "", "Enable development mode (text logging)")
	flag.StringVar(&cfg.sandboxNamespace, "sandbox-namespace", os.Getenv("ISOLA_SANDBOX_NAMESPACE"), "Namespace where sandboxes are created (required)")
	flag.Parse()

	logger := logging.New(logging.Config{
		Level:   cfg.logLevel,
		DevMode: cfg.devMode,
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	mgr, err := initControllerRuntime(ctx, logger, cfg)
	if err != nil {
		logger.Error("unable to create controller-runtime manager", "error", err)
		os.Exit(1)
	}

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

	humaConfig := huma.DefaultConfig("Isola Sandbox API", "1.0.0")
	humaConfig.Info.Description = "API for managing sandboxes"
	api := humachi.New(r, humaConfig)

	healthHandlers := handlers.NewHealthHandlers(logger, mgr.GetClient())
	handlers.RegisterHealthRoutes(api, healthHandlers)

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.httpPort),
		Handler: r,
	}

	go func() {
		logger.Info("starting api-gateway server", "port", cfg.httpPort)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down server")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("server shutdown error", "error", err)
	}

	logger.Info("server stopped")
}
