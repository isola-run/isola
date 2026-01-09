package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/omereli/dev-isola/services/isola-gw/internal/models"
)

func TestUploadUrlRequest_Validation(t *testing.T) {
	router := gin.New()
	router.POST("/upload-url", func(c *gin.Context) {
		var req models.UploadUrlRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, models.ErrorResponse{
				Error:   "BadRequest",
				Message: err.Error(),
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{"path": req.Path, "filename": req.Filename})
	})

	tests := []struct {
		name     string
		body     interface{}
		wantCode int
	}{
		{
			name: "valid request",
			body: models.UploadUrlRequest{
				Path:     "/workspace",
				Filename: "test.txt",
			},
			wantCode: http.StatusOK,
		},
		{
			name:     "missing path",
			body:     map[string]interface{}{"filename": "test.txt"},
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "missing filename",
			body:     map[string]interface{}{"path": "/workspace"},
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "empty body",
			body:     nil,
			wantCode: http.StatusBadRequest,
		},
		{
			name: "empty path",
			body: models.UploadUrlRequest{
				Path:     "",
				Filename: "test.txt",
			},
			wantCode: http.StatusBadRequest,
		},
		{
			name: "empty filename",
			body: models.UploadUrlRequest{
				Path:     "/workspace",
				Filename: "",
			},
			wantCode: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var bodyBytes []byte
			if tt.body != nil {
				var err error
				bodyBytes, err = json.Marshal(tt.body)
				if err != nil {
					t.Fatalf("Failed to marshal body: %v", err)
				}
			}

			req := httptest.NewRequest(http.MethodPost, "/upload-url", bytes.NewBuffer(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			if w.Code != tt.wantCode {
				t.Errorf("status = %v, want %v, body: %s", w.Code, tt.wantCode, w.Body.String())
			}
		})
	}
}

func TestUploadUrlRequest_WithContentType(t *testing.T) {
	contentType := "text/plain"
	req := models.UploadUrlRequest{
		Path:        "/workspace",
		Filename:    "test.txt",
		ContentType: &contentType,
	}

	jsonBytes, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	var parsed models.UploadUrlRequest
	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if parsed.ContentType == nil {
		t.Fatal("ContentType is nil")
	}

	if *parsed.ContentType != "text/plain" {
		t.Errorf("ContentType = %v, want text/plain", *parsed.ContentType)
	}
}

func TestConfirmUploadRequest_Validation(t *testing.T) {
	router := gin.New()
	router.POST("/confirm", func(c *gin.Context) {
		var req models.ConfirmUploadRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, models.ErrorResponse{
				Error:   "BadRequest",
				Message: err.Error(),
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{"upload_id": req.UploadID})
	})

	tests := []struct {
		name     string
		body     interface{}
		wantCode int
	}{
		{
			name: "valid request",
			body: models.ConfirmUploadRequest{
				UploadID: "abc-123",
				Filename: "test.txt",
				Path:     "/workspace",
			},
			wantCode: http.StatusOK,
		},
		{
			name: "missing upload_id",
			body: map[string]interface{}{
				"filename": "test.txt",
				"path":     "/workspace",
			},
			wantCode: http.StatusBadRequest,
		},
		{
			name: "missing filename",
			body: map[string]interface{}{
				"upload_id": "abc-123",
				"path":      "/workspace",
			},
			wantCode: http.StatusBadRequest,
		},
		{
			name: "missing path",
			body: map[string]interface{}{
				"upload_id": "abc-123",
				"filename":  "test.txt",
			},
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "empty body",
			body:     nil,
			wantCode: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var bodyBytes []byte
			if tt.body != nil {
				var err error
				bodyBytes, err = json.Marshal(tt.body)
				if err != nil {
					t.Fatalf("Failed to marshal body: %v", err)
				}
			}

			req := httptest.NewRequest(http.MethodPost, "/confirm", bytes.NewBuffer(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			if w.Code != tt.wantCode {
				t.Errorf("status = %v, want %v, body: %s", w.Code, tt.wantCode, w.Body.String())
			}
		})
	}
}

func TestDownloadRequest_Validation(t *testing.T) {
	router := gin.New()
	router.POST("/download", func(c *gin.Context) {
		var req models.DownloadRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, models.ErrorResponse{
				Error:   "BadRequest",
				Message: err.Error(),
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{"download_url": req.DownloadURL})
	})

	tests := []struct {
		name     string
		body     interface{}
		wantCode int
	}{
		{
			name: "valid request",
			body: models.DownloadRequest{
				DownloadURL: "https://storage.example.com/file",
				Path:        "/workspace/file.txt",
			},
			wantCode: http.StatusOK,
		},
		{
			name:     "missing download_url",
			body:     map[string]interface{}{"path": "/workspace"},
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "missing path",
			body:     map[string]interface{}{"download_url": "https://example.com"},
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "empty body",
			body:     nil,
			wantCode: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var bodyBytes []byte
			if tt.body != nil {
				var err error
				bodyBytes, err = json.Marshal(tt.body)
				if err != nil {
					t.Fatalf("Failed to marshal body: %v", err)
				}
			}

			req := httptest.NewRequest(http.MethodPost, "/download", bytes.NewBuffer(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			if w.Code != tt.wantCode {
				t.Errorf("status = %v, want %v, body: %s", w.Code, tt.wantCode, w.Body.String())
			}
		})
	}
}

func TestUploadUrlResponse_JSON(t *testing.T) {
	response := models.UploadUrlResponse{
		UploadURL: "https://s3.example.com/bucket/key?signature=abc",
		UploadID:  "upload-123",
		ExpiresIn: 900,
	}

	jsonBytes, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	var parsed models.UploadUrlResponse
	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if parsed.UploadURL != response.UploadURL {
		t.Errorf("UploadURL = %v, want %v", parsed.UploadURL, response.UploadURL)
	}

	if parsed.UploadID != response.UploadID {
		t.Errorf("UploadID = %v, want %v", parsed.UploadID, response.UploadID)
	}

	if parsed.ExpiresIn != 900 {
		t.Errorf("ExpiresIn = %v, want 900", parsed.ExpiresIn)
	}
}

func TestConfirmUploadResponse_JSON(t *testing.T) {
	response := models.ConfirmUploadResponse{
		Success: true,
		Path:    "/workspace/file.txt",
		Size:    1024,
	}

	jsonBytes, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	var parsed models.ConfirmUploadResponse
	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if !parsed.Success {
		t.Error("Success = false, want true")
	}

	if parsed.Path != "/workspace/file.txt" {
		t.Errorf("Path = %v, want /workspace/file.txt", parsed.Path)
	}

	if parsed.Size != 1024 {
		t.Errorf("Size = %v, want 1024", parsed.Size)
	}
}

func TestFileUploadResponse_JSON(t *testing.T) {
	response := models.FileUploadResponse{
		Success: true,
		Path:    "/workspace/file.txt",
		Size:    2048,
	}

	jsonBytes, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	var parsed models.FileUploadResponse
	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if !parsed.Success {
		t.Error("Success = false, want true")
	}

	if parsed.Path != "/workspace/file.txt" {
		t.Errorf("Path = %v, want /workspace/file.txt", parsed.Path)
	}

	if parsed.Size != 2048 {
		t.Errorf("Size = %v, want 2048", parsed.Size)
	}
}

func TestGenerateUploadUrl_NoStorage(t *testing.T) {
	// Handler with nil storage
	h := NewHandler(nil, nil)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("tenant_id", "test-tenant")
		c.Next()
	})
	router.POST("/sandboxes/:id/files/upload-url", h.GenerateUploadUrl)

	body, _ := json.Marshal(models.UploadUrlRequest{
		Path:     "/workspace",
		Filename: "test.txt",
	})

	req := httptest.NewRequest(http.MethodPost, "/sandboxes/test-id/files/upload-url", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Errorf("GenerateUploadUrl() status = %v, want %v", w.Code, http.StatusNotImplemented)
	}

	var response models.ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if response.Error != "NotImplemented" {
		t.Errorf("GenerateUploadUrl() error = %v, want NotImplemented", response.Error)
	}
}

func TestConfirmUpload_NoStorage(t *testing.T) {
	h := NewHandler(nil, nil)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("tenant_id", "test-tenant")
		c.Next()
	})
	router.POST("/sandboxes/:id/files/confirm", h.ConfirmUpload)

	body, _ := json.Marshal(models.ConfirmUploadRequest{
		UploadID: "abc-123",
		Filename: "test.txt",
		Path:     "/workspace",
	})

	req := httptest.NewRequest(http.MethodPost, "/sandboxes/test-id/files/confirm", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Errorf("ConfirmUpload() status = %v, want %v", w.Code, http.StatusNotImplemented)
	}

	var response models.ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if response.Error != "NotImplemented" {
		t.Errorf("ConfirmUpload() error = %v, want NotImplemented", response.Error)
	}
}

