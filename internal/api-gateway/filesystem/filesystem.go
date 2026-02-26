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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"syscall"

	"github.com/danielgtaylor/huma/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apigateway "github.com/isola-ai/isola/internal/api-gateway"
	"github.com/isola-ai/isola/internal/constants"
	sidecarapi "github.com/isola-ai/isola/internal/sidecar-api"
)

// --- Types ---

type FilesystemWriteInput struct {
	ID        string `path:"id" doc:"Sandbox identifier"`
	Path      string `query:"path" required:"true" minLength:"1" doc:"Destination path (absolute or relative to container cwd)"`
	Container string `query:"container,omitempty" doc:"Container name. Defaults to the only container if there is one, otherwise it's required."`
	apigateway.BodyStream
}

type FilesystemWriteResponse struct {
	AbsolutePath string `json:"absolutePath" example:"/workspace/file.txt" doc:"Absolute path where file was written"`
	BytesWritten int64  `json:"bytesWritten" example:"1024" doc:"Number of bytes written"`
}

type FilesystemWriteOutput struct {
	Body FilesystemWriteResponse
}

type FilesystemReadInput struct {
	ID        string `path:"id" doc:"Sandbox identifier"`
	Path      string `query:"path" required:"true" minLength:"1" doc:"Source path (absolute or relative to container cwd)"`
	Container string `query:"container,omitempty" doc:"Container name. Defaults to the only container if there is one, otherwise it's required."`
}

type FileInfo struct {
	Name  string `json:"name" example:"file.txt" doc:"Base name of the file or directory"`
	Path  string `json:"path" example:"/workspace/file.txt" doc:"Absolute path"`
	IsDir bool   `json:"isDir" doc:"True if the entry is a directory"`
	Size  int64  `json:"size" example:"1024" doc:"Size in bytes (0 for directories)"`
	Mode  string `json:"mode" example:"-rw-r--r--" doc:"Unix file mode string"`
}

type FilesystemListInput struct {
	ID        string `path:"id" doc:"Sandbox identifier"`
	Path      string `query:"path" required:"true" minLength:"1" doc:"Directory path (absolute or relative to container cwd)"`
	Container string `query:"container,omitempty" doc:"Container name. Defaults to the only container if there is one, otherwise it's required."`
}

type FilesystemListResponse struct {
	Entries []FileInfo `json:"entries" doc:"List of directory entries"`
}

type FilesystemListOutput struct {
	Body FilesystemListResponse
}

type FilesystemStatInput struct {
	ID        string `path:"id" doc:"Sandbox identifier"`
	Path      string `query:"path" required:"true" minLength:"1" doc:"Path to stat (absolute or relative to container cwd)"`
	Container string `query:"container,omitempty" doc:"Container name. Defaults to the only container if there is one, otherwise it's required."`
}

type FilesystemStatOutput struct {
	Body FileInfo
}

type FilesystemMkdirInput struct {
	ID        string `path:"id" doc:"Sandbox identifier"`
	Path      string `query:"path" required:"true" minLength:"1" doc:"Directory path to create (absolute or relative to container cwd)"`
	Container string `query:"container,omitempty" doc:"Container name. Defaults to the only container if there is one, otherwise it's required."`
}

type FilesystemMkdirResponse struct {
	AbsolutePath string `json:"absolutePath" example:"/workspace/new-dir" doc:"Absolute path of created directory"`
}

type FilesystemMkdirOutput struct {
	Body FilesystemMkdirResponse
}

type FilesystemRenameInput struct {
	ID        string `path:"id" doc:"Sandbox identifier"`
	Path      string `query:"path" required:"true" minLength:"1" doc:"Source path (absolute or relative to container cwd)"`
	NewPath   string `query:"newPath" required:"true" minLength:"1" doc:"Destination path (absolute or relative to container cwd)"`
	Container string `query:"container,omitempty" doc:"Container name. Defaults to the only container if there is one, otherwise it's required."`
}

