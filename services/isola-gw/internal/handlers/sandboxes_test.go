package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/omereli/dev-isola/services/isola-gw/internal/models"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestSandboxCRToModel(t *testing.T) {
	h := NewHandler(nil, nil)

	tests := []struct {
		name            string
		cr              *unstructured.Unstructured
		expectedID      string
		expectedName    string
		expectedState   models.SandboxState
		expectNil       bool
	}{
		{
			name: "valid CR with all fields",
			cr: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"name": "sandbox-abc12345",
						"labels": map[string]interface{}{
							"sandbox-id": "abc12345-6789-0123",
						},
						"annotations": map[string]interface{}{
							"isola.run/sandbox-name": "my-sandbox",
						},
					},
					"status": map[string]interface{}{
						"conditions": []interface{}{
							map[string]interface{}{
								"type":   "Ready",
								"status": "True",
							},
						},
					},
				},
			},
			expectedID:    "abc12345-6789-0123",
			expectedName:  "my-sandbox",
			expectedState: models.SandboxStateRunning,
			expectNil:     false,
		},
		{
			name: "CR without annotation uses CR name",
			cr: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"name": "sandbox-test",
						"labels": map[string]interface{}{
							"sandbox-id": "test-id-123",
						},
					},
				},
			},
			expectedID:    "test-id-123",
			expectedName:  "sandbox-test",
			expectedState: models.SandboxStatePending,
			expectNil:     false,
		},
		{
			name: "CR without status returns pending",
			cr: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"name": "sandbox-pending",
						"labels": map[string]interface{}{
							"sandbox-id": "pending-123",
						},
					},
				},
			},
			expectedID:    "pending-123",
			expectedName:  "sandbox-pending",
			expectedState: models.SandboxStatePending,
			expectNil:     false,
		},
		{
			name: "CR without metadata returns nil",
			cr: &unstructured.Unstructured{
				Object: map[string]interface{}{},
			},
			expectNil: true,
		},
		{
			name: "CR without sandbox-id label returns nil",
			cr: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"name":   "sandbox-test",
						"labels": map[string]interface{}{},
					},
				},
			},
			expectNil: true,
		},
		{
			name: "CR with empty sandbox-id returns nil",
			cr: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"name": "sandbox-test",
						"labels": map[string]interface{}{
							"sandbox-id": "",
						},
					},
				},
			},
			expectNil: true,
		},
		{
			name: "CR with TimedOut condition returns stopped",
			cr: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"name": "sandbox-timeout",
						"labels": map[string]interface{}{
							"sandbox-id": "timeout-123",
						},
					},
					"status": map[string]interface{}{
						"conditions": []interface{}{
							map[string]interface{}{
								"type":   "TimedOut",
								"status": "True",
							},
						},
					},
				},
			},
			expectedID:    "timeout-123",
			expectedName:  "sandbox-timeout",
			expectedState: models.SandboxStateStopped,
			expectNil:     false,
		},
		{
			name: "CR with short sandbox ID uses truncated name fallback",
			cr: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"labels": map[string]interface{}{
							"sandbox-id": "short",
						},
					},
				},
			},
			expectedID:    "short",
			expectedName:  "sandbox-short",
			expectedState: models.SandboxStatePending,
			expectNil:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := h.sandboxCRToModel(tt.cr)

			if tt.expectNil {
				if result != nil {
					t.Errorf("sandboxCRToModel() = %v, want nil", result)
				}
				return
			}

			if result == nil {
				t.Fatal("sandboxCRToModel() returned nil unexpectedly")
			}

			if result.ID != tt.expectedID {
				t.Errorf("sandboxCRToModel() ID = %v, want %v", result.ID, tt.expectedID)
			}

			if result.Name != tt.expectedName {
				t.Errorf("sandboxCRToModel() Name = %v, want %v", result.Name, tt.expectedName)
			}

			if result.State != tt.expectedState {
				t.Errorf("sandboxCRToModel() State = %v, want %v", result.State, tt.expectedState)
			}

			// Verify other fields are initialized
			if result.Env == nil {
				t.Error("sandboxCRToModel() Env is nil")
			}

			if result.Labels == nil {
				t.Error("sandboxCRToModel() Labels is nil")
			}

			if result.DesiredState == nil {
				t.Error("sandboxCRToModel() DesiredState is nil")
			}

			// CreatedAt should be set
			if result.CreatedAt.IsZero() {
				t.Error("sandboxCRToModel() CreatedAt is zero")
			}
		})
	}
}

