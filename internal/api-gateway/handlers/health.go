package handlers

import (
	"context"
	"log/slog"

	"github.com/danielgtaylor/huma/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	sandboxv1alpha1 "github.com/isola-ai/isola-sb/api/v1alpha1"
)

type HealthHandlers struct {
	logger    *slog.Logger
	k8sClient client.Client
}

func NewHealthHandlers(logger *slog.Logger, k8sClient client.Client) *HealthHandlers {
	return &HealthHandlers{
		logger:    logger,
		k8sClient: k8sClient,
	}
}

func (h *HealthHandlers) GetHealth(ctx context.Context, input *struct{}) (*HealthOutput, error) {
	return &HealthOutput{Body: HealthResponse{Status: "ok"}}, nil
}

func (h *HealthHandlers) GetReady(ctx context.Context, input *struct{}) (*HealthOutput, error) {
	sandboxList := &sandboxv1alpha1.SandboxList{}
	if err := h.k8sClient.List(ctx, sandboxList, client.Limit(1)); err != nil {
		h.logger.Error("readiness check failed", "error", err)
		return nil, huma.Error503ServiceUnavailable("not ready")
	}

	return &HealthOutput{Body: HealthResponse{Status: "ok"}}, nil
}