type FilesystemRenameResponse struct {
	AbsolutePath string `json:"absolutePath" example:"/workspace/new-name.txt" doc:"New absolute path after rename"`
}

type FilesystemRenameOutput struct {
	Body FilesystemRenameResponse
}

type FilesystemRemoveInput struct {
	ID        string `path:"id" doc:"Sandbox identifier"`
	Path      string `query:"path" required:"true" minLength:"1" doc:"Path to remove (absolute or relative to container cwd)"`
	Container string `query:"container,omitempty" doc:"Container name. Defaults to the only container if there is one, otherwise it's required."`
}

// --- Handlers ---

type Handlers struct {
	logger           *slog.Logger
	k8sClient        client.Client
	sandboxNamespace string
	httpClient       apigateway.HTTPDoer
	sidecarPort      int
}

func New(logger *slog.Logger, sandboxNamespace string, k8sClient client.Client, httpClient apigateway.HTTPDoer) *Handlers {
	return &Handlers{
		logger:           logger,
		k8sClient:        k8sClient,
		sandboxNamespace: sandboxNamespace,
		httpClient:       httpClient,
		sidecarPort:      constants.SidecarPort,
	}
}

func (h *Handlers) PostFilesystem(ctx context.Context, input *FilesystemWriteInput) (*FilesystemWriteOutput, error) {
	sb, err := apigateway.GetReadySandbox(ctx, h.k8sClient, h.sandboxNamespace, input.ID, h.logger)
	if err != nil {
		return nil, err
	}

	params := url.Values{}
	params.Set("path", input.Path)
	if input.Container != "" {
		params.Set("container", input.Container)
	}
	sidecarURL := fmt.Sprintf("http://%s:%d/filesystem?%s", sb.Status.PodIP, h.sidecarPort, params.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sidecarURL, input.Stream)
	if err != nil {
		h.logger.Error("failed to build sidecar request", "error", err)
		return nil, huma.Error500InternalServerError("failed to build sidecar request")
	}
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := h.httpClient.Do(req)
	if err != nil {
		h.logger.Error("sidecar request failed", "error", err, "id", input.ID)
		return nil, huma.Error502BadGateway("failed to reach sidecar")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return nil, apigateway.HandleSidecarError(resp, input.ID, h.logger)
	}

	_ = sidecarapi.FilesystemWriteResponse(FilesystemWriteResponse{}) // assert field compatibility
	var sidecarResp sidecarapi.FilesystemWriteResponse
	if err := json.NewDecoder(resp.Body).Decode(&sidecarResp); err != nil {
		h.logger.Error("failed to decode sidecar response", "error", err, "id", input.ID, "status", resp.StatusCode)
		return nil, huma.Error502BadGateway("invalid sidecar response")
	}

	return &FilesystemWriteOutput{
		Body: FilesystemWriteResponse{
			AbsolutePath: sidecarResp.AbsolutePath,
			BytesWritten: sidecarResp.BytesWritten,
		},
	}, nil
}

