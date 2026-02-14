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
			Required: true,
			Content: map[string]*huma.MediaType{
				"application/octet-stream": {
					Schema: &huma.Schema{Type: "string", Format: "binary"},
				},
			},
		},
		DefaultStatus: http.StatusCreated,
		Errors:        []int{http.StatusBadRequest},
	}, h.PostFilesystem)

	huma.Register(api, huma.Operation{
		OperationID: "getFilesystem",
		Method:      http.MethodGet,
		Path:        "/filesystem",
		Summary:     "Read a file from the sandbox filesystem",
		Description: "Reads a file from the specified path in the sandbox container",
		Tags:        []string{"filesystem"},
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
		Errors: []int{http.StatusBadRequest, http.StatusNotFound},
	}, h.GetFilesystem)
}

func RegisterCommandRoutes(api huma.API, h *CommandHandlers) {
	huma.Register(api, huma.Operation{
		OperationID:   "createCommand",
		Method:        http.MethodPost,
		Path:          "/commands",
		Summary:       "Start a command in the sandbox",
		Description:   "Starts a new command in the sandbox container and returns a command ID for tracking. Commands always run as root (UID 0, GID 0).",
		Tags:          []string{"commands"},
		DefaultStatus: http.StatusAccepted,
		Errors:        []int{http.StatusBadRequest},
	}, h.PostCommand)

	huma.Register(api, huma.Operation{
		OperationID: "getCommandStatus",
		Method:      http.MethodGet,
		Path:        "/commands/{cmdId}/status",
		Summary:     "Get command status",
		Description: "Returns the exit code of the command, or null if still running",
		Tags:        []string{"commands"},
		Errors:      []int{http.StatusNotFound},
	}, h.GetCommandStatus)

	huma.Register(api, huma.Operation{
		OperationID: "getCommandStdout",
		Method:      http.MethodGet,
		Path:        "/commands/{cmdId}/stdout",
		Summary:     "Stream command stdout",
		Description: "Streams the command's stdout as raw bytes. The connection remains open until the command exits. Supports resuming via ?offset=N query parameter.",
		Tags:        []string{"commands"},
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
		Errors: []int{http.StatusNotFound},
	}, h.GetCommandStdout)

	huma.Register(api, huma.Operation{
		OperationID: "getCommandStderr",
		Method:      http.MethodGet,
		Path:        "/commands/{cmdId}/stderr",
		Summary:     "Stream command stderr",
		Description: "Streams the command's stderr as raw bytes. The connection remains open until the command exits. Supports resuming via ?offset=N query parameter.",
		Tags:        []string{"commands"},
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
		Errors: []int{http.StatusNotFound},
	}, h.GetCommandStderr)

	huma.Register(api, huma.Operation{
		OperationID: "postCommandStdin",
		Method:      http.MethodPost,
		Path:        "/commands/{cmdId}/stdin",
		Summary:     "Write to command stdin",
		Description: "Writes raw bytes to the command's stdin",
		Tags:        []string{"commands"},
		RequestBody: &huma.RequestBody{
			Required: true,
			Content: map[string]*huma.MediaType{
				"application/octet-stream": {
					Schema: &huma.Schema{Type: "string", Format: "binary"},
				},
			},
		},
		DefaultStatus: http.StatusNoContent,
		Errors:        []int{http.StatusNotFound, http.StatusConflict},
	}, h.PostCommandStdin)

	huma.Register(api, huma.Operation{
		OperationID:   "deleteCommand",
		Method:        http.MethodDelete,
		Path:          "/commands/{cmdId}",
		Summary:       "Kill a command",
		Description:   "Kills the command process. Idempotent for already-exited commands.",
		Tags:          []string{"commands"},
		DefaultStatus: http.StatusNoContent,
		Errors:        []int{http.StatusNotFound},
	}, h.DeleteCommand)
}
