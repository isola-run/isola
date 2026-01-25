// Package middleware provides HTTP middleware for the isola-api service.
package middleware

import (
	"context"
	"net/http"

	"github.com/google/uuid"
)

type ctxKeyRequestID struct{}

// RequestID middleware generates or propagates X-Request-ID headers.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = uuid.New().String()
		}

		ctx := context.WithValue(r.Context(), ctxKeyRequestID{}, requestID)
		w.Header().Set("X-Request-ID", requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetRequestID retrieves the request ID from context.
func GetRequestID(ctx context.Context) string {
	if id, ok := ctx.Value(ctxKeyRequestID{}).(string); ok {
		return id
	}
	return ""
}
