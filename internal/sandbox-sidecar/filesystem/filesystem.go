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
	"syscall"

	"github.com/danielgtaylor/huma/v2"

	"github.com/isola-run/isola/internal/httputil"
	sandboxsidecar "github.com/isola-run/isola/internal/sandbox-sidecar"
	"github.com/isola-run/isola/internal/sandbox-sidecar/proc"
	sidecarapi "github.com/isola-run/isola/internal/sidecar-api"
)

type FilesystemWriteInput struct {
	Path      string `query:"path" required:"true" minLength:"1" doc:"Destination path (absolute or relative to container cwd)"`
	Container string `query:"container,omitempty" doc:"Container name. Defaults to the only container if there is one, otherwise it's required."`
	sandboxsidecar.BodyStream
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
	Body sidecarapi.ListFilesystemEntriesResponse
}

type FilesystemStatInput struct {
	Path      string `query:"path" required:"true" minLength:"1" doc:"Path to stat (absolute or relative to container cwd)"`
	Container string `query:"container,omitempty" doc:"Container name. Defaults to the only container if there is one, otherwise it's required."`
}

type FilesystemStatOutput struct {
	Body sidecarapi.FilesystemEntry
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

// resolveTarget resolves the container PID and turns a user-supplied path into
// both the container-visible absolute path and the host path under /proc/<pid>/root.
func (h *Handlers) resolveTarget(path, container string) (pid int, resolvedPath, targetPath string, err error) {
	pid, err = h.pidResolver.FindCachedContainerPID(container)
	if err != nil {
		if container == "" {
			h.logger.Warn("failed to determine container pid", "error", err)
		} else {
			h.logger.Warn("failed to determine container pid", "error", err, "container", container)
		}
		return 0, "", "", huma.Error400BadRequest("failed to determine container pid")
	}

	resolvedPath, herr := h.resolveAbsolutePath(path, pid)
	if herr != nil {
		h.logger.Error("failed to resolve path", "error", herr, "path", path, "container", container)
		return 0, "", "", herr
	}

	return pid, resolvedPath, filepath.Join(h.procFS.GetRoot(pid), resolvedPath), nil
}

func entryType(mode os.FileMode) string {
	switch {
	case mode.IsRegular():
		return "file"
	case mode.IsDir():
		return "directory"
	case mode&os.ModeSymlink != 0:
		return "symlink"
	default:
		return "other"
	}
}

// entryFromInfo builds a FilesystemEntry from lstat info. entryPath is the
// container-visible path; hostPath is the corresponding /proc/<pid>/root path.
func entryFromInfo(name, entryPath, hostPath string, info os.FileInfo) sidecarapi.FilesystemEntry {
	entry := sidecarapi.FilesystemEntry{
		Name:         name,
		Path:         entryPath,
		Type:         entryType(info.Mode()),
		Size:         info.Size(),
		Permissions:  fmt.Sprintf("%04o", info.Mode().Perm()),
		ModifiedTime: info.ModTime().UTC(),
	}
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		entry.UID = int(st.Uid)
		entry.GID = int(st.Gid)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		// Readlink returns the raw link content, which is the container's own
		// view of the target (host /proc prefix never leaks).
		if target, err := os.Readlink(hostPath); err == nil {
			entry.SymlinkTarget = target
		}
	}
	return entry
}

// errNotADirectory reports that a path component exists but is not a directory.
var errNotADirectory = errors.New("exists and is not a directory")

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
			return fmt.Errorf("%s %w", path, errNotADirectory)
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

