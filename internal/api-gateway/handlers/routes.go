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
		Description: "Returns the health status of the API",
		Tags:        []string{"health"},
	}, h.GetHealth)

	huma.Register(api, huma.Operation{
		OperationID: "getHealthz",
		Method:      http.MethodGet,
		Path:        "/healthz",
		Summary:     "Health check (alias)",
		Description: "Returns the health status of the API",
		Tags:        []string{"health"},
	}, h.GetHealth)

	huma.Register(api, huma.Operation{
		OperationID:   "getReady",
		Method:        http.MethodGet,
		Path:          "/ready",
		Summary:       "Readiness check",
		Description:   "Returns the readiness status of the API",
		Tags:          []string{"health"},
		DefaultStatus: http.StatusOK,
		Errors:        []int{http.StatusServiceUnavailable},
	}, h.GetReady)

	huma.Register(api, huma.Operation{
		OperationID:   "getReadyz",
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
		OperationID:   "createSandbox",
		Method:        http.MethodPost,
		Path:          "/sandboxes",
		Summary:       "Create a sandbox",
		Tags:          []string{"sandboxes"},
		DefaultStatus: http.StatusCreated,
		Errors:        []int{http.StatusBadRequest, http.StatusConflict},
	}, h.PostSandbox)

	huma.Register(api, huma.Operation{
		OperationID: "listSandboxes",
		Method:      http.MethodGet,
		Path:        "/sandboxes",
		Summary:     "List sandboxes",
		Tags:        []string{"sandboxes"},
	}, h.ListSandboxes)

	huma.Register(api, huma.Operation{
		OperationID: "getSandbox",
		Method:      http.MethodGet,
		Path:        "/sandboxes/{id}",
		Summary:     "Get sandbox details",
		Tags:        []string{"sandboxes"},
		Errors:      []int{http.StatusNotFound},
	}, h.GetSandbox)

	huma.Register(api, huma.Operation{
		OperationID: "deleteSandbox",
		Method:      http.MethodDelete,
		Path:        "/sandboxes/{id}",
		Summary:     "Delete a sandbox",
		Tags:        []string{"sandboxes"},
	}, h.DeleteSandbox)
}

func RegisterFilesystemRoutes(api huma.API, h *FilesystemHandlers) {
	huma.Register(api, huma.Operation{
		OperationID: "writeSandboxFilesystem",
		Method:      http.MethodPost,
		Path:        "/sandboxes/{id}/filesystem",
		Summary:     "Write a file to sandbox filesystem",
		Description: "Streams a file upload to the specified path in the sandbox container",
		Tags:        []string{"sandboxes", "filesystem"},
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
		Errors:        []int{http.StatusBadRequest, http.StatusNotFound, http.StatusConflict, http.StatusBadGateway},
	}, h.PostFilesystem)

	huma.Register(api, huma.Operation{
		OperationID: "readSandboxFilesystem",
		Method:      http.MethodGet,
		Path:        "/sandboxes/{id}/filesystem",
		Summary:     "Read a file from sandbox filesystem",
		Description: "Streams a file download from the specified path in the sandbox container",
		Tags:        []string{"sandboxes", "filesystem"},
		Responses: map[string]*huma.Response{
			"200": {
				Description: "File content",
				Content: map[string]*huma.MediaType{
					"application/octet-stream": {
						Schema: &huma.Schema{Type: "string", Format: "binary"},
					},
				},
			},
		},
		Errors: []int{http.StatusBadRequest, http.StatusNotFound, http.StatusConflict, http.StatusBadGateway},
	}, h.GetFilesystem)
}

