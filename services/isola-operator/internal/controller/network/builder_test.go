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

package network

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	sandboxv1alpha1 "github.com/isola-ai/isola-sb/services/isola-operator/api/v1alpha1"
)

func TestBuildCustomNetworkPolicy_NilNetwork(t *testing.T) {
	np, err := BuildCustomNetworkPolicy("test-sandbox", "default", nil)
	require.NoError(t, err)
	assert.Nil(t, np)
}

func TestBuildCustomNetworkPolicy_EmptyNetwork(t *testing.T) {
	network := &sandboxv1alpha1.NetworkSpec{}

	np, err := BuildCustomNetworkPolicy("test-sandbox", "default", network)
	require.NoError(t, err)
	assert.Nil(t, np, "empty network should not create policy")
}

func TestBuildCustomNetworkPolicy_WithAllowedEgressCIDRs(t *testing.T) {
	network := &sandboxv1alpha1.NetworkSpec{
		AllowedEgressCIDRs: []string{"8.8.8.0/24"},
	}

	np, err := BuildCustomNetworkPolicy("test-sandbox", "default", network)
	require.NoError(t, err)
	require.NotNil(t, np)

	assert.Equal(t, "test-sandbox-custom-netpol", np.Name)
	assert.Equal(t, "default", np.Namespace)
	assert.Equal(t, "test-sandbox", np.Labels[SandboxIDLabelKey])
	assert.Equal(t, "test-sandbox", np.Spec.PodSelector.MatchLabels[SandboxIDLabelKey])
	assert.Contains(t, np.Spec.PolicyTypes, networkingv1.PolicyTypeEgress)

	require.Len(t, np.Spec.Egress, 1)
	assert.Equal(t, "8.8.8.0/24", np.Spec.Egress[0].To[0].IPBlock.CIDR)
}

func TestBuildCustomNetworkPolicy_BlocksRiskyCIDRs(t *testing.T) {
	network := &sandboxv1alpha1.NetworkSpec{
		AllowedEgressCIDRs: []string{"0.0.0.0/0"},
	}

	np, err := BuildCustomNetworkPolicy("test-sandbox", "default", network)
	require.NoError(t, err)
	require.NotNil(t, np)

	require.Len(t, np.Spec.Egress, 1)
	egressRule := np.Spec.Egress[0]
	require.Len(t, egressRule.To, 1)

	ipBlock := egressRule.To[0].IPBlock
	assert.Equal(t, "0.0.0.0/0", ipBlock.CIDR)

	// Should block all private/internal CIDRs
	assert.Contains(t, ipBlock.Except, "10.0.0.0/8")
	assert.Contains(t, ipBlock.Except, "172.16.0.0/12")
	assert.Contains(t, ipBlock.Except, "192.168.0.0/16")
	assert.Contains(t, ipBlock.Except, "169.254.0.0/16")
}

func TestBuildCustomNetworkPolicy_DoesNotBlockNonOverlappingCIDRs(t *testing.T) {
	network := &sandboxv1alpha1.NetworkSpec{
		AllowedEgressCIDRs: []string{"8.8.0.0/16"},
	}

	np, err := BuildCustomNetworkPolicy("test-sandbox", "default", network)
	require.NoError(t, err)
	require.NotNil(t, np)

	require.Len(t, np.Spec.Egress, 1)
	ipBlock := np.Spec.Egress[0].To[0].IPBlock
	assert.Equal(t, "8.8.0.0/16", ipBlock.CIDR)
	assert.Empty(t, ipBlock.Except)
}

func TestBuildCustomNetworkPolicy_WithNameservers(t *testing.T) {
	network := &sandboxv1alpha1.NetworkSpec{
		Nameservers: []string{"8.8.8.8", "1.1.1.1"},
	}

	np, err := BuildCustomNetworkPolicy("test-sandbox", "default", network)
	require.NoError(t, err)
	require.NotNil(t, np)

	require.Len(t, np.Spec.Egress, 1)
	dnsRule := np.Spec.Egress[0]

	require.Len(t, dnsRule.To, 2)
	cidrs := []string{dnsRule.To[0].IPBlock.CIDR, dnsRule.To[1].IPBlock.CIDR}
	assert.Contains(t, cidrs, "8.8.8.8/32")
	assert.Contains(t, cidrs, "1.1.1.1/32")

	require.Len(t, dnsRule.Ports, 2)
}

