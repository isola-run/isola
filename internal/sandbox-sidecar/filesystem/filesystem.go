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

	"github.com/danielgtaylor/huma/v2"

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

type FilesystemListInput struct {
	Path      string `query:"path" required:"true" minLength:"1" doc:"Directory path (absolute or relative to container cwd)"`
	Container string `query:"container,omitempty" doc:"Container name. Defaults to the only container if there is one, otherwise it's required."`
}

type FilesystemListOutput struct {
	Body sidecarapi.FilesystemListResponse
}

type FilesystemStatInput struct {
	Path      string `query:"path" required:"true" minLength:"1" doc:"Path to stat (absolute or relative to container cwd)"`
	Container string `query:"container,omitempty" doc:"Container name. Defaults to the only container if there is one, otherwise it's required."`
}

type FilesystemStatOutput struct {
	Body sidecarapi.FilesystemStatResponse
}

type FilesystemMkdirInput struct {
	Path      string `query:"path" required:"true" minLength:"1" doc:"Directory path to create (absolute or relative to container cwd)"`
	Container string `query:"container,omitempty" doc:"Container name. Defaults to the only container if there is one, otherwise it's required."`
}

type FilesystemMkdirOutput struct {
	Body sidecarapi.FilesystemMkdirResponse
}

type FilesystemRenameInput struct {
	Path      string `query:"path" required:"true" minLength:"1" doc:"Source path (absolute or relative to container cwd)"`
	NewPath   string `query:"newPath" required:"true" minLength:"1" doc:"Destination path (absolute or relative to container cwd)"`
	Container string `query:"container,omitempty" doc:"Container name. Defaults to the only container if there is one, otherwise it's required."`
}

type FilesystemRenameOutput struct {
	Body sidecarapi.FilesystemRenameResponse
}

