// Package handlers provides HTTP request handlers for the isola-agent API.
package handlers

import (
	"context"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/omereli/dev-isola/services/isola-agent/internal/storage"
)

// Environment variable keys
const (
	EnvSandboxDataPath = "SANDBOX_DATA_PATH"
)

// Default values
const (
	DefaultSandboxDataPath = "/sandbox-data"
	DownloadTimeoutSeconds = 300
)

// Handler holds dependencies for HTTP handlers.
type Handler struct {
	sandboxDataPath string
	storage         *storage.BlobStorage
}

// NewHandler creates a new Handler instance.
// It reads configuration from environment variables and initializes storage.
func NewHandler() (*Handler, error) {
	sandboxDataPath := os.Getenv(EnvSandboxDataPath)
	if sandboxDataPath == "" {
		sandboxDataPath = DefaultSandboxDataPath
	}

	// Storage is optional - only needed for s3 functionality
	blobStorage, err := storage.GetStorage()
	if err != nil {
		log.Printf("Warning: blob storage not initialized: %v", err)
		log.Printf("S3 functionality will be disabled")
		blobStorage = nil
	}

	return &Handler{
		sandboxDataPath: sandboxDataPath,
		storage:         blobStorage,
	}, nil
}

// HealthResponse is the response for the health check endpoint.
type HealthResponse struct {
	Status string `json:"status"`
}

// UploadResponse is the response for the upload endpoint.
type UploadResponse struct {
	Success bool   `json:"success"`
	Path    string `json:"path"`
	Size    int64  `json:"size"`
}

// DownloadRequest is the request body for the download endpoint.
type DownloadRequest struct {
	DownloadURL string `json:"download_url" binding:"required"`
	Path        string `json:"path" binding:"required"`
	S3Key       string `json:"s3_key,omitempty"`
	DeleteAfter bool   `json:"delete_after,omitempty"`
}

// DownloadResponse is the response for the download endpoint.
type DownloadResponse struct {
	Success       bool   `json:"success"`
	Path          string `json:"path"`
	Size          int64  `json:"size"`
	DeletedFromS3 bool   `json:"deleted_from_s3"`
}

// ErrorResponse is the response for error cases.
type ErrorResponse struct {
	Error string `json:"error"`
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

	// Sanitize path to prevent directory traversal
	cleanPath, err := h.sanitizePath(targetPath)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	// Get the file from form data
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "file is required"})
		return
	}

	// Open the uploaded file
	src, err := file.Open()
	if err != nil {
		log.Printf("Failed to open uploaded file: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to read uploaded file"})
		return
	}
	defer src.Close()

	// Construct full target path
	fullPath := filepath.Join(h.sandboxDataPath, cleanPath)

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

	// Copy file content
	written, err := io.Copy(dst, src)
	if err != nil {
		log.Printf("Failed to write file content to %s: %v", fullPath, err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to write file"})
		return
	}

	log.Printf("Successfully uploaded file to %s (size: %d bytes)", fullPath, written)

	c.JSON(http.StatusOK, UploadResponse{
		Success: true,
		Path:    fullPath,
		Size:    written,
	})
}

// Download handles POST /download requests.
// Downloads a file from a presigned S3 URL to the sandbox shared volume.
func (h *Handler) Download(c *gin.Context) {
	var req DownloadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "invalid request: " + err.Error()})
		return
	}

	// Sanitize path to prevent directory traversal
	cleanPath, err := h.sanitizePath(req.Path)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	// Construct full target path
	fullPath := filepath.Join(h.sandboxDataPath, cleanPath)

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
			log.Printf("Timeout downloading file from S3")
			c.JSON(http.StatusGatewayTimeout, ErrorResponse{Error: "timeout downloading file from S3"})
			return
		}
		log.Printf("Failed to download file from S3: %v", err)
		c.JSON(http.StatusBadGateway, ErrorResponse{Error: "failed to download file from S3: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("S3 download returned HTTP %d", resp.StatusCode)
		c.JSON(http.StatusBadGateway, ErrorResponse{Error: "failed to download file from S3: HTTP " + resp.Status})
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

	// Delete from S3 if requested
	deletedFromS3 := false
	if req.DeleteAfter {
		if req.S3Key == "" {
			log.Printf("delete_after=true but no s3_key provided, skipping S3 deletion")
		} else if h.storage == nil {
			log.Printf("delete_after=true but storage not configured, skipping S3 deletion")
		} else {
			deleted, err := h.storage.Delete(c.Request.Context(), req.S3Key)
			if err != nil {
				// Log but don't fail - the file was already written
				log.Printf("Error deleting S3 object %s: %v", req.S3Key, err)
			} else if deleted {
				log.Printf("Deleted S3 object: %s", req.S3Key)
				deletedFromS3 = true
			} else {
				log.Printf("Failed to delete S3 object: %s", req.S3Key)
			}
		}
	}

	c.JSON(http.StatusOK, DownloadResponse{
		Success:       true,
		Path:          fullPath,
		Size:          written,
		DeletedFromS3: deletedFromS3,
	})
}

// sanitizePath validates and cleans a file path to prevent directory traversal.
func (h *Handler) sanitizePath(path string) (string, error) {
	// Remove leading slashes and normalize
	cleanPath := strings.TrimLeft(path, "/")

	// Check for directory traversal attempts
	if strings.Contains(cleanPath, "..") {
		return "", &pathError{msg: "invalid path: directory traversal not allowed"}
	}

	// Clean the path to normalize any redundant separators
	cleanPath = filepath.Clean(cleanPath)

	// After cleaning, check again for any remaining ".." components
	if strings.HasPrefix(cleanPath, "..") || strings.Contains(cleanPath, "/..") {
		return "", &pathError{msg: "invalid path: directory traversal not allowed"}
	}

	return cleanPath, nil
}

type pathError struct {
	msg string
}

func (e *pathError) Error() string {
	return e.msg
}

// RegisterRoutes registers all HTTP routes on the given Gin engine.
func (h *Handler) RegisterRoutes(r *gin.Engine) {
	r.GET("/health", h.Health)
	r.POST("/upload", h.Upload)
	r.POST("/download", h.Download)
}
