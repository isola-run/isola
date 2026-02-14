package handlers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
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

func (h *FilesystemHandlers) resolveAbsolutePath(path string, pid int) (string, huma.StatusError) {
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}

	cwd, err := h.procFS.GetCwd(pid)
	if err != nil {
		return "", huma.Error500InternalServerError("Failed to resolve path in sandbox container")
	}

	relativePath := filepath.Join(cwd, path) // returns a Clean path
	absolutePath, err := filepath.Abs(relativePath)
	if err != nil {
		return "", huma.Error500InternalServerError("Failed to resolve path in sandbox container")
	}

	return absolutePath, nil
}

// mkdirAllChown is like os.MkdirAll but chowns each newly created directory.
// Existing directories are left untouched.
func mkdirAllChown(path string, uid, gid int) error {
	parent := filepath.Dir(path)
	if parent == path {
		return nil
	}
	fi, err := os.Stat(path)
	if err == nil {
		if !fi.IsDir() {
			return fmt.Errorf("%s exists and is not a directory", path)
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}
	if err := mkdirAllChown(parent, uid, gid); err != nil {
		return err
	}
	if err := os.Mkdir(path, 0755); err != nil { //nolint:gosec
		if os.IsExist(err) {
			return nil
		}
		return err
	}
	return os.Chown(path, uid, gid)
}

func (h *FilesystemHandlers) PostFilesystem(_ context.Context, input *FilesystemWriteInput) (*FilesystemWriteOutput, error) {
	path := input.Path
	container := input.Container

	pid, err := h.findCachedContainerPID(container)
	if err != nil {
		if container == "" {
			h.logger.Warn("failed to determine container pid", "error", err)
			return nil, huma.Error400BadRequest("failed to determine container pid")
		}
		h.logger.Warn("failed to determine container pid", "error", err, "container", container)
		return nil, huma.Error400BadRequest("failed to determine container pid")
	}

	resolvedPath, err := h.resolveAbsolutePath(path, pid)
	if err != nil {
		h.logger.Error("failed to resolve path", "error", err, "path", path, "container", container)
		return nil, err
	}

	uid, gid, err := h.procFS.GetUIDGID(pid)
	if err != nil {
		h.logger.Error("failed to get container uid/gid", "error", err, "pid", pid)
		return nil, huma.Error500InternalServerError("failed to get container uid/gid")
	}

	// Build the host path via /proc/<pid>/root
	targetPath := filepath.Join(h.procFS.GetRoot(pid), resolvedPath)

	parentDir := filepath.Dir(targetPath)
	if err := mkdirAllChown(parentDir, uid, gid); err != nil {
		h.logger.Error("failed to create parent directories", "error", err, "path", parentDir)
		return nil, huma.Error500InternalServerError("failed to create parent directories")
	}

	dst, err := os.Create(targetPath) //nolint:gosec
	if err != nil {
		h.logger.Error("failed to create file", "error", err, "path", targetPath)
		return nil, huma.Error500InternalServerError("failed to create file")
	}
	defer func() {
		if err := dst.Close(); err != nil {
			h.logger.Error("failed to close written file", "error", err, "path", targetPath)
		}
	}()

	written, err := io.Copy(dst, input.Stream)
	if err != nil {
		h.logger.Error("failed to write file", "error", err, "path", targetPath)
		return nil, huma.Error500InternalServerError("failed to write file")
	}

	if err := os.Chmod(targetPath, 0666); err != nil { //nolint:gosec
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

func (h *FilesystemHandlers) GetFilesystem(_ context.Context, input *FilesystemReadInput) (*huma.StreamResponse, error) {
	path := input.Path
	container := input.Container

	pid, err := h.findCachedContainerPID(container)
	if err != nil {
		if container == "" {
			h.logger.Warn("failed to determine container pid", "error", err)
			return nil, huma.Error400BadRequest("failed to determine container pid")
		}
		h.logger.Warn("failed to determine container pid", "error", err, "container", container)
		return nil, huma.Error400BadRequest("failed to determine container pid")
	}

	resolvedPath, err := h.resolveAbsolutePath(path, pid)
	if err != nil {
		h.logger.Error("failed to resolve path", "error", err, "path", path, "container", container)
		return nil, err
	}

	// Build the host path via /proc/<pid>/root
	targetPath := filepath.Join(h.procFS.GetRoot(pid), resolvedPath)

	// Stat before Open to reject non-regular files (FIFOs, devices, sockets)
	// without blocking — os.Open on a FIFO blocks until a writer connects.
	info, err := os.Stat(targetPath) //nolint:gosec // path is within container root via /proc/<pid>/root
	if err != nil {
		if os.IsNotExist(err) {
			return nil, huma.Error404NotFound(fmt.Sprintf("file not found: %s", path))
		}
		h.logger.Error("failed to stat file", "error", err, "path", targetPath)
		return nil, huma.Error500InternalServerError("failed to stat file")
	}

	if !info.Mode().IsRegular() {
		return nil, huma.Error400BadRequest(fmt.Sprintf("not a regular file: %s", path))
	}

	f, err := os.Open(targetPath) //nolint:gosec // path is within container root via /proc/<pid>/root
	if err != nil {
		h.logger.Error("failed to open file", "error", err, "path", targetPath)
		return nil, huma.Error500InternalServerError("failed to open file")
	}

	// in the future, we might want to examine sendfile to optimize this
	// for now its definitely a premature optimization
	return &huma.StreamResponse{
		Body: func(ctx huma.Context) {
			defer func() { _ = f.Close() }()

			// file size might change while we stream it due to in-sandbox activity
			// so we don't set Content-Length and read until EOF, which is a reasonable
			// best effort. If the file is modified during write, the streamed file
			// might be inconsistent.
			ctx.SetHeader("Content-Type", "application/octet-stream")

			if _, err := io.Copy(ctx.BodyWriter(), f); err != nil {
				if errors.Is(err, context.Canceled) {
					h.logger.Warn("client disconnected during file stream", "error", err, "path", targetPath)
				} else {
					h.logger.Error("failed to stream file", "error", err, "path", targetPath)
				}
			}
		},
	}, nil
}
