// Package main is the entry point for the isola-agent service.
package main

import (
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/omereli/dev-isola/services/isola-agent/internal/handlers"
	agenttls "github.com/omereli/dev-isola/services/isola-agent/internal/tls"
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
	DefaultTLSPort  = "8443"
)

func main() {
	host := os.Getenv(EnvHTTPHost)
	if host == "" {
		host = DefaultHTTPHost
	}

	// Create handler
	handler, err := handlers.NewHandler()
	if err != nil {
		log.Fatalf("Failed to initialize handler: %v", err)
	}

	// Set Gin mode based on environment
	if os.Getenv("ISOLA_GIN_MODE") == "" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()

	// Add middleware
	r.Use(gin.Recovery())
	r.Use(gin.Logger())
	handler.RegisterRoutes(r)

	// Check if TLS is configured via environment variables
	cert, tlsEnabled, err := agenttls.LoadCertFromEnv()
	if err != nil {
		log.Fatalf("Failed to load TLS certificate: %v", err)
	}

	if tlsEnabled {
		// TLS mode - use port 8443
		port := os.Getenv(EnvHTTPPort)
		if port == "" {
			port = DefaultTLSPort
		}
		addr := fmt.Sprintf("%s:%s", host, port)
		log.Printf("Starting isola-agent TLS server on %s", addr)

		server := &http.Server{
			Addr:    addr,
			Handler: r,
			TLSConfig: &tls.Config{
				Certificates: []tls.Certificate{cert},
				MinVersion:   tls.VersionTLS13,
			},
		}

		if err := server.ListenAndServeTLS("", ""); err != nil {
			log.Fatalf("Failed to start TLS server: %v", err)
		}
	} else {
		// HTTP mode (fallback for backward compatibility during transition)
		port := os.Getenv(EnvHTTPPort)
		if port == "" {
			port = DefaultHTTPPort
		}
		addr := fmt.Sprintf("%s:%s", host, port)
		log.Printf("Starting isola-agent HTTP server on %s (TLS not configured)", addr)

		if err := r.Run(addr); err != nil {
			log.Fatalf("Failed to start server: %v", err)
		}
	}
}
