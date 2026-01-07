// Package handlers provides HTTP handlers for the isola-gw API.
package handlers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/omereli/dev-isola/services/isola-gw/internal/kubernetes"
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

// getSandboxStatusAndAddress retrieves sandbox status and validates it's running.
// Returns (status, agentAddress, shouldReturn) where shouldReturn is true if an error response was sent.
func (h *Handler) getSandboxStatusAndAddress(ctx context.Context, c *gin.Context, sandboxID string, logPrefix string) (*kubernetes.SandboxStatus, string, bool) {
	status, err := h.k8sManager.GetSandboxStatus(ctx, sandboxID)
	if err != nil {
		log.Printf("[%s] Failed to get sandbox %s status: %v", logPrefix, sandboxID, err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:   "InternalServerError",
			Message: "Failed to get sandbox status",
		})
		return nil, "", true
	}
	if status == nil {
		log.Printf("[%s] Sandbox %s not found", logPrefix, sandboxID)
		c.JSON(http.StatusNotFound, models.ErrorResponse{
			Error:   "NotFound",
			Message: "Sandbox not found",
		})
		return nil, "", true
	}

	if status.State != models.SandboxStateRunning {
		log.Printf("[%s] Sandbox %s not in running state: %s", logPrefix, sandboxID, status.State)
		c.JSON(http.StatusConflict, models.ErrorResponse{
			Error:   "Conflict",
			Message: "Sandbox must be in 'running' state, current state: " + string(status.State),
		})
		return nil, "", true
	}

	return status, status.AgentAddress, false
}