func (h *Handlers) GetFilesystem(ctx context.Context, input *FilesystemReadInput) (*huma.StreamResponse, error) {
	sb, err := apigateway.GetReadySandbox(ctx, h.k8sClient, h.sandboxNamespace, input.ID, h.logger)
	if err != nil {
		return nil, err
	}

	params := url.Values{}
	params.Set("path", input.Path)
	if input.Container != "" {
		params.Set("container", input.Container)
	}
	sidecarURL := fmt.Sprintf("http://%s:%d/filesystem?%s", sb.Status.PodIP, h.sidecarPort, params.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sidecarURL, nil)
	if err != nil {
		h.logger.Error("failed to build sidecar request", "error", err)
		return nil, huma.Error500InternalServerError("failed to build sidecar request")
	}

	resp, err := h.httpClient.Do(req) //nolint:bodyclose // closed in both error and streaming paths below
	if err != nil {
		h.logger.Error("sidecar request failed", "error", err, "id", input.ID)
		return nil, huma.Error502BadGateway("failed to reach sidecar")
	}

	if resp.StatusCode >= 400 {
		defer func() { _ = resp.Body.Close() }()
		return nil, apigateway.HandleSidecarError(resp, input.ID, h.logger)
	}

	return &huma.StreamResponse{
		Body: func(ctx huma.Context) {
			defer func() { _ = resp.Body.Close() }()
			// no-cache, since the file content change over time
			// private, since the file is of a specific sandbox
			ctx.SetHeader("Cache-Control", "no-cache, private")

			if ct := resp.Header.Get("Content-Type"); ct != "" {
				ctx.SetHeader("Content-Type", ct)
			}
			if cl := resp.Header.Get("Content-Length"); cl != "" {
				ctx.SetHeader("Content-Length", cl)
			}

			// The sandbox is untrusted and thus its sidecar is untrusted.
			// If we ever add a limitation to the size of files that can be read from a sandbox,
			// and for example require a bucket store for files > maxBytes, we should do something like:
			// limitedReader := io.LimitReader(resp.Body, maxBytes+1)
			// io.Copy(ctx.BodyWriter(), limitedReader)

			if _, err := io.Copy(ctx.BodyWriter(), resp.Body); err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, syscall.EPIPE) {
					h.logger.Warn("client disconnected during file stream", "error", err, "id", input.ID)
				} else {
					h.logger.Error("sidecar error streaming file", "error", err, "id", input.ID)
				}
			}
		},
	}, nil
}

func (h *Handlers) sidecarURL(podIP, sidecarPath string, params url.Values) string {
	return fmt.Sprintf("http://%s:%d%s?%s", podIP, h.sidecarPort, sidecarPath, params.Encode())
}

func filesystemParams(path, container string) url.Values {
	params := url.Values{}
	params.Set("path", path)
	if container != "" {
		params.Set("container", container)
	}
	return params
}

// proxyJSONGet proxies a GET request to the sidecar and decodes a JSON response.
func (h *Handlers) proxyJSONGet(ctx context.Context, sandboxID, sidecarURL string, dest any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sidecarURL, nil)
	if err != nil {
		h.logger.Error("failed to build sidecar request", "error", err)
		return huma.Error500InternalServerError("failed to build sidecar request")
	}

	resp, err := h.httpClient.Do(req)
	if err != nil {
		h.logger.Error("sidecar request failed", "error", err, "id", sandboxID)
		return huma.Error502BadGateway("failed to reach sidecar")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return apigateway.HandleSidecarError(resp, sandboxID, h.logger)
	}

	if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
		h.logger.Error("failed to decode sidecar response", "error", err, "id", sandboxID, "status", resp.StatusCode)
		return huma.Error502BadGateway("invalid sidecar response")
	}
	return nil
}

// proxySimple proxies a request with no body to the sidecar and decodes an optional JSON response.
func (h *Handlers) proxySimple(ctx context.Context, method, sandboxID, sidecarURL string, dest any) error {
	req, err := http.NewRequestWithContext(ctx, method, sidecarURL, nil)
	if err != nil {
		h.logger.Error("failed to build sidecar request", "error", err)
		return huma.Error500InternalServerError("failed to build sidecar request")
	}

	resp, err := h.httpClient.Do(req)
	if err != nil {
		h.logger.Error("sidecar request failed", "error", err, "id", sandboxID)
		return huma.Error502BadGateway("failed to reach sidecar")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return apigateway.HandleSidecarError(resp, sandboxID, h.logger)
	}

	if dest != nil {
		if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
			h.logger.Error("failed to decode sidecar response", "error", err, "id", sandboxID, "status", resp.StatusCode)
			return huma.Error502BadGateway("invalid sidecar response")
		}
	}
	return nil
}

