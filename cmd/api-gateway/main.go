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
	"github.com/go-logr/logr"
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

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(sandboxv1alpha1.AddToScheme(scheme))
}

type config struct {
	httpPort         int
	logLevel         string
	devMode          bool
	sandboxNamespace string
}

func initControllerRuntime(ctx context.Context, logger *slog.Logger, cfg config) (ctrl.Manager, error) {
	if cfg.sandboxNamespace == "" {
		return nil, errors.New("sandbox namespace is required")
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:  scheme,
		Metrics: metricsserver.Options{BindAddress: "0"},
		Cache: cache.Options{
			DefaultNamespaces: map[string]cache.Config{
				cfg.sandboxNamespace: {},
			},
			ByObject: map[client.Object]cache.ByObject{
				&sandboxv1alpha1.Sandbox{}:        {},
				&sandboxv1alpha1.RootfsSnapshot{}: {},
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("create manager: %w", err)
	}

	go func() {
		logger.Info("starting controller-runtime manager")
		if err := mgr.Start(ctx); err != nil {
			logger.Error("manager error", "error", err)
		}
	}()

	if !mgr.GetCache().WaitForCacheSync(ctx) {
		return nil, errors.New("cache sync failed")
	}
	logger.Info("cache synced")

	return mgr, nil
}

func main() {
	cfg := config{}

	flag.IntVar(&cfg.httpPort, "http-port", env.GetOrDefaultInt("ISOLA_HTTP_PORT", 8080), "HTTP server port")
	flag.StringVar(&cfg.logLevel, "log-level", env.GetOrDefault("ISOLA_LOG_LEVEL", "info"), "Log level (debug, info, warn, error)")
	flag.BoolVar(&cfg.devMode, "dev", env.GetOrDefault("ISOLA_DEV_MODE", "") != "", "Enable development mode (text logging)")
	flag.StringVar(&cfg.sandboxNamespace, "sandbox-namespace", os.Getenv("ISOLA_SANDBOX_NAMESPACE"), "Namespace where sandboxes are created (required)")
	flag.Parse()

	logger := logging.New(logging.Config{
		Level:   cfg.logLevel,
		DevMode: cfg.devMode,
	})
	ctrl.SetLogger(logr.FromSlogHandler(logger.Handler()))

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
	execHandlers := handlers.NewExecHandlers(logger, mgr.GetClient(), cfg.sandboxNamespace)

	handlers.RegisterHealthRoutes(api, healthHandlers)
	handlers.RegisterExecRoutes(api, execHandlers)

	sandboxHandlers := handlers.NewSandboxHandlers(logger, cfg.sandboxNamespace, mgr.GetClient())
	handlers.RegisterSandboxRoutes(api, sandboxHandlers)

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
