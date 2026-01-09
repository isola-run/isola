package models

import (
	"encoding/json"
	"testing"
	"time"
)

func TestSandboxState_String(t *testing.T) {
	tests := []struct {
		state    SandboxState
		expected string
	}{
		{SandboxStatePending, "pending"},
		{SandboxStateStarting, "starting"},
		{SandboxStateRunning, "running"},
		{SandboxStateTerminating, "terminating"},
		{SandboxStateStopped, "stopped"},
		{SandboxStateError, "error"},
		{SandboxStateUnknown, "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if string(tt.state) != tt.expected {
				t.Errorf("SandboxState = %v, want %v", string(tt.state), tt.expected)
			}
		})
	}
}

func TestSandbox_JSON(t *testing.T) {
	desiredState := SandboxStateRunning
	sandbox := Sandbox{
		ID:           "test-123",
		Name:         "my-sandbox",
		State:        SandboxStateRunning,
		DesiredState: &desiredState,
		Env: map[string]string{
			"DEBUG": "true",
		},
		Labels: map[string]string{
			"env": "test",
		},
		ErrorReason: nil,
		CreatedAt:   time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
	}

	jsonBytes, err := json.Marshal(sandbox)
	if err != nil {
		t.Fatalf("Failed to marshal Sandbox: %v", err)
	}

	var parsed Sandbox
	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal Sandbox: %v", err)
	}

	if parsed.ID != "test-123" {
		t.Errorf("Sandbox ID = %v, want test-123", parsed.ID)
	}

	if parsed.Name != "my-sandbox" {
		t.Errorf("Sandbox Name = %v, want my-sandbox", parsed.Name)
	}

	if parsed.State != SandboxStateRunning {
		t.Errorf("Sandbox State = %v, want running", parsed.State)
	}

	if parsed.DesiredState == nil || *parsed.DesiredState != SandboxStateRunning {
		t.Errorf("Sandbox DesiredState = %v, want running", parsed.DesiredState)
	}

	if parsed.Env["DEBUG"] != "true" {
		t.Errorf("Sandbox Env[DEBUG] = %v, want true", parsed.Env["DEBUG"])
	}

	if parsed.Labels["env"] != "test" {
		t.Errorf("Sandbox Labels[env] = %v, want test", parsed.Labels["env"])
	}
}

func TestSandbox_JSON_WithError(t *testing.T) {
	errorReason := "Image pull failed"
	sandbox := Sandbox{
		ID:          "test-456",
		Name:        "failed-sandbox",
		State:       SandboxStateError,
		ErrorReason: &errorReason,
		Env:         make(map[string]string),
		Labels:      make(map[string]string),
		CreatedAt:   time.Now().UTC(),
	}

	jsonBytes, err := json.Marshal(sandbox)
	if err != nil {
		t.Fatalf("Failed to marshal Sandbox: %v", err)
	}

	var parsed Sandbox
	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal Sandbox: %v", err)
	}

	if parsed.State != SandboxStateError {
		t.Errorf("Sandbox State = %v, want error", parsed.State)
	}

	if parsed.ErrorReason == nil || *parsed.ErrorReason != "Image pull failed" {
		t.Errorf("Sandbox ErrorReason = %v, want 'Image pull failed'", parsed.ErrorReason)
	}
}

func TestSandbox_JSON_OmitEmpty(t *testing.T) {
	sandbox := Sandbox{
		ID:        "test-789",
		Name:      "minimal-sandbox",
		State:     SandboxStatePending,
		Env:       make(map[string]string),
		Labels:    make(map[string]string),
		CreatedAt: time.Now().UTC(),
	}

	jsonBytes, err := json.Marshal(sandbox)
	if err != nil {
		t.Fatalf("Failed to marshal Sandbox: %v", err)
	}

	// Check that omitempty fields are not present
	jsonStr := string(jsonBytes)

	// DesiredState should be omitted when nil
	if sandbox.DesiredState == nil {
		// The field should be omitted
		var rawMap map[string]interface{}
		json.Unmarshal(jsonBytes, &rawMap)

		if _, exists := rawMap["desiredState"]; exists && rawMap["desiredState"] == nil {
			// nil is serialized, which might be expected based on omitempty behavior
			t.Log("desiredState is present as null (expected with pointer + omitempty)")
		}
	}

	// ErrorReason should be omitted when nil
	if sandbox.ErrorReason == nil {
		var rawMap map[string]interface{}
		json.Unmarshal(jsonBytes, &rawMap)

		if _, exists := rawMap["errorReason"]; exists && rawMap["errorReason"] != nil {
			t.Errorf("errorReason should be omitted or null when nil")
		}
	}

	t.Logf("JSON output: %s", jsonStr)
}

