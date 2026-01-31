package handlers

// HealthResponse is the response for the health check endpoint.
type HealthResponse struct {
	Status string `json:"status" example:"ok"`
}

// ErrorResponse is returned when an error occurs.
type ErrorResponse struct {
	Message string `json:"message" example:"path is required"`
}

// FilesystemWriteResponse is returned after a successful file write.
type FilesystemWriteResponse struct {
	AbsolutePath string `json:"absolute_path" example:"/workspace/file.txt"`
	BytesWritten int64  `json:"bytes_written" example:"1024"`
	Container    string `json:"container,omitempty" example:"main"`
}
