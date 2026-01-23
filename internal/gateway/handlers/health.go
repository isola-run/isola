// Package handlers provides HTTP handlers for the isola-gw API.
package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/isola-ai/isola-sb/internal/gateway/models"
)

func (h *Handler) HealthCheck(c *gin.Context) {
	healthStatus := models.HealthResponse{
		Status:    "healthy",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Components: map[string]string{
			"api": "healthy",
		},
		Version: "1.0.0",
	}

	c.JSON(http.StatusOK, healthStatus)
}

func (h *Handler) ReadinessCheck(c *gin.Context) {
	readyStatus := models.ReadyResponse{
		Status: "ready",
	}

	c.JSON(http.StatusOK, readyStatus)
}
