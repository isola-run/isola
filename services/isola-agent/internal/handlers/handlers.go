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

type FileInfoResponse struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	Exists bool   `json:"exists"`
	IsDir  bool   `json:"is_dir"`
}

type UploadToStorageRequest struct {
	Path      string `json:"path" binding:"required"`
	UploadURL string `json:"upload_url" binding:"required"`
}

type UploadToStorageResponse struct {
	Status string `json:"status"`
	Path   string `json:"path"`
	Size   int64  `json:"size"`
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
	defer func() {
		if err := src.Close(); err != nil {
			log.Printf("Warning: failed to close uploaded file: %v", err)
		}
	}()

	// Resolve path via /proc/<pid>/root to access main container's filesystem
	fullPath, err := h.procFS.ResolvePath(targetPath)
	if err != nil {
		log.Printf("Failed to resolve path via procfs: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to resolve path: " + err.Error()})
		return
	}

	// Ensure parent directories exist
	parentDir := filepath.Dir(fullPath)
	if err := os.MkdirAll(parentDir, 0750); err != nil {
		log.Printf("Failed to create parent directories for %s: %v", fullPath, err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to create directories"})
		return
	}

	// Create the destination file
	dst, err := os.Create(fullPath) //nolint:gosec // path is unsanitized but sandbox filesystem is isolated
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
	defer func() {
		if err := dst.Close(); err != nil {
			log.Printf("Warning: failed to close destination file: %v", err)
		}
	}()

	written, err := io.Copy(dst, src)
	if err != nil {
		log.Printf("Failed to write file content to %s: %v", fullPath, err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to write file"})
		return
	}

	// Explicitly close and check error before responding - ensures data is flushed
	if err := dst.Close(); err != nil {
		log.Printf("Failed to close file %s: %v", fullPath, err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to finalize file write"})
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
	if err := os.MkdirAll(parentDir, 0750); err != nil {
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
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("Warning: failed to close response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Storage download returned HTTP %d", resp.StatusCode)
		c.JSON(http.StatusBadGateway, ErrorResponse{Error: "failed to download file from storage: HTTP " + resp.Status})
		return
	}

	// Create the destination file
	dst, err := os.Create(fullPath) //nolint:gosec // path is unsanitized but sandbox filesystem is isolated
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
	defer func() {
		if err := dst.Close(); err != nil {
			log.Printf("Warning: failed to close destination file: %v", err)
		}
	}()

	// Copy file content
	written, err := io.Copy(dst, resp.Body)
	if err != nil {
		log.Printf("Failed to write file content to %s: %v", fullPath, err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to write file"})
		return
	}

	// Explicitly close and check error before responding - ensures data is flushed
	if err := dst.Close(); err != nil {
		log.Printf("Failed to close file %s: %v", fullPath, err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to finalize file write"})
		return
	}

	log.Printf("Successfully downloaded file to %s (size: %d bytes)", fullPath, written)

	c.JSON(http.StatusOK, DownloadResponse{
		Success: true,
		Path:    req.Path,
		Size:    written,
	})
}

// ReadFile streams a file from the main container's filesystem directly to the client.
func (h *Handler) ReadFile(c *gin.Context) {
	targetPath := c.Query("path")
	if targetPath == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "path query parameter is required"})
		return
	}

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

	if fileInfo.IsDir() {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "path is a directory, not a file"})
		return
	}

	fileSize := fileInfo.Size()
	if fileSize >= fileSizeThresholdBytes {
		log.Printf("File too large for direct read: %d bytes (threshold: %d bytes)", fileSize, fileSizeThresholdBytes)
		c.JSON(http.StatusRequestEntityTooLarge, ErrorResponse{
			Error: fmt.Sprintf("file size (%d bytes) exceeds direct download limit (%d bytes). Use download-url endpoint for large files.", fileSize, fileSizeThresholdBytes),
		})
		return
	}

	file, err := os.Open(fullPath) //nolint:gosec // path is resolved via procfs in sandboxed environment
	if err != nil {
		if os.IsPermission(err) {
			c.JSON(http.StatusForbidden, ErrorResponse{Error: "permission denied reading: " + targetPath})
			return
		}
		log.Printf("Failed to open file %s: %v", fullPath, err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to open file"})
		return
	}
	defer func() {
		if err := file.Close(); err != nil {
			log.Printf("Warning: failed to close file: %v", err)
		}
	}()

	fileName := filepath.Base(targetPath)
	log.Printf("Streaming file %s (size: %d bytes)", fullPath, fileSize)

	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", fileName))
	c.DataFromReader(http.StatusOK, fileSize, "application/octet-stream", file, nil)
}

// FileInfo returns metadata about a file in the main container's filesystem.
func (h *Handler) FileInfo(c *gin.Context) {
	targetPath := c.Query("path")
	if targetPath == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "path query parameter is required"})
		return
	}

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
				Path:   targetPath,
				Exists: false,
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

	c.JSON(http.StatusOK, FileInfoResponse{
		Path:   targetPath,
		Size:   fileInfo.Size(),
		Exists: true,
		IsDir:  fileInfo.IsDir(),
	})
}

