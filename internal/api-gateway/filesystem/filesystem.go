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
	"bytes"
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

	apigateway "github.com/isola-run/isola/internal/api-gateway"
	"github.com/isola-run/isola/internal/constants"
	"github.com/isola-run/isola/internal/httputil"
	sidecarapi "github.com/isola-run/isola/internal/sidecar-api"
)

type FilesystemWriteInput struct {
	SandboxID string `path:"sandboxId" minLength:"1" maxLength:"47" pattern:"^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$" doc:"Sandbox identifier"`
	Path      string `query:"path" required:"true" minLength:"1" doc:"Destination path (absolute or relative to container cwd)"`
	Container string `query:"container,omitempty" minLength:"1" maxLength:"63" pattern:"^[a-z0-9]([-a-z0-9]*[a-z0-9])?$" doc:"Container name. Defaults to the only container if there is one, otherwise it's required."`
	apigateway.BodyStream
}

type FilesystemReadInput struct {
	SandboxID string `path:"sandboxId" minLength:"1" maxLength:"47" pattern:"^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$" doc:"Sandbox identifier"`
	Path      string `query:"path" required:"true" minLength:"1" doc:"Source path (absolute or relative to container cwd)"`
	Container string `query:"container,omitempty" minLength:"1" maxLength:"63" pattern:"^[a-z0-9]([-a-z0-9]*[a-z0-9])?$" doc:"Container name. Defaults to the only container if there is one, otherwise it's required."`
}

type FilesystemListInput struct {
	SandboxID string `path:"sandboxId" minLength:"1" maxLength:"47" pattern:"^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$" doc:"Sandbox identifier"`
	Path      string `query:"path" required:"true" minLength:"1" doc:"Directory path (absolute or relative to container cwd)"`
	Container string `query:"container,omitempty" minLength:"1" maxLength:"63" pattern:"^[a-z0-9]([-a-z0-9]*[a-z0-9])?$" doc:"Container name. Defaults to the only container if there is one, otherwise it's required."`
}

type FilesystemListOutput struct {
	Body sidecarapi.ListFilesystemEntriesResponse
}

type FilesystemStatInput struct {
	SandboxID string `path:"sandboxId" minLength:"1" maxLength:"47" pattern:"^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$" doc:"Sandbox identifier"`
	Path      string `query:"path" required:"true" minLength:"1" doc:"Path to stat (absolute or relative to container cwd)"`
	Container string `query:"container,omitempty" minLength:"1" maxLength:"63" pattern:"^[a-z0-9]([-a-z0-9]*[a-z0-9])?$" doc:"Container name. Defaults to the only container if there is one, otherwise it's required."`
}

type FilesystemStatOutput struct {
	Body sidecarapi.FilesystemEntry
}

type FilesystemDeleteInput struct {
	SandboxID string `path:"sandboxId" minLength:"1" maxLength:"47" pattern:"^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$" doc:"Sandbox identifier"`
	Path      string `query:"path" required:"true" minLength:"1" doc:"Path to delete (absolute or relative to container cwd)"`
	Recursive bool   `query:"recursive,omitempty" doc:"Delete directories and their contents recursively"`
	Container string `query:"container,omitempty" minLength:"1" maxLength:"63" pattern:"^[a-z0-9]([-a-z0-9]*[a-z0-9])?$" doc:"Container name. Defaults to the only container if there is one, otherwise it's required."`
}

type FilesystemMkdirInput struct {
	SandboxID string `path:"sandboxId" minLength:"1" maxLength:"47" pattern:"^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$" doc:"Sandbox identifier"`
	Path      string `query:"path" required:"true" minLength:"1" doc:"Directory path to create (absolute or relative to container cwd)"`
	Container string `query:"container,omitempty" minLength:"1" maxLength:"63" pattern:"^[a-z0-9]([-a-z0-9]*[a-z0-9])?$" doc:"Container name. Defaults to the only container if there is one, otherwise it's required."`
}

