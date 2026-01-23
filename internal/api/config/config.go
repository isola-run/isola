// Package config provides configuration for the isola-api service.
package config

import "time"

// ShutdownTimeout is the maximum time to wait for server shutdown.
const ShutdownTimeout = 30 * time.Second

// Config holds the isola-api configuration.
type Config struct {
	// HTTPAddr is the address to listen for HTTP requests (e.g., ":8080").
	HTTPAddr string

	// MetricsAddr is the address for the metrics endpoint. Set to "0" to disable.
	MetricsAddr string

	// LogLevel sets the logging level (debug, info, warn, error).
	LogLevel string

	// DevMode enables development mode with console logging and gin debug mode.
	DevMode bool
}
