/*
Copyright 2025 isola.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGetNetworkTemplateName(t *testing.T) {
	tests := []struct {
		name     string
		sandbox  Sandbox
		expected string
	}{
		{
			name: "nil network config returns default template",
			sandbox: Sandbox{
				ObjectMeta: metav1.ObjectMeta{Name: "test-sandbox"},
				Spec: SandboxSpec{
					Template: TemplateConfig{TemplateRef: &SandboxTemplateReference{Name: "test"}},
					Network:  nil,
				},
			},
			expected: DefaultNetworkTemplate,
		},
		{
			name: "templateRef returns referenced name",
			sandbox: Sandbox{
				ObjectMeta: metav1.ObjectMeta{Name: "test-sandbox"},
				Spec: SandboxSpec{
					Template: TemplateConfig{TemplateRef: &SandboxTemplateReference{Name: "test"}},
					Network: &NetworkConfig{
						TemplateRef: &NetworkTemplateReference{Name: "custom-template"},
					},
				},
			},
			expected: "custom-template",
		},
		{
			name: "embedded spec returns owned template name",
			sandbox: Sandbox{
				ObjectMeta: metav1.ObjectMeta{Name: "my-sandbox"},
				Spec: SandboxSpec{
					Template: TemplateConfig{TemplateRef: &SandboxTemplateReference{Name: "test"}},
					Network: &NetworkConfig{
						Spec: &NetworkTemplateSpec{},
					},
				},
			},
			expected: "my-sandbox-network",
		},
		{
			name: "network config with nil templateRef and nil spec returns owned name",
			sandbox: Sandbox{
				ObjectMeta: metav1.ObjectMeta{Name: "edge-case"},
				Spec: SandboxSpec{
					Template: TemplateConfig{TemplateRef: &SandboxTemplateReference{Name: "test"}},
					Network: &NetworkConfig{
						TemplateRef: nil,
						Spec:        nil,
					},
				},
			},
			expected: "edge-case-network",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.sandbox.GetNetworkTemplateName()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetOwnedNetworkTemplateName(t *testing.T) {
	sandbox := Sandbox{
		ObjectMeta: metav1.ObjectMeta{Name: "my-sandbox"},
	}
	assert.Equal(t, "my-sandbox-network", sandbox.GetOwnedNetworkTemplateName())
}

func TestHasNetworkSpec(t *testing.T) {
	tests := []struct {
		name     string
		sandbox  Sandbox
		expected bool
	}{
		{
			name: "nil network returns false",
			sandbox: Sandbox{
				Spec: SandboxSpec{
					Template: TemplateConfig{TemplateRef: &SandboxTemplateReference{Name: "test"}},
					Network:  nil,
				},
			},
			expected: false,
		},
		{
			name: "network with templateRef only returns false",
			sandbox: Sandbox{
				Spec: SandboxSpec{
					Template: TemplateConfig{TemplateRef: &SandboxTemplateReference{Name: "test"}},
					Network: &NetworkConfig{
						TemplateRef: &NetworkTemplateReference{Name: "template"},
					},
				},
			},
			expected: false,
		},
		{
			name: "network with spec returns true",
			sandbox: Sandbox{
				Spec: SandboxSpec{
					Template: TemplateConfig{TemplateRef: &SandboxTemplateReference{Name: "test"}},
					Network: &NetworkConfig{
						Spec: &NetworkTemplateSpec{},
					},
				},
			},
			expected: true,
		},
		{
			name: "network with empty spec returns true",
			sandbox: Sandbox{
				Spec: SandboxSpec{
					Template: TemplateConfig{TemplateRef: &SandboxTemplateReference{Name: "test"}},
					Network: &NetworkConfig{
						Spec: &NetworkTemplateSpec{
							AllowedEgressCIDRs: []string{},
						},
					},
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.sandbox.HasNetworkSpec()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDefaultNetworkTemplateConstant(t *testing.T) {
	// Verify the constant matches the expected value
	assert.Equal(t, "isola-isolated", DefaultNetworkTemplate)
}

func TestGetTemplateName(t *testing.T) {
	tests := []struct {
		name     string
		sandbox  Sandbox
		expected string
	}{
		{
			name: "templateRef returns referenced name",
			sandbox: Sandbox{
				ObjectMeta: metav1.ObjectMeta{Name: "test-sandbox"},
				Spec: SandboxSpec{
					Template: TemplateConfig{
						TemplateRef: &SandboxTemplateReference{Name: "custom-template"},
					},
				},
			},
			expected: "custom-template",
		},
		{
			name: "embedded spec returns owned template name",
			sandbox: Sandbox{
				ObjectMeta: metav1.ObjectMeta{Name: "my-sandbox"},
				Spec: SandboxSpec{
					Template: TemplateConfig{
						Spec: &SandboxTemplateSpec{},
					},
				},
			},
			expected: "my-sandbox-template",
		},
		{
			name: "nil templateRef and nil spec returns owned name",
			sandbox: Sandbox{
				ObjectMeta: metav1.ObjectMeta{Name: "edge-case"},
				Spec: SandboxSpec{
					Template: TemplateConfig{
						TemplateRef: nil,
						Spec:        nil,
					},
				},
			},
			expected: "edge-case-template",
		},
		{
			name: "templateRef takes precedence when both are set",
			sandbox: Sandbox{
				ObjectMeta: metav1.ObjectMeta{Name: "both-set"},
				Spec: SandboxSpec{
					Template: TemplateConfig{
						TemplateRef: &SandboxTemplateReference{Name: "explicit-template"},
						Spec:        &SandboxTemplateSpec{},
					},
				},
			},
			expected: "explicit-template",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.sandbox.GetTemplateName()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetOwnedTemplateName(t *testing.T) {
	sandbox := Sandbox{
		ObjectMeta: metav1.ObjectMeta{Name: "my-sandbox"},
	}
	assert.Equal(t, "my-sandbox-template", sandbox.GetOwnedTemplateName())
}

func TestHasTemplateSpec(t *testing.T) {
	tests := []struct {
		name     string
		sandbox  Sandbox
		expected bool
	}{
		{
			name: "templateRef only returns false",
			sandbox: Sandbox{
				Spec: SandboxSpec{
					Template: TemplateConfig{
						TemplateRef: &SandboxTemplateReference{Name: "template"},
					},
				},
			},
			expected: false,
		},
		{
			name: "spec returns true",
			sandbox: Sandbox{
				Spec: SandboxSpec{
					Template: TemplateConfig{
						Spec: &SandboxTemplateSpec{},
					},
				},
			},
			expected: true,
		},
		{
			name: "both templateRef and spec returns true",
			sandbox: Sandbox{
				Spec: SandboxSpec{
					Template: TemplateConfig{
						TemplateRef: &SandboxTemplateReference{Name: "template"},
						Spec:        &SandboxTemplateSpec{},
					},
				},
			},
			expected: true,
		},
		{
			name: "neither templateRef nor spec returns false",
			sandbox: Sandbox{
				Spec: SandboxSpec{
					Template: TemplateConfig{},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.sandbox.HasTemplateSpec()
			assert.Equal(t, tt.expected, result)
		})
	}
}
