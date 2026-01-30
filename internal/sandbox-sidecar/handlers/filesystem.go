package handlers

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/isola-ai/isola-sb/internal/sandbox-sidecar/proc"
)

// PostFilesystem godoc
// @Summary Write a file to the sandbox filesystem
// @Description Writes a file to the specified path in the sandbox container
// @Tags filesystem
// @Accept application/octet-stream
// @Produce json
// @Param path query string true "Destination path (absolute or relative to container cwd)"
// @Param container query string false "Container name (defaults to main container)"
// @Success 200 {object} FilesystemWriteResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /filesystem [post]
func (h *Handler) PostFilesystem(c *gin.Context) {
	path := c.Query("path")
	container := c.Query("container")

	if path == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Message: "path query parameter is required"})
		return
	}

	// Reject null bytes in path
	if strings.ContainsRune(path, 0) {
		c.JSON(http.StatusBadRequest, ErrorResponse{Message: "path contains invalid characters"})
		return
	}

	pid, err := h.procFS.FindMarkedPID()
	if err != nil {
		if errors.Is(err, proc.ErrContainerNotFound) {
			c.JSON(http.StatusInternalServerError, ErrorResponse{Message: "container not found"})
			return
		}
		h.logger.Error("failed to find container PID", "error", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Message: "failed to find container"})
		return
	}

	// Resolve the path
	var resolvedPath string
	if filepath.IsAbs(path) {
		resolvedPath = filepath.Clean(path)
	} else {
		cwd, err := h.procFS.GetCwd(pid)
		if err != nil {
			h.logger.Error("failed to get container cwd", "error", err, "pid", pid)
			c.JSON(http.StatusInternalServerError, ErrorResponse{Message: "failed to get container working directory"})
			return
		}
		resolvedPath = filepath.Clean(filepath.Join(cwd, path))
	}

	// Build the host path
	hostPath := filepath.Join(h.procFS.GetRoot(pid), resolvedPath)

	// Create parent directories if needed
	parentDir := filepath.Dir(hostPath)
	if err := os.MkdirAll(parentDir, 0750); err != nil { //nolint:gosec // intentional permissions for container access
		h.logger.Error("failed to create parent directories", "error", err, "path", parentDir)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Message: "failed to create parent directories"})
		return
	}

	// Create the destination file
	dst, err := os.Create(hostPath) //nolint:gosec // path derived from validated query param
	if err != nil {
		h.logger.Error("failed to create file", "error", err, "path", hostPath)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Message: "failed to create file"})
		return
	}
	defer func() { _ = dst.Close() }()

	// Stream the body to the file
	written, err := io.Copy(dst, c.Request.Body)
	if err != nil {
		h.logger.Error("failed to write file", "error", err, "path", hostPath)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Message: "failed to write file"})
		return
	}

	// Set file permissions
	if err := os.Chmod(hostPath, 0600); err != nil {
		h.logger.Error("failed to set file permissions", "error", err, "path", hostPath)
		// Don't fail the request, file was written successfully
	}

	c.JSON(http.StatusOK, FilesystemWriteResponse{
		Path:         resolvedPath,
		BytesWritten: written,
		Container:    container,
	})
}
