// Package main is the entry point for the isola-agent service.
package main

import (
	"fmt"
	"log"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/omereli/dev-isola/services/isola-agent/internal/handlers"
)

// Environment variable keys
const (
	EnvHTTPHost = "HTTP_HOST"
	EnvHTTPPort = "HTTP_PORT"
)

// Default values
const (
	DefaultHTTPHost = "0.0.0.0"
	DefaultHTTPPort = "8080"
)

func main() {
	// Configure host and port from environment
	host := os.Getenv(EnvHTTPHost)
	if host == "" {
		host = DefaultHTTPHost
	}

	port := os.Getenv(EnvHTTPPort)
	if port == "" {
		port = DefaultHTTPPort
	}

	// Create handler
	handler, err := handlers.NewHandler()
	if err != nil {
		log.Fatalf("Failed to initialize handler: %v", err)
	}

	// Set Gin mode based on environment
	if os.Getenv("GIN_MODE") == "" {
		gin.SetMode(gin.ReleaseMode)
	}

	// Create Gin router
	r := gin.New()

	// Add middleware
	r.Use(gin.Recovery())
	r.Use(gin.Logger())

	// Register routes
	handler.RegisterRoutes(r)

	// Start server
	addr := fmt.Sprintf("%s:%s", host, port)
	log.Printf("Starting isola-agent server on %s", addr)

	if err := r.Run(addr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
