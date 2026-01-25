// Package models provides data structures for the isola-gw API.
package models

type ErrorResponse struct {
	Error   string                 `json:"error"`
	Message string                 `json:"message"`
	Details map[string]interface{} `json:"details,omitempty"`
}

type HealthResponse struct {
	Status     string            `json:"status"`
	Timestamp  string            `json:"timestamp"`
	Components map[string]string `json:"components,omitempty"`
	Version    string            `json:"version"`
}

type ReadyResponse struct {
	Status string `json:"status"`
}

type ExecuteCommandResponse struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exitCode"`
}

type FileUploadResponse struct {
	Success bool   `json:"success"`
	Path    string `json:"path"`
	Size    int64  `json:"size"`
}

type UploadUrlResponse struct {
	UploadURL string `json:"upload_url"`
	UploadID  string `json:"upload_id"`
	ExpiresIn int    `json:"expires_in"`
}

type ConfirmUploadResponse struct {
	Success bool   `json:"success"`
	Path    string `json:"path"`
	Size    int64  `json:"size"`
}

type DownloadResponse struct {
	Success bool   `json:"success"`
	Path    string `json:"path"`
	Size    int64  `json:"size"`
}

type FileDownloadResponse struct {
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	Content string `json:"content"`
}

// SandboxIngressResponse is returned when enabling ingress for a sandbox.
type SandboxIngressResponse struct {
	// URL is the public URL where the sandbox can be accessed.
	// The URL itself acts as authentication (presigned URL pattern).
	URL string `json:"url"`
	// Enabled indicates if ingress is enabled.
	Enabled bool `json:"enabled"`
}

// SandboxIngressStatus returns the current ingress status for a sandbox.
type SandboxIngressStatus struct {
	// Enabled indicates if ingress is enabled.
	Enabled bool `json:"enabled"`
	// URL is the public URL if ingress is enabled.
	URL string `json:"url,omitempty"`
	// Ready indicates if the ingress is ready to receive traffic.
	Ready bool `json:"ready"`
}
