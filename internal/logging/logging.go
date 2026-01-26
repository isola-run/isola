package logging

import (
	"log/slog"
	"os"
	"strings"
)

type Config struct {
	// Level is the minimum log level (debug, info, warn, error).
	Level string
	// DevMode enables human-readable text output instead of JSON.
	DevMode bool
}

// New creates a new slog.Logger with the given configuration.
func New(cfg Config) *slog.Logger {
	level := parseLevel(cfg.Level)

	opts := &slog.HandlerOptions{Level: level}

	var handler slog.Handler
	if cfg.DevMode {
		handler = slog.NewTextHandler(os.Stderr, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stderr, opts)
	}

	return slog.New(handler)
}

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
