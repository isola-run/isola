// Package main is the entry point for the isola-agent service.
package main

import (
	"fmt"
	"log"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/isola-ai/isola-sb/internal/agent/handlers"
)

const (
	defaultHost = "0.0.0.0"
	defaultPort = "8080"
)

func main() {
	host := getEnv("ISOLA_HTTP_HOST", defaultHost)
	port := getEnv("ISOLA_HTTP_PORT", defaultPort)

	handler, err := handlers.NewHandler()
	if err != nil {
		log.Fatalf("Failed to initialize handler: %v", err)
	}

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(gin.Logger())
	handler.RegisterRoutes(r)

	addr := fmt.Sprintf("%s:%s", host, port)
	log.Printf("Starting isola-agent on %s", addr)

	if err := r.Run(addr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
