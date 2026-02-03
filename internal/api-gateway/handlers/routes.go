package handlers

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
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
