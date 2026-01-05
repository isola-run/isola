// Package handlers provides HTTP handlers for the isola-gw API.
package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/omereli/dev-isola/services/isola-gw/internal/models"
)

const (
	fileSizeThresholdBytes = 5 * 1024 * 1024 // 5MB
)

func (h *Handler) UploadFile(c *gin.Context) {
	sandboxID := c.Param("id")

	// Get tenant ID from context
	tenantID, _ := c.Get("tenant_id")
	_ = tenantID

	ctx := c.Request.Context()

	state, ipAddress, _ := h.k8sManager.GetPodStatus(ctx, sandboxID)
	if state == nil {
		log.Printf("[UPLOAD] Sandbox %s not found", sandboxID)
		c.JSON(http.StatusNotFound, models.ErrorResponse{
			Error:   "NotFound",
			Message: "Sandbox not found",
		})
		return
	}

	if *state != models.SandboxStateRunning {
		log.Printf("[UPLOAD] Sandbox %s not in running state: %s", sandboxID, *state)
		c.JSON(http.StatusConflict, models.ErrorResponse{
			Error:   "Conflict",
			Message: "Sandbox must be in 'running' state, current state: " + string(*state),
		})
		return
	}

	if ipAddress == nil || *ipAddress == "" {
		log.Printf("[UPLOAD] Sandbox %s has no IP address", sandboxID)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:   "InternalServerError",
			Message: "Sandbox pod has no IP address",
		})
		return
	}

	// Parse multipart form
	form, err := c.MultipartForm()
	if err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:   "BadRequest",
			Message: "Failed to parse multipart form: " + err.Error(),
		})
		return
	}

	files := form.File["file"]
	if len(files) == 0 {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:   "BadRequest",
			Message: "No file provided",
		})
		return
	}

	fileHeader := files[0]
	file, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:   "BadRequest",
			Message: "Failed to open file: " + err.Error(),
		})
		return
	}
	defer file.Close()

	pathValues := form.Value["path"]
	if len(pathValues) == 0 {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:   "BadRequest",
			Message: "No path provided",
		})
		return
	}
	targetPath := pathValues[0]

	// Read file content to check size
	content, err := io.ReadAll(file)
	if err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:   "BadRequest",
			Message: "Failed to read file: " + err.Error(),
		})
		return
	}

	fileSize := int64(len(content))
	log.Printf("[UPLOAD] File size: %d bytes, threshold: %d bytes", fileSize, fileSizeThresholdBytes)

	if fileSize >= fileSizeThresholdBytes {
		log.Printf("[UPLOAD] File too large for direct upload: %d bytes", fileSize)
		c.JSON(http.StatusNotImplemented, models.ErrorResponse{
			Error:   "NotImplemented",
			Message: fmt.Sprintf("File size (%d bytes) exceeds direct upload limit (%d bytes). Signed URL upload is not yet implemented.", fileSize, fileSizeThresholdBytes),
		})
		return
	}

	agentURL := fmt.Sprintf("http://%s:%d/upload", *ipAddress, agentSidecarPort)
	log.Printf("[UPLOAD] Forwarding to agent at %s", agentURL)

	// Create multipart form for agent
	formData := &bytes.Buffer{}
	writer := multipart.NewWriter(formData)

	pathField, err := writer.CreateFormField("path")
	if err != nil {
		log.Printf("[UPLOAD] Failed to create path field: %v", err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:   "InternalServerError",
			Message: "Failed to create form field: " + err.Error(),
		})
		return
	}
	pathField.Write([]byte(targetPath))

	fileField, err := writer.CreateFormFile("file", fileHeader.Filename)
	if err != nil {
		writer.Close()
		log.Printf("[UPLOAD] Failed to create file field: %v", err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:   "InternalServerError",
			Message: "Failed to create file field: " + err.Error(),
		})
		return
	}
	fileField.Write(content)

	writer.Close()

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", agentURL, formData)
	if err != nil {
		log.Printf("[UPLOAD] Failed to create request: %v", err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:   "InternalServerError",
			Message: "Failed to create request: " + err.Error(),
		})
		return
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[UPLOAD] Failed to connect to agent at %s: %v", agentURL, err)
		c.JSON(http.StatusBadGateway, models.ErrorResponse{
			Error:   "BadGateway",
			Message: "Failed to connect to sandbox agent: " + err.Error(),
		})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Printf("[UPLOAD] Agent returned error: %d - %s", resp.StatusCode, string(bodyBytes))
		c.JSON(resp.StatusCode, models.ErrorResponse{
			Error:   "InternalServerError",
			Message: "Agent upload failed: " + string(bodyBytes),
		})
		return
	}

	// Parse agent response
	bodyBytes, _ := io.ReadAll(resp.Body)
	var agentResponse map[string]interface{}
	if len(bodyBytes) > 0 {
		if err := json.Unmarshal(bodyBytes, &agentResponse); err != nil {
			log.Printf("[UPLOAD] Failed to parse agent response, using defaults: %v", err)
			// If response parsing fails, return success with default values
			c.JSON(http.StatusOK, models.FileUploadResponse{
				Success: true,
				Path:    targetPath,
				Size:    fileSize,
			})
			return
		}
	}

	log.Printf("[UPLOAD] Successfully uploaded file to sandbox %s: %+v", sandboxID, agentResponse)

	success := true
	if val, ok := agentResponse["success"].(bool); ok {
		success = val
	}

	path := targetPath
	if val, ok := agentResponse["path"].(string); ok {
		path = val
	}

	size := fileSize
	if val, ok := agentResponse["size"].(float64); ok {
		size = int64(val)
	}

	c.JSON(http.StatusOK, models.FileUploadResponse{
		Success: success,
		Path:    path,
		Size:    size,
	})
}

