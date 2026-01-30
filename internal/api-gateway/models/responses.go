package models

// HealthResponse represents the health status of the service.
type HealthResponse struct {
	Status string `json:"status" example:"ok"`
}

// ErrorResponse represents an error response.
type ErrorResponse struct {
	Error     string `json:"error" example:"service not ready"`
	RequestID string `json:"request_id,omitempty" example:"abc123"`
}
