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

package command

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
	"time"

	"github.com/danielgtaylor/huma/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apigateway "github.com/isola-run/isola/internal/api-gateway"
	"github.com/isola-run/isola/internal/constants"
	"github.com/isola-run/isola/internal/httputil"
	sidecarapi "github.com/isola-run/isola/internal/sidecar-api"
)

type CreateCommandRequest struct {
	Args           []string          `json:"args" required:"true" minItems:"1" doc:"Argument vector: Args[0] is the executable path, Args[1:] are its arguments"`
	Env            map[string]string `json:"env,omitempty" doc:"Environment variable overrides"`
	Cwd            string            `json:"cwd,omitempty" doc:"Working directory inside the sandbox. Defaults to the target container process's working directory if omitted."`
	TimeoutSeconds *int              `json:"timeoutSeconds,omitempty" minimum:"1" doc:"Max execution time in seconds"`
}

type CreateCommandResponse struct {
	ID string `json:"id" doc:"Unique command identifier"`
}

type CommandStatusResponse struct {
	ExitCode *int `json:"exitCode" doc:"Process exit code, null if still running"`
}

type CreateSandboxCommandInput struct {
	SandboxID string `path:"sandboxId" minLength:"1" maxLength:"47" pattern:"^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$" doc:"Sandbox identifier"`
	Container string `query:"container,omitempty" minLength:"1" maxLength:"63" pattern:"^[a-z0-9]([-a-z0-9]*[a-z0-9])?$" doc:"Container name. Defaults to the only container if there is one, otherwise it's required."`
	Body      CreateCommandRequest
}

type CreateSandboxCommandOutput struct {
	Body CreateCommandResponse
}

type GetSandboxCommandStatusInput struct {
	SandboxID string `path:"sandboxId" minLength:"1" maxLength:"47" pattern:"^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$" doc:"Sandbox identifier"`
	ID        string `path:"id" pattern:"^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$" doc:"Command identifier"`
	// Lower than the sandbox-sidecar's max (30 seconds) so the gateway always terminates first
	// also aligns with the safe (assuming possible proxies etc) long polling value according to https://datatracker.ietf.org/doc/html/rfc6202
	// and of course it must be lower than the server's WriteTimeout.
	WaitSeconds int `query:"waitSeconds,omitempty" minimum:"0" maximum:"25" doc:"Max seconds to wait for the command to exit. 0 or absent returns immediately."`
}

type GetSandboxCommandStatusOutput struct {
	Body CommandStatusResponse
}

type GetSandboxCommandStreamInput struct {
	SandboxID   string `path:"sandboxId" minLength:"1" maxLength:"47" pattern:"^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$" doc:"Sandbox identifier"`
	ID          string `path:"id" pattern:"^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$" doc:"Command identifier"`
	LastEventID string `header:"Last-Event-ID" doc:"Byte offset to resume from (SSE Last-Event-ID)"`
}

type PostSandboxCommandStdinInput struct {
	SandboxID string `path:"sandboxId" minLength:"1" maxLength:"47" pattern:"^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$" doc:"Sandbox identifier"`
	ID        string `path:"id" pattern:"^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$" doc:"Command identifier"`
	apigateway.BodyStream
}

type CloseSandboxCommandStdinInput struct {
	SandboxID string `path:"sandboxId" minLength:"1" maxLength:"47" pattern:"^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$" doc:"Sandbox identifier"`
	ID        string `path:"id" pattern:"^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$" doc:"Command identifier"`
}

