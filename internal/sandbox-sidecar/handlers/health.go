package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type HealthHandler struct{}

func NewHealthHandler() *HealthHandler {
	return &HealthHandler{}
}

// GetHealth godoc
// @Summary Health check
// @Description Returns the health status of the sidecar
// @Tags health
// @Produce json
// @Success 200 {object} HealthResponse
// @Router /health [get]
func (h *HealthHandler) GetHealth(c *gin.Context) {
	c.JSON(http.StatusOK, HealthResponse{Status: "ok"})
}