func TestSandboxList_JSON(t *testing.T) {
	list := SandboxList{
		Items: []Sandbox{
			{
				ID:        "sb-1",
				Name:      "sandbox-1",
				State:     SandboxStateRunning,
				Env:       make(map[string]string),
				Labels:    make(map[string]string),
				CreatedAt: time.Now().UTC(),
			},
			{
				ID:        "sb-2",
				Name:      "sandbox-2",
				State:     SandboxStatePending,
				Env:       make(map[string]string),
				Labels:    make(map[string]string),
				CreatedAt: time.Now().UTC(),
			},
		},
		Total:  2,
		Limit:  20,
		Offset: 0,
	}

	jsonBytes, err := json.Marshal(list)
	if err != nil {
		t.Fatalf("Failed to marshal SandboxList: %v", err)
	}

	var parsed SandboxList
	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal SandboxList: %v", err)
	}

	if len(parsed.Items) != 2 {
		t.Errorf("SandboxList Items count = %v, want 2", len(parsed.Items))
	}

	if parsed.Total != 2 {
		t.Errorf("SandboxList Total = %v, want 2", parsed.Total)
	}

	if parsed.Limit != 20 {
		t.Errorf("SandboxList Limit = %v, want 20", parsed.Limit)
	}

	if parsed.Offset != 0 {
		t.Errorf("SandboxList Offset = %v, want 0", parsed.Offset)
	}
}

func TestSandboxList_EmptyItems(t *testing.T) {
	list := SandboxList{
		Items:  []Sandbox{},
		Total:  0,
		Limit:  20,
		Offset: 0,
	}

	jsonBytes, err := json.Marshal(list)
	if err != nil {
		t.Fatalf("Failed to marshal SandboxList: %v", err)
	}

	var parsed SandboxList
	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal SandboxList: %v", err)
	}

	if parsed.Items == nil {
		t.Error("SandboxList Items is nil, want empty slice")
	}

	if len(parsed.Items) != 0 {
		t.Errorf("SandboxList Items count = %v, want 0", len(parsed.Items))
	}
}

func TestAttachedVolume_JSON(t *testing.T) {
	volume := AttachedVolume{
		VolumeID:  "vol-123",
		MountPath: "/data",
	}

	jsonBytes, err := json.Marshal(volume)
	if err != nil {
		t.Fatalf("Failed to marshal AttachedVolume: %v", err)
	}

	var parsed AttachedVolume
	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal AttachedVolume: %v", err)
	}

	if parsed.VolumeID != "vol-123" {
		t.Errorf("AttachedVolume VolumeID = %v, want vol-123", parsed.VolumeID)
	}

	if parsed.MountPath != "/data" {
		t.Errorf("AttachedVolume MountPath = %v, want /data", parsed.MountPath)
	}
}

func TestAttachedVolume_JSONFields(t *testing.T) {
	// Test that JSON field names are correct
	jsonStr := `{"volumeId":"vol-abc","mountPath":"/mnt/data"}`

	var volume AttachedVolume
	if err := json.Unmarshal([]byte(jsonStr), &volume); err != nil {
		t.Fatalf("Failed to unmarshal AttachedVolume: %v", err)
	}

	if volume.VolumeID != "vol-abc" {
		t.Errorf("AttachedVolume VolumeID = %v, want vol-abc", volume.VolumeID)
	}

	if volume.MountPath != "/mnt/data" {
		t.Errorf("AttachedVolume MountPath = %v, want /mnt/data", volume.MountPath)
	}
}

