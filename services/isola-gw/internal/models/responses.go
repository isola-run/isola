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

// The client should poll using the download_id until ready becomes true.
type LargeFileDownloadResponse struct {
	DownloadID  string `json:"download_id"`
	Ready       bool   `json:"ready"`
	DownloadURL string `json:"download_url,omitempty"` // Only present when ready is true
	ExpiresIn   int    `json:"expires_in,omitempty"`   // Only present when ready is true
	Path        string `json:"path,omitempty"`
}