func (h *Handlers) PostFilesystem(_ context.Context, input *FilesystemWriteInput) (*struct{}, error) {
	pid, _, targetPath, err := h.resolveTarget(input.Path, input.Container)
	if err != nil {
		return nil, err
	}

	uid, gid, err := h.procFS.GetUIDGID(pid)
	if err != nil {
		h.logger.Error("failed to get container uid/gid", "error", err, "pid", pid)
		return nil, huma.Error500InternalServerError("failed to get container uid/gid")
	}

	parentDir := filepath.Dir(targetPath)
	if err := mkdirAllChown(parentDir, uid, gid); err != nil {
		h.logger.Error("failed to create parent directories", "error", err, "path", parentDir)
		if errors.Is(err, errNotADirectory) || errors.Is(err, syscall.ENOTDIR) {
			return nil, huma.Error409Conflict("a parent path component exists and is not a directory")
		}
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

	stream := httputil.NewDeadlineReader(input.Stream, input.ResponseController, httputil.StreamTimeout)

	if _, err := io.Copy(dst, stream); err != nil {
		h.logger.Error("failed to write file", "error", err, "path", targetPath)
		return nil, huma.Error500InternalServerError("failed to write file")
	}

	if err := os.Chmod(targetPath, 0666); err != nil { //nolint:gosec
		h.logger.Error("failed to set file permissions", "error", err, "path", targetPath)
	}

	if err := os.Chown(targetPath, uid, gid); err != nil {
		h.logger.Error("failed to set file ownership", "error", err, "path", targetPath, "uid", uid, "gid", gid)
	}

	return nil, nil
}

func (h *Handlers) GetFilesystem(_ context.Context, input *FilesystemReadInput) (*huma.StreamResponse, error) {
	path := input.Path

	_, _, targetPath, err := h.resolveTarget(path, input.Container)
	if err != nil {
		return nil, err
	}

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
					h.logger.Warn("client disconnected during file stream", "error", err, "path", targetPath)
				} else {
					h.logger.Error("failed to stream file", "error", err, "path", targetPath)
				}
			}
		},
	}, nil
}

func (h *Handlers) ListFilesystemEntries(_ context.Context, input *FilesystemListInput) (*FilesystemListOutput, error) {
	_, resolvedPath, targetPath, err := h.resolveTarget(input.Path, input.Container)
	if err != nil {
		return nil, err
	}

	dirEntries, err := os.ReadDir(targetPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, huma.Error404NotFound(fmt.Sprintf("directory not found: %s", input.Path))
		}
		if errors.Is(err, syscall.ENOTDIR) {
			return nil, huma.Error400BadRequest(fmt.Sprintf("not a directory: %s", input.Path))
		}
		h.logger.Error("failed to read directory", "error", err, "path", targetPath)
		return nil, huma.Error500InternalServerError("failed to read directory")
	}

	entries := make([]sidecarapi.FilesystemEntry, 0, len(dirEntries))
	for _, dirEntry := range dirEntries {
		info, err := dirEntry.Info()
		if err != nil {
			continue // entry removed between ReadDir and Info
		}
		name := dirEntry.Name()
		entries = append(entries, entryFromInfo(name, filepath.Join(resolvedPath, name), filepath.Join(targetPath, name), info))
	}

	return &FilesystemListOutput{Body: sidecarapi.ListFilesystemEntriesResponse{Entries: entries}}, nil
}

func (h *Handlers) StatFilesystemEntry(_ context.Context, input *FilesystemStatInput) (*FilesystemStatOutput, error) {
	_, resolvedPath, targetPath, err := h.resolveTarget(input.Path, input.Container)
	if err != nil {
		return nil, err
	}

	info, err := os.Lstat(targetPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, huma.Error404NotFound(fmt.Sprintf("path not found: %s", input.Path))
		}
		h.logger.Error("failed to stat path", "error", err, "path", targetPath)
		return nil, huma.Error500InternalServerError("failed to stat path")
	}

	return &FilesystemStatOutput{Body: entryFromInfo(filepath.Base(resolvedPath), resolvedPath, targetPath, info)}, nil
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
		DefaultStatus: http.StatusNoContent,
		Errors:        []int{http.StatusBadRequest, http.StatusConflict},
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
		OperationID: "listFilesystemEntries",
		Method:      http.MethodGet,
		Path:        "/filesystem/entries",
		Summary:     "List directory entries in the sandbox filesystem",
		Description: "Returns metadata for each entry in the specified directory. Symlinks are reported, not followed.",
		Tags:        []string{"filesystem"},
		Errors:      []int{http.StatusBadRequest, http.StatusNotFound},
	}, h.ListFilesystemEntries)

	huma.Register(api, huma.Operation{
		OperationID: "statFilesystemEntry",
		Method:      http.MethodGet,
		Path:        "/filesystem/stat",
		Summary:     "Stat a path in the sandbox filesystem",
		Description: "Returns metadata for the file, directory, or symlink at the specified path. Symlinks are reported, not followed.",
		Tags:        []string{"filesystem"},
		Errors:      []int{http.StatusBadRequest, http.StatusNotFound},
	}, h.StatFilesystemEntry)
}