func TestBuildCustomNetworkPolicy_NameserversWithInternetAccess(t *testing.T) {
	network := &sandboxv1alpha1.NetworkSpec{
		AllowAllInternet: true,
		Nameservers:      []string{"8.8.8.8"},
	}

	np, err := BuildCustomNetworkPolicy("test-sandbox", "default", network)
	require.NoError(t, err)
	// Custom policy created even with internet access - nameservers may be private IPs
	// that fall within blocked ranges
	require.NotNil(t, np)
	require.Len(t, np.Spec.Egress, 1)
	require.Len(t, np.Spec.Egress[0].Ports, 2)
	protocols := []corev1.Protocol{*np.Spec.Egress[0].Ports[0].Protocol, *np.Spec.Egress[0].Ports[1].Protocol}
	assert.Contains(t, protocols, corev1.ProtocolUDP)
	assert.Contains(t, protocols, corev1.ProtocolTCP)
	assert.Equal(t, int32(53), np.Spec.Egress[0].Ports[0].Port.IntVal)
	assert.Equal(t, int32(53), np.Spec.Egress[0].Ports[1].Port.IntVal)
}

func TestBuildCustomNetworkPolicy_InvalidNameserver(t *testing.T) {
	network := &sandboxv1alpha1.NetworkSpec{
		Nameservers: []string{"not-an-ip"},
	}

	_, err := BuildCustomNetworkPolicy("test-sandbox", "default", network)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid DNS server IP")
}

func TestBuildCustomNetworkPolicy_InvalidEgressCIDR(t *testing.T) {
	network := &sandboxv1alpha1.NetworkSpec{
		AllowedEgressCIDRs: []string{"also-invalid"},
	}

	_, err := BuildCustomNetworkPolicy("test-sandbox", "default", network)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid CIDR")
}

func TestBuildCustomNetworkPolicy_BlockedEgressCIDR(t *testing.T) {
	tests := []struct {
		name          string
		cidr          string
		errorContains string
	}{
		{
			name:          "CIDR inside blocked range",
			cidr:          "10.1.2.0/24",
			errorContains: "inside blocked range",
		},
		{
			name:          "CIDR equals blocked range",
			cidr:          "10.0.0.0/8",
			errorContains: "equals blocked range",
		},
		{
			name:          "cloud metadata range",
			cidr:          "169.254.169.254/32",
			errorContains: "inside blocked range",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			network := &sandboxv1alpha1.NetworkSpec{
				AllowedEgressCIDRs: []string{tt.cidr},
			}

			_, err := BuildCustomNetworkPolicy("test-sandbox", "default", network)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errorContains)
		})
	}
}

func TestBuildCustomNetworkPolicy_WithEgressPods(t *testing.T) {
	network := &sandboxv1alpha1.NetworkSpec{
		AllowedEgressPods: []sandboxv1alpha1.EgressPodRule{
			{
				Namespace: "kube-system",
				PodSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{
						"k8s-app": "kube-dns",
					},
				},
				Ports: []sandboxv1alpha1.NetworkPort{
					{Protocol: corev1.ProtocolUDP, Port: 53},
				},
			},
		},
	}

	np, err := BuildCustomNetworkPolicy("test-sandbox", "default", network)
	require.NoError(t, err)
	require.NotNil(t, np)

	require.Len(t, np.Spec.Egress, 1)
	egressRule := np.Spec.Egress[0]

	require.Len(t, egressRule.To, 1)
	peer := egressRule.To[0]

	require.NotNil(t, peer.NamespaceSelector)
	assert.Equal(t, "kube-system", peer.NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"])

	require.NotNil(t, peer.PodSelector)
	assert.Equal(t, "kube-dns", peer.PodSelector.MatchLabels["k8s-app"])

	require.Len(t, egressRule.Ports, 1)
	assert.Equal(t, int32(53), egressRule.Ports[0].Port.IntVal)
}

func TestBuildCustomNetworkPolicy_CombinedRules(t *testing.T) {
	network := &sandboxv1alpha1.NetworkSpec{
		Nameservers:        []string{"8.8.8.8"},
		AllowedEgressCIDRs: []string{"1.1.1.0/24"},
		AllowedEgressPods: []sandboxv1alpha1.EgressPodRule{
			{
				Namespace:   "my-namespace",
				PodSelector: metav1.LabelSelector{},
			},
		},
	}

	np, err := BuildCustomNetworkPolicy("test-sandbox", "default", network)
	require.NoError(t, err)
	require.NotNil(t, np)

	// Should have 3 egress rules: DNS IP + CIDR + pod selector
	require.Len(t, np.Spec.Egress, 3)
}

func TestBuildCustomNetworkPolicy_DeduplicatesCIDRs(t *testing.T) {
	network := &sandboxv1alpha1.NetworkSpec{
		AllowedEgressCIDRs: []string{"8.8.8.0/24", "8.8.8.0/24", "1.1.1.0/24"},
	}

	np, err := BuildCustomNetworkPolicy("test-sandbox", "default", network)
	require.NoError(t, err)
	require.NotNil(t, np)

	// Should have 2 egress rules (deduplicated)
	require.Len(t, np.Spec.Egress, 2)
}
