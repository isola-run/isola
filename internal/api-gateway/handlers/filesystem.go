package handlers

import (
	"context"
	"encoding/json"
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

// HTTPDoer abstracts HTTP request execution (satisfied by *http.Client), for faking it in tests.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

type FilesystemHandlers struct {
	logger           *slog.Logger
	k8sClient        client.Client
	sandboxNamespace string
	httpClient       HTTPDoer
	sidecarPort      int
}

func NewFilesystemHandlers(logger *slog.Logger, sandboxNamespace string, k8sClient client.Client, httpClient HTTPDoer) *FilesystemHandlers {
	return &FilesystemHandlers{
		logger:           logger,
		k8sClient:        k8sClient,
		sandboxNamespace: sandboxNamespace,
		httpClient:       httpClient,
		sidecarPort:      constants.SidecarPort,
	}
}

func (h *FilesystemHandlers) PostFilesystem(ctx context.Context, input *FilesystemWriteInput) (*FilesystemWriteOutput, error) {
	sb, err := getReadySandbox(ctx, h.k8sClient, h.sandboxNamespace, input.ID, h.logger)
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
		return nil, handleSidecarError(resp, input.ID, h.logger)
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

func (h *FilesystemHandlers) GetFilesystem(ctx context.Context, input *FilesystemReadInput) (*huma.StreamResponse, error) {
	sb, err := getReadySandbox(ctx, h.k8sClient, h.sandboxNamespace, input.ID, h.logger)
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
		return nil, handleSidecarError(resp, input.ID, h.logger)
	}

	return &huma.StreamResponse{
		Body: func(ctx huma.Context) {
			defer func() { _ = resp.Body.Close() }()

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
				h.logger.Error("failed to stream file from sidecar", "error", err, "id", input.ID)
			}
		},
	}, nil
}
