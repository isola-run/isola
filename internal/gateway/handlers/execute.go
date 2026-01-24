// Package handlers provides HTTP handlers for the isola-gw API.
package handlers

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/isola-ai/isola-sb/internal/gateway/models"
)

// ExecuteCommand handles POST /sandboxes/:name/execute
func (h *Handler) ExecuteCommand(c *gin.Context) {
	name := c.Param("name")

	var req models.ExecuteCommandRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:   "BadRequest",
			Message: err.Error(),
		})
		return
	}

	log.Printf("[EXECUTE] Request for sandbox '%s': %s", name, req.Command)

	tenantID, _ := c.Get("tenant_id")
	_ = tenantID // TODO:__OMER__ validate tenant_id belongs to the sandbox

	ctx := c.Request.Context()

	status, err := h.k8sManager.GetSandboxStatus(ctx, name)
	if err != nil {
		log.Printf("[EXECUTE] Failed to get sandbox '%s' status: %v", name, err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:   "InternalServerError",
			Message: "Failed to get sandbox status",
		})
		return
	}
	if status == nil {
		log.Printf("[EXECUTE] Sandbox '%s' not found", name)
		c.JSON(http.StatusNotFound, models.ErrorResponse{
			Error:   "NotFound",
			Message: "Sandbox not found",
		})
		return
	}

	if status.State != models.SandboxStateRunning {
		log.Printf("[EXECUTE] Sandbox '%s' not in running state: %s", name, status.State)
		c.JSON(http.StatusConflict, models.ErrorResponse{
			Error:   "Conflict",
			Message: "Sandbox must be in 'running' state, current state: " + string(status.State),
		})
		return
	}

	// Execute command in Kubernetes pod
	stdout, stderr, exitCode, err := h.k8sManager.ExecuteCommand(ctx, name, req.Command)
	if err != nil {
		log.Printf("[EXECUTE] Failed to execute command in sandbox '%s': %v", name, err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:   "InternalServerError",
			Message: "Failed to execute command: " + err.Error(),
		})
		return
	}

	log.Printf("[EXECUTE] Command completed for sandbox '%s': exit_code=%d", name, exitCode)

	c.JSON(http.StatusOK, models.ExecuteCommandResponse{
		Stdout:   stdout,
		Stderr:   stderr,
		ExitCode: exitCode,
	})
}