func RegisterCommandRoutes(api huma.API, h *CommandHandlers) {
	huma.Register(api, huma.Operation{
		OperationID:   "createSandboxCommand",
		Method:        http.MethodPost,
		Path:          "/sandboxes/{id}/commands",
		Summary:       "Start a command in a sandbox",
		Description:   "Starts a new command in the sandbox container and returns a command ID for tracking",
		Tags:          []string{"sandboxes", "commands"},
		DefaultStatus: http.StatusAccepted,
		Errors:        []int{http.StatusBadRequest, http.StatusNotFound, http.StatusConflict, http.StatusBadGateway},
	}, h.PostCommand)

	// todo benl: add long polling wait param (or just default to long poll)
	huma.Register(api, huma.Operation{
		OperationID: "getSandboxCommandStatus",
		Method:      http.MethodGet,
		Path:        "/sandboxes/{id}/commands/{cmdId}/status",
		Summary:     "Get command status",
		Description: "Returns the exit code of the command, or null if still running",
		Tags:        []string{"sandboxes", "commands"},
		Errors:      []int{http.StatusNotFound, http.StatusConflict, http.StatusBadGateway},
	}, h.GetCommandStatus)

	huma.Register(api, huma.Operation{
		OperationID: "getSandboxCommandStdout",
		Method:      http.MethodGet,
		Path:        "/sandboxes/{id}/commands/{cmdId}/stdout",
		Summary:     "Stream command stdout",
		Description: "Streams the command's stdout as raw bytes. The connection remains open until the command exits. Supports resuming via ?offset=N query parameter.",
		Tags:        []string{"sandboxes", "commands"},
		Responses: map[string]*huma.Response{
			"200": {
				Description: "Command stdout stream",
				Content: map[string]*huma.MediaType{
					"application/octet-stream": {
						Schema: &huma.Schema{Type: "string", Format: "binary"},
					},
				},
			},
		},
		Errors: []int{http.StatusNotFound, http.StatusConflict, http.StatusBadGateway},
	}, h.GetCommandStdout)

	huma.Register(api, huma.Operation{
		OperationID: "getSandboxCommandStderr",
		Method:      http.MethodGet,
		Path:        "/sandboxes/{id}/commands/{cmdId}/stderr",
		Summary:     "Stream command stderr",
		Description: "Streams the command's stderr as raw bytes. The connection remains open until the command exits. Supports resuming via ?offset=N query parameter.",
		Tags:        []string{"sandboxes", "commands"},
		Responses: map[string]*huma.Response{
			"200": {
				Description: "Command stderr stream",
				Content: map[string]*huma.MediaType{
					"application/octet-stream": {
						Schema: &huma.Schema{Type: "string", Format: "binary"},
					},
				},
			},
		},
		Errors: []int{http.StatusNotFound, http.StatusConflict, http.StatusBadGateway},
	}, h.GetCommandStderr)

	huma.Register(api, huma.Operation{
		OperationID: "postSandboxCommandStdin",
		Method:      http.MethodPost,
		Path:        "/sandboxes/{id}/commands/{cmdId}/stdin",
		Summary:     "Write to command stdin",
		Description: "Writes raw bytes to the command's stdin",
		Tags:        []string{"sandboxes", "commands"},
		RequestBody: &huma.RequestBody{
			Content: map[string]*huma.MediaType{
				"application/octet-stream": {
					Schema: &huma.Schema{Type: "string", Format: "binary"},
				},
			},
		},
		DefaultStatus: http.StatusNoContent,
		Errors:        []int{http.StatusNotFound, http.StatusConflict, http.StatusBadGateway},
	}, h.PostCommandStdin)

	huma.Register(api, huma.Operation{
		OperationID:   "deleteSandboxCommand",
		Method:        http.MethodDelete,
		Path:          "/sandboxes/{id}/commands/{cmdId}",
		Summary:       "Kill a command",
		Description:   "Kills the command process. Idempotent for already-exited commands.",
		Tags:          []string{"sandboxes", "commands"},
		DefaultStatus: http.StatusNoContent,
		Errors:        []int{http.StatusNotFound, http.StatusConflict, http.StatusBadGateway},
	}, h.DeleteCommand)
}