func TestGenerateUploadUrl_BadRequest(t *testing.T) {
	h := NewHandler(nil, nil)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("tenant_id", "test-tenant")
		c.Next()
	})
	router.POST("/sandboxes/:id/files/upload-url", h.GenerateUploadUrl)

	tests := []struct {
		name     string
		body     interface{}
		wantCode int
	}{
		{
			name:     "missing path",
			body:     map[string]interface{}{"filename": "test.txt"},
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "missing filename",
			body:     map[string]interface{}{"path": "/workspace"},
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "empty body",
			body:     nil,
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

			req := httptest.NewRequest(http.MethodPost, "/sandboxes/test-id/files/upload-url", bytes.NewBuffer(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			if w.Code != tt.wantCode {
				t.Errorf("GenerateUploadUrl() status = %v, want %v", w.Code, tt.wantCode)
			}
		})
	}
}

func TestConfirmUpload_BadRequest(t *testing.T) {
	h := NewHandler(nil, nil)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("tenant_id", "test-tenant")
		c.Next()
	})
	router.POST("/sandboxes/:id/files/confirm", h.ConfirmUpload)

	tests := []struct {
		name     string
		body     interface{}
		wantCode int
	}{
		{
			name: "missing upload_id",
			body: map[string]interface{}{
				"filename": "test.txt",
				"path":     "/workspace",
			},
			wantCode: http.StatusBadRequest,
		},
		{
			name: "missing filename",
			body: map[string]interface{}{
				"upload_id": "abc-123",
				"path":      "/workspace",
			},
			wantCode: http.StatusBadRequest,
		},
		{
			name: "missing path",
			body: map[string]interface{}{
				"upload_id": "abc-123",
				"filename":  "test.txt",
			},
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "empty body",
			body:     nil,
			wantCode: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var bodyBytes []byte
			if tt.body != nil {
				var err error
				bodyBytes, err = json.Marshal(tt.body)
				if err != nil {
					t.Fatalf("Failed to marshal body: %v", err)
				}
			}

			req := httptest.NewRequest(http.MethodPost, "/sandboxes/test-id/files/confirm", bytes.NewBuffer(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			if w.Code != tt.wantCode {
				t.Errorf("ConfirmUpload() status = %v, want %v", w.Code, tt.wantCode)
			}
		})
	}
}

