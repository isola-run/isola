// Package metrics provides Prometheus metrics for the isola-gw service.
package metrics

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// HTTPRequestDuration tracks HTTP request latency by method, endpoint, and status code.
	HTTPRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "isola",
			Subsystem: "gateway",
			Name:      "http_request_duration_seconds",
			Help:      "HTTP request latency in seconds",
			Buckets:   []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
		},
		[]string{"method", "endpoint", "status_code"},
	)

	// HTTPRequestsTotal counts total HTTP requests by method, endpoint, and status code.
	HTTPRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "isola",
			Subsystem: "gateway",
			Name:      "http_requests_total",
			Help:      "Total number of HTTP requests",
		},
		[]string{"method", "endpoint", "status_code"},
	)

	// SandboxOperationsTotal counts sandbox operations by operation type and status.
	SandboxOperationsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "isola",
			Subsystem: "gateway",
			Name:      "sandbox_operations_total",
			Help:      "Total sandbox operations",
		},
		[]string{"operation", "status"},
	)

	// SandboxOperationDuration tracks sandbox operation latency.
	SandboxOperationDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "isola",
			Subsystem: "gateway",
			Name:      "sandbox_operation_duration_seconds",
			Help:      "Sandbox operation latency in seconds",
			Buckets:   []float64{.01, .05, .1, .25, .5, 1, 2.5, 5, 10, 30},
		},
		[]string{"operation"},
	)

	// ActiveSandboxes tracks the number of currently active sandboxes (gauge).
	ActiveSandboxes = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "isola",
		Subsystem: "gateway",
		Name:      "active_sandboxes",
		Help:      "Number of currently active sandboxes",
	})

	// K8sAPICallDuration tracks latency of K8s API calls from gateway.
	K8sAPICallDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "isola",
			Subsystem: "gateway",
			Name:      "k8s_api_call_duration_seconds",
			Help:      "K8s API call latency in seconds",
			Buckets:   []float64{.01, .05, .1, .25, .5, 1, 2.5, 5},
		},
		[]string{"operation", "resource"},
	)

	// K8sAPICallsTotal counts K8s API calls by operation, resource, and status.
	K8sAPICallsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "isola",
			Subsystem: "gateway",
			Name:      "k8s_api_calls_total",
			Help:      "Total K8s API calls",
		},
		[]string{"operation", "resource", "status"},
	)

	// RateLimitRejections counts requests rejected due to rate limiting.
	RateLimitRejections = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "isola",
			Subsystem: "gateway",
			Name:      "rate_limit_rejections_total",
			Help:      "Total requests rejected due to rate limiting",
		},
		[]string{"endpoint"},
	)

	// InFlightRequests tracks the number of in-flight requests.
	InFlightRequests = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "isola",
		Subsystem: "gateway",
		Name:      "in_flight_requests",
		Help:      "Number of in-flight HTTP requests",
	})
)

// RecordSandboxOperation records metrics for a sandbox operation.
func RecordSandboxOperation(operation, status string, duration time.Duration) {
	SandboxOperationsTotal.WithLabelValues(operation, status).Inc()
	SandboxOperationDuration.WithLabelValues(operation).Observe(duration.Seconds())
}

// RecordK8sAPICall records metrics for a K8s API call.
func RecordK8sAPICall(operation, resource, status string, duration time.Duration) {
	K8sAPICallDuration.WithLabelValues(operation, resource).Observe(duration.Seconds())
	K8sAPICallsTotal.WithLabelValues(operation, resource, status).Inc()
}

// normalizeEndpoint converts a gin route pattern to a normalized form for metrics.
// This prevents high-cardinality from path parameters.
func normalizeEndpoint(c *gin.Context) string {
	// Use the registered route pattern (e.g., "/api/v1/sandboxes/:id")
	// rather than the actual path (e.g., "/api/v1/sandboxes/abc-123")
	if c.FullPath() != "" {
		return c.FullPath()
	}
	return c.Request.URL.Path
}

// MetricsMiddleware returns a gin middleware that records HTTP metrics.
func MetricsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		InFlightRequests.Inc()

		c.Next()

		InFlightRequests.Dec()
		duration := time.Since(start)
		statusCode := strconv.Itoa(c.Writer.Status())
		endpoint := normalizeEndpoint(c)

		HTTPRequestDuration.WithLabelValues(c.Request.Method, endpoint, statusCode).Observe(duration.Seconds())
		HTTPRequestsTotal.WithLabelValues(c.Request.Method, endpoint, statusCode).Inc()
	}
}
