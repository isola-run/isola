// Package server provides the HTTP server setup for isola-api.
package server

import (
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/isola-ai/isola-sb/internal/api/config"
	"github.com/isola-ai/isola-sb/internal/api/generated"
	"github.com/isola-ai/isola-sb/internal/api/middleware"
)

// NewRouter creates and configures the Gin router with all middleware and handlers.
func NewRouter(cfg config.Config, logger *zap.Logger, handler generated.ServerInterface) *gin.Engine {
	r := gin.New()

	// Register middleware in order: request ID first so it's available to others
	r.Use(middleware.RequestID())
	r.Use(middleware.Recovery(logger))
	r.Use(middleware.Logging(logger))

	// Register the generated OpenAPI handlers under /api/v1
	generated.RegisterHandlersWithOptions(r, handler, generated.GinServerOptions{
		BaseURL: "/api/v1",
	})

	return r
}
