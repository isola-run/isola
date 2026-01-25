// Package server provides the HTTP server setup for isola-api.
package server

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/isola-ai/isola-sb/internal/api/generated"
	"github.com/isola-ai/isola-sb/internal/api/middleware"
)

// NewRouter creates and configures the Chi router with all middleware and handlers.
func NewRouter(logger *slog.Logger, handler generated.ServerInterface) http.Handler {
	r := chi.NewRouter()

	// Register middleware in order: request ID first so it's available to others
	r.Use(middleware.RequestID)
	r.Use(middleware.Recovery(logger))
	r.Use(middleware.Logging(logger))

	// Register the generated OpenAPI handlers under /api/v1
	return generated.HandlerWithOptions(handler, generated.ChiServerOptions{
		BaseURL:    "/api/v1",
		BaseRouter: r,
	})
}
