package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	sandboxv1alpha1 "github.com/isola-ai/isola-sb/api/v1alpha1"
)

func getReadySandbox(ctx context.Context, k8sClient client.Client, namespace, id string, logger *slog.Logger) (*sandboxv1alpha1.Sandbox, error) {
	sb := &sandboxv1alpha1.Sandbox{}
	key := client.ObjectKey{Name: id, Namespace: namespace}

	if err := k8sClient.Get(ctx, key, sb); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Warn("sandbox not found", "id", id)
			return nil, huma.Error404NotFound(fmt.Sprintf("sandbox %q not found", id))
		}
		logger.Error("failed to get sandbox", "error", err, "id", id)
		return nil, k8sErrorToHuma(err, "failed to get sandbox")
	}

	if conditionsToStatus(sb.Status.Conditions) != "running" {
		logger.Warn("sandbox is not ready", "id", id, "status", conditionsToStatus(sb.Status.Conditions))
		return nil, huma.Error409Conflict("sandbox is not ready")
	}

	if sb.Status.PodIP == "" {
		logger.Warn("sandbox is not ready", "id", id)
		return nil, huma.Error409Conflict("sandbox is not ready")
	}

	return sb, nil
}

func handleSidecarError(resp *http.Response, sandboxID string, logger *slog.Logger) error {
	if resp.StatusCode >= 500 {
		logger.Error("sidecar returned server error", "id", sandboxID, "status", resp.StatusCode)
		return huma.Error502BadGateway("sidecar internal error")
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		logger.Warn("failed to read sidecar error body", "error", err, "id", sandboxID, "status", resp.StatusCode)
	}

	detail := http.StatusText(resp.StatusCode)
	var sidecarErr huma.ErrorModel
	if json.Unmarshal(body, &sidecarErr) == nil && sidecarErr.Detail != "" {
		detail = sidecarErr.Detail
	}

	logger.Debug("forwarding sidecar client error", "id", sandboxID, "status", resp.StatusCode, "detail", detail)
	return huma.NewError(resp.StatusCode, detail)
}
