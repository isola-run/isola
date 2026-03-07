// Copyright The Isola Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package filesystem

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/isola-ai/isola/internal/httputil"
	sandboxsidecar "github.com/isola-ai/isola/internal/sandbox-sidecar"
	"github.com/isola-ai/isola/internal/sandbox-sidecar/proc"
	sidecarapi "github.com/isola-ai/isola/internal/sidecar-api"
)

type FilesystemWriteInput struct {
	Path      string `query:"path" required:"true" minLength:"1" doc:"Destination path (absolute or relative to container cwd)"`
	Container string `query:"container,omitempty" doc:"Container name. Defaults to the only container if there is one, otherwise it's required."`
	sandboxsidecar.BodyStream
}

type FilesystemWriteOutput struct {
	Body sidecarapi.FilesystemWriteResponse
}

type FilesystemReadInput struct {
	Path      string `query:"path" required:"true" minLength:"1" doc:"Source path (absolute or relative to container cwd)"`
	Container string `query:"container,omitempty" doc:"Container name. Defaults to the only container if there is one, otherwise it's required."`
}

type Handlers struct {
	logger      *slog.Logger
	procFS      proc.ProcFS
	pidResolver *sandboxsidecar.PIDResolver
}

func New(logger *slog.Logger, procFS proc.ProcFS, pidResolver *sandboxsidecar.PIDResolver) *Handlers {
	return &Handlers{
		logger:      logger,
		procFS:      procFS,
		pidResolver: pidResolver,
	}
}

