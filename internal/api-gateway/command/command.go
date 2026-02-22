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

	apigateway "github.com/isola-ai/isola-sb/internal/api-gateway"
	"github.com/isola-ai/isola-sb/internal/constants"
	"github.com/isola-ai/isola-sb/internal/httputil"
	sidecarapi "github.com/isola-ai/isola-sb/internal/sidecar-api"
)

// --- Types ---

type CreateCommandRequest struct {
	Args    []string          `json:"args" required:"true" minItems:"1" doc:"Argument vector: Args[0] is the executable path, Args[1:] are its arguments"`
	Env     map[string]string `json:"env,omitempty" doc:"Environment variable overrides"`
	Cwd     string            `json:"cwd,omitempty" doc:"Working directory inside the sandbox. Defaults to the target container process's working directory if omitted."`
	Timeout *int              `json:"timeout,omitempty" minimum:"1" doc:"Max execution time in seconds"`
}

type CreateCommandResponse struct {
	CommandID string `json:"commandId" doc:"Unique command identifier"`
}

type CommandStatusResponse struct {
	ExitCode *int `json:"exitCode" doc:"Process exit code, null if still running"`
}

type CreateSandboxCommandInput struct {
	ID        string `path:"id" doc:"Sandbox identifier"`
	Container string `query:"container,omitempty" doc:"Container name. Defaults to the only container if there is one, otherwise it's required."`
	Body      CreateCommandRequest
}

type CreateSandboxCommandOutput struct {
	Body CreateCommandResponse
}

type GetSandboxCommandStatusInput struct {
	ID             string `path:"id" doc:"Sandbox identifier"`
	CmdID          string `path:"cmdId" pattern:"^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$" doc:"Command identifier"`
	TimeoutSeconds int    `query:"timeoutSeconds,omitempty" minimum:"0" maximum:"600" doc:"Max seconds to wait for the command to exit. 0 or absent returns immediately."`
}

type GetSandboxCommandStatusOutput struct {
	Body CommandStatusResponse
}

type GetSandboxCommandStreamInput struct {
	ID     string `path:"id" doc:"Sandbox identifier"`
	CmdID  string `path:"cmdId" pattern:"^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$" doc:"Command identifier"`
	Offset int64  `query:"offset,omitempty" minimum:"0" doc:"Byte offset to resume from (default 0)"`
}

type PostSandboxCommandStdinInput struct {
	ID    string `path:"id" doc:"Sandbox identifier"`
	CmdID string `path:"cmdId" pattern:"^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$" doc:"Command identifier"`
	apigateway.BodyStream
}

type CloseSandboxCommandStdinInput struct {
	ID    string `path:"id" doc:"Sandbox identifier"`
	CmdID string `path:"cmdId" pattern:"^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$" doc:"Command identifier"`
}

type DeleteSandboxCommandInput struct {
	ID    string `path:"id" doc:"Sandbox identifier"`
	CmdID string `path:"cmdId" pattern:"^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$" doc:"Command identifier"`
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

func (h *Handlers) PostCommand(ctx context.Context, input *CreateSandboxCommandInput) (*CreateSandboxCommandOutput, error) {
	sb, err := apigateway.GetReadySandbox(ctx, h.k8sClient, h.sandboxNamespace, input.ID, h.logger)
	if err != nil {
		return nil, err
	}

	params := url.Values{}
	if input.Container != "" {
		params.Set("container", input.Container)
	}
	sidecarURL := fmt.Sprintf("http://%s:%d/commands?%s", sb.Status.PodIP, h.sidecarPort, params.Encode())

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
		h.logger.Error("sidecar request failed", "error", err, "id", input.ID)
		return nil, huma.Error502BadGateway("failed to reach sidecar")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return nil, apigateway.HandleSidecarError(resp, input.ID, h.logger)
	}

	_ = sidecarapi.CreateCommandResponse(CreateCommandResponse{}) // assert field compatibility
	var sidecarResp sidecarapi.CreateCommandResponse
	if err := json.NewDecoder(resp.Body).Decode(&sidecarResp); err != nil {
		h.logger.Error("failed to decode sidecar response", "error", err, "id", input.ID)
		return nil, huma.Error502BadGateway("invalid sidecar response")
	}

	return &CreateSandboxCommandOutput{
		Body: CreateCommandResponse{CommandID: sidecarResp.CommandID},
	}, nil
}

