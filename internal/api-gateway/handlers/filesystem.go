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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	sandboxv1alpha1 "github.com/isola-ai/isola-sb/api/v1alpha1"
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
	sb := &sandboxv1alpha1.Sandbox{}
	key := client.ObjectKey{Name: input.ID, Namespace: h.sandboxNamespace}

	if err := h.k8sClient.Get(ctx, key, sb); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, huma.Error404NotFound(fmt.Sprintf("sandbox %q not found", input.ID))
		}
		h.logger.Error("failed to get sandbox", "error", err, "id", input.ID)
		return nil, k8sErrorToHuma(err, "failed to get sandbox")
	}

	// todo benl: stop using raw strings for sandbox status
	if conditionsToStatus(sb.Status.Conditions) != "running" {
		return nil, huma.Error409Conflict("sandbox is not ready")
	}

	if sb.Status.PodIP == "" { // should not happen if sandbox is ready ^
		return nil, huma.Error409Conflict("sandbox is not ready")
	}

	// Build sidecar URL
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
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, h.handleSidecarError(resp, input.ID)
	}

	var writeResp sidecarapi.FilesystemWriteResponse
	if err := json.NewDecoder(resp.Body).Decode(&writeResp); err != nil {
		h.logger.Error("failed to decode sidecar response", "error", err, "id", input.ID, "status", resp.StatusCode)
		return nil, huma.Error502BadGateway("invalid sidecar response")
	}

	return &FilesystemWriteOutput{Body: writeResp}, nil
}

func (h *FilesystemHandlers) GetFilesystem(ctx context.Context, input *FilesystemReadInput) (*huma.StreamResponse, error) {
	sb := &sandboxv1alpha1.Sandbox{}
	key := client.ObjectKey{Name: input.ID, Namespace: h.sandboxNamespace}

	if err := h.k8sClient.Get(ctx, key, sb); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, huma.Error404NotFound(fmt.Sprintf("sandbox %q not found", input.ID))
		}
		h.logger.Error("failed to get sandbox", "error", err, "id", input.ID)
		return nil, k8sErrorToHuma(err, "failed to get sandbox")
	}

	if conditionsToStatus(sb.Status.Conditions) != "running" {
		return nil, huma.Error409Conflict("sandbox is not ready")
	}

	if sb.Status.PodIP == "" {
		return nil, huma.Error409Conflict("sandbox is not ready")
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

	resp, err := h.httpClient.Do(req)
	if err != nil {
		h.logger.Error("sidecar request failed", "error", err, "id", input.ID)
		return nil, huma.Error502BadGateway("failed to reach sidecar")
	}

	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		return nil, h.handleSidecarError(resp, input.ID)
	}

	return &huma.StreamResponse{
		Body: func(ctx huma.Context) {
			defer resp.Body.Close()

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

func (h *FilesystemHandlers) handleSidecarError(resp *http.Response, sandboxID string) error {
	if resp.StatusCode >= 500 {
		h.logger.Error("sidecar returned server error", "id", sandboxID, "status", resp.StatusCode)
		return huma.Error502BadGateway("sidecar internal error")
	}

	// Read limited error body to avoid unbounded reads to memory from untrusted sandbox
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		h.logger.Warn("failed to read sidecar error body", "error", err, "id", sandboxID, "status", resp.StatusCode)
	}

	detail := http.StatusText(resp.StatusCode)
	var sidecarErr huma.ErrorModel
	if json.Unmarshal(body, &sidecarErr) == nil && sidecarErr.Detail != "" {
		detail = sidecarErr.Detail
	}

	h.logger.Debug("forwarding sidecar client error", "id", sandboxID, "status", resp.StatusCode, "detail", detail)
	return huma.NewError(resp.StatusCode, detail)
}
