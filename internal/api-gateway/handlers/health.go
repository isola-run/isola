package handlers

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/httplog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	sandboxv1alpha1 "github.com/isola-ai/isola-sb/api/v1alpha1"
)

type HealthHandlers struct {
	k8sClient client.Client
}

func NewHealthHandlers(k8sClient client.Client) *HealthHandlers {
	return &HealthHandlers{
		k8sClient: k8sClient,
	}
}

func (h *HealthHandlers) GetHealth(ctx context.Context, input *struct{}) (*HealthOutput, error) {
	return &HealthOutput{Body: HealthResponse{Status: "ok"}}, nil
}

func (h *HealthHandlers) GetReady(ctx context.Context, input *struct{}) (*HealthOutput, error) {
	sandboxList := &sandboxv1alpha1.SandboxList{}
	if err := h.k8sClient.List(ctx, sandboxList, client.Limit(1)); err != nil {
		httplog.LogEntry(ctx).Error("readiness check failed", "error", err)
		return nil, huma.Error503ServiceUnavailable("not ready")
	}

	return &HealthOutput{Body: HealthResponse{Status: "ok"}}, nil
}
