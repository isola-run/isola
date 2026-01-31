package handlers

import (
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/go-chi/render"

	"github.com/isola-ai/isola-sb/internal/sandbox-sidecar/proc"
)

type FilesystemHandler struct {
	logger *slog.Logger
	procFS proc.ProcFS

	// PID cache to avoid repeated /proc scans (containerName -> pid)
	pidMu      sync.RWMutex
	cachedPIDs map[string]int
}

func NewFilesystemHandler(logger *slog.Logger, procFS proc.ProcFS) *FilesystemHandler {
	return &FilesystemHandler{
		logger:     logger,
		procFS:     procFS,
		cachedPIDs: make(map[string]int),
	}
}

// findContainerPID returns the PID for the given container, using a cache to avoid repeated /proc scans.
// Validates cached PID still has the expected ISOLA_CONTAINER_NAME marker before returning.
func (h *FilesystemHandler) findCachedContainerPID(containerName string) (int, error) {
	h.pidMu.RLock()
	pid, ok := h.cachedPIDs[containerName]
	h.pidMu.RUnlock()

	// Validate cached PID still has the expected marker
	if ok {
		if name, found := proc.GetContainerName(pid); found && (containerName == "" || name == containerName) {
			return pid, nil
		}
	}

	// Cache miss or stale - rescan
	newPID, err := h.procFS.FindMarkedPID(containerName)
	if err != nil {
		return 0, err
	}

	h.pidMu.Lock()
	h.cachedPIDs[containerName] = newPID
	h.pidMu.Unlock()

	return newPID, nil
}

func resolveAbsolutePath(path string, pid int, procFS proc.ProcFS) (string, error) {
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	cwd, err := procFS.GetCwd(pid)
	if err != nil {
		return "", err
	}
	relativePath := filepath.Join(cwd, path) // returns a Clean path
	return filepath.Abs(relativePath)
}

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
func (h *FilesystemHandler) PostFilesystem(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	container := r.URL.Query().Get("container")

	if path == "" {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, ErrorResponse{Message: "path query parameter is required"})
		return
	}

	// Reject null bytes in path
	if strings.ContainsRune(path, 0) {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, ErrorResponse{Message: "path contains invalid characters"})
		return
	}

	pid, err := h.findCachedContainerPID(container)
	if err != nil {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, ErrorResponse{Message: "container not found"})
		return
	}

	uid, gid, err := h.procFS.GetUIDGID(pid)
	if err != nil {
		h.logger.Error("failed to get container uid/gid", "error", err, "pid", pid)
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, ErrorResponse{Message: "failed to get container uid/gid"})
		return
	}

	resolvedPath, err := resolveAbsolutePath(path, pid, h.procFS)
	if err != nil {
		h.logger.Error("failed to resolve path", "error", err, "pid", pid)
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, ErrorResponse{Message: "failed to resolve path"})
		return
	}

	// Build the host path via /proc/<pid>/root
	targetPath := filepath.Join(h.procFS.GetRoot(pid), resolvedPath)

	parentDir := filepath.Dir(targetPath)
	if err := os.MkdirAll(parentDir, 0755); err != nil { //nolint:gosec // intentional permissions for container access
		h.logger.Error("failed to create parent directories", "error", err, "path", parentDir)
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, ErrorResponse{Message: "failed to create parent directories"})
		return
	}

	dst, err := os.Create(targetPath) //nolint:gosec
	if err != nil {
		h.logger.Error("failed to create file", "error", err, "path", targetPath)
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, ErrorResponse{Message: "failed to create file"})
		return
	}
	defer func() { _ = dst.Close() }()

	// Stream the body to the file
	written, err := io.Copy(dst, r.Body)
	if err != nil {
		h.logger.Error("failed to write file", "error", err, "path", targetPath)
		render.Status(r, http.StatusInternalServerError)
		render.JSON(w, r, ErrorResponse{Message: "failed to write file"})
		return
	}

	if err := os.Chmod(targetPath, 0600); err != nil {
		h.logger.Error("failed to set file permissions", "error", err, "path", targetPath)
	}
	if err := os.Chown(targetPath, uid, gid); err != nil {
		h.logger.Error("failed to set file ownership", "error", err, "path", targetPath, "uid", uid, "gid", gid)
	}

	render.JSON(w, r, FilesystemWriteResponse{
		AbsolutePath: resolvedPath,
		BytesWritten: written,
		Container:    container,
	})
}
