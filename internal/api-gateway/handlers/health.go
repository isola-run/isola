package handlers

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/render"
	"sigs.k8s.io/controller-runtime/pkg/client"

	sandboxv1alpha1 "github.com/isola-ai/isola-sb/api/v1alpha1"
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

type HealthResponse struct {
	Status string `json:"status" example:"ok"`
}

type ErrorResponse struct {
	Message string `json:"message" example:"service not ready"`
}

// GetHealth godoc
// @Summary Health check
// @Description Returns the health status of the API
// @Tags health
// @Produce json
// @Success 200 {object} HealthResponse
// @Router /health [get]
func (h *Handler) GetHealth(w http.ResponseWriter, r *http.Request) {
	render.JSON(w, r, HealthResponse{Status: "ok"})
}

// GetReady godoc
// @Summary Readiness check
// @Description Returns the readiness status of the API
// @Tags health
// @Produce json
// @Success 200 {object} HealthResponse
// @Failure 503 {object} ErrorResponse
// @Router /ready [get]
func (h *Handler) GetReady(w http.ResponseWriter, r *http.Request) {
	sandboxList := &sandboxv1alpha1.SandboxList{}
	if err := h.k8sClient.List(r.Context(), sandboxList, client.Limit(1)); err != nil {
		h.logger.Error("readiness check failed", "error", err)
		render.Status(r, http.StatusServiceUnavailable)
		render.JSON(w, r, ErrorResponse{Message: "not ready"})
		return
	}

	render.JSON(w, r, HealthResponse{Status: "ok"})
}
