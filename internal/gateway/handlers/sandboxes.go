// Package handlers provides HTTP handlers for the isola-gw API.
package handlers

import (
	"context"
	"log" // TODO: Use slog
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/isola-ai/isola-sb/internal/gateway/models"
	"github.com/isola-ai/isola-sb/internal/gateway/naming"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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

	// Get all sandboxes
	allSandboxes, err := h.listAllSandboxes(ctx)
	if err != nil {
		log.Printf("Failed to list sandboxes: %v", err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:   "InternalServerError",
			Message: "Failed to list sandboxes",
		})
		return
	}

	// Filter by state if specified
	// TODO: __OMER__ add filter to list function to avoid second iteration
	items := make([]models.Sandbox, 0, len(allSandboxes))
	for _, sandbox := range allSandboxes {
		if params.State != nil && sandbox.State != *params.State {
			continue
		}
		items = append(items, sandbox)
	}

	// Apply pagination
	// TODO: __OMER__
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

	// Generate unique name if not provided
	name := req.Name
	if name == "" {
		name = naming.GenerateSandboxName()
	}

	now := time.Now().UTC()

	desiredState := models.SandboxStateStopped
	if req.AutoStart {
		desiredState = models.SandboxStateRunning
	}

	sandbox := &models.Sandbox{
		Name:         name,
		State:        models.SandboxStatePending,
		DesiredState: &desiredState,
		Env:          req.Env,
		Labels:       req.Labels,
		ErrorReason:  nil,
		CreatedAt:    now,
	}

	log.Printf("Creating sandbox: %+v", sandbox)

	ctx := c.Request.Context()
	templateName := name + "-template"
	success, errorReason := h.k8sManager.CreateSandboxCR(ctx, name, req, templateName)
	if !success {
		errMsg := "unknown error"
		if errorReason != nil {
			errMsg = *errorReason
		}
		log.Printf("Sandbox '%s' CR creation failed: %s", name, errMsg)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:   "InternalServerError",
			Message: "Failed to create sandbox: " + errMsg,
		})
		return
	}

	log.Printf("Sandbox '%s' created successfully", name)
	c.JSON(http.StatusCreated, sandbox)
}

func (h *Handler) GetSandbox(c *gin.Context) {
	name := c.Param("name")

	tenantID, _ := c.Get("tenant_id")
	_ = tenantID // TODO: validate tenant_id belongs to the sandbox

	ctx := c.Request.Context()

	sandbox, err := h.getSandbox(ctx, name)
	if err != nil {
		log.Printf("Failed to get sandbox '%s': %v", name, err)
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
	name := c.Param("name")

	var params models.TerminateSandboxParams
	if err := c.ShouldBindQuery(&params); err != nil {
		params.Force = false
	}

	// Get tenant ID from context
	tenantID, _ := c.Get("tenant_id")
	_ = tenantID     // TODO: validate tenant_id belongs to the sandbox
	_ = params.Force // TODO: implement force termination

	ctx := c.Request.Context()

	err := h.k8sManager.DeleteSandboxCR(ctx, name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			c.JSON(http.StatusNotFound, models.ErrorResponse{
				Error:   "NotFound",
				Message: "Sandbox not found",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:   "InternalServerError",
			Message: err.Error(),
		})
		return
	}

	c.Status(http.StatusNoContent)
}

// sandboxCRToModel converts a Sandbox CR (unstructured) to a Sandbox model
func (h *Handler) sandboxCRToModel(cr *unstructured.Unstructured) *models.Sandbox {
	metadata, found, _ := unstructured.NestedMap(cr.Object, "metadata")
	if !found {
		return nil
	}

	// Get name from metadata.name - this is the sole identifier
	name, ok := metadata["name"].(string)
	if !ok || name == "" {
		return nil
	}

	// Get creation timestamp
	createdAt := time.Now().UTC()
	if creationTimestamp, ok := metadata["creationTimestamp"].(string); ok {
		if parsed, err := time.Parse(time.RFC3339, creationTimestamp); err == nil {
			createdAt = parsed
		}
	}

	status, found, _ := unstructured.NestedMap(cr.Object, "status")
	state := models.SandboxStatePending
	var errorReason *string

	if found {
		// Check conditions for state
		conditions, found, _ := unstructured.NestedSlice(status, "conditions")
		if found {
			for _, cond := range conditions {
				condMap, ok := cond.(map[string]interface{})
				if !ok {
					continue
				}
				condType, _ := condMap["type"].(string)
				condStatus, _ := condMap["status"].(string)
				if condType == "Ready" {
					if condStatus == "True" {
						state = models.SandboxStateRunning
					}
					break
				}
				if condType == "TimedOut" && condStatus == "True" {
					state = models.SandboxStateStopped
					break
				}
			}
		}
	}

	desiredState := state
	return &models.Sandbox{
		Name:         name,
		State:        state,
		DesiredState: &desiredState,
		Env:          make(map[string]string),
		Labels:       make(map[string]string),
		ErrorReason:  errorReason,
		CreatedAt:    createdAt,
	}
}

// getSandbox gets a single sandbox by name
func (h *Handler) getSandbox(ctx context.Context, name string) (*models.Sandbox, error) {
	cr, err := h.k8sManager.GetSandboxCR(ctx, name)
	if err != nil {
		return nil, err
	}

	if cr == nil {
		return nil, nil
	}

	return h.sandboxCRToModel(cr), nil
}

// listAllSandboxes lists all sandboxes
func (h *Handler) listAllSandboxes(ctx context.Context) ([]models.Sandbox, error) {
	sandboxCRs, err := h.k8sManager.ListSandboxCRs(ctx)
	if err != nil {
		return nil, err
	}

	sandboxes := make([]models.Sandbox, 0, len(sandboxCRs))
	for _, cr := range sandboxCRs {
		sandbox := h.sandboxCRToModel(cr)
		if sandbox != nil {
			sandboxes = append(sandboxes, *sandbox)
		}
	}

	return sandboxes, nil
}
