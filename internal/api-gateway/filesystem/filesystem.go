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
	"net/url"
	"syscall"

	"github.com/danielgtaylor/huma/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apigateway "github.com/isola-run/isola/internal/api-gateway"
	"github.com/isola-run/isola/internal/constants"
	"github.com/isola-run/isola/internal/httputil"
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
				// io.Copy hides which side failed, so only errors a read cannot produce count as a client disconnect
				if errors.Is(err, context.Canceled) || errors.Is(err, syscall.EPIPE) {
					h.logger.Warn("client disconnected during file stream", "error", err, "id", input.SandboxID)
					return
				}
				h.logger.Error("sidecar error streaming file", "error", err, "id", input.SandboxID)
				// abort the stream with an error, using panic
				panic(http.ErrAbortHandler)
			}
		},
	}, nil
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
}