func TestListSandboxes_BadRequest(t *testing.T) {
	h := NewHandler(nil, nil)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("tenant_id", "test-tenant")
		c.Next()
	})
	router.GET("/sandboxes", h.ListSandboxes)

	tests := []struct {
		name        string
		queryParams string
		wantCode    int
	}{
		{
			name:        "limit too high",
			queryParams: "?limit=200",
			wantCode:    http.StatusBadRequest,
		},
		{
			name:        "limit too low",
			queryParams: "?limit=0",
			wantCode:    http.StatusBadRequest,
		},
		{
			name:        "negative offset",
			queryParams: "?offset=-1",
			wantCode:    http.StatusBadRequest,
		},
		{
			name:        "invalid limit type",
			queryParams: "?limit=abc",
			wantCode:    http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/sandboxes"+tt.queryParams, nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			if w.Code != tt.wantCode {
				t.Errorf("ListSandboxes() status = %v, want %v", w.Code, tt.wantCode)
			}

			var response models.ErrorResponse
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatalf("Failed to unmarshal response: %v", err)
			}

			if response.Error != "BadRequest" {
				t.Errorf("ListSandboxes() error = %v, want BadRequest", response.Error)
			}
		})
	}
}

func TestCreateSandbox_BadRequest(t *testing.T) {
	h := NewHandler(nil, nil)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("tenant_id", "test-tenant")
		c.Next()
	})
	router.POST("/sandboxes", h.CreateSandbox)

	tests := []struct {
		name     string
		body     interface{}
		wantCode int
	}{
		{
			name:     "empty body",
			body:     nil,
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "missing name",
			body:     map[string]interface{}{},
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "invalid JSON",
			body:     "not json",
			wantCode: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var bodyBytes []byte
			if tt.body != nil {
				if s, ok := tt.body.(string); ok {
					bodyBytes = []byte(s)
				} else {
					var err error
					bodyBytes, err = json.Marshal(tt.body)
					if err != nil {
						t.Fatalf("Failed to marshal body: %v", err)
					}
				}
			}

			req := httptest.NewRequest(http.MethodPost, "/sandboxes", bytes.NewBuffer(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			if w.Code != tt.wantCode {
				t.Errorf("CreateSandbox() status = %v, want %v", w.Code, tt.wantCode)
			}
		})
	}
}

func TestCreateSandboxRequest_Defaults(t *testing.T) {
	// Test that a valid request with minimal fields works
	req := models.CreateSandboxRequest{
		Name: "test-sandbox",
	}

	if req.Name != "test-sandbox" {
		t.Errorf("CreateSandboxRequest Name = %v, want test-sandbox", req.Name)
	}

	if req.Image != nil {
		t.Errorf("CreateSandboxRequest Image should be nil by default")
	}

	if req.CPU != nil {
		t.Errorf("CreateSandboxRequest CPU should be nil by default")
	}

	if req.Memory != nil {
		t.Errorf("CreateSandboxRequest Memory should be nil by default")
	}

	if req.AutoStart != false {
		t.Errorf("CreateSandboxRequest AutoStart should be false by default")
	}
}

func TestCreateSandboxRequest_WithFields(t *testing.T) {
	cpu := 2.0
	memory := 4.0
	image := "ubuntu:22.04"

	req := models.CreateSandboxRequest{
		Name:      "full-sandbox",
		Image:     &image,
		CPU:       &cpu,
		Memory:    &memory,
		AutoStart: true,
		Env: map[string]string{
			"DEBUG": "true",
		},
		Labels: map[string]string{
			"env": "test",
		},
	}

	if req.Name != "full-sandbox" {
		t.Errorf("CreateSandboxRequest Name = %v, want full-sandbox", req.Name)
	}

	if req.Image == nil || *req.Image != "ubuntu:22.04" {
		t.Errorf("CreateSandboxRequest Image = %v, want ubuntu:22.04", req.Image)
	}

	if req.CPU == nil || *req.CPU != 2.0 {
		t.Errorf("CreateSandboxRequest CPU = %v, want 2.0", req.CPU)
	}

	if req.Memory == nil || *req.Memory != 4.0 {
		t.Errorf("CreateSandboxRequest Memory = %v, want 4.0", req.Memory)
	}

	if !req.AutoStart {
		t.Errorf("CreateSandboxRequest AutoStart = %v, want true", req.AutoStart)
	}

	if req.Env["DEBUG"] != "true" {
		t.Errorf("CreateSandboxRequest Env[DEBUG] = %v, want true", req.Env["DEBUG"])
	}
}

func TestListSandboxesParams_Defaults(t *testing.T) {
	// Test parsing with defaults
	h := NewHandler(nil, nil)

	router := gin.New()
	var capturedParams models.ListSandboxesParams
	router.GET("/sandboxes", func(c *gin.Context) {
		// This will use Gin's binding which applies defaults
		if err := c.ShouldBindQuery(&capturedParams); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"limit": capturedParams.Limit, "offset": capturedParams.Offset})
	})
	_ = h

	req := httptest.NewRequest(http.MethodGet, "/sandboxes", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Request failed: %v", w.Body.String())
	}

	// Default limit is 20
	if capturedParams.Limit != 20 {
		t.Errorf("ListSandboxesParams Limit = %v, want 20", capturedParams.Limit)
	}

	// Default offset is 0
	if capturedParams.Offset != 0 {
		t.Errorf("ListSandboxesParams Offset = %v, want 0", capturedParams.Offset)
	}

	// State should be nil by default
	if capturedParams.State != nil {
		t.Errorf("ListSandboxesParams State = %v, want nil", capturedParams.State)
	}
}

