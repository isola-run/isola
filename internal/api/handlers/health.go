// Package handlers implements the API handlers for isola-api.
package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/isola-ai/isola-sb/internal/api/generated"
)

// Handler implements the generated ServerInterface.
type Handler struct {
	logger *zap.Logger
}

// NewHandler creates a new Handler instance.
func NewHandler(logger *zap.Logger) *Handler {
	return &Handler{logger: logger}
}

// GetHealth implements the health check endpoint.
func (h *Handler) GetHealth(c *gin.Context) {
	c.JSON(http.StatusOK, generated.HealthResponse{
		Status: "ok",
	})
}

// GetReady implements the readiness check endpoint.
func (h *Handler) GetReady(c *gin.Context) {
	// TODO: Add actual readiness checks (e.g., K8s client connectivity)
	c.JSON(http.StatusOK, generated.HealthResponse{
		Status: "ok",
	})
}

// Ensure Handler implements ServerInterface at compile time.
var _ generated.ServerInterface = (*Handler)(nil)