// UploadToStorage handles POST /upload-to-storage requests.
// Uploads a file from the sandbox filesystem to a presigned storage URL in the background.
func (h *Handler) UploadToStorage(c *gin.Context) {
	var req UploadToStorageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "invalid request: " + err.Error()})
		return
	}

	fullPath, err := h.procFS.ResolvePath(req.Path)
	if err != nil {
		log.Printf("Failed to resolve path via procfs: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to resolve path: " + err.Error()})
		return
	}

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

	if fileInfo.IsDir() {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "path is a directory, not a file"})
		return
	}

	fileSize := fileInfo.Size()

	// Start upload in background goroutine
	go h.uploadFileToStorage(fullPath, req.Path, req.UploadURL, fileSize)

	log.Printf("Initiated background upload of %s (size: %d bytes) to storage", fullPath, fileSize)

	c.JSON(http.StatusAccepted, UploadToStorageResponse{
		Status: "uploading",
		Path:   req.Path,
		Size:   fileSize,
	})
}

// uploadFileToStorage uploads a file to a presigned URL in the background.
func (h *Handler) uploadFileToStorage(fullPath, originalPath, uploadURL string, fileSize int64) {
	file, err := os.Open(fullPath) //nolint:gosec // path is resolved via procfs in sandboxed environment
	if err != nil {
		log.Printf("Background upload failed - could not open file %s: %v", fullPath, err)
		return
	}
	defer func() {
		if err := file.Close(); err != nil {
			log.Printf("Warning: failed to close file during background upload: %v", err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), DownloadTimeoutSeconds*time.Second)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPut, uploadURL, file)
	if err != nil {
		log.Printf("Background upload failed - could not create request for %s: %v", originalPath, err)
		return
	}

	httpReq.ContentLength = fileSize
	httpReq.Header.Set("Content-Type", "application/octet-stream")

	client := &http.Client{}
	resp, err := client.Do(httpReq)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			log.Printf("Background upload timed out for %s", originalPath)
			return
		}
		log.Printf("Background upload failed for %s: %v", originalPath, err)
		return
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("Warning: failed to close response body: %v", err)
		}
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("Background upload failed for %s: HTTP %d %s", originalPath, resp.StatusCode, resp.Status)
		return
	}

	log.Printf("Successfully uploaded %s to storage (size: %d bytes)", originalPath, fileSize)
}

// RegisterRoutes registers all HTTP routes on the given Gin engine.
func (h *Handler) RegisterRoutes(r *gin.Engine) {
	r.GET("/health", h.Health)
	r.POST("/upload", h.Upload)
	r.POST("/download", h.Download)
	r.GET("/read-file", h.ReadFile)
	r.GET("/file-info", h.FileInfo)
	r.POST("/upload-to-storage", h.UploadToStorage)
}
