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
		Description: "Returns the health status of the sidecar",
		Tags:        []string{"health"},
	}, h.GetHealth)

	huma.Register(api, huma.Operation{
		OperationID: "get-healthz",
		Method:      http.MethodGet,
		Path:        "/healthz",
		Summary:     "Health check (alias)",
		Description: "Returns the health status of the sidecar",
		Tags:        []string{"health"},
	}, h.GetHealth)
}

func RegisterFilesystemRoutes(api huma.API, h *FilesystemHandlers) {
	huma.Register(api, huma.Operation{
		OperationID: "post-filesystem",
		Method:      http.MethodPost,
		Path:        "/filesystem",
		Summary:     "Write a file to the sandbox filesystem",
		Description: "Writes a file to the specified path in the sandbox container",
		Tags:        []string{"filesystem"},
		// Since we use BodyStream resolver (no Body/RawBody field),
		// we need to manually specify the request body in OpenAPI
		RequestBody: &huma.RequestBody{
			Content: map[string]*huma.MediaType{
				"application/octet-stream": {
					Schema: &huma.Schema{Type: "string", Format: "binary"},
				},
			},
		},
		DefaultStatus: http.StatusOK,
		Errors:        []int{http.StatusBadRequest, http.StatusInternalServerError},
	}, h.PostFilesystem)
}

func RegisterExecRoutes(api huma.API, h *ExecHandlers) {
	huma.Register(api, huma.Operation{
		OperationID:   "post-exec",
		Method:        http.MethodPost,
		Path:          "/exec",
		Summary:       "Execute a command in the sandbox",
		Description:   "Executes a command in the sandbox container and returns the output",
		Tags:          []string{"exec"},
		DefaultStatus: http.StatusOK,
		Errors:        []int{http.StatusBadRequest, http.StatusInternalServerError, http.StatusGatewayTimeout},
	}, h.PostExec)
}

// RegisterWebSocketRoutes registers WebSocket endpoints on the chi router.
// These cannot use Huma as WebSocket upgrades require direct HTTP handler access.
func RegisterWebSocketRoutes(r chi.Router, execStream *ExecStreamHandler, ptyHandler *PTYHandler) {
	r.Get("/exec/stream", execStream.ServeHTTP)
	r.Get("/pty", ptyHandler.ServeHTTP)
}
