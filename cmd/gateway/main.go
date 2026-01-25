// Package main is the entry point for the isola-gw service.
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/isola-ai/isola-sb/internal/gateway/handlers"
	"github.com/isola-ai/isola-sb/internal/gateway/kubernetes"
	"github.com/isola-ai/isola-sb/internal/gateway/metrics"
	"github.com/isola-ai/isola-sb/internal/gateway/ratelimit"
	"github.com/isola-ai/isola-sb/internal/gateway/storage"
)

// Environment variable keys
const (
	EnvHTTPHost            = "ISOLA_HTTP_HOST"
	EnvHTTPPort            = "ISOLA_HTTP_PORT"
	EnvKubernetesNamespace = "ISOLA_KUBERNETES_NAMESPACE"
	EnvGinMode             = "GIN_MODE"
	EnvEnablePprof         = "ISOLA_ENABLE_PPROF"
	EnvPprofPort           = "ISOLA_PPROF_PORT"
	EnvRateLimitRPS        = "ISOLA_RATE_LIMIT_RPS"
	EnvRateLimitBurst      = "ISOLA_RATE_LIMIT_BURST"
)

// Default values
const (
	DefaultHTTPHost            = "0.0.0.0"
	DefaultHTTPPort            = "8080"
	DefaultKubernetesNamespace = "isola-sandboxes"
	DefaultPprofPort           = "6060"
	DefaultRateLimitRPS        = 50.0
	DefaultRateLimitBurst      = 100
)

func main() {
	if os.Getenv(EnvGinMode) == "" {
		gin.SetMode(gin.ReleaseMode)
	}

	host := getEnvOrDefault(EnvHTTPHost, DefaultHTTPHost)
	port := getEnvOrDefault(EnvHTTPPort, DefaultHTTPPort)
	namespace := getEnvOrDefault(EnvKubernetesNamespace, DefaultKubernetesNamespace)
	k8sManager := kubernetes.NewManager(namespace)

	ctx := context.Background()
	storageBucket := initStorage(ctx)

	// Start pprof server if enabled
	if os.Getenv(EnvEnablePprof) == "true" {
		pprofPort := getEnvOrDefault(EnvPprofPort, DefaultPprofPort)
		go func() {
			pprofAddr := fmt.Sprintf(":%s", pprofPort)
			log.Printf("pprof listening on %s", pprofAddr)
			if err := http.ListenAndServe(pprofAddr, nil); err != nil {
				log.Printf("pprof server error: %v", err)
			}
		}()
	}

	// Initialize rate limiter
	rateLimitConfig := ratelimit.DefaultConfig()
	if rps := getEnvFloat(EnvRateLimitRPS, DefaultRateLimitRPS); rps > 0 {
		rateLimitConfig.RequestsPerSecond = rps
	}
	if burst := getEnvInt(EnvRateLimitBurst, DefaultRateLimitBurst); burst > 0 {
		rateLimitConfig.BurstSize = burst
	}
	limiter := ratelimit.NewLimiter(rateLimitConfig)
	defer limiter.Stop()

	handler := handlers.NewHandler(k8sManager, storageBucket)

	r := gin.New()

	// Add middleware
	r.Use(gin.Logger())
	r.Use(gin.Recovery())
	r.Use(metrics.MetricsMiddleware())
	r.Use(ratelimit.Middleware(limiter))

	// Metrics endpoint (unauthenticated)
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

	handler.SetupRoutes(r)

	addr := fmt.Sprintf("%s:%s", host, port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in a goroutine
	go func() {
		log.Printf("Starting isola-gw server on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")

	// Graceful shutdown
	// TODO: __OMER__ Configure pod terminationGracePeriodSeconds in deployment.yaml to match this timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exited")
}

func initStorage(ctx context.Context) *storage.BucketWrapper {
	bucket, bucketName, err := storage.OpenBucket(ctx)
	if err != nil {
		log.Printf("Warning: Failed to initialize storage: %v. Large file uploads will not be available.", err)
		return nil
	}

	wrapper, err := storage.NewBucketWrapper(bucket, bucketName)
	if err != nil {
		log.Printf("Warning: Failed to create storage wrapper: %v. Large file uploads will not be available.", err)
		return nil
	}

	log.Printf("Storage initialized successfully with bucket: %s", bucketName)
	return wrapper
}

func getEnvOrDefault(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}

func getEnvFloat(key string, defaultValue float64) float64 {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	var result float64
	if _, err := fmt.Sscanf(value, "%f", &result); err != nil {
		return defaultValue
	}
	return result
}

func getEnvInt(key string, defaultValue int) int {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	var result int
	if _, err := fmt.Sscanf(value, "%d", &result); err != nil {
		return defaultValue
	}
	return result
}