func (h *Handler) GenerateUploadUrl(c *gin.Context) {
	sandboxID := c.Param("id")

	var req models.UploadUrlRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:   "BadRequest",
			Message: err.Error(),
		})
		return
	}

	log.Printf("[UPLOAD-URL] Request for sandbox %s: path=%s, filename=%s", sandboxID, req.Path, req.Filename)

	tenantID, _ := c.Get("tenant_id")
	tenantIDStr := tenantID.(string)

	ctx := c.Request.Context()

	if h.storage == nil {
		c.JSON(http.StatusNotImplemented, models.ErrorResponse{
			Error:   "NotImplemented",
			Message: "Storage not configured. Large file uploads are not available.",
		})
		return
	}

	state, _, _ := h.k8sManager.GetPodStatus(ctx, sandboxID)
	if state == nil {
		log.Printf("[UPLOAD-URL] Sandbox %s not found", sandboxID)
		c.JSON(http.StatusNotFound, models.ErrorResponse{
			Error:   "NotFound",
			Message: "Sandbox not found",
		})
		return
	}

	if *state != models.SandboxStateRunning {
		log.Printf("[UPLOAD-URL] Sandbox %s not in running state: %s", sandboxID, *state)
		c.JSON(http.StatusConflict, models.ErrorResponse{
			Error:   "Conflict",
			Message: "Sandbox must be in 'running' state, current state: " + string(*state),
		})
		return
	}

	// Generate unique upload ID and object key
	uploadID := uuid.New().String()
	objectKey := fmt.Sprintf("uploads/%s/%s/%s/%s", tenantIDStr, sandboxID, uploadID, req.Filename)

	// Generate presigned upload URL (15 minutes expiration)
	expiresIn := 900 // 15 minutes
	contentType := ""
	if req.ContentType != nil {
		contentType = *req.ContentType
	}

	uploadURL, err := h.storage.GeneratePresignedUploadURL(ctx, objectKey, expiresIn, contentType)
	if err != nil {
		log.Printf("[UPLOAD-URL] Failed to generate presigned URL: %v", err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:   "InternalServerError",
			Message: "Failed to generate presigned URL: " + err.Error(),
		})
		return
	}

	log.Printf("[UPLOAD-URL] Generated presigned URL for upload_id=%s, objectKey=%s", uploadID, objectKey)

	c.JSON(http.StatusOK, models.UploadUrlResponse{
		UploadURL: uploadURL,
		UploadID:  uploadID,
		ExpiresIn: expiresIn,
	})
}

