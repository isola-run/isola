// Package handlers implements the API handlers for isola-api.
package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/isola-ai/isola-sb/internal/api/generated"
)

// Handler implements the generated ServerInterface.
type Handler struct {
	logger *slog.Logger
}

// NewHandler creates a new Handler instance.
func NewHandler(logger *slog.Logger) *Handler {
	return &Handler{logger: logger}
}

// GetHealth implements the health check endpoint.
func (h *Handler) GetHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(generated.HealthResponse{
		Status: "ok",
	})
}

// GetReady implements the readiness check endpoint.
func (h *Handler) GetReady(w http.ResponseWriter, r *http.Request) {
	// TODO: Add actual readiness checks (e.g., K8s client connectivity)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(generated.HealthResponse{
		Status: "ok",
	})
}

// Ensure Handler implements ServerInterface at compile time.
var _ generated.ServerInterface = (*Handler)(nil)
