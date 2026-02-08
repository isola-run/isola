package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/danielgtaylor/huma/v2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	sandboxv1alpha1 "github.com/isola-ai/isola-sb/api/v1alpha1"
)

func (h *SandboxHandlers) PostFilesystem(ctx context.Context, input *FilesystemWriteInput) (*FilesystemWriteOutput, error) {
	sb := &sandboxv1alpha1.Sandbox{}
	key := client.ObjectKey{Name: input.ID, Namespace: h.sandboxNamespace}

	if err := h.k8sClient.Get(ctx, key, sb); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, huma.Error404NotFound(fmt.Sprintf("sandbox %q not found", input.ID))
		}
		h.logger.Error("failed to get sandbox", "error", err, "id", input.ID)
		return nil, k8sErrorToHuma(err, "failed to get sandbox")
	}

	if sb.Status.PodIP == "" {
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
		return nil, huma.Error502BadGateway("failed to reach sidecar")
	}
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := h.httpClient.Do(req)
	if err != nil {
		h.logger.Error("sidecar request failed", "error", err, "id", input.ID)
		return nil, huma.Error502BadGateway("failed to reach sidecar")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return nil, h.handleSidecarError(resp, input.ID)
	}

	var writeResp FilesystemWriteResponse
	if err := json.NewDecoder(resp.Body).Decode(&writeResp); err != nil {
		h.logger.Error("failed to decode sidecar response", "error", err, "id", input.ID)
		return nil, huma.Error502BadGateway("invalid sidecar response")
	}

	return &FilesystemWriteOutput{Body: writeResp}, nil
}

func (h *SandboxHandlers) handleSidecarError(resp *http.Response, sandboxID string) error {
	// Read limited error body to avoid unbounded reads
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		h.logger.Warn("failed to read sidecar error body", "error", err, "id", sandboxID, "status", resp.StatusCode)
	}

	if resp.StatusCode >= 500 {
		h.logger.Error("sidecar returned server error", "id", sandboxID, "status", resp.StatusCode)
		return huma.Error502BadGateway("sidecar internal error")
	}

	// Forward 4xx errors — try to extract detail from Huma error format
	var humaErr struct {
		Detail string `json:"detail"`
	}
	if json.Unmarshal(body, &humaErr) == nil && humaErr.Detail != "" {
		h.logger.Debug("forwarding sidecar client error", "id", sandboxID, "status", resp.StatusCode, "detail", humaErr.Detail)
		return huma.NewError(resp.StatusCode, humaErr.Detail)
	}

	h.logger.Debug("forwarding sidecar client error", "id", sandboxID, "status", resp.StatusCode)
	return huma.NewError(resp.StatusCode, http.StatusText(resp.StatusCode))
}
