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

func RegisterSandboxRoutes(api huma.API, h *SandboxHandlers) {
	huma.Register(api, huma.Operation{
		OperationID:   "create-sandbox",
		Method:        http.MethodPost,
		Path:          "/sandboxes",
		Summary:       "Create a sandbox",
		Tags:          []string{"sandboxes"},
		DefaultStatus: http.StatusCreated,
		Errors:        []int{http.StatusBadRequest, http.StatusConflict},
	}, h.PostSandbox)

	huma.Register(api, huma.Operation{
		OperationID: "list-sandboxes",
		Method:      http.MethodGet,
		Path:        "/sandboxes",
		Summary:     "List sandboxes",
		Tags:        []string{"sandboxes"},
	}, h.ListSandboxes)

	huma.Register(api, huma.Operation{
		OperationID: "get-sandbox",
		Method:      http.MethodGet,
		Path:        "/sandboxes/{id}",
		Summary:     "Get sandbox details",
		Tags:        []string{"sandboxes"},
		Errors:      []int{http.StatusNotFound},
	}, h.GetSandbox)

	huma.Register(api, huma.Operation{
		OperationID: "delete-sandbox",
		Method:      http.MethodDelete,
		Path:        "/sandboxes/{id}",
		Summary:     "Delete a sandbox",
		Tags:        []string{"sandboxes"},
	}, h.DeleteSandbox)

	huma.Register(api, huma.Operation{
		OperationID: "write-sandbox-filesystem",
		Method:      http.MethodPost,
		Path:        "/sandboxes/{id}/filesystem",
		Summary:     "Write a file to sandbox filesystem",
		Description: "Streams a file upload to the specified path in the sandbox container",
		Tags:        []string{"sandboxes"},
		RequestBody: &huma.RequestBody{
			Content: map[string]*huma.MediaType{
				"application/octet-stream": {
					Schema: &huma.Schema{Type: "string", Format: "binary"},
				},
			},
		},
		DefaultStatus: http.StatusOK,
		Errors:        []int{http.StatusBadRequest, http.StatusNotFound, http.StatusConflict, http.StatusBadGateway},
	}, h.PostFilesystem)
}
