package kubernetes

import "testing"

func TestSandboxResourceName(t *testing.T) {
	tests := []struct {
		name      string
		sandboxID string
		want      string
	}{
		{
			name:      "full UUID",
			sandboxID: "550e8400-e29b-41d4-a716-446655440000",
			want:      "sandbox-550e8400-e29b-41d4-a716-446655440000",
		},
		{
			name:      "short ID",
			sandboxID: "abc123",
			want:      "sandbox-abc123",
		},
		{
			name:      "empty ID",
			sandboxID: "",
			want:      "sandbox-",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sandboxResourceName(tt.sandboxID)
			if got != tt.want {
				t.Errorf("sandboxResourceName(%q) = %q, want %q", tt.sandboxID, got, tt.want)
			}
		})
	}
}

func TestSandboxResourceNameLength(t *testing.T) {
	// Verify that a full UUID results in a name under Kubernetes' 63 char limit
	fullUUID := "550e8400-e29b-41d4-a716-446655440000" // 36 chars
	name := sandboxResourceName(fullUUID)

	if len(name) > 63 {
		t.Errorf("sandboxResourceName with full UUID exceeds 63 char limit: got %d chars (%s)", len(name), name)
	}

	// Expected: "sandbox-" (8) + UUID (36) = 44 chars
	expectedLen := 44
	if len(name) != expectedLen {
		t.Errorf("sandboxResourceName length = %d, want %d", len(name), expectedLen)
	}
}
