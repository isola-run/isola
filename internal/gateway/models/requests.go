// Package models provides data structures for the isola-gw API.
// TODO: align validations with CRD openapi spec validations
package models

type CreateSandboxRequest struct {
	// Name is optional. If not provided, a unique name will be auto-generated.
	// If provided, must be DNS-safe (lowercase alphanumeric and hyphens, max 63 chars).
	Name      string            `json:"name,omitempty"`
	Image     *string           `json:"image,omitempty"`
	Region    string            `json:"region,omitempty"`
	CPU       *float64          `json:"cpu,omitempty"`
	Memory    *float64          `json:"memory,omitempty"`
	Disk      *float64          `json:"disk,omitempty"`
	GPU       int               `json:"gpu,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
	Volumes   []AttachedVolume  `json:"volumes,omitempty"`
	AutoStart bool              `json:"autoStart,omitempty"`
}

type ExecuteCommandRequest struct {
	Command string `json:"command" binding:"required"`
}

type FileUploadRequest struct {
	Path string `json:"path" binding:"required"`
}

type UploadUrlRequest struct {
	Path        string  `json:"path" binding:"required"`
	Filename    string  `json:"filename" binding:"required"`
	ContentType *string `json:"content_type,omitempty"`
}

type ConfirmUploadRequest struct {
	UploadID string `json:"upload_id" binding:"required"`
	Filename string `json:"filename" binding:"required"`
	Path     string `json:"path" binding:"required"`
}

type DownloadRequest struct {
	DownloadURL string `json:"download_url" binding:"required"`
	Path        string `json:"path" binding:"required"`
}

type ListSandboxesParams struct {
	State  *SandboxState `form:"state,omitempty"`
	Limit  int           `form:"limit,default=20" binding:"min=1,max=100"`
	Offset int           `form:"offset,default=0" binding:"min=0"`
}

type TerminateSandboxParams struct {
	Force bool `form:"force,default=false"`
}
