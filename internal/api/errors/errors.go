// Package errors provides error types and HTTP error response helpers.
package errors

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// ErrorResponse represents an API error response.
type ErrorResponse struct {
	Error     string `json:"error"`
	RequestID string `json:"request_id,omitempty"`
}

// Respond sends an error response with the given status code.
func Respond(c *gin.Context, statusCode int, message string) {
	requestID := c.GetHeader("X-Request-ID")
	c.JSON(statusCode, ErrorResponse{
		Error:     message,
		RequestID: requestID,
	})
}

// BadRequest sends a 400 Bad Request response.
func BadRequest(c *gin.Context, message string) {
	Respond(c, http.StatusBadRequest, message)
}

// Unauthorized sends a 401 Unauthorized response.
func Unauthorized(c *gin.Context, message string) {
	Respond(c, http.StatusUnauthorized, message)
}

// NotFound sends a 404 Not Found response.
func NotFound(c *gin.Context, message string) {
	Respond(c, http.StatusNotFound, message)
}

// InternalError sends a 500 Internal Server Error response.
func InternalError(c *gin.Context, message string) {
	Respond(c, http.StatusInternalServerError, message)
}

// ServiceUnavailable sends a 503 Service Unavailable response.
func ServiceUnavailable(c *gin.Context, message string) {
	Respond(c, http.StatusServiceUnavailable, message)
}