func (h *Handler) UploadFile(c *gin.Context) {
	sandboxID := c.Param("id")

	// Get tenant ID from context
	tenantID, _ := c.Get("tenant_id")
	_ = tenantID

	ctx := c.Request.Context()

	_, agentAddress, shouldReturn := h.getSandboxStatusAndAddress(ctx, c, sandboxID, "UPLOAD")
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

	agentURL := fmt.Sprintf("http://%s:%d/upload", agentAddress, agentSidecarPort)
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

// DownloadFile handles GET /sandboxes/:id/files?path=...
// For small files (under 5MB), reads the file directly from the sandbox agent.
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

	_, agentAddress, shouldReturn := h.getSandboxStatusAndAddress(ctx, c, sandboxID, "DOWNLOAD")
	if shouldReturn {
		return
	}

	// Call agent's /read-file endpoint
	agentURL := fmt.Sprintf("http://%s:%d/read-file?path=%s", agentAddress, agentSidecarPort, targetPath)
	log.Printf("[DOWNLOAD] Forwarding to agent at %s", agentURL)

	req, err := http.NewRequestWithContext(ctx, "GET", agentURL, nil)
	if err != nil {
		log.Printf("[DOWNLOAD] Failed to create request: %v", err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:   "InternalServerError",
			Message: "Failed to create request: " + err.Error(),
		})
		return
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[DOWNLOAD] Failed to connect to agent at %s: %v", agentURL, err)
		c.JSON(http.StatusBadGateway, models.ErrorResponse{
			Error:   "BadGateway",
			Message: "Failed to connect to sandbox agent: " + err.Error(),
		})
		return
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)

	// Handle error responses from agent
	if resp.StatusCode != http.StatusOK {
		log.Printf("[DOWNLOAD] Agent returned error: %d - %s", resp.StatusCode, string(bodyBytes))

		// Try to parse agent error response
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

	var agentResponse struct {
		Success bool   `json:"success"`
		Path    string `json:"path"`
		Size    int64  `json:"size"`
		Content []byte `json:"content"`
	}
	if err := json.Unmarshal(bodyBytes, &agentResponse); err != nil {
		log.Printf("[DOWNLOAD] Failed to parse agent response: %v", err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:   "InternalServerError",
			Message: "Failed to parse agent response: " + err.Error(),
		})
		return
	}

	log.Printf("[DOWNLOAD] Successfully downloaded file from sandbox %s: path=%s, size=%d", sandboxID, agentResponse.Path, agentResponse.Size)

	c.JSON(http.StatusOK, models.FileDownloadResponse{
		Path:    agentResponse.Path,
		Size:    agentResponse.Size,
		Content: base64.StdEncoding.EncodeToString(agentResponse.Content),
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

	_, _, shouldReturn := h.getSandboxStatusAndAddress(ctx, c, sandboxID, "UPLOAD-URL")
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

// GenerateDownloadUrl handles POST /sandboxes/:id/files/download-url
// For large files (over 5MB), orchestrates uploading the file from sandbox to S3
// and returns a presigned download URL to the client.
func (h *Handler) GenerateDownloadUrl(c *gin.Context) {
	sandboxID := c.Param("id")

	var req models.DownloadUrlRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:   "BadRequest",
			Message: err.Error(),
		})
		return
	}

	log.Printf("[DOWNLOAD-URL] Request for sandbox %s: path=%s", sandboxID, req.Path)

	tenantID, _ := c.Get("tenant_id")
	tenantIDStr := tenantID.(string)

	ctx := c.Request.Context()

	if h.storage == nil {
		c.JSON(http.StatusNotImplemented, models.ErrorResponse{
			Error:   "NotImplemented",
			Message: "Storage not configured. Large file downloads are not available.",
		})
		return
	}

	_, agentAddress, shouldReturn := h.getSandboxStatusAndAddress(ctx, c, sandboxID, "DOWNLOAD-URL")
	if shouldReturn {
		return
	}

	// Step 1: Call agent's /file-info to get file size and check existence
	// TODO: __OMER__ avoid this step
	fileInfoURL := fmt.Sprintf("http://%s:%d/file-info?path=%s", agentAddress, agentSidecarPort, req.Path)
	log.Printf("[DOWNLOAD-URL] Getting file info from agent at %s", fileInfoURL)

	fileInfoReq, err := http.NewRequestWithContext(ctx, "GET", fileInfoURL, nil)
	if err != nil {
		log.Printf("[DOWNLOAD-URL] Failed to create file-info request: %v", err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:   "InternalServerError",
			Message: "Failed to create request: " + err.Error(),
		})
		return
	}

	client := &http.Client{Timeout: 30 * time.Second}
	fileInfoResp, err := client.Do(fileInfoReq)
	if err != nil {
		log.Printf("[DOWNLOAD-URL] Failed to connect to agent at %s: %v", fileInfoURL, err)
		c.JSON(http.StatusBadGateway, models.ErrorResponse{
			Error:   "BadGateway",
			Message: "Failed to connect to sandbox agent: " + err.Error(),
		})
		return
	}
	defer fileInfoResp.Body.Close()

	fileInfoBody, _ := io.ReadAll(fileInfoResp.Body)

	if fileInfoResp.StatusCode != http.StatusOK {
		log.Printf("[DOWNLOAD-URL] Agent file-info returned error: %d - %s", fileInfoResp.StatusCode, string(fileInfoBody))
		var agentError struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(fileInfoBody, &agentError); err == nil && agentError.Error != "" {
			c.JSON(fileInfoResp.StatusCode, models.ErrorResponse{
				Error:   "AgentError",
				Message: agentError.Error,
			})
			return
		}
		c.JSON(fileInfoResp.StatusCode, models.ErrorResponse{
			Error:   "InternalServerError",
			Message: "Agent file-info failed: " + string(fileInfoBody),
		})
		return
	}

	var fileInfo models.FileInfoResponse
	if err := json.Unmarshal(fileInfoBody, &fileInfo); err != nil {
		log.Printf("[DOWNLOAD-URL] Failed to parse file-info response: %v", err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:   "InternalServerError",
			Message: "Failed to parse file-info response: " + err.Error(),
		})
		return
	}

	if !fileInfo.Exists {
		log.Printf("[DOWNLOAD-URL] File not found: %s", req.Path)
		c.JSON(http.StatusNotFound, models.ErrorResponse{
			Error:   "NotFound",
			Message: "File not found: " + req.Path,
		})
		return
	}

	log.Printf("[DOWNLOAD-URL] File info: path=%s, size=%d", fileInfo.Path, fileInfo.Size)

	// Step 2: Generate unique download ID and S3 object key
	downloadID := uuid.New().String()
	filename := filepath.Base(req.Path)
	objectKey := buildObjectKey("downloads", tenantIDStr, sandboxID, downloadID, filename)

	// Step 3: Generate presigned PUT URL for the agent to upload to S3
	expiresIn := 900 // 15 minutes
	uploadURL, err := h.storage.GeneratePresignedUploadURL(ctx, objectKey, expiresIn, "")
	if err != nil {
		log.Printf("[DOWNLOAD-URL] Failed to generate presigned upload URL: %v", err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:   "InternalServerError",
			Message: "Failed to generate presigned URL: " + err.Error(),
		})
		return
	}

	log.Printf("[DOWNLOAD-URL] Generated presigned upload URL for object: %s", objectKey)

	// Step 4: Tell agent to upload file to the presigned URL
	uploadToUrlReq := struct {
		UploadURL string `json:"upload_url"`
		Path      string `json:"path"`
	}{
		UploadURL: uploadURL,
		Path:      req.Path,
	}

	uploadToUrlBody, err := json.Marshal(uploadToUrlReq)
	if err != nil {
		log.Printf("[DOWNLOAD-URL] Failed to marshal upload-to-url request: %v", err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:   "InternalServerError",
			Message: "Failed to marshal request: " + err.Error(),
		})
		return
	}

	agentUploadURL := fmt.Sprintf("http://%s:%d/upload-to-url", agentAddress, agentSidecarPort)
	log.Printf("[DOWNLOAD-URL] Triggering agent upload at %s", agentUploadURL)

	uploadReq, err := http.NewRequestWithContext(ctx, "POST", agentUploadURL, bytes.NewReader(uploadToUrlBody))
	if err != nil {
		log.Printf("[DOWNLOAD-URL] Failed to create upload-to-url request: %v", err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:   "InternalServerError",
			Message: "Failed to create request: " + err.Error(),
		})
		return
	}
	uploadReq.Header.Set("Content-Type", "application/json")

	// Use a longer timeout for large file uploads
	uploadClient := &http.Client{Timeout: 5 * time.Minute}
	uploadResp, err := uploadClient.Do(uploadReq)
	if err != nil {
		log.Printf("[DOWNLOAD-URL] Failed to connect to agent for upload: %v", err)
		c.JSON(http.StatusBadGateway, models.ErrorResponse{
			Error:   "BadGateway",
			Message: "Failed to connect to sandbox agent: " + err.Error(),
		})
		return
	}
	defer uploadResp.Body.Close()

	if uploadResp.StatusCode != http.StatusOK {
		uploadRespBody, _ := io.ReadAll(uploadResp.Body)
		log.Printf("[DOWNLOAD-URL] Agent upload-to-url returned error: %d - %s", uploadResp.StatusCode, string(uploadRespBody))
		var agentError struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(uploadRespBody, &agentError); err == nil && agentError.Error != "" {
			c.JSON(uploadResp.StatusCode, models.ErrorResponse{
				Error:   "AgentError",
				Message: agentError.Error,
			})
			return
		}
		c.JSON(uploadResp.StatusCode, models.ErrorResponse{
			Error:   "InternalServerError",
			Message: "Agent upload-to-url failed: " + string(uploadRespBody),
		})
		return
	}

	log.Printf("[DOWNLOAD-URL] Agent successfully uploaded file to S3")

	// Step 5: Generate presigned GET URL for the client to download
	// TODO: __OMER__ what is the expiration time?
	downloadURL, err := h.storage.GeneratePresignedDownloadURL(ctx, objectKey, expiresIn)
	if err != nil {
		log.Printf("[DOWNLOAD-URL] Failed to generate presigned download URL: %v", err)
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:   "InternalServerError",
			Message: "Failed to generate download URL: " + err.Error(),
		})
		return
	}

	log.Printf("[DOWNLOAD-URL] Generated presigned download URL for download_id=%s", downloadID)

	c.JSON(http.StatusOK, models.DownloadUrlResponse{
		DownloadURL: downloadURL,
		DownloadID:  downloadID,
		ExpiresIn:   expiresIn,
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

	_, agentAddress, shouldReturn := h.getSandboxStatusAndAddress(ctx, c, sandboxID, "CONFIRM")
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
