// Package handlers provides HTTP request handlers for the isola-agent API.
package handlers

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
)

// Default values
const (
	DownloadTimeoutSeconds = 300
	fileSizeThresholdBytes = 5 * 1024 * 1024 // 5MB
)

type Handler struct {
	procFS *ProcFS
}

func NewHandler() (*Handler, error) {
	return &Handler{
		procFS: NewProcFS(),
	}, nil
}

type HealthResponse struct {
	Status string `json:"status"`
}

type UploadResponse struct {
	Success bool   `json:"success"`
	Path    string `json:"path"`
	Size    int64  `json:"size"`
}

type DownloadRequest struct {
	DownloadURL string `json:"download_url" binding:"required"`
	Path        string `json:"path" binding:"required"`
}

type DownloadResponse struct {
	Success bool   `json:"success"`
	Path    string `json:"path"`
	Size    int64  `json:"size"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

type ReadFileResponse struct {
	Success bool   `json:"success"`
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	Content []byte `json:"content"`
}

type FileInfoResponse struct {
	Success bool   `json:"success"`
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	Exists  bool   `json:"exists"`
}

type UploadToUrlRequest struct {
	UploadURL string `json:"upload_url" binding:"required"`
	Path      string `json:"path" binding:"required"`
}

type UploadToUrlResponse struct {
	Success bool   `json:"success"`
	Path    string `json:"path"`
	Size    int64  `json:"size"`
}

// Health handles GET /health requests.
func (h *Handler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, HealthResponse{Status: "healthy"})
}

// Upload handles POST /upload requests.
// Accepts multipart form with 'file' and 'path' fields.
func (h *Handler) Upload(c *gin.Context) {
	// Get the path from form data
	targetPath := c.PostForm("path")
	if targetPath == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "path is required"})
		return
	}

	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "file is required"})
		return
	}

	src, err := file.Open()
	if err != nil {
		log.Printf("Failed to open uploaded file: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to read uploaded file"})
		return
	}
	defer src.Close()

	// Resolve path via /proc/<pid>/root to access main container's filesystem
	fullPath, err := h.procFS.ResolvePath(targetPath)
	if err != nil {
		log.Printf("Failed to resolve path via procfs: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to resolve path: " + err.Error()})
		return
	}

	// Ensure parent directories exist
	parentDir := filepath.Dir(fullPath)
	if err := os.MkdirAll(parentDir, 0755); err != nil {
		log.Printf("Failed to create parent directories for %s: %v", fullPath, err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to create directories"})
		return
	}

	// Create the destination file
	dst, err := os.Create(fullPath)
	if err != nil {
		if os.IsPermission(err) {
			log.Printf("Permission denied writing to %s: %v", fullPath, err)
			c.JSON(http.StatusForbidden, ErrorResponse{Error: "permission denied: cannot write to " + targetPath})
			return
		}
		log.Printf("Failed to create file %s: %v", fullPath, err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to create file"})
		return
	}
	defer dst.Close()

	written, err := io.Copy(dst, src)
	if err != nil {
		log.Printf("Failed to write file content to %s: %v", fullPath, err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to write file"})
		return
	}

	log.Printf("Successfully uploaded file to %s (size: %d bytes)", fullPath, written)

	c.JSON(http.StatusOK, UploadResponse{
		Success: true,
		Path:    targetPath,
		Size:    written,
	})
}

// Download handles POST /download requests.
// Downloads a file from a presigned storage URL to the main container's filesystem.
func (h *Handler) Download(c *gin.Context) {
	var req DownloadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "invalid request: " + err.Error()})
		return
	}

	// Resolve path via /proc/<pid>/root to access main container's filesystem
	fullPath, err := h.procFS.ResolvePath(req.Path)
	if err != nil {
		log.Printf("Failed to resolve path via procfs: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to resolve path: " + err.Error()})
		return
	}

	// Ensure parent directories exist
	parentDir := filepath.Dir(fullPath)
	if err := os.MkdirAll(parentDir, 0755); err != nil {
		log.Printf("Failed to create parent directories for %s: %v", fullPath, err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to create directories"})
		return
	}

	// Download the file from the presigned URL
	ctx, cancel := context.WithTimeout(c.Request.Context(), DownloadTimeoutSeconds*time.Second)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, req.DownloadURL, nil)
	if err != nil {
		log.Printf("Failed to create download request: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to create download request"})
		return
	}

	client := &http.Client{}
	resp, err := client.Do(httpReq)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			log.Printf("Timeout downloading file from storage")
			c.JSON(http.StatusGatewayTimeout, ErrorResponse{Error: "timeout downloading file from storage"})
			return
		}
		log.Printf("Failed to download file from storage: %v", err)
		c.JSON(http.StatusBadGateway, ErrorResponse{Error: "failed to download file from storage: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Storage download returned HTTP %d", resp.StatusCode)
		c.JSON(http.StatusBadGateway, ErrorResponse{Error: "failed to download file from storage: HTTP " + resp.Status})
		return
	}

	// Create the destination file
	dst, err := os.Create(fullPath)
	if err != nil {
		if os.IsPermission(err) {
			log.Printf("Permission denied writing to %s: %v", fullPath, err)
			c.JSON(http.StatusForbidden, ErrorResponse{Error: "permission denied: cannot write to " + req.Path})
			return
		}
		log.Printf("Failed to create file %s: %v", fullPath, err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to create file"})
		return
	}
	defer dst.Close()

	// Copy file content
	written, err := io.Copy(dst, resp.Body)
	if err != nil {
		log.Printf("Failed to write file content to %s: %v", fullPath, err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to write file"})
		return
	}

	log.Printf("Successfully downloaded file to %s (size: %d bytes)", fullPath, written)

	c.JSON(http.StatusOK, DownloadResponse{
		Success: true,
		Path:    req.Path,
		Size:    written,
	})
}

// Reads a file from the main container's filesystem and returns its content.
func (h *Handler) ReadFile(c *gin.Context) {
	targetPath := c.Query("path")
	if targetPath == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "path query parameter is required"})
		return
	}

	// Resolve path via /proc/<pid>/root to access main container's filesystem
	fullPath, err := h.procFS.ResolvePath(targetPath)
	if err != nil {
		log.Printf("Failed to resolve path via procfs: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to resolve path: " + err.Error()})
		return
	}

	fileInfo, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			c.JSON(http.StatusNotFound, ErrorResponse{Error: "file not found: " + targetPath})
			return
		}
		if os.IsPermission(err) {
			c.JSON(http.StatusForbidden, ErrorResponse{Error: "permission denied: " + targetPath})
			return
		}
		log.Printf("Failed to stat file %s: %v", fullPath, err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to access file"})
		return
	}

	// Check if it's a directory
	if fileInfo.IsDir() {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "path is a directory, not a file"})
		return
	}

	// Check file size against threshold
	fileSize := fileInfo.Size()
	if fileSize >= fileSizeThresholdBytes {
		log.Printf("File too large for direct read: %d bytes (threshold: %d bytes)", fileSize, fileSizeThresholdBytes)
		c.JSON(http.StatusRequestEntityTooLarge, ErrorResponse{
			Error: fmt.Sprintf("file size (%d bytes) exceeds direct download limit (%d bytes). Use download-url endpoint for large files.", fileSize, fileSizeThresholdBytes),
		})
		return
	}

	content, err := os.ReadFile(fullPath)
	if err != nil {
		if os.IsPermission(err) {
			c.JSON(http.StatusForbidden, ErrorResponse{Error: "permission denied reading: " + targetPath})
			return
		}
		log.Printf("Failed to read file %s: %v", fullPath, err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to read file"})
		return
	}

	log.Printf("Successfully read file %s (size: %d bytes)", fullPath, len(content))

	c.JSON(http.StatusOK, ReadFileResponse{
		Success: true,
		Path:    targetPath,
		Size:    int64(len(content)),
		Content: content,
	})
}

// TODO: __OMER__ reduce code duplication
// GetFileInfo handles GET /file-info requests.
// Returns file metadata (size, exists) without reading the file content.
func (h *Handler) GetFileInfo(c *gin.Context) {
	targetPath := c.Query("path")
	if targetPath == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "path query parameter is required"})
		return
	}

	// Resolve path via /proc/<pid>/root to access main container's filesystem
	fullPath, err := h.procFS.ResolvePath(targetPath)
	if err != nil {
		log.Printf("Failed to resolve path via procfs: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to resolve path: " + err.Error()})
		return
	}

	fileInfo, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			c.JSON(http.StatusOK, FileInfoResponse{
				Success: true,
				Path:    targetPath,
				Size:    0,
				Exists:  false,
			})
			return
		}
		if os.IsPermission(err) {
			c.JSON(http.StatusForbidden, ErrorResponse{Error: "permission denied: " + targetPath})
			return
		}
		log.Printf("Failed to stat file %s: %v", fullPath, err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to access file"})
		return
	}

	// Check if it's a directory
	if fileInfo.IsDir() {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "path is a directory, not a file"})
		return
	}

	log.Printf("Successfully retrieved file info for %s (size: %d bytes)", fullPath, fileInfo.Size())

	c.JSON(http.StatusOK, FileInfoResponse{
		Success: true,
		Path:    targetPath,
		Size:    fileInfo.Size(),
		Exists:  true,
	})
}

// UploadToUrl handles POST /upload-to-url requests.
// Reads a file from the main container's filesystem and uploads it to a presigned storage URL.
func (h *Handler) UploadToUrl(c *gin.Context) {
	var req UploadToUrlRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "invalid request: " + err.Error()})
		return
	}

	// Resolve path via /proc/<pid>/root to access main container's filesystem
	fullPath, err := h.procFS.ResolvePath(req.Path)
	if err != nil {
		log.Printf("Failed to resolve path via procfs: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to resolve path: " + err.Error()})
		return
	}

	// Stat the file to check existence and get size
	fileInfo, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			c.JSON(http.StatusNotFound, ErrorResponse{Error: "file not found: " + req.Path})
			return
		}
		if os.IsPermission(err) {
			c.JSON(http.StatusForbidden, ErrorResponse{Error: "permission denied: " + req.Path})
			return
		}
		log.Printf("Failed to stat file %s: %v", fullPath, err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to access file"})
		return
	}

	// Check if it's a directory
	if fileInfo.IsDir() {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "path is a directory, not a file"})
		return
	}

	fileSize := fileInfo.Size()

	// Open the file for reading
	file, err := os.Open(fullPath)
	if err != nil {
		if os.IsPermission(err) {
			c.JSON(http.StatusForbidden, ErrorResponse{Error: "permission denied reading: " + req.Path})
			return
		}
		log.Printf("Failed to open file %s: %v", fullPath, err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to open file"})
		return
	}
	defer file.Close()

	// Create the HTTP PUT request with the file as body
	ctx, cancel := context.WithTimeout(c.Request.Context(), DownloadTimeoutSeconds*time.Second)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPut, req.UploadURL, file)
	if err != nil {
		log.Printf("Failed to create upload request: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to create upload request"})
		return
	}

	// Set content length for the upload
	httpReq.ContentLength = fileSize

	client := &http.Client{}
	resp, err := client.Do(httpReq)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			log.Printf("Timeout uploading file to storage")
			c.JSON(http.StatusGatewayTimeout, ErrorResponse{Error: "timeout uploading file to storage"})
			return
		}
		log.Printf("Failed to upload file to storage: %v", err)
		c.JSON(http.StatusBadGateway, ErrorResponse{Error: "failed to upload file to storage: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	// Presigned PUT returns 200 OK on success
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("Storage upload returned HTTP %d: %s", resp.StatusCode, string(body))
		c.JSON(http.StatusBadGateway, ErrorResponse{Error: fmt.Sprintf("failed to upload file to storage: HTTP %s", resp.Status)})
		return
	}

	log.Printf("Successfully uploaded file %s to storage (size: %d bytes)", fullPath, fileSize)

	c.JSON(http.StatusOK, UploadToUrlResponse{
		Success: true,
		Path:    req.Path,
		Size:    fileSize,
	})
}

// RegisterRoutes registers all HTTP routes on the given Gin engine.
func (h *Handler) RegisterRoutes(r *gin.Engine) {
	r.GET("/health", h.Health)
	r.POST("/upload", h.Upload)
	r.POST("/download", h.Download)
	r.GET("/read-file", h.ReadFile)
	r.GET("/file-info", h.GetFileInfo)
	r.POST("/upload-to-url", h.UploadToUrl)
}
