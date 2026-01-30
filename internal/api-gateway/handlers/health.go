package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"sigs.k8s.io/controller-runtime/pkg/client"

	sandboxv1alpha1 "github.com/isola-ai/isola-sb/api/v1alpha1"
	"github.com/isola-ai/isola-sb/internal/api-gateway/generated"
)

type Handler struct {
	logger    *slog.Logger
	k8sClient client.Client
}

func NewHandler(logger *slog.Logger, k8sClient client.Client) *Handler {
	return &Handler{
		logger:    logger,
		k8sClient: k8sClient,
	}
}

func (h *Handler) GetHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(generated.HealthResponse{
		Status: "ok",
	})
}

func (h *Handler) GetReady(w http.ResponseWriter, r *http.Request) {
	sandboxList := &sandboxv1alpha1.SandboxList{}
	if err := h.k8sClient.List(r.Context(), sandboxList, client.Limit(1)); err != nil {
		h.logger.Error("readiness check failed", "error", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(generated.HealthResponse{Status: "not ready"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(generated.HealthResponse{Status: "ok"})
}

// Ensure Handler implements ServerInterface at compile time.
var _ generated.ServerInterface = (*Handler)(nil)