type FilesystemRemoveInput struct {
	Path      string `query:"path" required:"true" minLength:"1" doc:"Path to remove (absolute or relative to container cwd)"`
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

func (h *Handlers) resolveContainerPID(container string) (int, error) {
	pid, err := h.pidResolver.FindCachedContainerPID(container)
	if err != nil {
		h.logger.Warn("failed to determine container pid", "error", err, "container", container)
		return 0, huma.Error400BadRequest("failed to determine container pid")
	}
	return pid, nil
}

// resolveHostPath resolves a user-provided path to a host path via /proc/<pid>/root.
func (h *Handlers) resolveHostPath(path, container string) (hostPath, absPath string, err error) {
	pid, pidErr := h.resolveContainerPID(container)
	if pidErr != nil {
		return "", "", pidErr
	}
	resolvedPath, resolveErr := h.resolveAbsolutePath(path, pid)
	if resolveErr != nil {
		h.logger.Error("failed to resolve path", "error", resolveErr, "path", path, "container", container)
		return "", "", resolveErr
	}
	return filepath.Join(h.procFS.GetRoot(pid), resolvedPath), resolvedPath, nil
}

func fileInfoFromOS(info os.FileInfo, absPath string) sidecarapi.FileInfo {
	return sidecarapi.FileInfo{
		Name:  info.Name(),
		Path:  absPath,
		IsDir: info.IsDir(),
		Size:  info.Size(),
		Mode:  info.Mode().String(),
	}
}

func (h *Handlers) PostFilesystem(_ context.Context, input *FilesystemWriteInput) (*FilesystemWriteOutput, error) {
	path := input.Path
	container := input.Container

	pid, err := h.resolveContainerPID(container)
	if err != nil {
		return nil, err
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

func (h *Handlers) GetFilesystem(_ context.Context, input *FilesystemReadInput) (*huma.StreamResponse, error) {
	targetPath, _, err := h.resolveHostPath(input.Path, input.Container)
	if err != nil {
		return nil, err
	}

	// Stat before Open to reject non-regular files (FIFOs, devices, sockets)
	// without blocking — os.Open on a FIFO blocks until a writer connects.
	info, err := os.Stat(targetPath) //nolint:gosec // path is within container root via /proc/<pid>/root
	if err != nil {
		if os.IsNotExist(err) {
			return nil, huma.Error404NotFound(fmt.Sprintf("file not found: %s", input.Path))
		}
		h.logger.Error("failed to stat file", "error", err, "path", targetPath)
		return nil, huma.Error500InternalServerError("failed to stat file")
	}

	if !info.Mode().IsRegular() {
		return nil, huma.Error400BadRequest(fmt.Sprintf("not a regular file: %s", input.Path))
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

func (h *Handlers) ListFilesystem(_ context.Context, input *FilesystemListInput) (*FilesystemListOutput, error) {
	targetPath, resolvedPath, err := h.resolveHostPath(input.Path, input.Container)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(targetPath) //nolint:gosec
	if err != nil {
		if os.IsNotExist(err) {
			return nil, huma.Error404NotFound(fmt.Sprintf("directory not found: %s", input.Path))
		}
		h.logger.Error("failed to stat directory", "error", err, "path", targetPath)
		return nil, huma.Error500InternalServerError("failed to stat directory")
	}

	if !info.IsDir() {
		return nil, huma.Error400BadRequest(fmt.Sprintf("not a directory: %s", input.Path))
	}

	dirEntries, err := os.ReadDir(targetPath) //nolint:gosec
	if err != nil {
		h.logger.Error("failed to read directory", "error", err, "path", targetPath)
		return nil, huma.Error500InternalServerError("failed to read directory")
	}

	entries := make([]sidecarapi.FileInfo, 0, len(dirEntries))
	for _, de := range dirEntries {
		fi, err := de.Info()
		if err != nil {
			h.logger.Warn("failed to stat directory entry, skipping", "error", err, "name", de.Name())
			continue
		}
		entryAbsPath := filepath.Join(resolvedPath, de.Name())
		entries = append(entries, fileInfoFromOS(fi, entryAbsPath))
	}

	return &FilesystemListOutput{
		Body: sidecarapi.FilesystemListResponse{Entries: entries},
	}, nil
}

func (h *Handlers) StatFilesystem(_ context.Context, input *FilesystemStatInput) (*FilesystemStatOutput, error) {
	targetPath, resolvedPath, err := h.resolveHostPath(input.Path, input.Container)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(targetPath) //nolint:gosec
	if err != nil {
		if os.IsNotExist(err) {
			return nil, huma.Error404NotFound(fmt.Sprintf("path not found: %s", input.Path))
		}
		h.logger.Error("failed to stat path", "error", err, "path", targetPath)
		return nil, huma.Error500InternalServerError("failed to stat path")
	}

	return &FilesystemStatOutput{
		Body: fileInfoFromOS(info, resolvedPath),
	}, nil
}

func (h *Handlers) MkdirFilesystem(_ context.Context, input *FilesystemMkdirInput) (*FilesystemMkdirOutput, error) {
	pid, err := h.resolveContainerPID(input.Container)
	if err != nil {
		return nil, err
	}

	resolvedPath, resolveErr := h.resolveAbsolutePath(input.Path, pid)
	if resolveErr != nil {
		h.logger.Error("failed to resolve path", "error", resolveErr, "path", input.Path)
		return nil, resolveErr
	}

	uid, gid, err := h.procFS.GetUIDGID(pid)
	if err != nil {
		h.logger.Error("failed to get container uid/gid", "error", err, "pid", pid)
		return nil, huma.Error500InternalServerError("failed to get container uid/gid")
	}

	targetPath := filepath.Join(h.procFS.GetRoot(pid), resolvedPath)
	if err := mkdirAllChown(targetPath, uid, gid); err != nil {
		h.logger.Error("failed to create directory", "error", err, "path", targetPath)
		return nil, huma.Error500InternalServerError("failed to create directory")
	}

	return &FilesystemMkdirOutput{
		Body: sidecarapi.FilesystemMkdirResponse{AbsolutePath: resolvedPath},
	}, nil
}

func (h *Handlers) RenameFilesystem(_ context.Context, input *FilesystemRenameInput) (*FilesystemRenameOutput, error) {
	srcHost, _, err := h.resolveHostPath(input.Path, input.Container)
	if err != nil {
		return nil, err
	}

	// Resolve destination path using same container
	pid, pidErr := h.resolveContainerPID(input.Container)
	if pidErr != nil {
		return nil, pidErr
	}
	newResolvedPath, resolveErr := h.resolveAbsolutePath(input.NewPath, pid)
	if resolveErr != nil {
		h.logger.Error("failed to resolve new path", "error", resolveErr, "path", input.NewPath)
		return nil, resolveErr
	}
	dstHost := filepath.Join(h.procFS.GetRoot(pid), newResolvedPath)

	if _, err := os.Stat(srcHost); err != nil { //nolint:gosec
		if os.IsNotExist(err) {
			return nil, huma.Error404NotFound(fmt.Sprintf("path not found: %s", input.Path))
		}
		h.logger.Error("failed to stat source path", "error", err, "path", srcHost)
		return nil, huma.Error500InternalServerError("failed to stat source path")
	}

	// Ensure parent of destination exists
	uid, gid, err := h.procFS.GetUIDGID(pid)
	if err != nil {
		h.logger.Error("failed to get container uid/gid", "error", err, "pid", pid)
		return nil, huma.Error500InternalServerError("failed to get container uid/gid")
	}
	if err := mkdirAllChown(filepath.Dir(dstHost), uid, gid); err != nil {
		h.logger.Error("failed to create parent directories", "error", err, "path", filepath.Dir(dstHost))
		return nil, huma.Error500InternalServerError("failed to create parent directories")
	}

	if err := os.Rename(srcHost, dstHost); err != nil {
		h.logger.Error("failed to rename", "error", err, "from", srcHost, "to", dstHost)
		return nil, huma.Error500InternalServerError("failed to rename")
	}

	return &FilesystemRenameOutput{
		Body: sidecarapi.FilesystemRenameResponse{AbsolutePath: newResolvedPath},
	}, nil
}

func (h *Handlers) RemoveFilesystem(_ context.Context, input *FilesystemRemoveInput) (*struct{}, error) {
	targetPath, _, err := h.resolveHostPath(input.Path, input.Container)
	if err != nil {
		return nil, err
	}

	if _, err := os.Lstat(targetPath); err != nil { //nolint:gosec
		if os.IsNotExist(err) {
			return nil, huma.Error404NotFound(fmt.Sprintf("path not found: %s", input.Path))
		}
		h.logger.Error("failed to stat path", "error", err, "path", targetPath)
		return nil, huma.Error500InternalServerError("failed to stat path")
	}

	if err := os.RemoveAll(targetPath); err != nil {
		h.logger.Error("failed to remove", "error", err, "path", targetPath)
		return nil, huma.Error500InternalServerError("failed to remove")
	}

	return nil, nil
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

	huma.Register(api, huma.Operation{
		OperationID: "listFilesystem",
		Method:      http.MethodGet,
		Path:        "/filesystem/list",
		Summary:     "List directory contents",
		Description: "Lists files and directories at the specified path in the sandbox container",
		Tags:        []string{"filesystem"},
		Errors:      []int{http.StatusBadRequest, http.StatusNotFound},
	}, h.ListFilesystem)

	huma.Register(api, huma.Operation{
		OperationID: "statFilesystem",
		Method:      http.MethodGet,
		Path:        "/filesystem/stat",
		Summary:     "Get file or directory info",
		Description: "Returns metadata about a file or directory in the sandbox container",
		Tags:        []string{"filesystem"},
		Errors:      []int{http.StatusBadRequest, http.StatusNotFound},
	}, h.StatFilesystem)

	huma.Register(api, huma.Operation{
		OperationID:   "mkdirFilesystem",
		Method:        http.MethodPost,
		Path:          "/filesystem/mkdir",
		Summary:       "Create a directory",
		Description:   "Creates a directory and all parent directories at the specified path",
		Tags:          []string{"filesystem"},
		DefaultStatus: http.StatusCreated,
		Errors:        []int{http.StatusBadRequest},
	}, h.MkdirFilesystem)

	huma.Register(api, huma.Operation{
		OperationID: "renameFilesystem",
		Method:      http.MethodPost,
		Path:        "/filesystem/rename",
		Summary:     "Rename or move a file or directory",
		Description: "Renames or moves a file or directory within the sandbox container",
		Tags:        []string{"filesystem"},
		Errors:      []int{http.StatusBadRequest, http.StatusNotFound},
	}, h.RenameFilesystem)

	huma.Register(api, huma.Operation{
		OperationID:   "removeFilesystem",
		Method:        http.MethodDelete,
		Path:          "/filesystem",
		Summary:       "Remove a file or directory",
		Description:   "Removes a file or directory (recursively) from the sandbox container",
		Tags:          []string{"filesystem"},
		DefaultStatus: http.StatusNoContent,
		Errors:        []int{http.StatusBadRequest, http.StatusNotFound},
	}, h.RemoveFilesystem)
}
