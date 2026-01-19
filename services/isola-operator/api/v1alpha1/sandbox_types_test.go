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

func TestHasCustomNetworkRules(t *testing.T) {
	tests := []struct {
		name     string
		sandbox  Sandbox
		expected bool
	}{
		{
			name: "nil network returns false",
			sandbox: Sandbox{
				Spec: SandboxSpec{Network: nil},
			},
			expected: false,
		},
		{
			name: "empty network config returns false",
			sandbox: Sandbox{
				Spec: SandboxSpec{
					Network: &NetworkConfig{},
				},
			},
			expected: false,
		},
		{
			name: "network with only boolean flags returns false",
			sandbox: Sandbox{
				Spec: SandboxSpec{
					Network: &NetworkConfig{
						AllowInternet:   true,
						AllowClusterDNS: true,
					},
				},
			},
			expected: false,
		},
		{
			name: "network with DNS servers returns true",
			sandbox: Sandbox{
				Spec: SandboxSpec{
					Network: &NetworkConfig{
						DNS: []string{"8.8.8.8"},
					},
				},
			},
			expected: true,
		},
		{
			name: "network with CIDR rules returns true",
			sandbox: Sandbox{
				Spec: SandboxSpec{
					Network: &NetworkConfig{
						AllowedCIDRs: []CIDREgressRule{
							{CIDR: "10.0.0.0/8"},
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "network with pod rules returns true",
			sandbox: Sandbox{
				Spec: SandboxSpec{
					Network: &NetworkConfig{
						AllowedPods: []PodEgressRule{
							{Namespace: "kube-system"},
						},
					},
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.sandbox.HasCustomNetworkRules()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetCustomNetworkPolicyName(t *testing.T) {
	sandbox := Sandbox{
		ObjectMeta: metav1.ObjectMeta{Name: "my-sandbox"},
	}
	assert.Equal(t, "my-sandbox-egress", sandbox.GetCustomNetworkPolicyName())
}

func TestGetNetworkLabels(t *testing.T) {
	tests := []struct {
		name     string
		sandbox  Sandbox
		expected map[string]string
	}{
		{
			name: "nil network returns only sandbox label",
			sandbox: Sandbox{
				ObjectMeta: metav1.ObjectMeta{Name: "test-sandbox"},
				Spec:       SandboxSpec{Network: nil},
			},
			expected: map[string]string{
				LabelSandboxName: "test-sandbox",
			},
		},
		{
			name: "empty network config returns only sandbox label",
			sandbox: Sandbox{
				ObjectMeta: metav1.ObjectMeta{Name: "test-sandbox"},
				Spec: SandboxSpec{
					Network: &NetworkConfig{},
				},
			},
			expected: map[string]string{
				LabelSandboxName: "test-sandbox",
			},
		},
		{
			name: "allowInternet=true adds internet label",
			sandbox: Sandbox{
				ObjectMeta: metav1.ObjectMeta{Name: "test-sandbox"},
				Spec: SandboxSpec{
					Network: &NetworkConfig{
						AllowInternet: true,
					},
				},
			},
			expected: map[string]string{
				LabelSandboxName:     "test-sandbox",
				LabelAllowInternet:   "true",
			},
		},
		{
			name: "allowClusterDNS=true adds dns label",
			sandbox: Sandbox{
				ObjectMeta: metav1.ObjectMeta{Name: "test-sandbox"},
				Spec: SandboxSpec{
					Network: &NetworkConfig{
						AllowClusterDNS: true,
					},
				},
			},
			expected: map[string]string{
				LabelSandboxName:     "test-sandbox",
				LabelAllowClusterDNS: "true",
			},
		},
		{
			name: "both flags set adds both labels",
			sandbox: Sandbox{
				ObjectMeta: metav1.ObjectMeta{Name: "test-sandbox"},
				Spec: SandboxSpec{
					Network: &NetworkConfig{
						AllowInternet:   true,
						AllowClusterDNS: true,
					},
				},
			},
			expected: map[string]string{
				LabelSandboxName:     "test-sandbox",
				LabelAllowInternet:   "true",
				LabelAllowClusterDNS: "true",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.sandbox.GetNetworkLabels()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestLabelConstants(t *testing.T) {
	// Verify the label constants have expected values
	assert.Equal(t, "isola.run/allow-internet", LabelAllowInternet)
	assert.Equal(t, "isola.run/allow-cluster-dns", LabelAllowClusterDNS)
	assert.Equal(t, "isola.run/sandbox", LabelSandboxName)
}
