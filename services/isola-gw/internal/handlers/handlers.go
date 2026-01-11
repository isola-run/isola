// Package handlers provides HTTP handlers for the isola-gw API.
package handlers

import (
	"github.com/gin-gonic/gin"
	"github.com/omereli/dev-isola/services/isola-gw/internal/kubernetes"
	"github.com/omereli/dev-isola/services/isola-gw/internal/storage"
)

const (
	agentSidecarPort = 8080
)

type Handler struct {
	k8sManager *kubernetes.Manager
	storage    *storage.BucketWrapper
}

func NewHandler(k8sManager *kubernetes.Manager, storageBucket *storage.BucketWrapper) *Handler {
	return &Handler{
		k8sManager: k8sManager,
		storage:    storageBucket,
	}
}

func (h *Handler) SetupRoutes(r *gin.Engine) {
	// System endpoints
	r.GET("/health", h.HealthCheck)
	r.GET("/ready", h.ReadinessCheck)

	// API routes with authentication
	api := r.Group("/api/v1")
	api.Use(h.APIKeyAuth())
	{
		// Sandbox CRUD
		api.GET("/sandboxes", h.ListSandboxes)
		api.POST("/sandboxes", h.CreateSandbox)
		api.GET("/sandboxes/:id", h.GetSandbox)
		api.DELETE("/sandboxes/:id", h.TerminateSandbox)

		// Command execution
		api.POST("/sandboxes/:id/execute", h.ExecuteCommand)

		// File operations
		api.GET("/sandboxes/:id/files", h.DownloadFile)
		api.POST("/sandboxes/:id/files", h.UploadFile)
		api.POST("/sandboxes/:id/files/upload-url", h.GenerateUploadUrl)
		api.POST("/sandboxes/:id/files/confirm", h.ConfirmUpload)
		api.POST("/sandboxes/:id/files/download-url", h.GenerateDownloadUrl)
	}
}

func (h *Handler) APIKeyAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		apiKey := c.GetHeader("X-API-Key")
		if apiKey == "" {
			c.JSON(401, gin.H{
				"error":   "Unauthorized",
				"message": "Missing API key",
			})
			c.Abort()
			return
		}

		// Extract tenant ID from API key (simple demo implementation)
		tenantID := tenantFromAPIKey(apiKey)
		c.Set("tenant_id", tenantID)
		c.Next()
	}
}

// TODO: benl __OMER__ change this
func tenantFromAPIKey(apiKey string) string {
	if apiKey == "iso_sk_a1b2c3d4e5f67890a1b2c3d4e5f67890" {
		return "2280e575-f37d-4329-b033-9de263ce7625"
	}
	if apiKey == "iso_sk_demo" {
		return "e766a1e8-4b0e-4bb7-9612-80b9c1c8cd87"
	}
	return "e766a1e8-4b0e-4bb7-9612-80b9c1c8cd87"
}