type FilesystemMoveInput struct {
	SandboxID string `path:"sandboxId" minLength:"1" maxLength:"47" pattern:"^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$" doc:"Sandbox identifier"`
	Container string `query:"container,omitempty" minLength:"1" maxLength:"63" pattern:"^[a-z0-9]([-a-z0-9]*[a-z0-9])?$" doc:"Container name. Defaults to the only container if there is one, otherwise it's required."`
	Body      sidecarapi.MoveFilesystemEntryRequest
}

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

func (h *Handlers) PostFilesystem(ctx context.Context, input *FilesystemWriteInput) (*struct{}, error) {
	sb, err := apigateway.GetReadySandbox(ctx, h.k8sClient, h.sandboxNamespace, input.SandboxID, h.logger)
	if err != nil {
		return nil, err
	}

	params := url.Values{}
	params.Set("path", input.Path)
	if input.Container != "" {
		params.Set("container", input.Container)
	}
	sidecarURL := fmt.Sprintf("http://%s:%d/v1/filesystem?%s", sb.Status.PodIP, h.sidecarPort, params.Encode())

	stream := httputil.NewDeadlineReader(input.Stream, input.ResponseController, httputil.StreamTimeout)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sidecarURL, stream)
	if err != nil {
		h.logger.Error("failed to build sidecar request", "error", err)
		return nil, huma.Error500InternalServerError("failed to build sidecar request")
	}
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := h.httpClient.Do(req)
	if err != nil {
		h.logger.Error("sidecar request failed", "error", err, "id", input.SandboxID)
		return nil, huma.Error502BadGateway("failed to reach sidecar")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return nil, apigateway.HandleSidecarError(resp, input.SandboxID, h.logger)
	}

	return nil, nil
}

func (h *Handlers) GetFilesystem(ctx context.Context, input *FilesystemReadInput) (*huma.StreamResponse, error) {
	sb, err := apigateway.GetReadySandbox(ctx, h.k8sClient, h.sandboxNamespace, input.SandboxID, h.logger)
	if err != nil {
		return nil, err
	}

	params := url.Values{}
	params.Set("path", input.Path)
	if input.Container != "" {
		params.Set("container", input.Container)
	}
	sidecarURL := fmt.Sprintf("http://%s:%d/v1/filesystem?%s", sb.Status.PodIP, h.sidecarPort, params.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sidecarURL, nil)
	if err != nil {
		h.logger.Error("failed to build sidecar request", "error", err)
		return nil, huma.Error500InternalServerError("failed to build sidecar request")
	}

	resp, err := h.httpClient.Do(req) //nolint:bodyclose // closed in both error and streaming paths below
	if err != nil {
		h.logger.Error("sidecar request failed", "error", err, "id", input.SandboxID)
		return nil, huma.Error502BadGateway("failed to reach sidecar")
	}

	if resp.StatusCode >= 400 {
		defer func() { _ = resp.Body.Close() }()
		return nil, apigateway.HandleSidecarError(resp, input.SandboxID, h.logger)
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

			rc := httputil.ResponseController(ctx)
			dw := httputil.NewDeadlineWriter(ctx.BodyWriter(), rc, httputil.StreamTimeout)

			if _, err := io.Copy(dw, resp.Body); err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, syscall.EPIPE) {
					h.logger.Warn("client disconnected during file stream", "error", err, "id", input.SandboxID)
				} else {
					h.logger.Error("sidecar error streaming file", "error", err, "id", input.SandboxID)
				}
			}
		},
	}, nil
}

