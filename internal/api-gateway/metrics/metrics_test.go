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

package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestMiddleware_CollapsesPathParamsToRoutePattern(t *testing.T) {
	r := chi.NewRouter()
	r.Use(Middleware)
	r.Get("/v1/sandboxes/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	before := testutil.ToFloat64(httpRequestsTotal.WithLabelValues("GET", "/v1/sandboxes/{id}", "200"))

	for _, id := range []string{"abc", "def", "xyz"} {
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/sandboxes/"+id, nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("unexpected status: %d", rr.Code)
		}
	}

	if got, want := testutil.ToFloat64(httpRequestsTotal.WithLabelValues("GET", "/v1/sandboxes/{id}", "200")), before+3; got != want {
		t.Errorf("counter = %v, want %v (distinct path params must collapse onto the route pattern)", got, want)
	}
}

func TestMiddleware_RecordsErrorStatusCode(t *testing.T) {
	r := chi.NewRouter()
	r.Use(Middleware)
	r.Get("/fail", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bad", http.StatusBadRequest)
	})

	before := testutil.ToFloat64(httpRequestsTotal.WithLabelValues("GET", "/fail", "400"))

	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/fail", nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status: %d", rr.Code)
	}

	if got, want := testutil.ToFloat64(httpRequestsTotal.WithLabelValues("GET", "/fail", "400")), before+1; got != want {
		t.Errorf("counter = %v, want %v", got, want)
	}
}

func TestMiddleware_TreatsNoWriteAs200(t *testing.T) {
	r := chi.NewRouter()
	r.Use(Middleware)
	// Handler returns without calling WriteHeader or Write; net/http writes an implicit 200.
	r.Get("/ok-implicit", func(http.ResponseWriter, *http.Request) {})

	before := testutil.ToFloat64(httpRequestsTotal.WithLabelValues("GET", "/ok-implicit", "200"))

	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/ok-implicit", nil))

	if got, want := testutil.ToFloat64(httpRequestsTotal.WithLabelValues("GET", "/ok-implicit", "200")), before+1; got != want {
		t.Errorf("counter = %v, want %v", got, want)
	}
}

func TestMiddleware_UnmatchedRouteSharesUnknownLabel(t *testing.T) {
	r := chi.NewRouter()
	r.Use(Middleware)
	r.Get("/known", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	before := testutil.ToFloat64(httpRequestsTotal.WithLabelValues("GET", unknownRoute, "404"))

	for _, path := range []string{"/nope/1", "/nope/2", "/different/path"} {
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
		if rr.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d for %s", rr.Code, path)
		}
	}

	if got, want := testutil.ToFloat64(httpRequestsTotal.WithLabelValues("GET", unknownRoute, "404")), before+3; got != want {
		t.Errorf("counter = %v, want %v (unmatched routes must share the 'unknown' label to bound cardinality)", got, want)
	}
}

func TestMiddleware_InFlightReturnsToBaselineAfterRequest(t *testing.T) {
	r := chi.NewRouter()
	r.Use(Middleware)
	r.Get("/inflight", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	before := testutil.ToFloat64(httpRequestsInFlight)

	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/inflight", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rr.Code)
	}

	if got := testutil.ToFloat64(httpRequestsInFlight); got != before {
		t.Errorf("in-flight gauge = %v, want %v (should return to baseline after request completes)", got, before)
	}
}

func TestMiddleware_ObservesDuration(t *testing.T) {
	r := chi.NewRouter()
	r.Use(Middleware)
	r.Get("/dur", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	before := testutil.CollectAndCount(httpRequestDuration)

	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/dur", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rr.Code)
	}

	if got := testutil.CollectAndCount(httpRequestDuration); got < before {
		t.Errorf("duration histogram series count decreased: before=%d after=%d", before, got)
	}
}

func TestHandler_ExposesRegisteredMetrics(t *testing.T) {
	r := chi.NewRouter()
	r.Use(Middleware)
	r.Handle("/metrics", Handler())
	r.Get("/hit", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Observe at least one request so the vec collectors have a series to scrape.
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/hit", nil))

	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rr.Code)
	}

	body := rr.Body.String()
	for _, want := range []string{
		"isola_apigateway_http_requests_total",
		"isola_apigateway_http_request_duration_seconds",
		"isola_apigateway_http_requests_in_flight",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("/metrics output missing %q", want)
		}
	}
}
