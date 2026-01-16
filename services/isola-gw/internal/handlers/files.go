// Package handlers provides HTTP handlers for the isola-gw API.
package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/isola-ai/isola-sb/services/isola-gw/internal/models"
)

const (
	fileSizeThresholdBytes = 5 * 1024 * 1024 // 5MB
)

// buildObjectKey constructs an S3 object key path.
// format: {type}/{tenantID}/{sandboxID}/{id}/{filename}
func buildObjectKey(objectType string, tenantID string, sandboxID string, id string, filename string) string {
	return fmt.Sprintf("%s/%s/%s/%s/%s", objectType, tenantID, sandboxID, id, filename)
}

// getSandboxStatusAndAddress retrieves sandbox status and validates it's running.
// Returns (agentAddress, shouldReturn) where shouldReturn is true if an error response was sent.
func (h *Handler) getSandboxStatusAndAddress(ctx context.Context, c *gin.Context, sandboxID string, logPrefix string) (string, bool) {
	status, err := h.k8sManager.GetSandboxStatus(ctx, sandboxID)
	if err != nil {
		log.Printf("[%s] Failed to get sandbox %s status: %v", logPrefix, sandboxID, err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:   "InternalServerError",
			Message: "Failed to get sandbox status",
		})
		return "", true
	}
	if status == nil {
		log.Printf("[%s] Sandbox %s not found", logPrefix, sandboxID)
		c.JSON(http.StatusNotFound, models.ErrorResponse{
			Error:   "NotFound",
			Message: "Sandbox not found",
		})
		return "", true
	}

	if status.State != models.SandboxStateRunning {
		log.Printf("[%s] Sandbox %s not in running state: %s", logPrefix, sandboxID, status.State)
		c.JSON(http.StatusConflict, models.ErrorResponse{
			Error:   "Conflict",
			Message: "Sandbox must be in 'running' state, current state: " + string(status.State),
		})
		return "", true
	}

	return status.AgentAddress, false
}

func (h *Handler) UploadFile(c *gin.Context) {
	sandboxID := c.Param("id")

	// Get tenant ID from context
	tenantID, _ := c.Get("tenant_id")
	_ = tenantID

	ctx := c.Request.Context()

	agentAddress, shouldReturn := h.getSandboxStatusAndAddress(ctx, c, sandboxID, "UPLOAD")
	if shouldReturn {
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
	defer func() {
		if err := file.Close(); err != nil {
			log.Printf("Warning: failed to close uploaded file: %v", err)
		}
	}()

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

	agentURL := fmt.Sprintf("http://%s:%d/upload", agentAddress, agentSidecarPort)
	log.Printf("[UPLOAD] Forwarding to agent at %s", agentURL)

	// Create multipart form for agent
	formData := &bytes.Buffer{}
	writer := multipart.NewWriter(formData)
	defer func() {
		if err := writer.Close(); err != nil {
			log.Printf("Warning: failed to close multipart writer: %v", err)
		}
	}()

	pathField, err := writer.CreateFormField("path")
	if err != nil {
		log.Printf("Failed to create path field: %v", err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:   "InternalServerError",
			Message: "Failed to process file upload",
		})
		return
	}
	if _, err := pathField.Write([]byte(targetPath)); err != nil {
		log.Printf("Failed to write path field: %v", err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:   "InternalServerError",
			Message: "Failed to process file upload",
		})
		return
	}

	fileField, err := writer.CreateFormFile("file", fileHeader.Filename)
	if err != nil {
		log.Printf("Failed to create file field: %v", err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:   "InternalServerError",
			Message: "Failed to process file upload",
		})
		return
	}
	if _, err := fileField.Write(content); err != nil {
		log.Printf("Failed to write file content: %v", err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:   "InternalServerError",
			Message: "Failed to process file upload",
		})
		return
	}

	if err := writer.Close(); err != nil {
		log.Printf("Failed to finalize multipart writer: %v", err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:   "InternalServerError",
			Message: "Failed to process file upload",
		})
		return
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", agentURL, formData)
	if err != nil {
		log.Printf("Failed to create request: %v", err)
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
		log.Printf("Failed to connect to agent at %s: %v", agentURL, err)
		c.JSON(http.StatusBadGateway, models.ErrorResponse{
			Error:   "BadGateway",
			Message: "Failed to connect to sandbox agent: " + err.Error(),
		})
		return
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("Warning: failed to close response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Printf("Agent returned error: %d - %s", resp.StatusCode, string(bodyBytes))
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
			log.Printf("Failed to parse agent response, using defaults: %v", err)
			// If response parsing fails, return success with default values
			c.JSON(http.StatusOK, models.FileUploadResponse{
				Success: true,
				Path:    targetPath,
				Size:    fileSize,
			})
			return
		}
	}

	log.Printf("Successfully uploaded file to sandbox %s: %+v", sandboxID, agentResponse)

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

