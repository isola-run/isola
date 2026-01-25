package middleware

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/isola-ai/isola-sb/internal/api/generated"
)

// Recovery middleware recovers from panics and logs them with slog.
func Recovery(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					requestID := GetRequestID(r.Context())

					logger.Error("panic recovered",
						slog.Any("panic", rec),
						slog.String("request_id", requestID),
						slog.String("path", r.URL.Path),
						slog.String("method", r.Method),
						slog.String("stack", string(debug.Stack())),
					)

					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusInternalServerError)

					resp := generated.ErrorResponse{
						Error:     "internal server error",
						RequestId: &requestID,
					}
					_ = json.NewEncoder(w).Encode(resp)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