func (h *Handlers) ListFilesystem(ctx context.Context, input *FilesystemListInput) (*FilesystemListOutput, error) {
	sb, err := apigateway.GetReadySandbox(ctx, h.k8sClient, h.sandboxNamespace, input.ID, h.logger)
	if err != nil {
		return nil, err
	}

	u := h.sidecarURL(sb.Status.PodIP, "/filesystem/list", filesystemParams(input.Path, input.Container))

	_ = sidecarapi.FilesystemListResponse(FilesystemListResponse{}) // assert field compatibility
	var sidecarResp sidecarapi.FilesystemListResponse
	if err := h.proxyJSONGet(ctx, input.ID, u, &sidecarResp); err != nil {
		return nil, err
	}

	entries := make([]FileInfo, len(sidecarResp.Entries))
	for i, e := range sidecarResp.Entries {
		_ = sidecarapi.FileInfo(FileInfo{}) // assert field compatibility
		entries[i] = FileInfo(e)
	}

	return &FilesystemListOutput{
		Body: FilesystemListResponse{Entries: entries},
	}, nil
}

func (h *Handlers) StatFilesystem(ctx context.Context, input *FilesystemStatInput) (*FilesystemStatOutput, error) {
	sb, err := apigateway.GetReadySandbox(ctx, h.k8sClient, h.sandboxNamespace, input.ID, h.logger)
	if err != nil {
		return nil, err
	}

	u := h.sidecarURL(sb.Status.PodIP, "/filesystem/stat", filesystemParams(input.Path, input.Container))

	_ = sidecarapi.FileInfo(FileInfo{}) // assert field compatibility
	var sidecarResp sidecarapi.FileInfo
	if err := h.proxyJSONGet(ctx, input.ID, u, &sidecarResp); err != nil {
		return nil, err
	}

	return &FilesystemStatOutput{
		Body: FileInfo(sidecarResp),
	}, nil
}

func (h *Handlers) MkdirFilesystem(ctx context.Context, input *FilesystemMkdirInput) (*FilesystemMkdirOutput, error) {
	sb, err := apigateway.GetReadySandbox(ctx, h.k8sClient, h.sandboxNamespace, input.ID, h.logger)
	if err != nil {
		return nil, err
	}

	u := h.sidecarURL(sb.Status.PodIP, "/filesystem/mkdir", filesystemParams(input.Path, input.Container))

	_ = sidecarapi.FilesystemMkdirResponse(FilesystemMkdirResponse{}) // assert field compatibility
	var sidecarResp sidecarapi.FilesystemMkdirResponse
	if err := h.proxySimple(ctx, http.MethodPost, input.ID, u, &sidecarResp); err != nil {
		return nil, err
	}

	return &FilesystemMkdirOutput{
		Body: FilesystemMkdirResponse{AbsolutePath: sidecarResp.AbsolutePath},
	}, nil
}

func (h *Handlers) RenameFilesystem(ctx context.Context, input *FilesystemRenameInput) (*FilesystemRenameOutput, error) {
	sb, err := apigateway.GetReadySandbox(ctx, h.k8sClient, h.sandboxNamespace, input.ID, h.logger)
	if err != nil {
		return nil, err
	}

	params := filesystemParams(input.Path, input.Container)
	params.Set("newPath", input.NewPath)
	u := h.sidecarURL(sb.Status.PodIP, "/filesystem/rename", params)

	_ = sidecarapi.FilesystemRenameResponse(FilesystemRenameResponse{}) // assert field compatibility
	var sidecarResp sidecarapi.FilesystemRenameResponse
	if err := h.proxySimple(ctx, http.MethodPost, input.ID, u, &sidecarResp); err != nil {
		return nil, err
	}

	return &FilesystemRenameOutput{
		Body: FilesystemRenameResponse{AbsolutePath: sidecarResp.AbsolutePath},
	}, nil
}

func (h *Handlers) RemoveFilesystem(ctx context.Context, input *FilesystemRemoveInput) (*struct{}, error) {
	sb, err := apigateway.GetReadySandbox(ctx, h.k8sClient, h.sandboxNamespace, input.ID, h.logger)
	if err != nil {
		return nil, err
	}

	u := h.sidecarURL(sb.Status.PodIP, "/filesystem", filesystemParams(input.Path, input.Container))

	if err := h.proxySimple(ctx, http.MethodDelete, input.ID, u, nil); err != nil {
		return nil, err
	}

	return nil, nil
}