// DownloadFile handles GET /sandboxes/:id/files?path=...
// Streams the file directly from the sandbox agent to the client without buffering.
func (h *Handler) DownloadFile(c *gin.Context) {
	sandboxID := c.Param("id")
	targetPath := c.Query("path")

	if targetPath == "" {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:   "BadRequest",
			Message: "path query parameter is required",
		})
		return
	}

	ctx := c.Request.Context()

	agentAddress, shouldReturn := h.getSandboxStatusAndAddress(ctx, c, sandboxID, "DOWNLOAD")
	if shouldReturn {
		return
	}

	// Call agent's /read-file endpoint for streaming response
	agentURL := fmt.Sprintf("http://%s:%d/read-file?path=%s", agentAddress, agentSidecarPort, url.QueryEscape(targetPath))
	log.Printf("[DOWNLOAD] Streaming from agent at %s", agentURL)

	req, err := http.NewRequestWithContext(ctx, "GET", agentURL, nil)
	if err != nil {
		log.Printf("[DOWNLOAD] Failed to create request: %v", err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:   "InternalServerError",
			Message: "Failed to create request: " + err.Error(),
		})
		return
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[DOWNLOAD] Failed to connect to agent at %s: %v", agentURL, err)
		c.JSON(http.StatusBadGateway, models.ErrorResponse{
			Error:   "BadGateway",
			Message: "Failed to connect to sandbox agent: " + err.Error(),
		})
		return
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("[DOWNLOAD] Warning: failed to close response body: %v", err)
		}
	}()

	// Handle error responses from agent (errors are JSON, success is raw bytes)
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		log.Printf("[DOWNLOAD] Agent returned error: %d - %s", resp.StatusCode, string(bodyBytes))

		var agentError struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(bodyBytes, &agentError); err == nil && agentError.Error != "" {
			c.JSON(resp.StatusCode, models.ErrorResponse{
				Error:   "AgentError",
				Message: agentError.Error,
			})
			return
		}

		c.JSON(resp.StatusCode, models.ErrorResponse{
			Error:   "InternalServerError",
			Message: "Agent read-file failed: " + string(bodyBytes),
		})
		return
	}

	// Stream the response directly to the client
	contentLength := resp.ContentLength
	if contentLength > fileSizeThresholdBytes {
		log.Printf("[DOWNLOAD] File too large: %d bytes (limit: %d)", contentLength, fileSizeThresholdBytes)
		c.JSON(http.StatusRequestEntityTooLarge, models.ErrorResponse{
			Error:   "FileTooLarge",
			Message: fmt.Sprintf("File size (%d bytes) exceeds maximum allowed size (%d bytes)", contentLength, fileSizeThresholdBytes),
		})
		return
	}

	log.Printf("[DOWNLOAD] Streaming file from sandbox %s: path=%s, size=%d", sandboxID, targetPath, contentLength)

	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		c.Header("Content-Disposition", cd)
	}
	c.DataFromReader(http.StatusOK, contentLength, "application/octet-stream", resp.Body, nil)
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

	tenantID, exists := c.Get("tenant_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{
			Error:   "Unauthorized",
			Message: "Missing tenant ID",
		})
		return
	}
	tenantIDStr, ok := tenantID.(string)
	if !ok {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:   "InternalServerError",
			Message: "Invalid tenant ID type",
		})
		return
	}

	ctx := c.Request.Context()

	if h.storage == nil {
		c.JSON(http.StatusNotImplemented, models.ErrorResponse{
			Error:   "NotImplemented",
			Message: "Storage not configured. Large file uploads are not available.",
		})
		return
	}

	_, shouldReturn := h.getSandboxStatusAndAddress(ctx, c, sandboxID, "UPLOAD-URL")
	if shouldReturn {
		return
	}

	// Generate unique upload ID and object key
	uploadID := uuid.New().String()
	objectKey := buildObjectKey("uploads", tenantIDStr, sandboxID, uploadID, req.Filename)

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
	tenantID, exists := c.Get("tenant_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, models.ErrorResponse{
			Error:   "Unauthorized",
			Message: "Missing tenant ID",
		})
		return
	}
	tenantIDStr, ok := tenantID.(string)
	if !ok {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:   "InternalServerError",
			Message: "Invalid tenant ID type",
		})
		return
	}

	ctx := c.Request.Context()

	if h.storage == nil {
		c.JSON(http.StatusNotImplemented, models.ErrorResponse{
			Error:   "NotImplemented",
			Message: "Storage not configured. Large file uploads are not available.",
		})
		return
	}

	agentAddress, shouldReturn := h.getSandboxStatusAndAddress(ctx, c, sandboxID, "CONFIRM")
	if shouldReturn {
		return
	}

	// Reconstruct object key from upload_id and filename
	objectKey := buildObjectKey("uploads", tenantIDStr, sandboxID, req.UploadID, req.Filename)
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
	agentURL := fmt.Sprintf("http://%s:%d/download", agentAddress, agentSidecarPort)
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
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("Warning: failed to close response body: %v", err)
		}
	}()

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
