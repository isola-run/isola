package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/omereli/dev-isola/services/isola-gw/internal/kubernetes"
	"github.com/omereli/dev-isola/services/isola-gw/internal/models"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// MockK8sManager implements a mock kubernetes manager for testing
type MockK8sManager struct {
	ListSandboxCRsFunc   func(ctx context.Context) ([]*unstructured.Unstructured, error)
	GetSandboxCRFunc     func(ctx context.Context, sandboxID string) (*unstructured.Unstructured, error)
	CreateSandboxCRFunc  func(ctx context.Context, sandboxID string, req models.CreateSandboxRequest, templateName string) (bool, *string)
	DeleteSandboxCRFunc  func(ctx context.Context, sandboxID string) (bool, *string)
	GetSandboxStatusFunc func(ctx context.Context, sandboxID string) (*kubernetes.SandboxStatus, error)
	ExecuteCommandFunc   func(ctx context.Context, sandboxID string, command string) (string, string, int, error)
}

// TestTenantFromAPIKey tests the API key to tenant ID mapping
func TestTenantFromAPIKey(t *testing.T) {
	tests := []struct {
		name     string
		apiKey   string
		expected string
	}{
		{
			name:     "known API key 1",
			apiKey:   "iso_sk_a1b2c3d4e5f67890a1b2c3d4e5f67890",
			expected: "2280e575-f37d-4329-b033-9de263ce7625",
		},
		{
			name:     "demo API key",
			apiKey:   "iso_sk_demo",
			expected: "e766a1e8-4b0e-4bb7-9612-80b9c1c8cd87",
		},
		{
			name:     "unknown API key returns default",
			apiKey:   "unknown_key",
			expected: "e766a1e8-4b0e-4bb7-9612-80b9c1c8cd87",
		},
		{
			name:     "empty API key returns default",
			apiKey:   "",
			expected: "e766a1e8-4b0e-4bb7-9612-80b9c1c8cd87",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tenantFromAPIKey(tt.apiKey)
			if result != tt.expected {
				t.Errorf("tenantFromAPIKey(%q) = %v, want %v", tt.apiKey, result, tt.expected)
			}
		})
	}
}

func TestAPIKeyAuth_MissingKey(t *testing.T) {
	h := NewHandler(nil, nil)

	router := gin.New()
	api := router.Group("/api/v1")
	api.Use(h.APIKeyAuth())
	api.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/test", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("APIKeyAuth() status = %v, want %v", w.Code, http.StatusUnauthorized)
	}

	var response map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if response["error"] != "Unauthorized" {
		t.Errorf("APIKeyAuth() error = %v, want Unauthorized", response["error"])
	}

	if response["message"] != "Missing API key" {
		t.Errorf("APIKeyAuth() message = %v, want 'Missing API key'", response["message"])
	}
}

func TestAPIKeyAuth_ValidKey(t *testing.T) {
	h := NewHandler(nil, nil)

	var capturedTenantID interface{}
	router := gin.New()
	api := router.Group("/api/v1")
	api.Use(h.APIKeyAuth())
	api.GET("/test", func(c *gin.Context) {
		capturedTenantID, _ = c.Get("tenant_id")
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/test", nil)
	req.Header.Set("X-API-Key", "iso_sk_demo")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("APIKeyAuth() status = %v, want %v", w.Code, http.StatusOK)
	}

	if capturedTenantID != "e766a1e8-4b0e-4bb7-9612-80b9c1c8cd87" {
		t.Errorf("APIKeyAuth() tenant_id = %v, want e766a1e8-4b0e-4bb7-9612-80b9c1c8cd87", capturedTenantID)
	}
}

func TestAPIKeyAuth_CustomKey(t *testing.T) {
	h := NewHandler(nil, nil)

	var capturedTenantID interface{}
	router := gin.New()
	api := router.Group("/api/v1")
	api.Use(h.APIKeyAuth())
	api.GET("/test", func(c *gin.Context) {
		capturedTenantID, _ = c.Get("tenant_id")
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/test", nil)
	req.Header.Set("X-API-Key", "iso_sk_a1b2c3d4e5f67890a1b2c3d4e5f67890")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("APIKeyAuth() status = %v, want %v", w.Code, http.StatusOK)
	}

	if capturedTenantID != "2280e575-f37d-4329-b033-9de263ce7625" {
		t.Errorf("APIKeyAuth() tenant_id = %v, want 2280e575-f37d-4329-b033-9de263ce7625", capturedTenantID)
	}
}

func TestSetupRoutes(t *testing.T) {
	h := NewHandler(nil, nil)
	router := gin.New()
	h.SetupRoutes(router)

	routes := router.Routes()

	expectedRoutes := map[string]string{
		"GET:/health":                       "HealthCheck",
		"GET:/ready":                        "ReadinessCheck",
		"GET:/api/v1/sandboxes":             "ListSandboxes",
		"POST:/api/v1/sandboxes":            "CreateSandbox",
		"GET:/api/v1/sandboxes/:id":         "GetSandbox",
		"DELETE:/api/v1/sandboxes/:id":      "TerminateSandbox",
		"POST:/api/v1/sandboxes/:id/execute": "ExecuteCommand",
		"POST:/api/v1/sandboxes/:id/files":  "UploadFile",
		"POST:/api/v1/sandboxes/:id/files/upload-url": "GenerateUploadUrl",
		"POST:/api/v1/sandboxes/:id/files/confirm":    "ConfirmUpload",
	}

	routeMap := make(map[string]bool)
	for _, route := range routes {
		key := route.Method + ":" + route.Path
		routeMap[key] = true
	}

	for expectedRoute := range expectedRoutes {
		if !routeMap[expectedRoute] {
			t.Errorf("SetupRoutes() missing route: %s", expectedRoute)
		}
	}
}

func TestNewHandler(t *testing.T) {
	manager := kubernetes.NewManager("test-ns")
	h := NewHandler(manager, nil)

	if h == nil {
		t.Fatal("NewHandler() returned nil")
	}

	if h.k8sManager != manager {
		t.Error("NewHandler() did not set k8sManager correctly")
	}

	if h.storage != nil {
		t.Error("NewHandler() storage should be nil when passed nil")
	}
}
