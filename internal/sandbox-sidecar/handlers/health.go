package handlers

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/isola-ai/isola-sb/internal/sandbox-sidecar/proc"
)

// Handler handles HTTP requests for the sandbox sidecar.
type Handler struct {
	logger *slog.Logger
	procFS proc.ProcFS
}

// NewHandler creates a new Handler with the given logger and proc filesystem.
func NewHandler(logger *slog.Logger, procFS proc.ProcFS) *Handler {
	return &Handler{
		logger: logger,
		procFS: procFS,
	}
}

// GetHealth godoc
// @Summary Health check
// @Description Returns the health status of the sidecar
// @Tags health
// @Produce json
// @Success 200 {object} HealthResponse
// @Router /health [get]
func (h *Handler) GetHealth(c *gin.Context) {
	c.JSON(http.StatusOK, HealthResponse{Status: "ok"})
}