func (h *Handlers) GetCommandStatus(ctx context.Context, input *GetSandboxCommandStatusInput) (*GetSandboxCommandStatusOutput, error) {
	sb, err := apigateway.GetReadySandbox(ctx, h.k8sClient, h.sandboxNamespace, input.ID, h.logger)
	if err != nil {
		return nil, err
	}

	sidecarURL := fmt.Sprintf("http://%s:%d/commands/%s/status", sb.Status.PodIP, h.sidecarPort, input.CmdID)
	if input.TimeoutSeconds > 0 {
		sidecarURL += fmt.Sprintf("?timeoutSeconds=%d", input.TimeoutSeconds)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sidecarURL, nil)
	if err != nil {
		h.logger.Error("failed to build sidecar request", "error", err)
		return nil, huma.Error500InternalServerError("failed to build sidecar request")
	}

	resp, err := h.httpClient.Do(req)
	if err != nil {
		h.logger.Error("sidecar request failed", "error", err, "id", input.ID)
		return nil, huma.Error502BadGateway("failed to reach sidecar")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return nil, apigateway.HandleSidecarError(resp, input.ID, h.logger)
	}

	_ = sidecarapi.CommandStatusResponse(CommandStatusResponse{}) // assert field compatibility
	var sidecarResp sidecarapi.CommandStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&sidecarResp); err != nil {
		h.logger.Error("failed to decode sidecar response", "error", err, "id", input.ID)
		return nil, huma.Error502BadGateway("invalid sidecar response")
	}

	return &GetSandboxCommandStatusOutput{
		Body: CommandStatusResponse{ExitCode: sidecarResp.ExitCode},
	}, nil
}

func (h *Handlers) GetCommandStdout(ctx context.Context, input *GetSandboxCommandStreamInput) (*huma.StreamResponse, error) {
	return h.proxyStream(ctx, input.ID, input.CmdID, "stdout", input.Offset)
}

func (h *Handlers) GetCommandStderr(ctx context.Context, input *GetSandboxCommandStreamInput) (*huma.StreamResponse, error) {
	return h.proxyStream(ctx, input.ID, input.CmdID, "stderr", input.Offset)
}

func (h *Handlers) proxyStream(ctx context.Context, sandboxID, cmdID, stream string, offset int64) (*huma.StreamResponse, error) {
	sb, err := apigateway.GetReadySandbox(ctx, h.k8sClient, h.sandboxNamespace, sandboxID, h.logger)
	if err != nil {
		return nil, err
	}

	sidecarURL := fmt.Sprintf("http://%s:%d/commands/%s/%s", sb.Status.PodIP, h.sidecarPort, cmdID, stream)
	if offset > 0 {
		sidecarURL += fmt.Sprintf("?offset=%d", offset)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sidecarURL, nil)
	if err != nil {
		h.logger.Error("failed to build sidecar request", "error", err)
		return nil, huma.Error500InternalServerError("failed to build sidecar request")
	}

	// todo benl: can probably refactor the code around here to something like doSidecarRequest
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

			ctx.SetHeader("Content-Type", "application/octet-stream")
			// no-cache, since the stream change over time
			// private, since the stream is of a specific sandbox
			ctx.SetHeader("Cache-Control", "no-cache, private")
			// X-Accel-Buffering: no, disable nginx buffering (serve immediately)
			ctx.SetHeader("X-Accel-Buffering", "no")

			fw := httputil.NewTimedFlushWriter(ctx.BodyWriter(), 100*time.Millisecond)
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
	sb, err := apigateway.GetReadySandbox(ctx, h.k8sClient, h.sandboxNamespace, input.ID, h.logger)
	if err != nil {
		return nil, err
	}

	sidecarURL := fmt.Sprintf("http://%s:%d/commands/%s/stdin", sb.Status.PodIP, h.sidecarPort, input.CmdID)

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

	return nil, nil
}

func (h *Handlers) CloseCommandStdin(ctx context.Context, input *CloseSandboxCommandStdinInput) (*struct{}, error) {
	sb, err := apigateway.GetReadySandbox(ctx, h.k8sClient, h.sandboxNamespace, input.ID, h.logger)
	if err != nil {
		return nil, err
	}

	sidecarURL := fmt.Sprintf("http://%s:%d/commands/%s/stdin/close", sb.Status.PodIP, h.sidecarPort, input.CmdID)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sidecarURL, nil)
	if err != nil {
		h.logger.Error("failed to build sidecar request", "error", err)
		return nil, huma.Error500InternalServerError("failed to build sidecar request")
	}

	resp, err := h.httpClient.Do(req)
	if err != nil {
		h.logger.Error("sidecar request failed", "error", err, "id", input.ID)
		return nil, huma.Error502BadGateway("failed to reach sidecar")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return nil, apigateway.HandleSidecarError(resp, input.ID, h.logger)
	}

	return nil, nil
}

func (h *Handlers) DeleteCommand(ctx context.Context, input *DeleteSandboxCommandInput) (*struct{}, error) {
	sb, err := apigateway.GetReadySandbox(ctx, h.k8sClient, h.sandboxNamespace, input.ID, h.logger)
	if err != nil {
		return nil, err
	}

	sidecarURL := fmt.Sprintf("http://%s:%d/commands/%s", sb.Status.PodIP, h.sidecarPort, input.CmdID)

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, sidecarURL, nil)
	if err != nil {
		h.logger.Error("failed to build sidecar request", "error", err)
		return nil, huma.Error500InternalServerError("failed to build sidecar request")
	}

	resp, err := h.httpClient.Do(req)
	if err != nil {
		h.logger.Error("sidecar request failed", "error", err, "id", input.ID)
		return nil, huma.Error502BadGateway("failed to reach sidecar")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return nil, apigateway.HandleSidecarError(resp, input.ID, h.logger)
	}

	return nil, nil
}

