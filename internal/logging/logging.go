// Package logging provides shared slog configuration for all isola services.
package logging

import (
	"context"
	"log/slog"
	"os"
	"strings"
)

// Config holds logging configuration.
type Config struct {
	// Level is the minimum log level (debug, info, warn, error).
	Level string
	// DevMode enables human-readable text output instead of JSON.
	DevMode bool
}

// ctxKey is the context key for storing the logger.
type ctxKey struct{}

// New creates a new slog.Logger with the given configuration.
func New(cfg Config) *slog.Logger {
	level := parseLevel(cfg.Level)

	var handler slog.Handler
	opts := &slog.HandlerOptions{
		Level: level,
	}

	if cfg.DevMode {
		handler = slog.NewTextHandler(os.Stderr, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stderr, opts)
	}

	return slog.New(handler)
}

// NewContext returns a new context with the given logger.
func NewContext(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, ctxKey{}, logger)
}

// FromContext returns the logger from the context, or the default logger if none is set.
func FromContext(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(ctxKey{}).(*slog.Logger); ok {
		return logger
	}
	return slog.Default()
}

// With returns a logger with additional attributes.
func With(logger *slog.Logger, args ...any) *slog.Logger {
	return logger.With(args...)
}

// parseLevel converts a string level to slog.Level.
func parseLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
