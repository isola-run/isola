// Package handlers provides HTTP request handlers for the isola-agent API.
package handlers

import (
	"context"
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
// Downloads a file from a presigned S3 URL to the main container's filesystem.
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

	c.JSON(http.StatusOK, DownloadResponse{
		Success: true,
		Path:    req.Path,
		Size:    written,
	})
}

// RegisterRoutes registers all HTTP routes on the given Gin engine.
func (h *Handler) RegisterRoutes(r *gin.Engine) {
	r.GET("/health", h.Health)
	r.POST("/upload", h.Upload)
	r.POST("/download", h.Download)
}
