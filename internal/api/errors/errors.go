// Package errors provides error types and HTTP error response helpers.
package errors

import (
	"encoding/json"
	"net/http"

	"github.com/isola-ai/isola-sb/internal/api/generated"
	"github.com/isola-ai/isola-sb/internal/api/middleware"
)

// Respond sends an error response with the given status code.
func Respond(w http.ResponseWriter, r *http.Request, statusCode int, message string) {
	requestID := middleware.GetRequestID(r.Context())
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	resp := generated.ErrorResponse{
		Error: message,
	}
	if requestID != "" {
		resp.RequestId = &requestID
	}
	_ = json.NewEncoder(w).Encode(resp)
}

// BadRequest sends a 400 Bad Request response.
func BadRequest(w http.ResponseWriter, r *http.Request, message string) {
	Respond(w, r, http.StatusBadRequest, message)
}

// Unauthorized sends a 401 Unauthorized response.
func Unauthorized(w http.ResponseWriter, r *http.Request, message string) {
	Respond(w, r, http.StatusUnauthorized, message)
}

// NotFound sends a 404 Not Found response.
func NotFound(w http.ResponseWriter, r *http.Request, message string) {
	Respond(w, r, http.StatusNotFound, message)
}

// InternalError sends a 500 Internal Server Error response.
func InternalError(w http.ResponseWriter, r *http.Request, message string) {
	Respond(w, r, http.StatusInternalServerError, message)
}

// ServiceUnavailable sends a 503 Service Unavailable response.
func ServiceUnavailable(w http.ResponseWriter, r *http.Request, message string) {
	Respond(w, r, http.StatusServiceUnavailable, message)
}
