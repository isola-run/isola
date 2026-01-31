package handlers

import (
	"net/http"

	"github.com/go-chi/render"
)

type HealthHandler struct{}

func NewHealthHandler() *HealthHandler {
	return &HealthHandler{}
}

// GetHealth godoc
// @Summary Health check
// @Description Returns the health status of the sidecar
// @Tags health
// @Produce json
// @Success 200 {object} HealthResponse
// @Router /health [get]
func (h *HealthHandler) GetHealth(w http.ResponseWriter, r *http.Request) {
	render.JSON(w, r, HealthResponse{Status: "ok"})
}
