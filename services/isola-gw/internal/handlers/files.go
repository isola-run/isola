// Package handlers provides HTTP handlers for the isola-gw API.
package handlers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/omereli/dev-isola/services/isola-gw/internal/models"
)

const (
	fileSizeThresholdBytes = 5 * 1024 * 1024 // 5MB
)

// buildObjectKey constructs an S3 object key path.
// format: {type}/{tenantID}/{sandboxID}/{id}/{filename}
func buildObjectKey(objectType string, tenantID string, sandboxID string, id string, filename string) string {
	return fmt.Sprintf("%s/%s/%s/%s/%s", objectType, tenantID, sandboxID, id, filename)
}

// generateDownloadID creates a deterministic download ID from tenant, sandbox, and path.
// Same file always gets the same ID, enabling stateless operation.
func generateDownloadID(tenantID, sandboxID, path string) string {
	h := sha256.New()
	h.Write([]byte(tenantID + sandboxID + path))
	return hex.EncodeToString(h.Sum(nil))[:32]
}

// buildDownloadObjectKey constructs an S3 object key for downloads.
// format: downloads/{tenantID}/{sandboxID}/{downloadID}/{filename}
func buildDownloadObjectKey(tenantID, sandboxID, downloadID, filename string) string {
	return buildObjectKey("downloads", tenantID, sandboxID, downloadID, filename)
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

// DownloadFile handles GET /sandboxes/:id/files
// Supports two modes:
//   - With path param: Check file size, return directly if small, or initiate S3 upload if large
//   - With download_id param: Check S3 for object, return presigned download URL or "uploading" status
func (h *Handler) DownloadFile(c *gin.Context) {
	sandboxID := c.Param("id")
	targetPath := c.Query("path")
	downloadID := c.Query("download_id")

	// Get tenant ID from context
	tenantID, _ := c.Get("tenant_id")
	tenantIDStr := tenantID.(string)

	ctx := c.Request.Context()

	// Mode 2: Polling for download status using download_id
	if downloadID != "" {
		h.handleDownloadStatus(c, ctx, tenantIDStr, sandboxID, downloadID)
		return
	}

	// Mode 1: Initiate download using path
	if targetPath == "" {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:   "BadRequest",
			Message: "path or download_id query parameter is required",
		})
		return
	}

	agentAddress, shouldReturn := h.getSandboxStatusAndAddress(ctx, c, sandboxID, "DOWNLOAD")
	if shouldReturn {
		return
	}

	// First, get file info to check size
	fileInfo, err := h.getFileInfo(ctx, agentAddress, targetPath)
	if err != nil {
		log.Printf("[DOWNLOAD] Failed to get file info: %v", err)
		c.JSON(http.StatusBadGateway, models.ErrorResponse{
			Error:   "BadGateway",
			Message: "Failed to get file info from sandbox agent: " + err.Error(),
		})
		return
	}

	if !fileInfo.Exists {
		c.JSON(http.StatusNotFound, models.ErrorResponse{
			Error:   "NotFound",
			Message: "File not found: " + targetPath,
		})
		return
	}

	if fileInfo.IsDir {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:   "BadRequest",
			Message: "Path is a directory, not a file",
		})
		return
	}

	// Small file: stream directly from agent
	if fileInfo.Size < fileSizeThresholdBytes {
		h.streamFileFromAgent(c, ctx, agentAddress, sandboxID, targetPath)
		return
	}

	// Large file: initiate S3 upload flow
	h.initiateLargeFileDownload(c, ctx, tenantIDStr, sandboxID, agentAddress, targetPath, fileInfo.Size)
}

// fileInfoResult holds the result from the agent's /file-info endpoint.
type fileInfoResult struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	Exists bool   `json:"exists"`
	IsDir  bool   `json:"is_dir"`
}

// getFileInfo calls the agent's /file-info endpoint to get file metadata.
func (h *Handler) getFileInfo(ctx context.Context, agentAddress, path string) (*fileInfoResult, error) {
	agentURL := fmt.Sprintf("http://%s:%d/file-info?path=%s", agentAddress, agentSidecarPort, url.QueryEscape(path))

	req, err := http.NewRequestWithContext(ctx, "GET", agentURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to agent: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("[DOWNLOAD] Warning: failed to close file-info response body: %v", err)
		}
	}()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var agentError struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(bodyBytes, &agentError); err == nil && agentError.Error != "" {
			return nil, fmt.Errorf("agent error: %s", agentError.Error)
		}
		return nil, fmt.Errorf("agent returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var result fileInfoResult
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &result, nil
}

// streamFileFromAgent streams a file directly from the agent to the client.
func (h *Handler) streamFileFromAgent(c *gin.Context, ctx context.Context, agentAddress, sandboxID, targetPath string) {
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

	contentLength := resp.ContentLength
	log.Printf("[DOWNLOAD] Streaming file from sandbox %s: path=%s, size=%d", sandboxID, targetPath, contentLength)

	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		c.Header("Content-Disposition", cd)
	}
	c.DataFromReader(http.StatusOK, contentLength, "application/octet-stream", resp.Body, nil)
}

