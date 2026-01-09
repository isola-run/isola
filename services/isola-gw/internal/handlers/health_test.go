package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/omereli/dev-isola/services/isola-gw/internal/models"
)

func TestHealthCheck(t *testing.T) {
	h := NewHandler(nil, nil)

	router := gin.New()
	router.GET("/health", h.HealthCheck)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("HealthCheck() status = %v, want %v", w.Code, http.StatusOK)
	}

	var response models.HealthResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if response.Status != "healthy" {
		t.Errorf("HealthCheck() Status = %v, want healthy", response.Status)
	}

	if response.Version != "1.0.0" {
		t.Errorf("HealthCheck() Version = %v, want 1.0.0", response.Version)
	}

	if response.Components == nil {
		t.Fatal("HealthCheck() Components is nil")
	}

	if response.Components["api"] != "healthy" {
		t.Errorf("HealthCheck() Components[api] = %v, want healthy", response.Components["api"])
	}

	// Verify timestamp is valid RFC3339
	_, err := time.Parse(time.RFC3339, response.Timestamp)
	if err != nil {
		t.Errorf("HealthCheck() Timestamp is not valid RFC3339: %v", response.Timestamp)
	}
}

func TestHealthCheck_ResponseHeaders(t *testing.T) {
	h := NewHandler(nil, nil)

	router := gin.New()
	router.GET("/health", h.HealthCheck)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json; charset=utf-8" {
		t.Errorf("HealthCheck() Content-Type = %v, want application/json; charset=utf-8", contentType)
	}
}

func TestReadinessCheck(t *testing.T) {
	h := NewHandler(nil, nil)

	router := gin.New()
	router.GET("/ready", h.ReadinessCheck)

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("ReadinessCheck() status = %v, want %v", w.Code, http.StatusOK)
	}

	var response models.ReadyResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if response.Status != "ready" {
		t.Errorf("ReadinessCheck() Status = %v, want ready", response.Status)
	}
}

func TestReadinessCheck_ResponseHeaders(t *testing.T) {
	h := NewHandler(nil, nil)

	router := gin.New()
	router.GET("/ready", h.ReadinessCheck)

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json; charset=utf-8" {
		t.Errorf("ReadinessCheck() Content-Type = %v, want application/json; charset=utf-8", contentType)
	}
}

func TestHealthEndpoints_MethodNotAllowed(t *testing.T) {
	h := NewHandler(nil, nil)

	router := gin.New()
	router.GET("/health", h.HealthCheck)
	router.GET("/ready", h.ReadinessCheck)

	tests := []struct {
		name     string
		method   string
		endpoint string
	}{
		{"POST to health", http.MethodPost, "/health"},
		{"PUT to health", http.MethodPut, "/health"},
		{"DELETE to health", http.MethodDelete, "/health"},
		{"POST to ready", http.MethodPost, "/ready"},
		{"PUT to ready", http.MethodPut, "/ready"},
		{"DELETE to ready", http.MethodDelete, "/ready"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.endpoint, nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			if w.Code != http.StatusNotFound {
				t.Errorf("%s %s: status = %v, want %v", tt.method, tt.endpoint, w.Code, http.StatusNotFound)
			}
		})
	}
}