// callSidecar resolves the ready sandbox, performs a request against its sidecar,
// and maps transport and sidecar-side failures to Huma errors. On success the
// caller must close the response body.
func (h *Handlers) callSidecar(ctx context.Context, sandboxID, method, sidecarPath string, params url.Values, jsonBody any) (*http.Response, error) {
	sb, err := apigateway.GetReadySandbox(ctx, h.k8sClient, h.sandboxNamespace, sandboxID, h.logger)
	if err != nil {
		return nil, err
	}

	sidecarURL := fmt.Sprintf("http://%s:%d%s", sb.Status.PodIP, h.sidecarPort, sidecarPath)
	if len(params) > 0 {
		sidecarURL += "?" + params.Encode()
	}

	var bodyReader io.Reader
	if jsonBody != nil {
		body, err := json.Marshal(jsonBody)
		if err != nil {
			h.logger.Error("failed to marshal sidecar request", "error", err)
			return nil, huma.Error500InternalServerError("failed to marshal sidecar request")
		}
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, sidecarURL, bodyReader)
	if err != nil {
		h.logger.Error("failed to build sidecar request", "error", err)
		return nil, huma.Error500InternalServerError("failed to build sidecar request")
	}
	if jsonBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := h.httpClient.Do(req)
	if err != nil {
		h.logger.Error("sidecar request failed", "error", err, "id", sandboxID)
		return nil, huma.Error502BadGateway("failed to reach sidecar")
	}

	if resp.StatusCode >= 400 {
		defer func() { _ = resp.Body.Close() }()
		return nil, apigateway.HandleSidecarError(resp, sandboxID, h.logger)
	}

	return resp, nil
}

// fetchJSON performs a sidecar request via callSidecar and decodes its JSON
// response body into T, a shared sidecar-api contract type.
func fetchJSON[T any](ctx context.Context, h *Handlers, sandboxID, method, sidecarPath string, params url.Values, jsonBody any) (T, error) {
	var out T
	resp, err := h.callSidecar(ctx, sandboxID, method, sidecarPath, params, jsonBody)
	if err != nil {
		return out, err
	}
	defer func() { _ = resp.Body.Close() }()

	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		h.logger.Error("failed to decode sidecar response", "error", err, "id", sandboxID)
		return out, huma.Error502BadGateway("invalid sidecar response")
	}
	return out, nil
}

