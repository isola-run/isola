// Package config provides configuration for the isola-api service.
package config

import "time"

// ShutdownTimeout is the maximum time to wait for server shutdown.
const ShutdownTimeout = 30 * time.Second

// Config holds the isola-api configuration.
type Config struct {
	// HTTPAddr is the address to listen for HTTP requests (e.g., ":8080").
	HTTPAddr string

	// LogLevel sets the logging level (debug, info, warn, error).
	LogLevel string

	// DevMode enables development mode with text logging output.
	DevMode bool
}
