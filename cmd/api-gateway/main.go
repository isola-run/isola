package main

//go:generate go tool swag init -g main.go -o ../../docs --parseDependency --parseInternal

// @title           Isola API
// @version         0.1.0
// @description     API for managing sandboxed execution environments
// @host            localhost:8080
// @BasePath        /api/v1

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/gin-contrib/requestid"
	sloggin "github.com/gin-contrib/slog"
	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	sandboxv1alpha1 "github.com/isola-ai/isola-sb/api/v1alpha1"
	_ "github.com/isola-ai/isola-sb/docs"
	"github.com/isola-ai/isola-sb/internal/api-gateway/handlers"
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

func main() {
	cfg := config{}

	flag.IntVar(&cfg.httpPort, "http-port", getEnvOrDefaultInt("ISOLA_HTTP_PORT", 8080), "HTTP server port")
	flag.StringVar(&cfg.logLevel, "log-level", getEnvOrDefault("ISOLA_LOG_LEVEL", "info"), "Log level (debug, info, warn, error)")
	flag.BoolVar(&cfg.devMode, "dev", getEnvOrDefault("ISOLA_DEV_MODE", "") != "", "Enable development mode (text logging)")
	flag.StringVar(&cfg.sandboxNamespace, "sandbox-namespace", os.Getenv("ISOLA_SANDBOX_NAMESPACE"), "Namespace where sandboxes are created (required)")
	flag.Parse()

	if cfg.sandboxNamespace == "" {
		fmt.Fprintln(os.Stderr, "ISOLA_SANDBOX_NAMESPACE is required")
		os.Exit(1)
	}

	logger := logging.New(logging.Config{
		Level:   cfg.logLevel,
		DevMode: cfg.devMode,
	})

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
		os.Exit(1)
	}

	// Graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		logger.Info("starting controller-runtime manager")
		if err := mgr.Start(ctx); err != nil {
			logger.Error("manager error", "error", err)
		}
	}()

	if !mgr.GetCache().WaitForCacheSync(ctx) {
		logger.Error("cache sync failed")
		os.Exit(1)
	}
	logger.Info("cache synced")

	if !cfg.devMode {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(requestid.New())
	r.Use(sloggin.SetLogger(sloggin.WithLogger(func(_ *gin.Context, l *slog.Logger) *slog.Logger {
		return logger
	})))

	handler := handlers.NewHandler(logger, mgr.GetClient())

	v1 := r.Group("/api/v1")
	{
		v1.GET("/health", handler.GetHealth)
		v1.GET("/ready", handler.GetReady)
	}

	r.GET("/docs/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

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

func getEnvOrDefault(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}

func getEnvOrDefaultInt(key string, defaultValue int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return defaultValue
}