func Register(api huma.API, h *Handlers) {
	huma.Register(api, huma.Operation{
		OperationID:   "createSandboxCommand",
		Method:        http.MethodPost,
		Path:          "/sandboxes/{id}/commands",
		Summary:       "Start a command in a sandbox",
		Description:   "Starts a new command in the sandbox container and returns a command ID for tracking. Commands always run as root (UID 0, GID 0).",
		Tags:          []string{"sandboxes", "commands"},
		DefaultStatus: http.StatusAccepted,
		Errors:        []int{http.StatusBadRequest, http.StatusNotFound, http.StatusConflict, http.StatusBadGateway},
	}, h.PostCommand)

	huma.Register(api, huma.Operation{
		OperationID: "getSandboxCommandStatus",
		Method:      http.MethodGet,
		Path:        "/sandboxes/{id}/commands/{cmdId}/status",
		Summary:     "Get command status",
		Description: "Returns the exit code of the command, or null if still running. Supports long-polling via ?timeoutSeconds=N to block until the command exits or the timeout expires.",
		Tags:        []string{"sandboxes", "commands"},
		Errors:      []int{http.StatusNotFound, http.StatusConflict, http.StatusBadGateway},
	}, h.GetCommandStatus)

	huma.Register(api, huma.Operation{
		OperationID: "getSandboxCommandStdout",
		Method:      http.MethodGet,
		Path:        "/sandboxes/{id}/commands/{cmdId}/stdout",
		Summary:     "Stream command stdout",
		Description: "Streams the command's stdout as raw bytes. The connection remains open until the command exits. Supports resuming via ?offset=N query parameter.",
		Tags:        []string{"sandboxes", "commands"},
		Responses: map[string]*huma.Response{
			"200": {
				Description: "Command stdout stream",
				Content: map[string]*huma.MediaType{
					"application/octet-stream": {
						Schema: &huma.Schema{Type: "string", Format: "binary"},
					},
				},
			},
		},
		Errors: []int{http.StatusNotFound, http.StatusConflict, http.StatusBadGateway},
	}, h.GetCommandStdout)

	huma.Register(api, huma.Operation{
		OperationID: "getSandboxCommandStderr",
		Method:      http.MethodGet,
		Path:        "/sandboxes/{id}/commands/{cmdId}/stderr",
		Summary:     "Stream command stderr",
		Description: "Streams the command's stderr as raw bytes. The connection remains open until the command exits. Supports resuming via ?offset=N query parameter.",
		Tags:        []string{"sandboxes", "commands"},
		Responses: map[string]*huma.Response{
			"200": {
				Description: "Command stderr stream",
				Content: map[string]*huma.MediaType{
					"application/octet-stream": {
						Schema: &huma.Schema{Type: "string", Format: "binary"},
					},
				},
			},
		},
		Errors: []int{http.StatusNotFound, http.StatusConflict, http.StatusBadGateway},
	}, h.GetCommandStderr)

	huma.Register(api, huma.Operation{
		OperationID: "postSandboxCommandStdin",
		Method:      http.MethodPost,
		Path:        "/sandboxes/{id}/commands/{cmdId}/stdin",
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
		Errors:        []int{http.StatusNotFound, http.StatusConflict, http.StatusBadGateway},
	}, h.PostCommandStdin)

	huma.Register(api, huma.Operation{
		OperationID:   "closeSandboxCommandStdin",
		Method:        http.MethodPost,
		Path:          "/sandboxes/{id}/commands/{cmdId}/stdin/close",
		Summary:       "Close command stdin",
		Description:   "Closes the command's stdin pipe, sending EOF to the process",
		Tags:          []string{"sandboxes", "commands"},
		DefaultStatus: http.StatusNoContent,
		Errors:        []int{http.StatusNotFound, http.StatusConflict, http.StatusBadGateway},
	}, h.CloseCommandStdin)

	huma.Register(api, huma.Operation{
		OperationID:   "deleteSandboxCommand",
		Method:        http.MethodDelete,
		Path:          "/sandboxes/{id}/commands/{cmdId}",
		Summary:       "Kill a command",
		Description:   "Kills the command process. Idempotent for already-exited commands.",
		Tags:          []string{"sandboxes", "commands"},
		DefaultStatus: http.StatusNoContent,
		Errors:        []int{http.StatusNotFound, http.StatusConflict, http.StatusBadGateway},
	}, h.DeleteCommand)
}
