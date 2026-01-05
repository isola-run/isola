// Package handlers provides HTTP handlers for the isola-gw API.
package handlers

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/omereli/dev-isola/services/isola-gw/internal/models"
)

// ExecuteCommand handles POST /sandboxes/:id/execute
func (h *Handler) ExecuteCommand(c *gin.Context) {
	sandboxID := c.Param("id")

	var req models.ExecuteCommandRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:   "BadRequest",
			Message: err.Error(),
		})
		return
	}

	log.Printf("[EXECUTE] Request for sandbox %s: %s", sandboxID, req.Command)

	tenantID, _ := c.Get("tenant_id")
	_ = tenantID // TODO:__OMER__ validate tenant_id belongs to the sandbox

	ctx := c.Request.Context()

	state, _, _ := h.k8sManager.GetPodStatus(ctx, sandboxID)
	if state == nil {
		log.Printf("[EXECUTE] Sandbox %s not found", sandboxID)
		c.JSON(http.StatusNotFound, models.ErrorResponse{
			Error:   "NotFound",
			Message: "Sandbox not found",
		})
		return
	}

	if *state != models.SandboxStateRunning {
		log.Printf("[EXECUTE] Sandbox %s not in running state: %s", sandboxID, *state)
		c.JSON(http.StatusConflict, models.ErrorResponse{
			Error:   "Conflict",
			Message: "Sandbox must be in 'running' state, current state: " + string(*state),
		})
		return
	}

	// Execute command in Kubernetes pod
	stdout, stderr, exitCode, err := h.k8sManager.ExecuteCommand(ctx, sandboxID, req.Command)
	if err != nil {
		log.Printf("[EXECUTE] Failed to execute command in sandbox %s: %v", sandboxID, err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:   "InternalServerError",
			Message: "Failed to execute command: " + err.Error(),
		})
		return
	}

	log.Printf("[EXECUTE] Command completed for sandbox %s: exit_code=%d", sandboxID, exitCode)

	c.JSON(http.StatusOK, models.ExecuteCommandResponse{
		Stdout:   stdout,
		Stderr:   stderr,
		ExitCode: exitCode,
	})
}