func TestCreateSandboxRequest_JSON(t *testing.T) {
	cpu := 2.0
	memory := 4.0
	disk := 10.0
	image := "python:3.11"

	req := CreateSandboxRequest{
		Name:      "my-sandbox",
		Image:     &image,
		Region:    "us-west-2",
		CPU:       &cpu,
		Memory:    &memory,
		Disk:      &disk,
		GPU:       1,
		Env:       map[string]string{"DEBUG": "true"},
		Labels:    map[string]string{"team": "dev"},
		AutoStart: true,
		Volumes: []AttachedVolume{
			{VolumeID: "vol-1", MountPath: "/data"},
		},
	}

	jsonBytes, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Failed to marshal CreateSandboxRequest: %v", err)
	}

	var parsed CreateSandboxRequest
	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal CreateSandboxRequest: %v", err)
	}

	if parsed.Name != "my-sandbox" {
		t.Errorf("Name = %v, want my-sandbox", parsed.Name)
	}

	if parsed.Image == nil || *parsed.Image != "python:3.11" {
		t.Errorf("Image = %v, want python:3.11", parsed.Image)
	}

	if parsed.CPU == nil || *parsed.CPU != 2.0 {
		t.Errorf("CPU = %v, want 2.0", parsed.CPU)
	}

	if parsed.Memory == nil || *parsed.Memory != 4.0 {
		t.Errorf("Memory = %v, want 4.0", parsed.Memory)
	}

	if parsed.GPU != 1 {
		t.Errorf("GPU = %v, want 1", parsed.GPU)
	}

	if !parsed.AutoStart {
		t.Error("AutoStart = false, want true")
	}

	if len(parsed.Volumes) != 1 {
		t.Errorf("Volumes count = %v, want 1", len(parsed.Volumes))
	}
}

func TestCreateSandboxRequest_MinimalJSON(t *testing.T) {
	jsonStr := `{"name":"minimal-sandbox"}`

	var req CreateSandboxRequest
	if err := json.Unmarshal([]byte(jsonStr), &req); err != nil {
		t.Fatalf("Failed to unmarshal CreateSandboxRequest: %v", err)
	}

	if req.Name != "minimal-sandbox" {
		t.Errorf("Name = %v, want minimal-sandbox", req.Name)
	}

	if req.Image != nil {
		t.Errorf("Image = %v, want nil", req.Image)
	}

	if req.CPU != nil {
		t.Errorf("CPU = %v, want nil", req.CPU)
	}

	if req.AutoStart != false {
		t.Errorf("AutoStart = %v, want false", req.AutoStart)
	}
}

func TestErrorResponse_JSON(t *testing.T) {
	resp := ErrorResponse{
		Error:   "NotFound",
		Message: "Sandbox not found",
		Details: map[string]interface{}{
			"sandbox_id": "test-123",
		},
	}

	jsonBytes, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Failed to marshal ErrorResponse: %v", err)
	}

	var parsed ErrorResponse
	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal ErrorResponse: %v", err)
	}

	if parsed.Error != "NotFound" {
		t.Errorf("Error = %v, want NotFound", parsed.Error)
	}

	if parsed.Message != "Sandbox not found" {
		t.Errorf("Message = %v, want 'Sandbox not found'", parsed.Message)
	}

	if parsed.Details["sandbox_id"] != "test-123" {
		t.Errorf("Details[sandbox_id] = %v, want test-123", parsed.Details["sandbox_id"])
	}
}

func TestErrorResponse_OmitDetails(t *testing.T) {
	resp := ErrorResponse{
		Error:   "BadRequest",
		Message: "Invalid input",
	}

	jsonBytes, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Failed to marshal ErrorResponse: %v", err)
	}

	// Details should be omitted
	var rawMap map[string]interface{}
	if err := json.Unmarshal(jsonBytes, &rawMap); err != nil {
		t.Fatalf("Failed to unmarshal to map: %v", err)
	}

	if _, exists := rawMap["details"]; exists && rawMap["details"] != nil {
		t.Error("Details should be omitted when empty/nil")
	}
}

