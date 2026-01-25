package middleware

import (
	"log/slog"
	"net/http"
	"time"
)

// responseWriter wraps http.ResponseWriter to capture status code.
type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

// Logging middleware logs HTTP requests using slog.
func Logging(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			wrapped := &responseWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(wrapped, r)

			latency := time.Since(start)
			requestID := GetRequestID(r.Context())

			attrs := []any{
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", wrapped.status),
				slog.Duration("latency", latency),
				slog.String("client_ip", r.RemoteAddr),
			}

			if r.URL.RawQuery != "" {
				attrs = append(attrs, slog.String("query", r.URL.RawQuery))
			}
			if requestID != "" {
				attrs = append(attrs, slog.String("request_id", requestID))
			}

			switch {
			case wrapped.status >= 500:
				logger.Error("request completed", attrs...)
			case wrapped.status >= 400:
				logger.Warn("request completed", attrs...)
			default:
				logger.Info("request completed", attrs...)
			}
		})
	}
}
