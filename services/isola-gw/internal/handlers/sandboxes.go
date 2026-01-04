// Package handlers provides HTTP handlers for the isola-gw API.
package handlers

import (
	"context"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/omereli/dev-isola/services/isola-gw/internal/models"
)

func (h *Handler) ListSandboxes(c *gin.Context) {
	var params models.ListSandboxesParams
	if err := c.ShouldBindQuery(&params); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:   "BadRequest",
			Message: err.Error(),
		})
		return
	}

	tenantID, _ := c.Get("tenant_id")
	_ = tenantID // TODO: __OMER__ filter by tenant

	ctx := c.Request.Context()

	// List pods from Kubernetes
	pods, err := h.k8sManager.ListPods(ctx, nil)
	if err != nil {
		log.Printf("Failed to list pods: %v", err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:   "InternalServerError",
			Message: "Failed to list sandboxes",
		})
		return
	}

	// Convert pods to sandboxes
	items := make([]models.Sandbox, 0)
	for _, podData := range pods {
		sandboxID, ok := podData["sandbox_id"].(string)
		if !ok || sandboxID == "" {
			continue
		}

		sandbox, err := h.getSandboxFromK8s(ctx, sandboxID)
		if err != nil {
			log.Printf("Failed to get sandbox %s: %v", sandboxID, err)
			continue
		}

		if sandbox == nil {
			continue
		}

		// Filter by state if specified
		if params.State != nil && sandbox.State != *params.State {
			continue
		}

		items = append(items, *sandbox)
	}

	// Apply pagination
	total := len(items)
	start := params.Offset
	if start > total {
		start = total
	}
	end := start + params.Limit
	if end > total {
		end = total
	}

	var paginatedItems []models.Sandbox
	if start < end {
		paginatedItems = items[start:end]
	} else {
		paginatedItems = []models.Sandbox{}
	}

	log.Printf("There are %d sandboxes", total)

	c.JSON(http.StatusOK, models.SandboxList{
		Items:  paginatedItems,
		Total:  total,
		Limit:  params.Limit,
		Offset: params.Offset,
	})
}

func (h *Handler) CreateSandbox(c *gin.Context) {
	var req models.CreateSandboxRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:   "BadRequest",
			Message: err.Error(),
		})
		return
	}

	// Get tenant ID from context
	tenantID, _ := c.Get("tenant_id")
	_ = tenantID // TODO: __OMER__ mark sandbox as owned by tenant

	sandboxID := uuid.New().String()
	now := time.Now().UTC()

	desiredState := models.SandboxStateStopped
	if req.AutoStart {
		desiredState = models.SandboxStateRunning
	}

	sandbox := &models.Sandbox{
		ID:           sandboxID,
		Name:         req.Name,
		State:        models.SandboxStatePending,
		DesiredState: &desiredState,
		Env:          req.Env,
		Labels:       req.Labels,
		ErrorReason:  nil,
		CreatedAt:    now,
	}

	log.Printf("Creating sandbox: %+v", sandbox)

	// Create sandbox asynchronously in background
	ctx := context.Background()
	go h.handleSandboxCreation(ctx, tenantID.(string), sandboxID, req, req.AutoStart)

	log.Printf("Sandbox request created: %+v", sandbox)
	c.JSON(http.StatusCreated, sandbox)
}

func (h *Handler) GetSandbox(c *gin.Context) {
	sandboxID := c.Param("id")

	tenantID, _ := c.Get("tenant_id")
	_ = tenantID // TODO: validate tenant_id belongs to the sandbox

	ctx := c.Request.Context()

	sandbox, err := h.getSandboxFromK8s(ctx, sandboxID)
	if err != nil {
		log.Printf("Failed to get sandbox %s: %v", sandboxID, err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:   "InternalServerError",
			Message: "Failed to get sandbox",
		})
		return
	}

	if sandbox == nil {
		c.JSON(http.StatusNotFound, models.ErrorResponse{
			Error:   "NotFound",
			Message: "Sandbox not found",
		})
		return
	}

	c.JSON(http.StatusOK, sandbox)
}

func (h *Handler) TerminateSandbox(c *gin.Context) {
	sandboxID := c.Param("id")

	var params models.TerminateSandboxParams
	if err := c.ShouldBindQuery(&params); err != nil {
		params.Force = false
	}

		// Get tenant ID from context
	tenantID, _ := c.Get("tenant_id")
	_ = tenantID // TODO: validate tenant_id belongs to the sandbox
	_ = params.Force // TODO: implement force termination

	ctx := c.Request.Context()

	success, errorReason := h.k8sManager.DeleteSandboxCR(ctx, sandboxID)
	if !success {
		statusCode := http.StatusInternalServerError
		if errorReason != nil && strings.Contains(*errorReason, "not found") {
			statusCode = http.StatusNotFound
		}
		c.JSON(statusCode, models.ErrorResponse{
			Error:   "InternalServerError",
			Message: *errorReason,
		})
		return
	}

	c.Status(http.StatusNoContent)
}

// getSandboxFromK8s fetches sandbox details from Kubernetes-api
// TODO: __OMER__ use informer
func (h *Handler) getSandboxFromK8s(ctx context.Context, sandboxID string) (*models.Sandbox, error) {
	state, _, errorReason, name := h.k8sManager.GetSandboxCRStatus(ctx, sandboxID)
	if errorReason != nil && *errorReason == "Sandbox not found" {
		return nil, nil
	}

	if state == nil {
		return nil, nil
	}

	sandboxName := sandboxID[:min(8, len(sandboxID))]
	if name != nil {
		sandboxName = *name
	} else {
		sandboxName = "sandbox-" + sandboxName
	}

	desiredState := models.SandboxState(*state)
	return &models.Sandbox{
		ID:           sandboxID,
		Name:         sandboxName,
		State:        models.SandboxState(*state),
		DesiredState: &desiredState,
		Env:          make(map[string]string),
		Labels:       make(map[string]string),
		ErrorReason:  errorReason,
		CreatedAt:    time.Now().UTC(), // TODO: get actual creation time from CR
	}, nil
}

func (h *Handler) handleSandboxCreation(ctx context.Context, tenantID, sandboxID string, req models.CreateSandboxRequest, autoStart bool) {
	backend := getSandboxBackend()
	if backend == "kubernetes" {
		templateName := "sandbox-template-" + sandboxID
		success, errorReason := h.k8sManager.CreateSandboxCR(ctx, sandboxID, req, templateName)
		if !success {
			if errorReason != nil {
				log.Printf("Sandbox %s CR creation failed: %s", sandboxID, *errorReason)
			} else {
				log.Printf("Sandbox %s CR creation failed: unknown error", sandboxID)
			}
		} else {
			log.Printf("Sandbox %s CR creation result success=%v", sandboxID, success)
		}
	} else {
		log.Printf("Sandbox %s creation failed: Agent backend is not available", sandboxID)
	}
}