func (h *Handlers) resolveAbsolutePath(path string, pid int) (string, huma.StatusError) {
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

// toRelative converts an absolute path to a relative path suitable for os.Root methods.
// os.Root rejects absolute paths — all names must be relative to the root.
func toRelative(absPath string) string {
	return strings.TrimPrefix(absPath, "/")
}

// mkdirAllChown is like os.Root.MkdirAll but chowns each newly created directory.
// Existing directories are left untouched. path must be relative to root.
func mkdirAllChown(root *os.Root, path string, uid, gid int) error {
	parent := filepath.Dir(path)
	if parent == path {
		return nil
	}
	fi, err := root.Stat(path)
	if err == nil {
		if !fi.IsDir() {
			return fmt.Errorf("%s exists and is not a directory", path)
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}
	if err := mkdirAllChown(root, parent, uid, gid); err != nil {
		return err
	}
	if err := root.Mkdir(path, 0755); err != nil { //nolint:gosec
		if os.IsExist(err) {
			return nil
		}
		return err
	}
	return root.Chown(path, uid, gid)
}

func (h *Handlers) PostFilesystem(_ context.Context, input *FilesystemWriteInput) (*FilesystemWriteOutput, error) {
	path := input.Path
	container := input.Container

	pid, err := h.pidResolver.FindCachedContainerPID(container)
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

	root, err := os.OpenRoot(h.procFS.GetRoot(pid))
	if err != nil {
		h.logger.Error("failed to open container root", "error", err)
		return nil, huma.Error500InternalServerError("failed to open container root")
	}
	defer func() { _ = root.Close() }()

	relPath := toRelative(resolvedPath)

	parentDir := filepath.Dir(relPath)
	if parentDir != "." {
		if err := mkdirAllChown(root, parentDir, uid, gid); err != nil {
			h.logger.Error("failed to create parent directories", "error", err, "path", parentDir)
			return nil, huma.Error500InternalServerError("failed to create parent directories")
		}
	}

	// Symlink escapes from mkdirAllChown or OpenFile return 500 — see comment in GetFilesystem
	// for why this isn't 403 (unexported errPathEscapes, golang/go#74640).
	dst, err := root.OpenFile(relPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0666) //nolint:gosec
	if err != nil {
		h.logger.Error("failed to create file", "error", err, "path", resolvedPath)
		return nil, huma.Error500InternalServerError("failed to create file")
	}
	defer func() {
		if err := dst.Close(); err != nil {
			h.logger.Error("failed to close written file", "error", err, "path", resolvedPath)
		}
	}()

	stream := httputil.NewDeadlineReader(input.Stream, input.ResponseController, httputil.StreamTimeout)

	written, err := io.Copy(dst, stream)
	if err != nil {
		h.logger.Error("failed to write file", "error", err, "path", resolvedPath)
		return nil, huma.Error500InternalServerError("failed to write file")
	}

	// Use file handle directly for chmod/chown to avoid TOCTOU re-resolving the path
	if err := dst.Chmod(0666); err != nil { //nolint:gosec
		h.logger.Error("failed to set file permissions", "error", err, "path", resolvedPath)
	}

	if err := dst.Chown(uid, gid); err != nil {
		h.logger.Error("failed to set file ownership", "error", err, "path", resolvedPath, "uid", uid, "gid", gid)
	}

	return &FilesystemWriteOutput{
		Body: sidecarapi.FilesystemWriteResponse{
			AbsolutePath: resolvedPath,
			BytesWritten: written,
		},
	}, nil
}

func (h *Handlers) GetFilesystem(_ context.Context, input *FilesystemReadInput) (*huma.StreamResponse, error) {
	path := input.Path
	container := input.Container

	pid, err := h.pidResolver.FindCachedContainerPID(container)
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

	root, err := os.OpenRoot(h.procFS.GetRoot(pid))
	if err != nil {
		h.logger.Error("failed to open container root", "error", err)
		return nil, huma.Error500InternalServerError("failed to open container root")
	}
	defer func() { _ = root.Close() }()

	relPath := toRelative(resolvedPath)

	// Stat before Open to reject non-regular files (FIFOs, devices, sockets)
	// without blocking — os.Open on a FIFO blocks until a writer connects.
	info, err := root.Stat(relPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, huma.Error404NotFound(fmt.Sprintf("file not found: %s", path))
		}
		// Symlink escapes land here (os.Root returns unexported errPathEscapes).
		// Ideally this would be 403 Forbidden, but Go does not export the sentinel
		// (proposal: https://github.com/golang/go/issues/74640) and detecting it
		// requires fragile string matching. Revisit when os.ErrPathEscapes is available.
		h.logger.Error("failed to stat file", "error", err, "path", resolvedPath)
		return nil, huma.Error500InternalServerError("failed to stat file")
	}

	if !info.Mode().IsRegular() {
		return nil, huma.Error400BadRequest(fmt.Sprintf("not a regular file: %s", path))
	}

	// root.Open returns a file with its own fd from openat — valid after root is closed
	f, err := root.Open(relPath)
	if err != nil {
		h.logger.Error("failed to open file", "error", err, "path", resolvedPath)
		return nil, huma.Error500InternalServerError("failed to open file")
	}

	return &huma.StreamResponse{
		Body: func(ctx huma.Context) {
			defer func() { _ = f.Close() }()

			// file size might change while we stream it due to in-sandbox activity
			// so we don't set Content-Length and read until EOF, which is a reasonable
			// best effort. If the file is modified during write, the streamed file
			// might be inconsistent.
			ctx.SetHeader("Content-Type", "application/octet-stream")

			rc := httputil.ResponseController(ctx)
			dw := httputil.NewDeadlineWriter(ctx.BodyWriter(), rc, httputil.StreamTimeout)

			if _, err := io.Copy(dw, f); err != nil {
				if errors.Is(err, context.Canceled) {
					h.logger.Warn("client disconnected during file stream", "error", err, "path", resolvedPath)
				} else {
					h.logger.Error("failed to stream file", "error", err, "path", resolvedPath)
				}
			}
		},
	}, nil
}

func Register(api huma.API, h *Handlers) {
	huma.Register(api, huma.Operation{
		OperationID: "postFilesystem",
		Method:      http.MethodPost,
		Path:        "/filesystem",
		Summary:     "Write a file to the sandbox filesystem",
		Description: "Writes a file to the specified path in the sandbox container",
		Tags:        []string{"filesystem"},
		// Since we use BodyStream resolver (no Body/RawBody field),
		// we need to manually specify the request body in OpenAPI
		RequestBody: &huma.RequestBody{
			Required: true,
			Content: map[string]*huma.MediaType{
				"application/octet-stream": {
					Schema: &huma.Schema{Type: "string", Format: "binary"},
				},
			},
		},
		DefaultStatus: http.StatusCreated,
		Errors:        []int{http.StatusBadRequest},
	}, h.PostFilesystem)

	huma.Register(api, huma.Operation{
		OperationID: "getFilesystem",
		Method:      http.MethodGet,
		Path:        "/filesystem",
		Summary:     "Read a file from the sandbox filesystem",
		Description: "Reads a file from the specified path in the sandbox container",
		Tags:        []string{"filesystem"},
		Responses: map[string]*huma.Response{
			"200": {
				Description: "File content",
				Content: map[string]*huma.MediaType{
					"application/octet-stream": {
						Schema: &huma.Schema{Type: "string", Format: "binary"},
					},
				},
			},
		},
		Errors: []int{http.StatusBadRequest, http.StatusNotFound},
	}, h.GetFilesystem)
}