func Register(api huma.API, h *Handlers) {
	huma.Register(api, huma.Operation{
		OperationID: "writeSandboxFilesystem",
		Method:      http.MethodPost,
		Path:        "/sandboxes/{id}/filesystem",
		Summary:     "Write a file to sandbox filesystem",
		Description: "Streams a file upload to the specified path in the sandbox container",
		Tags:        []string{"sandboxes", "filesystem"},
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
		Errors:        []int{http.StatusBadRequest, http.StatusNotFound, http.StatusConflict, http.StatusBadGateway},
	}, h.PostFilesystem)

	huma.Register(api, huma.Operation{
		OperationID: "readSandboxFilesystem",
		Method:      http.MethodGet,
		Path:        "/sandboxes/{id}/filesystem",
		Summary:     "Read a file from sandbox filesystem",
		Description: "Streams a file download from the specified path in the sandbox container",
		Tags:        []string{"sandboxes", "filesystem"},
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
		Errors: []int{http.StatusBadRequest, http.StatusNotFound, http.StatusConflict, http.StatusBadGateway},
	}, h.GetFilesystem)

	huma.Register(api, huma.Operation{
		OperationID: "listSandboxFilesystem",
		Method:      http.MethodGet,
		Path:        "/sandboxes/{id}/filesystem/list",
		Summary:     "List directory contents",
		Description: "Lists files and directories at the specified path in the sandbox container",
		Tags:        []string{"sandboxes", "filesystem"},
		Errors:      []int{http.StatusBadRequest, http.StatusNotFound, http.StatusConflict, http.StatusBadGateway},
	}, h.ListFilesystem)

	huma.Register(api, huma.Operation{
		OperationID: "statSandboxFilesystem",
		Method:      http.MethodGet,
		Path:        "/sandboxes/{id}/filesystem/stat",
		Summary:     "Get file or directory info",
		Description: "Returns metadata about a file or directory in the sandbox container",
		Tags:        []string{"sandboxes", "filesystem"},
		Errors:      []int{http.StatusBadRequest, http.StatusNotFound, http.StatusConflict, http.StatusBadGateway},
	}, h.StatFilesystem)

	huma.Register(api, huma.Operation{
		OperationID:   "mkdirSandboxFilesystem",
		Method:        http.MethodPost,
		Path:          "/sandboxes/{id}/filesystem/mkdir",
		Summary:       "Create a directory",
		Description:   "Creates a directory and all parent directories at the specified path in the sandbox container",
		Tags:          []string{"sandboxes", "filesystem"},
		DefaultStatus: http.StatusCreated,
		Errors:        []int{http.StatusBadRequest, http.StatusNotFound, http.StatusConflict, http.StatusBadGateway},
	}, h.MkdirFilesystem)

	huma.Register(api, huma.Operation{
		OperationID: "renameSandboxFilesystem",
		Method:      http.MethodPost,
		Path:        "/sandboxes/{id}/filesystem/rename",
		Summary:     "Rename or move a file or directory",
		Description: "Renames or moves a file or directory within the sandbox container",
		Tags:        []string{"sandboxes", "filesystem"},
		Errors:      []int{http.StatusBadRequest, http.StatusNotFound, http.StatusConflict, http.StatusBadGateway},
	}, h.RenameFilesystem)

	huma.Register(api, huma.Operation{
		OperationID:   "removeSandboxFilesystem",
		Method:        http.MethodDelete,
		Path:          "/sandboxes/{id}/filesystem",
		Summary:       "Remove a file or directory",
		Description:   "Removes a file or directory (recursively) from the sandbox container",
		Tags:          []string{"sandboxes", "filesystem"},
		DefaultStatus: http.StatusNoContent,
		Errors:        []int{http.StatusBadRequest, http.StatusNotFound, http.StatusConflict, http.StatusBadGateway},
	}, h.RemoveFilesystem)
}
