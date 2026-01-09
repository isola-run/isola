package kubernetes

import (
	"testing"

	"github.com/omereli/dev-isola/services/isola-gw/internal/models"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestGetImage(t *testing.T) {
	tests := []struct {
		name     string
		input    *string
		expected string
	}{
		{
			name:     "nil input returns default",
			input:    nil,
			expected: "python:3.11",
		},
		{
			name:     "empty string returns default",
			input:    strPtr(""),
			expected: "python:3.11",
		},
		{
			name:     "custom image is returned",
			input:    strPtr("ubuntu:22.04"),
			expected: "ubuntu:22.04",
		},
		{
			name:     "custom image with tag",
			input:    strPtr("node:18-alpine"),
			expected: "node:18-alpine",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getImage(tt.input)
			if result != tt.expected {
				t.Errorf("getImage() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGetCPU(t *testing.T) {
	tests := []struct {
		name     string
		input    *float64
		expected float64
	}{
		{
			name:     "nil input returns default",
			input:    nil,
			expected: 1.0,
		},
		{
			name:     "custom CPU is returned",
			input:    float64Ptr(2.5),
			expected: 2.5,
		},
		{
			name:     "zero CPU is returned",
			input:    float64Ptr(0),
			expected: 0,
		},
		{
			name:     "fractional CPU is returned",
			input:    float64Ptr(0.5),
			expected: 0.5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getCPU(tt.input)
			if result != tt.expected {
				t.Errorf("getCPU() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGetMemory(t *testing.T) {
	tests := []struct {
		name     string
		input    *float64
		expected float64
	}{
		{
			name:     "nil input returns default",
			input:    nil,
			expected: 1.0,
		},
		{
			name:     "custom memory is returned",
			input:    float64Ptr(4.0),
			expected: 4.0,
		},
		{
			name:     "zero memory is returned",
			input:    float64Ptr(0),
			expected: 0,
		},
		{
			name:     "fractional memory is returned",
			input:    float64Ptr(0.25),
			expected: 0.25,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getMemory(tt.input)
			if result != tt.expected {
				t.Errorf("getMemory() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestEnvToK8sEnv(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]string
		expected []map[string]interface{}
	}{
		{
			name:     "nil map returns empty slice",
			input:    nil,
			expected: []map[string]interface{}{},
		},
		{
			name:     "empty map returns empty slice",
			input:    map[string]string{},
			expected: []map[string]interface{}{},
		},
		{
			name: "single env var",
			input: map[string]string{
				"FOO": "bar",
			},
			expected: []map[string]interface{}{
				{"name": "FOO", "value": "bar"},
			},
		},
		{
			name: "multiple env vars",
			input: map[string]string{
				"FOO":   "bar",
				"DEBUG": "true",
			},
			expected: []map[string]interface{}{
				{"name": "FOO", "value": "bar"},
				{"name": "DEBUG", "value": "true"},
			},
		},
		{
			name: "env var with empty value",
			input: map[string]string{
				"EMPTY": "",
			},
			expected: []map[string]interface{}{
				{"name": "EMPTY", "value": ""},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := envToK8sEnv(tt.input)

			if len(result) != len(tt.expected) {
				t.Errorf("envToK8sEnv() returned %d items, want %d", len(result), len(tt.expected))
				return
			}

			// Convert result to map for easier comparison (order is not guaranteed)
			resultMap := make(map[string]string)
			for _, item := range result {
				name, _ := item["name"].(string)
				value, _ := item["value"].(string)
				resultMap[name] = value
			}

			expectedMap := make(map[string]string)
			for _, item := range tt.expected {
				name, _ := item["name"].(string)
				value, _ := item["value"].(string)
				expectedMap[name] = value
			}

			for k, v := range expectedMap {
				if resultMap[k] != v {
					t.Errorf("envToK8sEnv() key %s = %v, want %v", k, resultMap[k], v)
				}
			}
		})
	}
}

func TestGetAgentAddress(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		sandboxID string
		expected  string
	}{
		{
			name:      "standard sandbox ID",
			namespace: "isola-sandboxes",
			sandboxID: "abc12345-6789-0123-4567-890abcdef012",
			expected:  "sandbox-abc12345-pod.sandbox-agents.isola-sandboxes.svc.cluster.local",
		},
		{
			name:      "short sandbox ID",
			namespace: "isola-sandboxes",
			sandboxID: "short",
			expected:  "sandbox-short-pod.sandbox-agents.isola-sandboxes.svc.cluster.local",
		},
		{
			name:      "exactly 8 char sandbox ID",
			namespace: "test-ns",
			sandboxID: "12345678",
			expected:  "sandbox-12345678-pod.sandbox-agents.test-ns.svc.cluster.local",
		},
		{
			name:      "different namespace",
			namespace: "production",
			sandboxID: "abc12345-rest-of-id",
			expected:  "sandbox-abc12345-pod.sandbox-agents.production.svc.cluster.local",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewManager(tt.namespace)
			result := m.getAgentAddress(tt.sandboxID)
			if result != tt.expected {
				t.Errorf("getAgentAddress() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestParseStateFromConditions(t *testing.T) {
	tests := []struct {
		name            string
		sandbox         *unstructured.Unstructured
		expectedState   models.SandboxState
		expectError     bool
	}{
		{
			name: "no status returns pending",
			sandbox: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"name": "sandbox-test",
					},
				},
			},
			expectedState: models.SandboxStatePending,
			expectError:   false,
		},
		{
			name: "no conditions returns pending",
			sandbox: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"name": "sandbox-test",
					},
					"status": map[string]interface{}{},
				},
			},
			expectedState: models.SandboxStatePending,
			expectError:   false,
		},
		{
			name: "Ready=True returns running",
			sandbox: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"name": "sandbox-test",
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
			expectedState: models.SandboxStateRunning,
			expectError:   false,
		},
		{
			name: "Ready=False with PodPending reason returns pending",
			sandbox: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"name": "sandbox-test",
					},
					"status": map[string]interface{}{
						"conditions": []interface{}{
							map[string]interface{}{
								"type":   "Ready",
								"status": "False",
								"reason": "PodPending",
							},
						},
					},
				},
			},
			expectedState: models.SandboxStatePending,
			expectError:   false,
		},
		{
			name: "Ready=False with PodCreating reason returns pending",
			sandbox: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"name": "sandbox-test",
					},
					"status": map[string]interface{}{
						"conditions": []interface{}{
							map[string]interface{}{
								"type":   "Ready",
								"status": "False",
								"reason": "PodCreating",
							},
						},
					},
				},
			},
			expectedState: models.SandboxStatePending,
			expectError:   false,
		},
		{
			name: "Ready=False with Reconciling reason returns pending",
			sandbox: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"name": "sandbox-test",
					},
					"status": map[string]interface{}{
						"conditions": []interface{}{
							map[string]interface{}{
								"type":   "Ready",
								"status": "False",
								"reason": "Reconciling",
							},
						},
					},
				},
			},
			expectedState: models.SandboxStatePending,
			expectError:   false,
		},
		{
			name: "Ready=False with PodFailed reason returns error",
			sandbox: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"name": "sandbox-test",
					},
					"status": map[string]interface{}{
						"conditions": []interface{}{
							map[string]interface{}{
								"type":    "Ready",
								"status":  "False",
								"reason":  "PodFailed",
								"message": "Container crashed",
							},
						},
					},
				},
			},
			expectedState: models.SandboxStateError,
			expectError:   true,
		},
		{
			name: "Ready=False with PodCreationFailed reason returns error",
			sandbox: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"name": "sandbox-test",
					},
					"status": map[string]interface{}{
						"conditions": []interface{}{
							map[string]interface{}{
								"type":    "Ready",
								"status":  "False",
								"reason":  "PodCreationFailed",
								"message": "Image pull failed",
							},
						},
					},
				},
			},
			expectedState: models.SandboxStateError,
			expectError:   true,
		},
		{
			name: "Ready=False with PodSucceeded reason returns stopped",
			sandbox: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"name": "sandbox-test",
					},
					"status": map[string]interface{}{
						"conditions": []interface{}{
							map[string]interface{}{
								"type":   "Ready",
								"status": "False",
								"reason": "PodSucceeded",
							},
						},
					},
				},
			},
			expectedState: models.SandboxStateStopped,
			expectError:   false,
		},
		{
			name: "Ready=False with TimedOut reason returns stopped",
			sandbox: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"name": "sandbox-test",
					},
					"status": map[string]interface{}{
						"conditions": []interface{}{
							map[string]interface{}{
								"type":   "Ready",
								"status": "False",
								"reason": "TimedOut",
							},
						},
					},
				},
			},
			expectedState: models.SandboxStateStopped,
			expectError:   false,
		},
		{
			name: "Ready=False with Deleting reason returns stopped",
			sandbox: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"name": "sandbox-test",
					},
					"status": map[string]interface{}{
						"conditions": []interface{}{
							map[string]interface{}{
								"type":   "Ready",
								"status": "False",
								"reason": "Deleting",
							},
						},
					},
				},
			},
			expectedState: models.SandboxStateStopped,
			expectError:   false,
		},
		{
			name: "Ready=False with NetworkConfigNotApplied returns pending",
			sandbox: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"name": "sandbox-test",
					},
					"status": map[string]interface{}{
						"conditions": []interface{}{
							map[string]interface{}{
								"type":   "Ready",
								"status": "False",
								"reason": "NetworkConfigNotApplied",
							},
						},
					},
				},
			},
			expectedState: models.SandboxStatePending,
			expectError:   false,
		},
		{
			name: "Ready=False with NetworkTemplateNotFound returns pending",
			sandbox: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"name": "sandbox-test",
					},
					"status": map[string]interface{}{
						"conditions": []interface{}{
							map[string]interface{}{
								"type":   "Ready",
								"status": "False",
								"reason": "NetworkTemplateNotFound",
							},
						},
					},
				},
			},
			expectedState: models.SandboxStatePending,
			expectError:   false,
		},
		{
			name: "Ready=False with NetworkTemplateDeleting returns pending",
			sandbox: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"name": "sandbox-test",
					},
					"status": map[string]interface{}{
						"conditions": []interface{}{
							map[string]interface{}{
								"type":   "Ready",
								"status": "False",
								"reason": "NetworkTemplateDeleting",
							},
						},
					},
				},
			},
			expectedState: models.SandboxStatePending,
			expectError:   false,
		},
		{
			name: "Ready=False with unknown reason returns pending",
			sandbox: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"name": "sandbox-test",
					},
					"status": map[string]interface{}{
						"conditions": []interface{}{
							map[string]interface{}{
								"type":   "Ready",
								"status": "False",
								"reason": "SomeUnknownReason",
							},
						},
					},
				},
			},
			expectedState: models.SandboxStatePending,
			expectError:   false,
		},
		{
			name: "network not ready but pod ready returns pending",
			sandbox: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"name": "sandbox-test",
					},
					"status": map[string]interface{}{
						"conditions": []interface{}{
							map[string]interface{}{
								"type":   "Ready",
								"status": "False",
								"reason": "NetworkConfigNotApplied",
							},
							map[string]interface{}{
								"type":   "PodReady",
								"status": "True",
							},
						},
					},
				},
			},
			expectedState: models.SandboxStatePending,
			expectError:   false,
		},
		{
			name: "multiple conditions with Ready=True",
			sandbox: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"name": "sandbox-test",
					},
					"status": map[string]interface{}{
						"conditions": []interface{}{
							map[string]interface{}{
								"type":   "PodReady",
								"status": "True",
							},
							map[string]interface{}{
								"type":   "Ready",
								"status": "True",
							},
						},
					},
				},
			},
			expectedState: models.SandboxStateRunning,
			expectError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewManager("test-namespace")
			state, errorReason := m.parseStateFromConditions(tt.sandbox)

			if state != tt.expectedState {
				t.Errorf("parseStateFromConditions() state = %v, want %v", state, tt.expectedState)
			}

			if tt.expectError && errorReason == nil {
				t.Error("parseStateFromConditions() expected error reason but got nil")
			}

			if !tt.expectError && errorReason != nil {
				t.Errorf("parseStateFromConditions() got unexpected error reason: %v", *errorReason)
			}
		})
	}
}

func TestNewManager(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
	}{
		{
			name:      "creates manager with namespace",
			namespace: "isola-sandboxes",
		},
		{
			name:      "creates manager with different namespace",
			namespace: "production",
		},
		{
			name:      "creates manager with empty namespace",
			namespace: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewManager(tt.namespace)
			if m == nil {
				t.Fatal("NewManager() returned nil")
			}
			if m.namespace != tt.namespace {
				t.Errorf("NewManager() namespace = %v, want %v", m.namespace, tt.namespace)
			}
		})
	}
}

// Helper functions for creating pointers
func strPtr(s string) *string {
	return &s
}

func float64Ptr(f float64) *float64 {
	return &f
}
