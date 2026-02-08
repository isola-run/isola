package handlers

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

func RegisterHealthRoutes(api huma.API, h *HealthHandlers) {
	huma.Register(api, huma.Operation{
		OperationID: "getHealth",
		Method:      http.MethodGet,
		Path:        "/health",
		Summary:     "Health check",
		Description: "Returns the health status of the sidecar",
		Tags:        []string{"health"},
	}, h.GetHealth)

	huma.Register(api, huma.Operation{
		OperationID: "getHealthz",
		Method:      http.MethodGet,
		Path:        "/healthz",
		Summary:     "Health check (alias)",
		Description: "Returns the health status of the sidecar",
		Tags:        []string{"health"},
	}, h.GetHealth)
}

func RegisterFilesystemRoutes(api huma.API, h *FilesystemHandlers) {
	huma.Register(api, huma.Operation{
		OperationID: "postFilesystem",
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
		DefaultStatus: http.StatusCreated,
		Errors:        []int{http.StatusBadRequest, http.StatusInternalServerError},
	}, h.PostFilesystem)
}
