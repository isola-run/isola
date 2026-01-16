// Package main is the entry point for the isola-gw service.
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/isola-ai/isola-sb/services/isola-gw/internal/handlers"
	"github.com/isola-ai/isola-sb/services/isola-gw/internal/kubernetes"
	"github.com/isola-ai/isola-sb/services/isola-gw/internal/storage"
)

// Environment variable keys
const (
	EnvHTTPHost            = "ISOLA_HTTP_HOST"
	EnvHTTPPort            = "ISOLA_HTTP_PORT"
	EnvKubernetesNamespace = "ISOLA_KUBERNETES_NAMESPACE"
	EnvGinMode             = "GIN_MODE"
)

// Default values
const (
	DefaultHTTPHost            = "0.0.0.0"
	DefaultHTTPPort            = "8080"
	DefaultKubernetesNamespace = "isola-sandboxes"
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

	handler := handlers.NewHandler(k8sManager, storageBucket)

	r := gin.New()

	// Add middleware
	r.Use(gin.Logger())
	r.Use(gin.Recovery())

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