// initiateLargeFileDownload starts the S3 upload flow for large files.
func (h *Handler) initiateLargeFileDownload(c *gin.Context, ctx context.Context, tenantID, sandboxID, agentAddress, targetPath string, fileSize int64) {
	if h.storage == nil {
		c.JSON(http.StatusNotImplemented, models.ErrorResponse{
			Error:   "NotImplemented",
			Message: "Storage not configured. Large file downloads are not available.",
		})
		return
	}

	// Generate deterministic download ID
	downloadID := generateDownloadID(tenantID, sandboxID, targetPath)
	filename := filepath.Base(targetPath)
	objectKey := buildDownloadObjectKey(tenantID, sandboxID, downloadID, filename)

	log.Printf("[DOWNLOAD] Large file detected (%d bytes), initiating S3 upload: downloadID=%s", fileSize, downloadID)

	// Generate presigned upload URL for the agent
	expiresIn := 900 // 15 minutes
	uploadURL, err := h.storage.GeneratePresignedUploadURL(ctx, objectKey, expiresIn, "application/octet-stream")
	if err != nil {
		log.Printf("[DOWNLOAD] Failed to generate presigned upload URL: %v", err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:   "InternalServerError",
			Message: "Failed to generate upload URL: " + err.Error(),
		})
		return
	}

	// Tell agent to upload the file to S3
	if err := h.triggerAgentUploadToStorage(ctx, agentAddress, targetPath, uploadURL); err != nil {
		log.Printf("[DOWNLOAD] Failed to trigger agent upload: %v", err)
		c.JSON(http.StatusBadGateway, models.ErrorResponse{
			Error:   "BadGateway",
			Message: "Failed to initiate file upload: " + err.Error(),
		})
		return
	}

	log.Printf("[DOWNLOAD] Initiated upload for downloadID=%s, objectKey=%s", downloadID, objectKey)

	c.JSON(http.StatusAccepted, models.LargeFileDownloadResponse{
		DownloadID: downloadID,
		Status:     "uploading",
		Path:       targetPath,
	})
}

// triggerAgentUploadToStorage calls the agent's /upload-to-storage endpoint.
func (h *Handler) triggerAgentUploadToStorage(ctx context.Context, agentAddress, path, uploadURL string) error {
	agentURL := fmt.Sprintf("http://%s:%d/upload-to-storage", agentAddress, agentSidecarPort)

	reqBody := struct {
		Path      string `json:"path"`
		UploadURL string `json:"upload_url"`
	}{
		Path:      path,
		UploadURL: uploadURL,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", agentURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to agent: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("[DOWNLOAD] Warning: failed to close upload-to-storage response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("agent returned status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// handleDownloadStatus checks if a large file download is ready and returns the presigned URL.
func (h *Handler) handleDownloadStatus(c *gin.Context, ctx context.Context, tenantID, sandboxID, downloadID string) {
	if h.storage == nil {
		c.JSON(http.StatusNotImplemented, models.ErrorResponse{
			Error:   "NotImplemented",
			Message: "Storage not configured. Large file downloads are not available.",
		})
		return
	}

	// We need to find the object - we know the prefix but not the filename
	// For now, we'll use a wildcard approach by checking the downloads prefix
	// Since downloadID is deterministic, we can look for any file in that directory
	objectPrefix := fmt.Sprintf("downloads/%s/%s/%s/", tenantID, sandboxID, downloadID)

	// Check if object exists by listing objects with this prefix
	objectKey, err := h.findDownloadObject(ctx, objectPrefix)
	if err != nil {
		log.Printf("[DOWNLOAD] Error checking for download object: %v", err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:   "InternalServerError",
			Message: "Failed to check download status: " + err.Error(),
		})
		return
	}

	if objectKey == "" {
		// Object doesn't exist yet, still uploading
		log.Printf("[DOWNLOAD] Download %s not ready yet (object not found at prefix %s)", downloadID, objectPrefix)
		c.JSON(http.StatusOK, models.LargeFileDownloadResponse{
			DownloadID: downloadID,
			Status:     "uploading",
			Path:       "",
		})
		return
	}

	// Object exists, generate presigned download URL
	expiresIn := 3600 // 1 hour
	downloadURL, err := h.storage.GeneratePresignedDownloadURL(ctx, objectKey, expiresIn)
	if err != nil {
		log.Printf("[DOWNLOAD] Failed to generate presigned download URL: %v", err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:   "InternalServerError",
			Message: "Failed to generate download URL: " + err.Error(),
		})
		return
	}

	log.Printf("[DOWNLOAD] Download %s ready, generated presigned URL for %s", downloadID, objectKey)

	c.JSON(http.StatusOK, models.DownloadReadyResponse{
		DownloadID:  downloadID,
		Status:      "ready",
		DownloadURL: downloadURL,
		ExpiresIn:   expiresIn,
	})
}

// findDownloadObject finds the download object key by prefix.
// Returns empty string if not found.
func (h *Handler) findDownloadObject(ctx context.Context, prefix string) (string, error) {
	// Since we're checking for a single file in a specific directory,
	// we can use ObjectExists with the exact prefix pattern.
	// However, gocloud's blob.Bucket.Exists needs an exact key.
	// We'll need to use List to find the object.
	exists, err := h.storage.ObjectExistsWithPrefix(ctx, prefix)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", nil
	}

	// Get the full object key by listing
	key, err := h.storage.GetFirstObjectWithPrefix(ctx, prefix)
	if err != nil {
		return "", err
	}
	return key, nil
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
