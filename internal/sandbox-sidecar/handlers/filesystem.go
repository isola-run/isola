package handlers

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/danielgtaylor/huma/v2"

	"github.com/isola-ai/isola-sb/internal/sandbox-sidecar/proc"
	sidecarapi "github.com/isola-ai/isola-sb/internal/sidecar-api"
)

type FilesystemHandlers struct {
	logger *slog.Logger
	procFS proc.ProcFS

	// PID cache to avoid repeated /proc scans (containerName -> pid)
	pidMu      sync.RWMutex
	cachedPIDs map[string]int
}

func NewFilesystemHandlers(logger *slog.Logger, procFS proc.ProcFS) *FilesystemHandlers {
	return &FilesystemHandlers{
		logger:     logger,
		procFS:     procFS,
		cachedPIDs: make(map[string]int),
	}
}

func (h *FilesystemHandlers) findCachedContainerPID(containerName string) (int, error) {
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

func (h *FilesystemHandlers) PostFilesystem(ctx context.Context, input *FilesystemWriteInput) (*FilesystemWriteOutput, error) {
	path := input.Path
	container := input.Container

	// Reject null bytes in path
	if strings.ContainsRune(path, 0) {
		return nil, huma.Error400BadRequest("path contains invalid characters")
	}

	pid, err := h.findCachedContainerPID(container)
	if err != nil {
		return nil, huma.Error400BadRequest("container not found")
	}

	uid, gid, err := h.procFS.GetUIDGID(pid)
	if err != nil {
		h.logger.Error("failed to get container uid/gid", "error", err, "pid", pid)
		return nil, huma.Error500InternalServerError("failed to get container uid/gid")
	}

	resolvedPath, err := resolveAbsolutePath(path, pid, h.procFS)
	if err != nil {
		h.logger.Error("failed to resolve path", "error", err, "pid", pid)
		return nil, huma.Error500InternalServerError("failed to resolve path")
	}

	// Build the host path via /proc/<pid>/root
	targetPath := filepath.Join(h.procFS.GetRoot(pid), resolvedPath)

	parentDir := filepath.Dir(targetPath)
	if err := os.MkdirAll(parentDir, 0755); err != nil { //nolint:gosec // intentional permissions for container access
		h.logger.Error("failed to create parent directories", "error", err, "path", parentDir)
		return nil, huma.Error500InternalServerError("failed to create parent directories")
	}

	dst, err := os.Create(targetPath) //nolint:gosec
	if err != nil {
		h.logger.Error("failed to create file", "error", err, "path", targetPath)
		return nil, huma.Error500InternalServerError("failed to create file")
	}
	defer func() { _ = dst.Close() }()

	// Stream the body to the file
	written, err := io.Copy(dst, input.Stream)
	if err != nil {
		h.logger.Error("failed to write file", "error", err, "path", targetPath)
		return nil, huma.Error500InternalServerError("failed to write file")
	}

	if err := os.Chmod(targetPath, 0600); err != nil {
		h.logger.Error("failed to set file permissions", "error", err, "path", targetPath)
	}
	if err := os.Chown(targetPath, uid, gid); err != nil {
		h.logger.Error("failed to set file ownership", "error", err, "path", targetPath, "uid", uid, "gid", gid)
	}

	return &FilesystemWriteOutput{
		Body: sidecarapi.FilesystemWriteResponse{
			AbsolutePath: resolvedPath,
			BytesWritten: written,
		},
	}, nil
}
