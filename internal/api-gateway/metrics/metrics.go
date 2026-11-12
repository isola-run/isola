// Copyright The Isola Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package metrics provides Prometheus instrumentation for the api-gateway HTTP server.
//
// Metrics are registered on prometheus.DefaultRegisterer; expose them via Handler.
// Wire the HTTP middleware into a chi router via Middleware so the route pattern
// label stays bounded (raw URLs would explode cardinality on path params).
package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// unknownRoute labels requests that do not match any registered route (e.g. 404s)
// so a malicious probe across random URLs does not blow up label cardinality.
const unknownRoute = "unknown"

// latencyBuckets extends prometheus.DefBuckets with longer-tail buckets for
// long-poll endpoints (gateway allows waitSeconds up to 25) and SSE streams.
var latencyBuckets = append(append([]float64{}, prometheus.DefBuckets...), 15, 30, 60)

var (
	httpRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "isola",
		Subsystem: "apigateway",
		Name:      "http_requests_total",
		Help:      "Total HTTP requests processed, partitioned by method, matched route pattern, and response status code.",
	}, []string{"method", "path", "code"})

	httpRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "isola",
		Subsystem: "apigateway",
		Name:      "http_request_duration_seconds",
		Help:      "HTTP request latency in seconds, partitioned by method, matched route pattern, and response status code.",
		Buckets:   latencyBuckets,
	}, []string{"method", "path", "code"})

	httpRequestsInFlight = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "isola",
		Subsystem: "apigateway",
		Name:      "http_requests_in_flight",
		Help:      "Current number of HTTP requests being served.",
	})
)

// Middleware instruments HTTP requests with Prometheus metrics.
//
// It must be installed on a chi router: the matched route pattern is read from
// chi.RouteContext AFTER next.ServeHTTP returns, because chi only populates it
// during routing (see https://github.com/go-chi/chi/issues/270).
//
// Register before other middleware (i.e. call r.Use(metrics.Middleware) first)
// so the observed duration covers the full server-side processing time.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)

		httpRequestsInFlight.Inc()
		defer httpRequestsInFlight.Dec()

		next.ServeHTTP(ww, r)

		path := unknownRoute
		if rctx := chi.RouteContext(r.Context()); rctx != nil {
			if p := rctx.RoutePattern(); p != "" {
				path = p
			}
		}

		status := ww.Status()
		if status == 0 {
			// Handler returned without WriteHeader or Write: net/http emits 200.
			status = http.StatusOK
		}
		code := strconv.Itoa(status)

		httpRequestsTotal.WithLabelValues(r.Method, path, code).Inc()
		httpRequestDuration.WithLabelValues(r.Method, path, code).Observe(time.Since(start).Seconds())
	})
}

// Handler returns the HTTP handler that exposes metrics for Prometheus scraping.
// Mount it at /metrics on the chi router.
func Handler() http.Handler {
	return promhttp.Handler()
}
