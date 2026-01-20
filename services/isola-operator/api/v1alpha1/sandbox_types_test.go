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

func TestNeedsCustomNetworkPolicy(t *testing.T) {
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
			name: "empty network spec returns false",
			sandbox: Sandbox{
				Spec: SandboxSpec{
					Network: &NetworkSpec{},
				},
			},
			expected: false,
		},
		{
			name: "allowAllInternet only returns false",
			sandbox: Sandbox{
				Spec: SandboxSpec{
					Network: &NetworkSpec{
						AllowAllInternet: true,
					},
				},
			},
			expected: false,
		},
		{
			name: "allowClusterDNS only returns false",
			sandbox: Sandbox{
				Spec: SandboxSpec{
					Network: &NetworkSpec{
						AllowClusterDNS: true,
					},
				},
			},
			expected: false,
		},
		{
			name: "allowedEgressCIDRs returns true",
			sandbox: Sandbox{
				Spec: SandboxSpec{
					Network: &NetworkSpec{
						AllowedEgressCIDRs: []string{"8.8.8.0/24"},
					},
				},
			},
			expected: true,
		},
		{
			name: "allowedEgressPods returns true",
			sandbox: Sandbox{
				Spec: SandboxSpec{
					Network: &NetworkSpec{
						AllowedEgressPods: []EgressPodRule{
							{Namespace: "kube-system", PodSelector: metav1.LabelSelector{}},
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "nameservers without internet access returns true",
			sandbox: Sandbox{
				Spec: SandboxSpec{
					Network: &NetworkSpec{
						Nameservers:      []string{"8.8.8.8"},
						AllowAllInternet: false,
					},
				},
			},
			expected: true,
		},
		{
			name: "nameservers with internet access returns false",
			sandbox: Sandbox{
				Spec: SandboxSpec{
					Network: &NetworkSpec{
						Nameservers:      []string{"8.8.8.8"},
						AllowAllInternet: true,
					},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.sandbox.NeedsCustomNetworkPolicy()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetCustomNetworkPolicyName(t *testing.T) {
	sandbox := Sandbox{
		ObjectMeta: metav1.ObjectMeta{Name: "my-sandbox"},
	}
	assert.Equal(t, "my-sandbox-custom-netpol", sandbox.GetCustomNetworkPolicyName())
}