// ConfirmUpload handles POST /sandboxes/:id/files/confirm
func (h *Handler) ConfirmUpload(c *gin.Context) {
	sandboxID := c.Param("id")

	var req models.ConfirmUploadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:   "BadRequest",
			Message: err.Error(),
		})
		return
	}

	log.Printf("[CONFIRM] Request for sandbox %s: upload_id=%s, filename=%s, path=%s", sandboxID, req.UploadID, req.Filename, req.Path)

	// Get tenant ID from context
	tenantID, _ := c.Get("tenant_id")
	tenantIDStr := tenantID.(string)

	ctx := c.Request.Context()

	if h.storage == nil {
		c.JSON(http.StatusNotImplemented, models.ErrorResponse{
			Error:   "NotImplemented",
			Message: "Storage not configured. Large file uploads are not available.",
		})
		return
	}

	state, ipAddress, _ := h.k8sManager.GetPodStatus(ctx, sandboxID)
	if state == nil {
		log.Printf("[CONFIRM] Sandbox %s not found", sandboxID)
		c.JSON(http.StatusNotFound, models.ErrorResponse{
			Error:   "NotFound",
			Message: "Sandbox not found",
		})
		return
	}

	if *state != models.SandboxStateRunning {
		log.Printf("[CONFIRM] Sandbox %s not in running state: %s", sandboxID, *state)
		c.JSON(http.StatusConflict, models.ErrorResponse{
			Error:   "Conflict",
			Message: "Sandbox must be in 'running' state, current state: " + string(*state),
		})
		return
	}

	if ipAddress == nil || *ipAddress == "" {
		log.Printf("[CONFIRM] Sandbox %s has no IP address", sandboxID)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:   "InternalServerError",
			Message: "Sandbox pod has no IP address",
		})
		return
	}

	// Reconstruct object key from upload_id and filename
	objectKey := fmt.Sprintf("uploads/%s/%s/%s/%s", tenantIDStr, sandboxID, req.UploadID, req.Filename)
	targetPath := req.Path

	// Generate presigned download URL for the agent
	downloadURL, err := h.storage.GeneratePresignedDownloadURL(ctx, objectKey, 900) // 15 minutes
	if err != nil {
		log.Printf("[CONFIRM] Failed to generate presigned download URL: %v", err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:   "InternalServerError",
			Message: "Failed to generate download URL: " + err.Error(),
		})
		return
	}

	// Call agent's /download endpoint
	agentURL := fmt.Sprintf("http://%s:%d/download", *ipAddress, agentSidecarPort)
	log.Printf("[CONFIRM] Triggering agent download at %s", agentURL)

	downloadReq := models.DownloadRequest{
		DownloadURL: downloadURL,
		Path:        targetPath,
	}

	reqBody, err := json.Marshal(downloadReq)
	if err != nil {
		log.Printf("[CONFIRM] Failed to marshal download request: %v", err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:   "InternalServerError",
			Message: "Failed to marshal request: " + err.Error(),
		})
		return
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", agentURL, bytes.NewReader(reqBody))
	if err != nil {
		log.Printf("[CONFIRM] Failed to create request: %v", err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:   "InternalServerError",
			Message: "Failed to create request: " + err.Error(),
		})
		return
	}

	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		log.Printf("[CONFIRM] Failed to connect to agent at %s: %v", agentURL, err)
		c.JSON(http.StatusBadGateway, models.ErrorResponse{
			Error:   "BadGateway",
			Message: "Failed to connect to sandbox agent: " + err.Error(),
		})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Printf("[CONFIRM] Agent returned error: %d - %s", resp.StatusCode, string(bodyBytes))
		c.JSON(resp.StatusCode, models.ErrorResponse{
			Error:   "InternalServerError",
			Message: "Agent download failed: " + string(bodyBytes),
		})
		return
	}

	var agentResponse map[string]interface{}
	bodyBytes, _ := io.ReadAll(resp.Body)
	if len(bodyBytes) > 0 {
		if err := json.Unmarshal(bodyBytes, &agentResponse); err != nil {
			log.Printf("[CONFIRM] Failed to parse agent response: %v", err)
		}
	}

	log.Printf("[CONFIRM] Successfully triggered download for sandbox %s", sandboxID)

	// Delete the file from object storage after successful download
	if err := h.storage.DeleteObject(ctx, objectKey); err != nil {
		// Log the error but don't fail the request - file was successfully delivered
		log.Printf("[CONFIRM] Failed to delete object %s: %v", objectKey, err)
	} else {
		log.Printf("[CONFIRM] Deleted object: %s", objectKey)
	}

	path := targetPath
	size := int64(0)
	if val, ok := agentResponse["path"].(string); ok {
		path = val
	}
	if val, ok := agentResponse["size"].(float64); ok {
		size = int64(val)
	}

	c.JSON(http.StatusOK, models.ConfirmUploadResponse{
		Success: true,
		Path:    path,
		Size:    size,
	})
}

