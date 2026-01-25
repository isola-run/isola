// Package handlers provides HTTP request handlers for the isola-agent API.
package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type Handler struct{}

func NewHandler() (*Handler, error) {
	return &Handler{}, nil
}

type HealthResponse struct {
	Status string `json:"status"`
}

// Health handles GET /health requests.
func (h *Handler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, HealthResponse{Status: "healthy"})
}

// RegisterRoutes registers all HTTP routes on the given Gin engine.
func (h *Handler) RegisterRoutes(r *gin.Engine) {
	r.GET("/health", h.Health)
}