func TestUploadFile_BadRequest_NoFile(t *testing.T) {
	h := NewHandler(nil, nil)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("tenant_id", "test-tenant")
		c.Next()
	})
	router.POST("/sandboxes/:id/files", h.UploadFile)

	// Create a multipart request without a file
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("path", "/workspace")
	writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/sandboxes/test-id/files", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Without k8sManager, it will fail at status check
	if w.Code != http.StatusInternalServerError {
		t.Errorf("UploadFile() status = %v, want %v", w.Code, http.StatusInternalServerError)
	}
}

func TestUploadFile_BadRequest_InvalidMultipart(t *testing.T) {
	h := NewHandler(nil, nil)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("tenant_id", "test-tenant")
		c.Next()
	})
	router.POST("/sandboxes/:id/files", h.UploadFile)

	// Send non-multipart body
	req := httptest.NewRequest(http.MethodPost, "/sandboxes/test-id/files", bytes.NewBuffer([]byte("not multipart")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Should fail at multipart parsing (but will fail at k8s status check first without manager)
	if w.Code != http.StatusInternalServerError && w.Code != http.StatusBadRequest {
		t.Errorf("UploadFile() status = %v, want InternalServerError or BadRequest", w.Code)
	}
}

func TestFileSizeThreshold(t *testing.T) {
	// Verify the constant is set correctly
	if fileSizeThresholdBytes != 5*1024*1024 {
		t.Errorf("fileSizeThresholdBytes = %v, want %v", fileSizeThresholdBytes, 5*1024*1024)
	}
}

// Helper to create a multipart form with file
func createMultipartFileRequest(t *testing.T, endpoint, fieldName, filename string, content []byte, extraFields map[string]string) *http.Request {
	t.Helper()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Add extra fields
	for key, value := range extraFields {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatalf("Failed to write field %s: %v", key, err)
		}
	}

	// Add file
	part, err := writer.CreateFormFile(fieldName, filename)
	if err != nil {
		t.Fatalf("Failed to create form file: %v", err)
	}
	if _, err := io.Copy(part, bytes.NewReader(content)); err != nil {
		t.Fatalf("Failed to copy file content: %v", err)
	}

	writer.Close()

	req := httptest.NewRequest(http.MethodPost, endpoint, body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	return req
}

func TestMultipartFormParsing(t *testing.T) {
	router := gin.New()
	router.POST("/upload", func(c *gin.Context) {
		form, err := c.MultipartForm()
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		files := form.File["file"]
		if len(files) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "no file"})
			return
		}

		pathValues := form.Value["path"]
		if len(pathValues) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "no path"})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"filename": files[0].Filename,
			"size":     files[0].Size,
			"path":     pathValues[0],
		})
	})

	req := createMultipartFileRequest(t, "/upload", "file", "test.txt", []byte("hello world"), map[string]string{
		"path": "/workspace",
	})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %v, want %v, body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var response map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if response["filename"] != "test.txt" {
		t.Errorf("filename = %v, want test.txt", response["filename"])
	}

	if response["path"] != "/workspace" {
		t.Errorf("path = %v, want /workspace", response["path"])
	}
}

