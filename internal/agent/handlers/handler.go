// Package handlers provides HTTP request handlers for the isola-agent API.
package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Handler contains dependencies for all HTTP handlers.
type Handler struct {
	procFS         *ProcFS
	processManager *ProcessManager
}

// NewHandler creates a new Handler instance.
func NewHandler() (*Handler, error) {
	return &Handler{
		procFS:         NewProcFS(),
		processManager: NewProcessManager(),
	}, nil
}

// RegisterRoutes registers all HTTP routes on the given Gin engine.
func (h *Handler) RegisterRoutes(r *gin.Engine) {
	// Health
	r.GET("/health", h.Health)

	// Command execution
	r.POST("/exec", h.Exec)
	r.GET("/exec/:id", h.GetExec)
	r.POST("/exec/:id/kill", h.KillExec)

	// File operations
	r.POST("/files/write", h.WriteFile)
	r.GET("/files/read", h.ReadFile)
	r.GET("/files/stat", h.StatFile)
	r.GET("/files/list", h.ListDir)
	r.DELETE("/files", h.DeleteFile)
	r.POST("/files/upload", h.UploadFile)
}

// Common response types

// ErrorResponse represents an error response.
type ErrorResponse struct {
	Error string `json:"error"`
}

// HealthResponse represents a health check response.
type HealthResponse struct {
	Status string `json:"status"`
}

// Health handles GET /health requests.
func (h *Handler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, HealthResponse{Status: "healthy"})
}
