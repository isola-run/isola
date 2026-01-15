// Package main is the entry point for the isola-agent service.
package main

import (
	"fmt"
	"log"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/isola-ai/isola-sb/services/isola-agent/internal/handlers"
)

// Environment variable keys
const (
	EnvHTTPHost = "ISOLA_HTTP_HOST"
	EnvHTTPPort = "ISOLA_HTTP_PORT"
)

// Default values
const (
	DefaultHTTPHost = "0.0.0.0"
	DefaultHTTPPort = "8080"
)

func main() {
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

	r := gin.New()

	// Add middleware
	r.Use(gin.Recovery())
	r.Use(gin.Logger())
	handler.RegisterRoutes(r)

	addr := fmt.Sprintf("%s:%s", host, port)
	log.Printf("Starting isola-agent server on %s", addr)

	if err := r.Run(addr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
