package handlers

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"
)

func RegisterHealthRoutes(api huma.API, h *HealthHandlers) {
	huma.Register(api, huma.Operation{
		OperationID: "get-health",
		Method:      http.MethodGet,
		Path:        "/health",
		Summary:     "Health check",
		Description: "Returns the health status of the API",
		Tags:        []string{"health"},
	}, h.GetHealth)

	huma.Register(api, huma.Operation{
		OperationID: "get-healthz",
		Method:      http.MethodGet,
		Path:        "/healthz",
		Summary:     "Health check (alias)",
		Description: "Returns the health status of the API",
		Tags:        []string{"health"},
	}, h.GetHealth)

	huma.Register(api, huma.Operation{
		OperationID:   "get-ready",
		Method:        http.MethodGet,
		Path:          "/ready",
		Summary:       "Readiness check",
		Description:   "Returns the readiness status of the API",
		Tags:          []string{"health"},
		DefaultStatus: http.StatusOK,
		Errors:        []int{http.StatusServiceUnavailable},
	}, h.GetReady)

	huma.Register(api, huma.Operation{
		OperationID:   "get-readyz",
		Method:        http.MethodGet,
		Path:          "/readyz",
		Summary:       "Readiness check (alias)",
		Description:   "Returns the readiness status of the API",
		Tags:          []string{"health"},
		DefaultStatus: http.StatusOK,
		Errors:        []int{http.StatusServiceUnavailable},
	}, h.GetReady)
}

// RegisterSandboxRoutes registers sandbox-related API routes.
func RegisterSandboxRoutes(api huma.API, h *SandboxHandlers) {
	huma.Register(api, huma.Operation{
		OperationID:   "post-sandbox-exec",
		Method:        http.MethodPost,
		Path:          "/sandboxes/{sandbox_name}/exec",
		Summary:       "Execute a command in a sandbox",
		Description:   "Executes a command in the specified sandbox and returns stdout, stderr, and exit code",
		Tags:          []string{"sandbox"},
		DefaultStatus: http.StatusOK,
		Errors:        []int{http.StatusBadRequest, http.StatusNotFound, http.StatusBadGateway, http.StatusGatewayTimeout},
	}, h.PostExec)
}

// RegisterSandboxWebSocketRoutes registers WebSocket routes for sandbox interactions.
// These cannot use Huma as WebSocket upgrades require direct HTTP handler access.
func RegisterSandboxWebSocketRoutes(r chi.Router, h *WebSocketProxyHandler) {
	r.Get("/sandboxes/{sandbox_name}/exec/stream", h.ExecStreamHandler)
	r.Get("/sandboxes/{sandbox_name}/pty", h.PTYHandler)
}