type DeleteSandboxCommandInput struct {
	SandboxID string `path:"sandboxId" minLength:"1" maxLength:"47" pattern:"^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$" doc:"Sandbox identifier"`
	ID        string `path:"id" pattern:"^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$" doc:"Command identifier"`
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

func (h *Handlers) PostCommand(ctx context.Context, input *CreateSandboxCommandInput) (*CreateSandboxCommandOutput, error) {
	sb, err := apigateway.GetReadySandbox(ctx, h.k8sClient, h.sandboxNamespace, input.SandboxID, h.logger)
	if err != nil {
		return nil, err
	}

	params := url.Values{}
	if input.Container != "" {
		params.Set("container", input.Container)
	}
	sidecarURL := fmt.Sprintf("http://%s:%d/v1/commands?%s", sb.Status.PodIP, h.sidecarPort, params.Encode())

	_ = sidecarapi.CreateCommandRequest(CreateCommandRequest{}) // assert field compatibility
	body, err := json.Marshal(input.Body)
	if err != nil {
		h.logger.Error("failed to marshal command request", "error", err)
		return nil, huma.Error500InternalServerError("failed to marshal command request")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sidecarURL, bytes.NewReader(body))
	if err != nil {
		h.logger.Error("failed to build sidecar request", "error", err)
		return nil, huma.Error500InternalServerError("failed to build sidecar request")
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := h.httpClient.Do(req)
	if err != nil {
		h.logger.Error("sidecar request failed", "error", err, "id", input.SandboxID)
		return nil, huma.Error502BadGateway("failed to reach sidecar")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return nil, apigateway.HandleSidecarError(resp, input.SandboxID, h.logger)
	}

	_ = sidecarapi.CreateCommandResponse(CreateCommandResponse{}) // assert field compatibility
	var sidecarResp sidecarapi.CreateCommandResponse
	if err := json.NewDecoder(resp.Body).Decode(&sidecarResp); err != nil {
		h.logger.Error("failed to decode sidecar response", "error", err, "id", input.SandboxID)
		return nil, huma.Error502BadGateway("invalid sidecar response")
	}

	return &CreateSandboxCommandOutput{
		Body: CreateCommandResponse{ID: sidecarResp.ID},
	}, nil
}

func (h *Handlers) GetCommandStatus(ctx context.Context, input *GetSandboxCommandStatusInput) (*GetSandboxCommandStatusOutput, error) {
	sb, err := apigateway.GetReadySandbox(ctx, h.k8sClient, h.sandboxNamespace, input.SandboxID, h.logger)
	if err != nil {
		return nil, err
	}

	sidecarURL := fmt.Sprintf("http://%s:%d/v1/commands/%s/status", sb.Status.PodIP, h.sidecarPort, input.ID)
	if input.WaitSeconds > 0 {
		sidecarURL += fmt.Sprintf("?waitSeconds=%d", input.WaitSeconds)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sidecarURL, nil)
	if err != nil {
		h.logger.Error("failed to build sidecar request", "error", err)
		return nil, huma.Error500InternalServerError("failed to build sidecar request")
	}

	resp, err := h.httpClient.Do(req)
	if err != nil {
		h.logger.Error("sidecar request failed", "error", err, "id", input.SandboxID)
		return nil, huma.Error502BadGateway("failed to reach sidecar")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return nil, apigateway.HandleSidecarError(resp, input.SandboxID, h.logger)
	}

	_ = sidecarapi.CommandStatusResponse(CommandStatusResponse{}) // assert field compatibility
	var sidecarResp sidecarapi.CommandStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&sidecarResp); err != nil {
		h.logger.Error("failed to decode sidecar response", "error", err, "id", input.SandboxID)
		return nil, huma.Error502BadGateway("invalid sidecar response")
	}

	return &GetSandboxCommandStatusOutput{
		Body: CommandStatusResponse{ExitCode: sidecarResp.ExitCode},
	}, nil
}

func (h *Handlers) GetCommandStdout(ctx context.Context, input *GetSandboxCommandStreamInput) (*huma.StreamResponse, error) {
	return h.proxyStream(ctx, input.SandboxID, input.ID, "stdout", input.LastEventID)
}

func (h *Handlers) GetCommandStderr(ctx context.Context, input *GetSandboxCommandStreamInput) (*huma.StreamResponse, error) {
	return h.proxyStream(ctx, input.SandboxID, input.ID, "stderr", input.LastEventID)
}

func (h *Handlers) proxyStream(ctx context.Context, sandboxID, cmdID, stream, lastEventID string) (*huma.StreamResponse, error) {
	sb, err := apigateway.GetReadySandbox(ctx, h.k8sClient, h.sandboxNamespace, sandboxID, h.logger)
	if err != nil {
		return nil, err
	}

	sidecarURL := fmt.Sprintf("http://%s:%d/v1/commands/%s/%s", sb.Status.PodIP, h.sidecarPort, cmdID, stream)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sidecarURL, nil)
	if err != nil {
		h.logger.Error("failed to build sidecar request", "error", err)
		return nil, huma.Error500InternalServerError("failed to build sidecar request")
	}
	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}

	resp, err := h.httpClient.Do(req) //nolint:bodyclose // closed in both error and streaming paths below
	if err != nil {
		h.logger.Error("sidecar request failed", "error", err, "id", sandboxID)
		return nil, huma.Error502BadGateway("failed to reach sidecar")
	}

	if resp.StatusCode >= 400 {
		defer func() { _ = resp.Body.Close() }()
		return nil, apigateway.HandleSidecarError(resp, sandboxID, h.logger)
	}

	return &huma.StreamResponse{
		Body: func(ctx huma.Context) {
			defer func() { _ = resp.Body.Close() }()

			ctx.SetHeader("Content-Type", "text/event-stream")
			// no-cache, since the stream change over time
			// private, since the stream is of a specific sandbox
			ctx.SetHeader("Cache-Control", "no-cache, private")
			// X-Accel-Buffering: no, disable nginx buffering (serve immediately)
			ctx.SetHeader("X-Accel-Buffering", "no")

			rc := httputil.ResponseController(ctx)
			dw := httputil.NewDeadlineWriter(ctx.BodyWriter(), rc, httputil.StreamTimeout)
			fw := httputil.NewTimedFlushWriter(dw, 100*time.Millisecond)
			defer fw.Stop()

			if _, err := io.Copy(fw, resp.Body); err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, syscall.EPIPE) {
					h.logger.Warn("client disconnected during command stream", "error", err, "id", sandboxID)
				} else {
					h.logger.Error("sidecar error streaming command output", "error", err, "id", sandboxID)
				}
			}
		},
	}, nil
}