// callSidecarNoContent performs a sidecar request whose successful response has
// no body, closing the response. It is the void-operation counterpart to fetchJSON.
func (h *Handlers) callSidecarNoContent(ctx context.Context, sandboxID, method, sidecarPath string, params url.Values, jsonBody any) error {
	resp, err := h.callSidecar(ctx, sandboxID, method, sidecarPath, params, jsonBody)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

func pathParams(path, container string) url.Values {
	params := url.Values{}
	params.Set("path", path)
	if container != "" {
		params.Set("container", container)
	}
	return params
}

func (h *Handlers) ListFilesystemEntries(ctx context.Context, input *FilesystemListInput) (*FilesystemListOutput, error) {
	body, err := fetchJSON[sidecarapi.ListFilesystemEntriesResponse](ctx, h, input.SandboxID, http.MethodGet, "/v1/filesystem/entries", pathParams(input.Path, input.Container), nil)
	if err != nil {
		return nil, err
	}
	return &FilesystemListOutput{Body: body}, nil
}

func (h *Handlers) StatFilesystemEntry(ctx context.Context, input *FilesystemStatInput) (*FilesystemStatOutput, error) {
	body, err := fetchJSON[sidecarapi.FilesystemEntry](ctx, h, input.SandboxID, http.MethodGet, "/v1/filesystem/stat", pathParams(input.Path, input.Container), nil)
	if err != nil {
		return nil, err
	}
	return &FilesystemStatOutput{Body: body}, nil
}

func (h *Handlers) DeleteFilesystemEntry(ctx context.Context, input *FilesystemDeleteInput) (*struct{}, error) {
	params := pathParams(input.Path, input.Container)
	if input.Recursive {
		params.Set("recursive", "true")
	}
	if err := h.callSidecarNoContent(ctx, input.SandboxID, http.MethodDelete, "/v1/filesystem", params, nil); err != nil {
		return nil, err
	}
	return nil, nil
}

func (h *Handlers) CreateFilesystemDirectory(ctx context.Context, input *FilesystemMkdirInput) (*struct{}, error) {
	if err := h.callSidecarNoContent(ctx, input.SandboxID, http.MethodPost, "/v1/filesystem/directories", pathParams(input.Path, input.Container), nil); err != nil {
		return nil, err
	}
	return nil, nil
}

func (h *Handlers) MoveFilesystemEntry(ctx context.Context, input *FilesystemMoveInput) (*struct{}, error) {
	params := url.Values{}
	if input.Container != "" {
		params.Set("container", input.Container)
	}
	if err := h.callSidecarNoContent(ctx, input.SandboxID, http.MethodPost, "/v1/filesystem/move", params, input.Body); err != nil {
		return nil, err
	}
	return nil, nil
}

func Register(api huma.API, h *Handlers) {
	huma.Register(api, huma.Operation{
		OperationID: "writeSandboxFilesystem",
		Method:      http.MethodPost,
		Path:        "/sandboxes/{sandboxId}/filesystem",
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
		DefaultStatus: http.StatusNoContent,
		Errors:        []int{http.StatusBadRequest, http.StatusNotFound, http.StatusConflict, http.StatusBadGateway},
	}, h.PostFilesystem)

	huma.Register(api, huma.Operation{
		OperationID: "readSandboxFilesystem",
		Method:      http.MethodGet,
		Path:        "/sandboxes/{sandboxId}/filesystem",
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
		OperationID: "listSandboxFilesystemEntries",
		Method:      http.MethodGet,
		Path:        "/sandboxes/{sandboxId}/filesystem/entries",
		Summary:     "List directory entries in sandbox filesystem",
		Description: "Returns metadata for each entry in the specified directory inside the sandbox container. Symlinks are reported, not followed.",
		Tags:        []string{"sandboxes", "filesystem"},
		Errors:      []int{http.StatusBadRequest, http.StatusNotFound, http.StatusConflict, http.StatusBadGateway},
	}, h.ListFilesystemEntries)

	huma.Register(api, huma.Operation{
		OperationID: "statSandboxFilesystemEntry",
		Method:      http.MethodGet,
		Path:        "/sandboxes/{sandboxId}/filesystem/stat",
		Summary:     "Stat a path in sandbox filesystem",
		Description: "Returns metadata for the file, directory, or symlink at the specified path inside the sandbox container. Symlinks are reported, not followed.",
		Tags:        []string{"sandboxes", "filesystem"},
		Errors:      []int{http.StatusBadRequest, http.StatusNotFound, http.StatusConflict, http.StatusBadGateway},
	}, h.StatFilesystemEntry)

	huma.Register(api, huma.Operation{
		OperationID:   "deleteSandboxFilesystemEntry",
		Method:        http.MethodDelete,
		Path:          "/sandboxes/{sandboxId}/filesystem",
		Summary:       "Delete a file or directory from sandbox filesystem",
		Description:   "Deletes the file, empty directory, or symlink at the specified path in the sandbox container. Set recursive=true to delete a directory and its contents.",
		Tags:          []string{"sandboxes", "filesystem"},
		DefaultStatus: http.StatusNoContent,
		Errors:        []int{http.StatusBadRequest, http.StatusNotFound, http.StatusConflict, http.StatusBadGateway},
	}, h.DeleteFilesystemEntry)

	huma.Register(api, huma.Operation{
		OperationID:   "createSandboxFilesystemDirectory",
		Method:        http.MethodPost,
		Path:          "/sandboxes/{sandboxId}/filesystem/directories",
		Summary:       "Create a directory in sandbox filesystem",
		Description:   "Creates a directory at the specified path in the sandbox container, including missing parent directories. Idempotent if the directory already exists.",
		Tags:          []string{"sandboxes", "filesystem"},
		DefaultStatus: http.StatusNoContent,
		Errors:        []int{http.StatusBadRequest, http.StatusNotFound, http.StatusConflict, http.StatusBadGateway},
	}, h.CreateFilesystemDirectory)

	huma.Register(api, huma.Operation{
		OperationID:   "moveSandboxFilesystemEntry",
		Method:        http.MethodPost,
		Path:          "/sandboxes/{sandboxId}/filesystem/move",
		Summary:       "Move a file or directory within sandbox filesystem",
		Description:   "Renames or moves a file, directory, or symlink inside the sandbox container. Parent directories of the destination are created automatically. An existing destination file is overwritten.",
		Tags:          []string{"sandboxes", "filesystem"},
		DefaultStatus: http.StatusNoContent,
		Errors:        []int{http.StatusBadRequest, http.StatusNotFound, http.StatusConflict, http.StatusBadGateway},
	}, h.MoveFilesystemEntry)
}
