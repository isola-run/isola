// Package handlers provides HTTP request handlers for the isola-agent API.
package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/go-chi/chi/v5"
)

// Default values
const (
	DownloadTimeoutSeconds = 300
	fileSizeThresholdBytes = 5 * 1024 * 1024 // 5MB
)

type Handler struct {
	procFS *ProcFS
	logger *slog.Logger
}

func NewHandler(logger *slog.Logger) (*Handler, error) {
	return &Handler{
		procFS: NewProcFS(),
		logger: logger,
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
	DownloadURL string `json:"download_url"`
	Path        string `json:"path"`
}

type DownloadResponse struct {
	Success bool   `json:"success"`
	Path    string `json:"path"`
	Size    int64  `json:"size"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

func (h *Handler) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (h *Handler) writeError(w http.ResponseWriter, status int, msg string) {
	h.writeJSON(w, status, ErrorResponse{Error: msg})
}

// Health handles GET /health requests.
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	h.writeJSON(w, http.StatusOK, HealthResponse{Status: "healthy"})
}

// Upload handles POST /upload requests.
// Accepts multipart form with 'file' and 'path' fields.
func (h *Handler) Upload(w http.ResponseWriter, r *http.Request) {
	// Parse multipart form (32MB max memory)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		h.writeError(w, http.StatusBadRequest, "failed to parse multipart form")
		return
	}

	// Get the path from form data
	targetPath := r.FormValue("path")
	if targetPath == "" {
		h.writeError(w, http.StatusBadRequest, "path is required")
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "file is required")
		return
	}
	defer func() {
		if err := file.Close(); err != nil {
			h.logger.Warn("failed to close uploaded file", "error", err)
		}
	}()

	// Resolve path via /proc/<pid>/root to access main container's filesystem
	fullPath, err := h.procFS.ResolvePath(targetPath)
	if err != nil {
		h.logger.Error("failed to resolve path via procfs", "error", err)
		h.writeError(w, http.StatusInternalServerError, "failed to resolve path: "+err.Error())
		return
	}

	// Ensure parent directories exist
	parentDir := filepath.Dir(fullPath)
	if err := os.MkdirAll(parentDir, 0750); err != nil {
		h.logger.Error("failed to create parent directories", "path", fullPath, "error", err)
		h.writeError(w, http.StatusInternalServerError, "failed to create directories")
		return
	}

	// Create the destination file
	dst, err := os.Create(fullPath) //nolint:gosec // path is unsanitized but sandbox filesystem is isolated
	if err != nil {
		if os.IsPermission(err) {
			h.logger.Warn("permission denied writing file", "path", fullPath, "error", err)
			h.writeError(w, http.StatusForbidden, "permission denied: cannot write to "+targetPath)
			return
		}
		h.logger.Error("failed to create file", "path", fullPath, "error", err)
		h.writeError(w, http.StatusInternalServerError, "failed to create file")
		return
	}
	defer func() {
		if err := dst.Close(); err != nil {
			h.logger.Warn("failed to close destination file", "error", err)
		}
	}()

	written, err := io.Copy(dst, file)
	if err != nil {
		h.logger.Error("failed to write file content", "path", fullPath, "error", err)
		h.writeError(w, http.StatusInternalServerError, "failed to write file")
		return
	}

	// Explicitly close and check error before responding - ensures data is flushed
	if err := dst.Close(); err != nil {
		h.logger.Error("failed to close file", "path", fullPath, "error", err)
		h.writeError(w, http.StatusInternalServerError, "failed to finalize file write")
		return
	}

	h.logger.Info("uploaded file", "path", fullPath, "size", written)

	h.writeJSON(w, http.StatusOK, UploadResponse{
		Success: true,
		Path:    targetPath,
		Size:    written,
	})
}

// Download handles POST /download requests.
// Downloads a file from a presigned storage URL to the main container's filesystem.
func (h *Handler) Download(w http.ResponseWriter, r *http.Request) {
	var req DownloadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}

	if req.DownloadURL == "" {
		h.writeError(w, http.StatusBadRequest, "download_url is required")
		return
	}
	if req.Path == "" {
		h.writeError(w, http.StatusBadRequest, "path is required")
		return
	}

	// Resolve path via /proc/<pid>/root to access main container's filesystem
	fullPath, err := h.procFS.ResolvePath(req.Path)
	if err != nil {
		h.logger.Error("failed to resolve path via procfs", "error", err)
		h.writeError(w, http.StatusInternalServerError, "failed to resolve path: "+err.Error())
		return
	}

	// Ensure parent directories exist
	parentDir := filepath.Dir(fullPath)
	if err := os.MkdirAll(parentDir, 0750); err != nil {
		h.logger.Error("failed to create parent directories", "path", fullPath, "error", err)
		h.writeError(w, http.StatusInternalServerError, "failed to create directories")
		return
	}

	// Download the file from the presigned URL
	ctx, cancel := context.WithTimeout(r.Context(), DownloadTimeoutSeconds*time.Second)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, req.DownloadURL, nil)
	if err != nil {
		h.logger.Error("failed to create download request", "error", err)
		h.writeError(w, http.StatusInternalServerError, "failed to create download request")
		return
	}

	client := &http.Client{}
	resp, err := client.Do(httpReq)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			h.logger.Warn("timeout downloading file from storage")
			h.writeError(w, http.StatusGatewayTimeout, "timeout downloading file from storage")
			return
		}
		h.logger.Error("failed to download file from storage", "error", err)
		h.writeError(w, http.StatusBadGateway, "failed to download file from storage: "+err.Error())
		return
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			h.logger.Warn("failed to close response body", "error", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		h.logger.Warn("storage download returned error", "status", resp.StatusCode)
		h.writeError(w, http.StatusBadGateway, "failed to download file from storage: HTTP "+resp.Status)
		return
	}

	// Create the destination file
	dst, err := os.Create(fullPath) //nolint:gosec // path is unsanitized but sandbox filesystem is isolated
	if err != nil {
		if os.IsPermission(err) {
			h.logger.Warn("permission denied writing file", "path", fullPath, "error", err)
			h.writeError(w, http.StatusForbidden, "permission denied: cannot write to "+req.Path)
			return
		}
		h.logger.Error("failed to create file", "path", fullPath, "error", err)
		h.writeError(w, http.StatusInternalServerError, "failed to create file")
		return
	}
	defer func() {
		if err := dst.Close(); err != nil {
			h.logger.Warn("failed to close destination file", "error", err)
		}
	}()

	// Copy file content
	written, err := io.Copy(dst, resp.Body)
	if err != nil {
		h.logger.Error("failed to write file content", "path", fullPath, "error", err)
		h.writeError(w, http.StatusInternalServerError, "failed to write file")
		return
	}

	// Explicitly close and check error before responding - ensures data is flushed
	if err := dst.Close(); err != nil {
		h.logger.Error("failed to close file", "path", fullPath, "error", err)
		h.writeError(w, http.StatusInternalServerError, "failed to finalize file write")
		return
	}

	h.logger.Info("downloaded file", "path", fullPath, "size", written)

	h.writeJSON(w, http.StatusOK, DownloadResponse{
		Success: true,
		Path:    req.Path,
		Size:    written,
	})
}

// ReadFile streams a file from the main container's filesystem directly to the client.
func (h *Handler) ReadFile(w http.ResponseWriter, r *http.Request) {
	targetPath := r.URL.Query().Get("path")
	if targetPath == "" {
		h.writeError(w, http.StatusBadRequest, "path query parameter is required")
		return
	}

	fullPath, err := h.procFS.ResolvePath(targetPath)
	if err != nil {
		h.logger.Error("failed to resolve path via procfs", "error", err)
		h.writeError(w, http.StatusInternalServerError, "failed to resolve path: "+err.Error())
		return
	}

	fileInfo, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			h.writeError(w, http.StatusNotFound, "file not found: "+targetPath)
			return
		}
		if os.IsPermission(err) {
			h.writeError(w, http.StatusForbidden, "permission denied: "+targetPath)
			return
		}
		h.logger.Error("failed to stat file", "path", fullPath, "error", err)
		h.writeError(w, http.StatusInternalServerError, "failed to access file")
		return
	}

	if fileInfo.IsDir() {
		h.writeError(w, http.StatusBadRequest, "path is a directory, not a file")
		return
	}

	fileSize := fileInfo.Size()
	if fileSize >= fileSizeThresholdBytes {
		h.logger.Warn("file too large for direct read", "size", fileSize, "threshold", fileSizeThresholdBytes)
		h.writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf(
			"file size (%d bytes) exceeds direct download limit (%d bytes). Use download-url endpoint for large files.",
			fileSize, fileSizeThresholdBytes,
		))
		return
	}

	file, err := os.Open(fullPath) //nolint:gosec // path is resolved via procfs in sandboxed environment
	if err != nil {
		if os.IsPermission(err) {
			h.writeError(w, http.StatusForbidden, "permission denied reading: "+targetPath)
			return
		}
		h.logger.Error("failed to open file", "path", fullPath, "error", err)
		h.writeError(w, http.StatusInternalServerError, "failed to open file")
		return
	}
	defer func() {
		if err := file.Close(); err != nil {
			h.logger.Warn("failed to close file", "error", err)
		}
	}()

	fileName := filepath.Base(targetPath)
	h.logger.Info("streaming file", "path", fullPath, "size", fileSize)

	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", fileName))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", fileSize))
	_, _ = io.Copy(w, file)
}

// RegisterRoutes registers all HTTP routes on the given Chi router.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Get("/health", h.Health)
	r.Post("/upload", h.Upload)
	r.Post("/download", h.Download)
	r.Get("/read-file", h.ReadFile)
}
