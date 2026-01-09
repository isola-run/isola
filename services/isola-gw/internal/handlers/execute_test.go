package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/omereli/dev-isola/services/isola-gw/internal/models"
)

func TestExecuteCommandRequest_Validation(t *testing.T) {
	router := gin.New()
	router.POST("/execute", func(c *gin.Context) {
		var req models.ExecuteCommandRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, models.ErrorResponse{
				Error:   "BadRequest",
				Message: err.Error(),
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{"command": req.Command})
	})

	tests := []struct {
		name     string
		body     interface{}
		wantCode int
	}{
		{
			name:     "valid command",
			body:     models.ExecuteCommandRequest{Command: "echo hello"},
			wantCode: http.StatusOK,
		},
		{
			name:     "empty body",
			body:     nil,
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "missing command field",
			body:     map[string]interface{}{},
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "empty command string",
			body:     models.ExecuteCommandRequest{Command: ""},
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

			req := httptest.NewRequest(http.MethodPost, "/execute", bytes.NewBuffer(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			if w.Code != tt.wantCode {
				t.Errorf("status = %v, want %v, body: %s", w.Code, tt.wantCode, w.Body.String())
			}
		})
	}
}

func TestExecuteCommandResponse_JSON(t *testing.T) {
	response := models.ExecuteCommandResponse{
		Stdout:   "hello world\n",
		Stderr:   "",
		ExitCode: 0,
	}

	jsonBytes, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("Failed to marshal ExecuteCommandResponse: %v", err)
	}

	var parsed models.ExecuteCommandResponse
	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal ExecuteCommandResponse: %v", err)
	}

	if parsed.Stdout != "hello world\n" {
		t.Errorf("ExecuteCommandResponse Stdout = %v, want 'hello world\\n'", parsed.Stdout)
	}

	if parsed.Stderr != "" {
		t.Errorf("ExecuteCommandResponse Stderr = %v, want empty", parsed.Stderr)
	}

	if parsed.ExitCode != 0 {
		t.Errorf("ExecuteCommandResponse ExitCode = %v, want 0", parsed.ExitCode)
	}
}

func TestExecuteCommandResponse_WithError(t *testing.T) {
	response := models.ExecuteCommandResponse{
		Stdout:   "",
		Stderr:   "command not found: foo",
		ExitCode: 127,
	}

	jsonBytes, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("Failed to marshal ExecuteCommandResponse: %v", err)
	}

	var parsed models.ExecuteCommandResponse
	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal ExecuteCommandResponse: %v", err)
	}

	if parsed.Stdout != "" {
		t.Errorf("ExecuteCommandResponse Stdout = %v, want empty", parsed.Stdout)
	}

	if parsed.Stderr != "command not found: foo" {
		t.Errorf("ExecuteCommandResponse Stderr = %v, want 'command not found: foo'", parsed.Stderr)
	}

	if parsed.ExitCode != 127 {
		t.Errorf("ExecuteCommandResponse ExitCode = %v, want 127", parsed.ExitCode)
	}
}

func TestExecuteCommand_InvalidSandboxID(t *testing.T) {
	h := NewHandler(nil, nil)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("tenant_id", "test-tenant")
		c.Next()
	})
	router.POST("/sandboxes/:id/execute", h.ExecuteCommand)

	// Execute against a sandbox without a k8s manager (will fail on status check)
	body, _ := json.Marshal(models.ExecuteCommandRequest{Command: "echo test"})
	req := httptest.NewRequest(http.MethodPost, "/sandboxes/invalid-id/execute", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Should fail because k8sManager is nil
	if w.Code != http.StatusInternalServerError {
		t.Errorf("ExecuteCommand() status = %v, want %v", w.Code, http.StatusInternalServerError)
	}
}

func TestExecuteCommand_BadRequest(t *testing.T) {
	h := NewHandler(nil, nil)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("tenant_id", "test-tenant")
		c.Next()
	})
	router.POST("/sandboxes/:id/execute", h.ExecuteCommand)

	tests := []struct {
		name        string
		body        interface{}
		contentType string
		wantCode    int
	}{
		{
			name:        "missing command",
			body:        map[string]interface{}{},
			contentType: "application/json",
			wantCode:    http.StatusBadRequest,
		},
		{
			name:        "empty request body",
			body:        nil,
			contentType: "application/json",
			wantCode:    http.StatusBadRequest,
		},
		{
			name:        "invalid JSON",
			body:        "not json",
			contentType: "application/json",
			wantCode:    http.StatusBadRequest,
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

			req := httptest.NewRequest(http.MethodPost, "/sandboxes/test-id/execute", bytes.NewBuffer(bodyBytes))
			req.Header.Set("Content-Type", tt.contentType)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			if w.Code != tt.wantCode {
				t.Errorf("ExecuteCommand() status = %v, want %v", w.Code, tt.wantCode)
			}

			var response models.ErrorResponse
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatalf("Failed to unmarshal response: %v", err)
			}

			if response.Error != "BadRequest" {
				t.Errorf("ExecuteCommand() error = %v, want BadRequest", response.Error)
			}
		})
	}
}

func TestExecuteCommand_ExtractionOfSandboxID(t *testing.T) {
	capturedID := ""

	router := gin.New()
	router.POST("/sandboxes/:id/execute", func(c *gin.Context) {
		capturedID = c.Param("id")
		c.JSON(http.StatusOK, gin.H{"id": capturedID})
	})

	tests := []struct {
		name       string
		sandboxID  string
		expectedID string
	}{
		{
			name:       "UUID format",
			sandboxID:  "abc12345-6789-0123-4567-890abcdef012",
			expectedID: "abc12345-6789-0123-4567-890abcdef012",
		},
		{
			name:       "short ID",
			sandboxID:  "short",
			expectedID: "short",
		},
		{
			name:       "with special chars (URL encoded)",
			sandboxID:  "test-id_123",
			expectedID: "test-id_123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			capturedID = ""

			body, _ := json.Marshal(models.ExecuteCommandRequest{Command: "test"})
			req := httptest.NewRequest(http.MethodPost, "/sandboxes/"+tt.sandboxID+"/execute", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			if capturedID != tt.expectedID {
				t.Errorf("Captured sandbox ID = %v, want %v", capturedID, tt.expectedID)
			}
		})
	}
}

func TestExecuteCommand_JSONFields(t *testing.T) {
	// Test that the request/response JSON field names are correct
	reqJSON := `{"command":"ls -la"}`
	var req models.ExecuteCommandRequest
	if err := json.Unmarshal([]byte(reqJSON), &req); err != nil {
		t.Fatalf("Failed to unmarshal request: %v", err)
	}

	if req.Command != "ls -la" {
		t.Errorf("ExecuteCommandRequest Command = %v, want 'ls -la'", req.Command)
	}

	respJSON := `{"stdout":"output","stderr":"error","exitCode":1}`
	var resp models.ExecuteCommandResponse
	if err := json.Unmarshal([]byte(respJSON), &resp); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if resp.Stdout != "output" {
		t.Errorf("ExecuteCommandResponse Stdout = %v, want 'output'", resp.Stdout)
	}

	if resp.Stderr != "error" {
		t.Errorf("ExecuteCommandResponse Stderr = %v, want 'error'", resp.Stderr)
	}

	if resp.ExitCode != 1 {
		t.Errorf("ExecuteCommandResponse ExitCode = %v, want 1", resp.ExitCode)
	}
}
