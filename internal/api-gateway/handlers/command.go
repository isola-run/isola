package handlers

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

	"github.com/danielgtaylor/huma/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/isola-ai/isola-sb/internal/constants"
	sidecarapi "github.com/isola-ai/isola-sb/internal/sidecar-api"
)

type CommandHandlers struct {
	logger           *slog.Logger
	k8sClient        client.Client
	sandboxNamespace string
	httpClient       HTTPDoer
	sidecarPort      int
}

func NewCommandHandlers(logger *slog.Logger, sandboxNamespace string, k8sClient client.Client, httpClient HTTPDoer) *CommandHandlers {
	return &CommandHandlers{
		logger:           logger,
		k8sClient:        k8sClient,
		sandboxNamespace: sandboxNamespace,
		httpClient:       httpClient,
		sidecarPort:      constants.SidecarPort,
	}
}

func (h *CommandHandlers) PostCommand(ctx context.Context, input *CreateSandboxCommandInput) (*CreateSandboxCommandOutput, error) {
	sb, err := getReadySandbox(ctx, h.k8sClient, h.sandboxNamespace, input.ID, h.logger)
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
		return nil, handleSidecarError(resp, input.ID, h.logger)
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

func (h *CommandHandlers) GetCommandStatus(ctx context.Context, input *GetSandboxCommandStatusInput) (*GetSandboxCommandStatusOutput, error) {
	sb, err := getReadySandbox(ctx, h.k8sClient, h.sandboxNamespace, input.ID, h.logger)
	if err != nil {
		return nil, err
	}

	sidecarURL := fmt.Sprintf("http://%s:%d/commands/%s/status", sb.Status.PodIP, h.sidecarPort, input.CmdID)

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
		return nil, handleSidecarError(resp, input.ID, h.logger)
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

func (h *CommandHandlers) GetCommandStdout(ctx context.Context, input *GetSandboxCommandStreamInput) (*huma.StreamResponse, error) {
	return h.proxyStream(ctx, input.ID, input.CmdID, "stdout", input.Offset)
}

func (h *CommandHandlers) GetCommandStderr(ctx context.Context, input *GetSandboxCommandStreamInput) (*huma.StreamResponse, error) {
	return h.proxyStream(ctx, input.ID, input.CmdID, "stderr", input.Offset)
}

func (h *CommandHandlers) proxyStream(ctx context.Context, sandboxID, cmdID, stream string, offset int64) (*huma.StreamResponse, error) {
	sb, err := getReadySandbox(ctx, h.k8sClient, h.sandboxNamespace, sandboxID, h.logger)
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
		return nil, handleSidecarError(resp, sandboxID, h.logger)
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

			w := ctx.BodyWriter()
			flusher, canFlush := w.(http.Flusher)
			buf := make([]byte, 4096)

			for {
				n, readErr := resp.Body.Read(buf)
				if n > 0 {
					if _, writeErr := w.Write(buf[:n]); writeErr != nil {
						return
					}
					if canFlush {
						flusher.Flush()
					}
				}
				if readErr != nil {
					if errors.Is(readErr, context.Canceled) {
						h.logger.Warn("client disconnected during command stream", "id", sandboxID)
					} else if !errors.Is(readErr, io.EOF) {
						h.logger.Error("unexpected error streaming command output", "error", readErr, "id", sandboxID)
					}
					return
				}
			}
		},
	}, nil
}

func (h *CommandHandlers) PostCommandStdin(ctx context.Context, input *PostSandboxCommandStdinInput) (*struct{}, error) {
	sb, err := getReadySandbox(ctx, h.k8sClient, h.sandboxNamespace, input.ID, h.logger)
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
		return nil, handleSidecarError(resp, input.ID, h.logger)
	}

	return nil, nil
}

func (h *CommandHandlers) DeleteCommand(ctx context.Context, input *DeleteSandboxCommandInput) (*struct{}, error) {
	sb, err := getReadySandbox(ctx, h.k8sClient, h.sandboxNamespace, input.ID, h.logger)
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
		return nil, handleSidecarError(resp, input.ID, h.logger)
	}

	return nil, nil
}
