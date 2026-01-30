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

	"github.com/gin-contrib/requestid"
	"github.com/gin-gonic/gin"
	sloggin "github.com/samber/slog-gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	_ "github.com/isola-ai/isola-sb/api/openapi" // swagger docs
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
		logger.Error("sandbox namespace is required")
		return nil, errors.New("sandbox namespace is required")
	}

	// Create controller-runtime Manager with namespace-scoped cache
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:  scheme,
		Metrics: metricsserver.Options{BindAddress: "0"},
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

func initGinServer(logger *slog.Logger, cfg config, mgr ctrl.Manager) (*http.Server, error) {
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

	handler := handlers.NewHandler(logger, mgr.GetClient())

	v1 := r.Group("/api/v1")
	{
		v1.GET("/health", handler.GetHealth)
		v1.GET("/ready", handler.GetReady)
	}

	// Swagger docs
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.httpPort),
		Handler: r,
	}

	return srv, nil
}

// @title Isola Sandbox API
// @version 1.0
// @description API for managing sandboxes

// @BasePath /api/v1
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

	// Graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	mgr, err := initControllerRuntime(ctx, logger, cfg)
	if err != nil {
		logger.Error("unable to create controller-runtime manager", "error", err)
		os.Exit(1)
	}

	srv, err := initGinServer(logger, cfg, mgr)
	if err != nil {
		logger.Error("unable to create gin server", "error", err)
		os.Exit(1)
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