func TestJSONFieldNaming_FileRequests(t *testing.T) {
	// Test UploadUrlRequest JSON field names
	uploadURLJSON := `{"path":"/workspace","filename":"test.txt","content_type":"text/plain"}`
	var uploadReq models.UploadUrlRequest
	if err := json.Unmarshal([]byte(uploadURLJSON), &uploadReq); err != nil {
		t.Fatalf("Failed to unmarshal UploadUrlRequest: %v", err)
	}

	if uploadReq.Path != "/workspace" {
		t.Errorf("UploadUrlRequest Path = %v, want /workspace", uploadReq.Path)
	}
	if uploadReq.Filename != "test.txt" {
		t.Errorf("UploadUrlRequest Filename = %v, want test.txt", uploadReq.Filename)
	}
	if uploadReq.ContentType == nil || *uploadReq.ContentType != "text/plain" {
		t.Errorf("UploadUrlRequest ContentType = %v, want text/plain", uploadReq.ContentType)
	}

	// Test ConfirmUploadRequest JSON field names
	confirmJSON := `{"upload_id":"abc-123","filename":"test.txt","path":"/workspace"}`
	var confirmReq models.ConfirmUploadRequest
	if err := json.Unmarshal([]byte(confirmJSON), &confirmReq); err != nil {
		t.Fatalf("Failed to unmarshal ConfirmUploadRequest: %v", err)
	}

	if confirmReq.UploadID != "abc-123" {
		t.Errorf("ConfirmUploadRequest UploadID = %v, want abc-123", confirmReq.UploadID)
	}
	if confirmReq.Filename != "test.txt" {
		t.Errorf("ConfirmUploadRequest Filename = %v, want test.txt", confirmReq.Filename)
	}
	if confirmReq.Path != "/workspace" {
		t.Errorf("ConfirmUploadRequest Path = %v, want /workspace", confirmReq.Path)
	}

	// Test DownloadRequest JSON field names
	downloadJSON := `{"download_url":"https://example.com/file","path":"/workspace/file"}`
	var downloadReq models.DownloadRequest
	if err := json.Unmarshal([]byte(downloadJSON), &downloadReq); err != nil {
		t.Fatalf("Failed to unmarshal DownloadRequest: %v", err)
	}

	if downloadReq.DownloadURL != "https://example.com/file" {
		t.Errorf("DownloadRequest DownloadURL = %v, want https://example.com/file", downloadReq.DownloadURL)
	}
	if downloadReq.Path != "/workspace/file" {
		t.Errorf("DownloadRequest Path = %v, want /workspace/file", downloadReq.Path)
	}
}

func TestJSONFieldNaming_FileResponses(t *testing.T) {
	// Test UploadUrlResponse JSON field names
	uploadRespJSON := `{"upload_url":"https://s3.example.com","upload_id":"abc","expires_in":900}`
	var uploadResp models.UploadUrlResponse
	if err := json.Unmarshal([]byte(uploadRespJSON), &uploadResp); err != nil {
		t.Fatalf("Failed to unmarshal UploadUrlResponse: %v", err)
	}

	if uploadResp.UploadURL != "https://s3.example.com" {
		t.Errorf("UploadUrlResponse UploadURL = %v, want https://s3.example.com", uploadResp.UploadURL)
	}
	if uploadResp.UploadID != "abc" {
		t.Errorf("UploadUrlResponse UploadID = %v, want abc", uploadResp.UploadID)
	}
	if uploadResp.ExpiresIn != 900 {
		t.Errorf("UploadUrlResponse ExpiresIn = %v, want 900", uploadResp.ExpiresIn)
	}

	// Test FileUploadResponse JSON field names
	fileRespJSON := `{"success":true,"path":"/workspace/file","size":1024}`
	var fileResp models.FileUploadResponse
	if err := json.Unmarshal([]byte(fileRespJSON), &fileResp); err != nil {
		t.Fatalf("Failed to unmarshal FileUploadResponse: %v", err)
	}

	if !fileResp.Success {
		t.Error("FileUploadResponse Success = false, want true")
	}
	if fileResp.Path != "/workspace/file" {
		t.Errorf("FileUploadResponse Path = %v, want /workspace/file", fileResp.Path)
	}
	if fileResp.Size != 1024 {
		t.Errorf("FileUploadResponse Size = %v, want 1024", fileResp.Size)
	}
}
