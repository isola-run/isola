package handlers

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	logger *slog.Logger
}

func NewHandler(logger *slog.Logger) *Handler {
	return &Handler{
		logger: logger,
	}
}

type HealthResponse struct {
	Status string `json:"status" example:"ok"`
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