func (h *Handlers) PostCommandStdin(ctx context.Context, input *PostSandboxCommandStdinInput) (*struct{}, error) {
	sb, err := apigateway.GetReadySandbox(ctx, h.k8sClient, h.sandboxNamespace, input.SandboxID, h.logger)
	if err != nil {
		return nil, err
	}

	sidecarURL := fmt.Sprintf("http://%s:%d/v1/commands/%s/stdin", sb.Status.PodIP, h.sidecarPort, input.ID)

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

func (h *Handlers) CloseCommandStdin(ctx context.Context, input *CloseSandboxCommandStdinInput) (*struct{}, error) {
	sb, err := apigateway.GetReadySandbox(ctx, h.k8sClient, h.sandboxNamespace, input.SandboxID, h.logger)
	if err != nil {
		return nil, err
	}

	sidecarURL := fmt.Sprintf("http://%s:%d/v1/commands/%s/stdin/close", sb.Status.PodIP, h.sidecarPort, input.ID)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sidecarURL, nil)
	if err != nil {
		h.logger.Error("failed to build sidecar request", "error", err)
		return nil, huma.Error500InternalServerError("failed to build sidecar request")
	}

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

func (h *Handlers) DeleteCommand(ctx context.Context, input *DeleteSandboxCommandInput) (*struct{}, error) {
	sb, err := apigateway.GetReadySandbox(ctx, h.k8sClient, h.sandboxNamespace, input.SandboxID, h.logger)
	if err != nil {
		return nil, err
	}

	sidecarURL := fmt.Sprintf("http://%s:%d/v1/commands/%s", sb.Status.PodIP, h.sidecarPort, input.ID)

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, sidecarURL, nil)
	if err != nil {
		h.logger.Error("failed to build sidecar request", "error", err)
		return nil, huma.Error500InternalServerError("failed to build sidecar request")
	}

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

func Register(api huma.API, h *Handlers) {
	huma.Register(api, huma.Operation{
		OperationID:   "createSandboxCommand",
		Method:        http.MethodPost,
		Path:          "/sandboxes/{sandboxId}/commands",
		Summary:       "Start a command in a sandbox",
		Description:   "Starts a new command in the sandbox container and returns a command ID for tracking. Commands always run as root (UID 0, GID 0).",
		Tags:          []string{"sandboxes", "commands"},
		DefaultStatus: http.StatusAccepted,
		Errors:        []int{http.StatusUnauthorized, http.StatusBadRequest, http.StatusNotFound, http.StatusConflict, http.StatusBadGateway},
	}, h.PostCommand)

	huma.Register(api, huma.Operation{
		OperationID: "getSandboxCommandStatus",
		Method:      http.MethodGet,
		Path:        "/sandboxes/{sandboxId}/commands/{id}/status",
		Summary:     "Get command status",
		Description: "Returns the exit code of the command, or null if still running. Supports long-polling via ?waitSeconds=N to block until the command exits or the wait expires.",
		Tags:        []string{"sandboxes", "commands"},
		Errors:      []int{http.StatusUnauthorized, http.StatusNotFound, http.StatusConflict, http.StatusBadGateway},
	}, h.GetCommandStatus)

	huma.Register(api, huma.Operation{
		OperationID: "getSandboxCommandStdout",
		Method:      http.MethodGet,
		Path:        "/sandboxes/{sandboxId}/commands/{id}/stdout",
		Summary:     "Stream command stdout",
		Description: "Streams the command's stdout as Server-Sent Events. The connection remains open until the command exits. Supports resuming via Last-Event-ID header.",
		Tags:        []string{"sandboxes", "commands"},
		Responses: map[string]*huma.Response{
			"200": {
				Description: "Command stdout stream",
				Content: map[string]*huma.MediaType{
					"text/event-stream": {
						Schema: &huma.Schema{Type: "string"},
					},
				},
			},
		},
		Errors: []int{http.StatusUnauthorized, http.StatusNotFound, http.StatusConflict, http.StatusBadGateway},
	}, h.GetCommandStdout)

	huma.Register(api, huma.Operation{
		OperationID: "getSandboxCommandStderr",
		Method:      http.MethodGet,
		Path:        "/sandboxes/{sandboxId}/commands/{id}/stderr",
		Summary:     "Stream command stderr",
		Description: "Streams the command's stderr as Server-Sent Events. The connection remains open until the command exits. Supports resuming via Last-Event-ID header.",
		Tags:        []string{"sandboxes", "commands"},
		Responses: map[string]*huma.Response{
			"200": {
				Description: "Command stderr stream",
				Content: map[string]*huma.MediaType{
					"text/event-stream": {
						Schema: &huma.Schema{Type: "string"},
					},
				},
			},
		},
		Errors: []int{http.StatusUnauthorized, http.StatusNotFound, http.StatusConflict, http.StatusBadGateway},
	}, h.GetCommandStderr)

	huma.Register(api, huma.Operation{
		OperationID: "postSandboxCommandStdin",
		Method:      http.MethodPost,
		Path:        "/sandboxes/{sandboxId}/commands/{id}/stdin",
		Summary:     "Write to command stdin",
		Description: "Writes raw bytes to the command's stdin",
		Tags:        []string{"sandboxes", "commands"},
		RequestBody: &huma.RequestBody{
			Required: true,
			Content: map[string]*huma.MediaType{
				"application/octet-stream": {
					Schema: &huma.Schema{Type: "string", Format: "binary"},
				},
			},
		},
		DefaultStatus: http.StatusNoContent,
		Errors:        []int{http.StatusUnauthorized, http.StatusNotFound, http.StatusConflict, http.StatusBadGateway},
	}, h.PostCommandStdin)

	huma.Register(api, huma.Operation{
		OperationID:   "closeSandboxCommandStdin",
		Method:        http.MethodPost,
		Path:          "/sandboxes/{sandboxId}/commands/{id}/stdin/close",
		Summary:       "Close command stdin",
		Description:   "Closes the command's stdin pipe",
		Tags:          []string{"sandboxes", "commands"},
		DefaultStatus: http.StatusNoContent,
		Errors:        []int{http.StatusUnauthorized, http.StatusNotFound, http.StatusConflict, http.StatusBadGateway},
	}, h.CloseCommandStdin)

	huma.Register(api, huma.Operation{
		OperationID:   "deleteSandboxCommand",
		Method:        http.MethodDelete,
		Path:          "/sandboxes/{sandboxId}/commands/{id}",
		Summary:       "Kill a command",
		Description:   "Kills the command process. Idempotent for already-exited commands.",
		Tags:          []string{"sandboxes", "commands"},
		DefaultStatus: http.StatusNoContent,
		Errors:        []int{http.StatusUnauthorized, http.StatusNotFound, http.StatusConflict, http.StatusBadGateway},
	}, h.DeleteCommand)
}
