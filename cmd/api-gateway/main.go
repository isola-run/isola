// Copyright The Isola Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

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
	"k8s.io/client-go/discovery"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	sandboxv1alpha1 "github.com/isola-run/isola/api/v1alpha1"
	"github.com/isola-run/isola/internal/api-gateway/command"
	"github.com/isola-run/isola/internal/api-gateway/filesystem"
	"github.com/isola-run/isola/internal/api-gateway/health"
	"github.com/isola-run/isola/internal/api-gateway/metrics"
	"github.com/isola-run/isola/internal/api-gateway/rootfssnapshot"
	"github.com/isola-run/isola/internal/api-gateway/sandbox"
	"github.com/isola-run/isola/internal/api-gateway/version"
	"github.com/isola-run/isola/internal/env"
	"github.com/isola-run/isola/internal/logging"
	internalversion "github.com/isola-run/isola/internal/version"
)

const (
	shutdownGracePeriod        = 25 * time.Second // < default k8s terminationGracePeriodSeconds (30 seconds)
	managerShutdownGracePeriod = 20 * time.Second // < shutdownGracePeriod

	serverReadHeaderTimeout = 10 * time.Second
	serverReadTimeout       = 30 * time.Second
	serverIdleTimeout       = 120 * time.Second
	// have the api-gateway writeTimeout > maximal allowed api-gateway long poll interval (waitSeconds) == 25 seconds.
	// have the api-gateway writeTimeout < upstream server timeouts:
	// kube-apiserver - 60 seconds by default (but we limit to k8sClientTimeout below),
	// sandbox-sidecar - 75 seconds writeTimeout.
	serverWriteTimeout = 45 * time.Second
	k8sClientTimeout   = 30 * time.Second
)

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

	restConfig := ctrl.GetConfigOrDie()
	restConfig.Timeout = k8sClientTimeout

	mgr, err := ctrl.NewManager(restConfig, ctrl.Options{
		Scheme:                  scheme,
		Logger:                  logr.FromSlogHandler(logger.Handler()),
		Metrics:                 metricsserver.Options{BindAddress: "0"},
		GracefulShutdownTimeout: ptr.To(managerShutdownGracePeriod),
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

func initSandboxClient() *http.Client {
	// currently no Timeout set as it's hard to expect the size of the files
	// for different usecases, and no demand for making it configurable atm
	return &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse // sidecar should never redirect
		},
	}
}

func main() {
	cfg := config{}

	flag.IntVar(&cfg.httpPort, "http-port", env.GetOrDefaultInt("ISOLA_HTTP_PORT", 8080), "HTTP server port")
	flag.StringVar(&cfg.logLevel, "log-level", env.GetOrDefault("ISOLA_LOG_LEVEL", "info"), "Log level (debug, info, warn, error)")
	flag.BoolVar(&cfg.devMode, "dev-mode", env.GetOrDefault("ISOLA_DEV_MODE", "") != "", "Enable development mode (text logging)")
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

	// Non-fatal: cache sync already proved the apiserver is reachable.
	if dc, err := discovery.NewDiscoveryClientForConfig(rest.CopyConfig(mgr.GetConfig())); err != nil {
		logger.Warn("unable to build discovery client for kubernetes version log", "error", err)
	} else if info, err := dc.ServerVersion(); err != nil {
		logger.Warn("unable to fetch kubernetes server version", "error", err)
	} else {
		logger.Info("connected to kubernetes", "gitVersion", info.GitVersion, "platform", info.Platform)
	}

	r := chi.NewRouter()
	// Install metrics first so the histogram observes the total server-side latency,
	// including httplog and the chi.Recoverer (installed by httplog) layers beneath it.
	r.Use(metrics.Middleware)
	// httplog.RequestLogger automatically includes chi's RequestID and Recoverer middleware
	r.Use(httplog.RequestLogger(&httplog.Logger{
		Logger: logger,
		Options: httplog.Options{
			LogLevel: slog.LevelInfo,
			JSON:     !cfg.devMode,
			Concise:  true,
		},
	}))
	r.Handle("/metrics", metrics.Handler())

	logger.Info("starting api-gateway", "version", internalversion.Get())

	humaConfig := huma.DefaultConfig("Isola Sandbox API", internalversion.Get().GitVersion)
	humaConfig.Info.Description = "API for managing sandboxes"
	api := humachi.New(r, humaConfig)

	health.Register(api, health.New(logger, mgr.GetClient()))
	version.Register(api, version.New())

	v1 := huma.NewGroup(api, "/v1")
	sandbox.Register(v1, sandbox.New(logger, cfg.sandboxNamespace, mgr.GetClient()))
	rootfssnapshot.Register(v1, rootfssnapshot.New(logger, cfg.sandboxNamespace, mgr.GetClient()))

	sandboxClient := initSandboxClient()

	filesystem.Register(v1, filesystem.New(logger, cfg.sandboxNamespace, mgr.GetClient(), sandboxClient))
	command.Register(v1, command.New(logger, cfg.sandboxNamespace, mgr.GetClient(), sandboxClient))

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.httpPort),
		Handler:           r,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
		ReadHeaderTimeout: serverReadHeaderTimeout,
		ReadTimeout:       serverReadTimeout,
		WriteTimeout:      serverWriteTimeout,
		IdleTimeout:       serverIdleTimeout,
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

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGracePeriod)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("server shutdown error", "error", err)
	}

	logger.Info("server stopped")
}