func TestListSandboxesParams_CustomValues(t *testing.T) {
	router := gin.New()
	var capturedParams models.ListSandboxesParams
	router.GET("/sandboxes", func(c *gin.Context) {
		if err := c.ShouldBindQuery(&capturedParams); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/sandboxes?limit=50&offset=10&state=running", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Request failed: %v", w.Body.String())
	}

	if capturedParams.Limit != 50 {
		t.Errorf("ListSandboxesParams Limit = %v, want 50", capturedParams.Limit)
	}

	if capturedParams.Offset != 10 {
		t.Errorf("ListSandboxesParams Offset = %v, want 10", capturedParams.Offset)
	}

	if capturedParams.State == nil || *capturedParams.State != models.SandboxStateRunning {
		t.Errorf("ListSandboxesParams State = %v, want running", capturedParams.State)
	}
}

func TestTerminateSandboxParams(t *testing.T) {
	router := gin.New()
	var capturedParams models.TerminateSandboxParams
	router.DELETE("/sandboxes/:id", func(c *gin.Context) {
		_ = c.ShouldBindQuery(&capturedParams)
		c.JSON(http.StatusOK, gin.H{"force": capturedParams.Force})
	})

	tests := []struct {
		name        string
		queryParams string
		wantForce   bool
	}{
		{
			name:        "default force is false",
			queryParams: "",
			wantForce:   false,
		},
		{
			name:        "force=true",
			queryParams: "?force=true",
			wantForce:   true,
		},
		{
			name:        "force=false explicit",
			queryParams: "?force=false",
			wantForce:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			capturedParams = models.TerminateSandboxParams{}
			req := httptest.NewRequest(http.MethodDelete, "/sandboxes/test-id"+tt.queryParams, nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			if capturedParams.Force != tt.wantForce {
				t.Errorf("TerminateSandboxParams Force = %v, want %v", capturedParams.Force, tt.wantForce)
			}
		})
	}
}

func TestSandboxState_Values(t *testing.T) {
	tests := []struct {
		state    models.SandboxState
		expected string
	}{
		{models.SandboxStatePending, "pending"},
		{models.SandboxStateStarting, "starting"},
		{models.SandboxStateRunning, "running"},
		{models.SandboxStateTerminating, "terminating"},
		{models.SandboxStateStopped, "stopped"},
		{models.SandboxStateError, "error"},
		{models.SandboxStateUnknown, "unknown"},
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			if string(tt.state) != tt.expected {
				t.Errorf("SandboxState = %v, want %v", string(tt.state), tt.expected)
			}
		})
	}
}

func TestSandboxList_JSON(t *testing.T) {
	list := models.SandboxList{
		Items: []models.Sandbox{
			{
				ID:        "test-1",
				Name:      "sandbox-1",
				State:     models.SandboxStateRunning,
				CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			},
		},
		Total:  1,
		Limit:  20,
		Offset: 0,
	}

	jsonBytes, err := json.Marshal(list)
	if err != nil {
		t.Fatalf("Failed to marshal SandboxList: %v", err)
	}

	var parsed models.SandboxList
	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal SandboxList: %v", err)
	}

	if parsed.Total != 1 {
		t.Errorf("SandboxList Total = %v, want 1", parsed.Total)
	}

	if parsed.Limit != 20 {
		t.Errorf("SandboxList Limit = %v, want 20", parsed.Limit)
	}

	if len(parsed.Items) != 1 {
		t.Errorf("SandboxList Items length = %v, want 1", len(parsed.Items))
	}

	if parsed.Items[0].ID != "test-1" {
		t.Errorf("SandboxList Items[0].ID = %v, want test-1", parsed.Items[0].ID)
	}
}

func TestSandboxList_EmptyItems(t *testing.T) {
	list := models.SandboxList{
		Items:  []models.Sandbox{},
		Total:  0,
		Limit:  20,
		Offset: 0,
	}

	jsonBytes, err := json.Marshal(list)
	if err != nil {
		t.Fatalf("Failed to marshal SandboxList: %v", err)
	}

	var parsed models.SandboxList
	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal SandboxList: %v", err)
	}

	if parsed.Items == nil {
		t.Error("SandboxList Items is nil, want empty slice")
	}

	if len(parsed.Items) != 0 {
		t.Errorf("SandboxList Items length = %v, want 0", len(parsed.Items))
	}
}