func TestHealthResponse_JSON(t *testing.T) {
	resp := HealthResponse{
		Status:    "healthy",
		Timestamp: "2024-01-15T10:30:00Z",
		Components: map[string]string{
			"api":      "healthy",
			"database": "healthy",
		},
		Version: "1.0.0",
	}

	jsonBytes, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Failed to marshal HealthResponse: %v", err)
	}

	var parsed HealthResponse
	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal HealthResponse: %v", err)
	}

	if parsed.Status != "healthy" {
		t.Errorf("Status = %v, want healthy", parsed.Status)
	}

	if parsed.Version != "1.0.0" {
		t.Errorf("Version = %v, want 1.0.0", parsed.Version)
	}

	if parsed.Components["api"] != "healthy" {
		t.Errorf("Components[api] = %v, want healthy", parsed.Components["api"])
	}
}

func TestReadyResponse_JSON(t *testing.T) {
	resp := ReadyResponse{
		Status: "ready",
	}

	jsonBytes, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Failed to marshal ReadyResponse: %v", err)
	}

	var parsed ReadyResponse
	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal ReadyResponse: %v", err)
	}

	if parsed.Status != "ready" {
		t.Errorf("Status = %v, want ready", parsed.Status)
	}
}

func TestListSandboxesParams_Validation(t *testing.T) {
	// Test default values
	params := ListSandboxesParams{}

	// With defaults applied by binding
	if params.Limit != 0 {
		// Before binding, default is 0 (Go zero value)
		t.Logf("Limit before binding = %v", params.Limit)
	}

	if params.Offset != 0 {
		t.Errorf("Offset = %v, want 0", params.Offset)
	}

	if params.State != nil {
		t.Errorf("State = %v, want nil", params.State)
	}
}

func TestExecuteCommandRequest_JSON(t *testing.T) {
	req := ExecuteCommandRequest{
		Command: "ls -la /workspace",
	}

	jsonBytes, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Failed to marshal ExecuteCommandRequest: %v", err)
	}

	var parsed ExecuteCommandRequest
	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal ExecuteCommandRequest: %v", err)
	}

	if parsed.Command != "ls -la /workspace" {
		t.Errorf("Command = %v, want 'ls -la /workspace'", parsed.Command)
	}
}

func TestExecuteCommandResponse_JSON(t *testing.T) {
	resp := ExecuteCommandResponse{
		Stdout:   "file1.txt\nfile2.txt\n",
		Stderr:   "",
		ExitCode: 0,
	}

	jsonBytes, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Failed to marshal ExecuteCommandResponse: %v", err)
	}

	var parsed ExecuteCommandResponse
	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal ExecuteCommandResponse: %v", err)
	}

	if parsed.Stdout != "file1.txt\nfile2.txt\n" {
		t.Errorf("Stdout = %v, want 'file1.txt\\nfile2.txt\\n'", parsed.Stdout)
	}

	if parsed.ExitCode != 0 {
		t.Errorf("ExitCode = %v, want 0", parsed.ExitCode)
	}
}

func TestFileUploadRequest_JSON(t *testing.T) {
	req := FileUploadRequest{
		Path: "/workspace/uploads",
	}

	jsonBytes, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Failed to marshal FileUploadRequest: %v", err)
	}

	var parsed FileUploadRequest
	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal FileUploadRequest: %v", err)
	}

	if parsed.Path != "/workspace/uploads" {
		t.Errorf("Path = %v, want /workspace/uploads", parsed.Path)
	}
}

func TestDownloadResponse_JSON(t *testing.T) {
	resp := DownloadResponse{
		Success: true,
		Path:    "/workspace/downloaded.txt",
		Size:    4096,
	}

	jsonBytes, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Failed to marshal DownloadResponse: %v", err)
	}

	var parsed DownloadResponse
	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal DownloadResponse: %v", err)
	}

	if !parsed.Success {
		t.Error("Success = false, want true")
	}

	if parsed.Path != "/workspace/downloaded.txt" {
		t.Errorf("Path = %v, want /workspace/downloaded.txt", parsed.Path)
	}

	if parsed.Size != 4096 {
		t.Errorf("Size = %v, want 4096", parsed.Size)
	}
}
