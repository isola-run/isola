// Package handlers provides HTTP handlers for the isola-gw API.
package handlers

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/isola-ai/isola-sb/internal/gateway/models"
)

// EnableIngress enables external HTTP(S) access to a sandbox.
// POST /api/v1/sandboxes/:id/ingress
func (h *Handler) EnableIngress(c *gin.Context) {
	sandboxID := c.Param("id")

	var req models.EnableIngressRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// Use default port if not provided
		req.Port = 8080
	}

	if req.Port == 0 {
		req.Port = 8080
	}

	// Get tenant ID from context
	tenantID, _ := c.Get("tenant_id")
	_ = tenantID // TODO: validate tenant_id belongs to the sandbox

	ctx := c.Request.Context()

	// Create SandboxIngress CR
	url, err := h.k8sManager.CreateSandboxIngressCR(ctx, sandboxID, req.Port)
	if err != nil {
		log.Printf("Failed to create SandboxIngress for sandbox %s: %v", sandboxID, err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:   "InternalServerError",
			Message: "Failed to enable ingress: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusCreated, models.SandboxIngressResponse{
		URL:     url,
		Enabled: true,
	})
}

// DisableIngress disables external HTTP(S) access to a sandbox.
// DELETE /api/v1/sandboxes/:id/ingress
func (h *Handler) DisableIngress(c *gin.Context) {
	sandboxID := c.Param("id")

	// Get tenant ID from context
	tenantID, _ := c.Get("tenant_id")
	_ = tenantID // TODO: validate tenant_id belongs to the sandbox

	ctx := c.Request.Context()

	// Delete SandboxIngress CR
	err := h.k8sManager.DeleteSandboxIngressCR(ctx, sandboxID)
	if err != nil {
		log.Printf("Failed to delete SandboxIngress for sandbox %s: %v", sandboxID, err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:   "InternalServerError",
			Message: "Failed to disable ingress: " + err.Error(),
		})
		return
	}

	c.Status(http.StatusNoContent)
}

// GetIngressStatus returns the current ingress status for a sandbox.
// GET /api/v1/sandboxes/:id/ingress
func (h *Handler) GetIngressStatus(c *gin.Context) {
	sandboxID := c.Param("id")

	// Get tenant ID from context
	tenantID, _ := c.Get("tenant_id")
	_ = tenantID // TODO: validate tenant_id belongs to the sandbox

	ctx := c.Request.Context()

	// Get SandboxIngress CR status
	status, err := h.k8sManager.GetSandboxIngressStatus(ctx, sandboxID)
	if err != nil {
		log.Printf("Failed to get SandboxIngress status for sandbox %s: %v", sandboxID, err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:   "InternalServerError",
			Message: "Failed to get ingress status: " + err.Error(),
		})
		return
	}

	if status == nil {
		c.JSON(http.StatusOK, models.SandboxIngressStatus{
			Enabled: false,
			Ready:   false,
		})
		return
	}

	c.JSON(http.StatusOK, status)
}
