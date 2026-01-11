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

	sandboxv1alpha1 "github.com/omereli/dev-isola/services/isola-operator/api/v1alpha1"
)

func TestGetNetworkPolicyName(t *testing.T) {
	tests := []struct {
		name         string
		templateName string
		expected     string
	}{
		{
			name:         "simple name",
			templateName: "isola-isolated",
			expected:     "isola-isolated-netpol",
		},
		{
			name:         "custom template",
			templateName: "my-custom-template",
			expected:     "my-custom-template-netpol",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetNetworkPolicyName(tt.templateName)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBuildNetworkPolicy_Basic(t *testing.T) {
	template := &sandboxv1alpha1.NetworkTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-template",
			Namespace: "default",
		},
		Spec: sandboxv1alpha1.NetworkTemplateSpec{},
	}

	np, err := BuildNetworkPolicy(template)
	require.NoError(t, err)

	assert.Equal(t, "test-template-netpol", np.Name)
	assert.Equal(t, "default", np.Namespace)
	assert.Equal(t, "test-template", np.Labels[NetworkTemplateLabelKey])
	assert.Equal(t, "test-template", np.Spec.PodSelector.MatchLabels[NetworkTemplateLabelKey])
	assert.Contains(t, np.Spec.PolicyTypes, networkingv1.PolicyTypeIngress)
	assert.Contains(t, np.Spec.PolicyTypes, networkingv1.PolicyTypeEgress)
}

func TestBuildNetworkPolicy_IngressIsEmpty(t *testing.T) {
	// Ingress from isola-gw is now handled by a separate Helm-installed NetworkPolicy
	// that selects pods with label `app: isola-sandbox`
	template := &sandboxv1alpha1.NetworkTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-template",
			Namespace: "default",
		},
		Spec: sandboxv1alpha1.NetworkTemplateSpec{},
	}

	np, err := BuildNetworkPolicy(template)
	require.NoError(t, err)

	// Ingress should be nil (default deny, actual ingress handled by Helm)
	assert.Nil(t, np.Spec.Ingress)
}

func TestBuildNetworkPolicy_WithAllowedEgressCIDRs(t *testing.T) {
	// Use public CIDR since private ranges are blocked
	template := &sandboxv1alpha1.NetworkTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-template",
			Namespace: "default",
		},
		Spec: sandboxv1alpha1.NetworkTemplateSpec{
			AllowedEgressCIDRs: []string{"8.8.8.0/24"},
		},
	}

	np, err := BuildNetworkPolicy(template)
	require.NoError(t, err)

	require.Len(t, np.Spec.Egress, 1)
	assert.Equal(t, "8.8.8.0/24", np.Spec.Egress[0].To[0].IPBlock.CIDR)
}

func TestBuildNetworkPolicy_BlocksRiskyCIDRs(t *testing.T) {
	template := &sandboxv1alpha1.NetworkTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-template",
			Namespace: "default",
		},
		Spec: sandboxv1alpha1.NetworkTemplateSpec{
			AllowedEgressCIDRs: []string{"0.0.0.0/0"},
		},
	}

	np, err := BuildNetworkPolicy(template)
	require.NoError(t, err)

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

func TestBuildNetworkPolicy_DoesNotBlockNonOverlappingCIDRs(t *testing.T) {
	// Use a public IP range that doesn't overlap with any blocked CIDRs
	template := &sandboxv1alpha1.NetworkTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-template",
			Namespace: "default",
		},
		Spec: sandboxv1alpha1.NetworkTemplateSpec{
			AllowedEgressCIDRs: []string{"8.8.0.0/16"}, // Google DNS range - public, no overlap
		},
	}

	np, err := BuildNetworkPolicy(template)
	require.NoError(t, err)

	require.Len(t, np.Spec.Egress, 1)
	ipBlock := np.Spec.Egress[0].To[0].IPBlock
	assert.Equal(t, "8.8.0.0/16", ipBlock.CIDR)
	assert.Empty(t, ipBlock.Except)
}

func TestBuildNetworkPolicy_WithNameservers_SingleIP(t *testing.T) {
	template := &sandboxv1alpha1.NetworkTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-template",
			Namespace: "default",
		},
		Spec: sandboxv1alpha1.NetworkTemplateSpec{
			Nameservers: []string{"8.8.8.8"},
		},
	}

	np, err := BuildNetworkPolicy(template)
	require.NoError(t, err)

	require.Len(t, np.Spec.Egress, 1)
	dnsRule := np.Spec.Egress[0]

	require.Len(t, dnsRule.To, 1)
	require.NotNil(t, dnsRule.To[0].IPBlock)
	assert.Equal(t, "8.8.8.8/32", dnsRule.To[0].IPBlock.CIDR)

	require.Len(t, dnsRule.Ports, 2)
	var hasUDP, hasTCP bool
	for _, port := range dnsRule.Ports {
		if *port.Protocol == corev1.ProtocolUDP && port.Port.IntVal == 53 {
			hasUDP = true
		}
		if *port.Protocol == corev1.ProtocolTCP && port.Port.IntVal == 53 {
			hasTCP = true
		}
	}
	assert.True(t, hasUDP, "should have UDP port 53")
	assert.True(t, hasTCP, "should have TCP port 53")
}

func TestBuildNetworkPolicy_WithNameservers_MultipleIPs(t *testing.T) {
	template := &sandboxv1alpha1.NetworkTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-template",
			Namespace: "default",
		},
		Spec: sandboxv1alpha1.NetworkTemplateSpec{
			Nameservers: []string{"8.8.8.8", "1.1.1.1"},
		},
	}

	np, err := BuildNetworkPolicy(template)
	require.NoError(t, err)

	require.Len(t, np.Spec.Egress, 1)
	dnsRule := np.Spec.Egress[0]

	require.Len(t, dnsRule.To, 2)
	cidrs := []string{dnsRule.To[0].IPBlock.CIDR, dnsRule.To[1].IPBlock.CIDR}
	assert.Contains(t, cidrs, "8.8.8.8/32")
	assert.Contains(t, cidrs, "1.1.1.1/32")

	// Single rule with both IPs, port 53 UDP/TCP
	require.Len(t, dnsRule.Ports, 2)
}

func TestBuildNetworkPolicy_WithNameservers_IPv6(t *testing.T) {
	template := &sandboxv1alpha1.NetworkTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-template",
			Namespace: "default",
		},
		Spec: sandboxv1alpha1.NetworkTemplateSpec{
			Nameservers: []string{"2001:4860:4860::8888"},
		},
	}

	np, err := BuildNetworkPolicy(template)
	require.NoError(t, err)

	require.Len(t, np.Spec.Egress, 1)
	dnsRule := np.Spec.Egress[0]

	require.Len(t, dnsRule.To, 1)
	// IPv6 should use /128 prefix
	assert.Equal(t, "2001:4860:4860::8888/128", dnsRule.To[0].IPBlock.CIDR)
}

func TestBuildNetworkPolicy_WithNameservers_Empty_NoDNSRule(t *testing.T) {
	template := &sandboxv1alpha1.NetworkTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-template",
			Namespace: "default",
		},
		Spec: sandboxv1alpha1.NetworkTemplateSpec{
			Nameservers:        []string{}, // Explicitly empty
			AllowedEgressCIDRs: []string{"8.8.8.0/24"},
		},
	}

	np, err := BuildNetworkPolicy(template)
	require.NoError(t, err)

	// Should have exactly 1 egress rule (CIDR only, no DNS)
	require.Len(t, np.Spec.Egress, 1)

	// Verify no DNS rule exists (no port 53)
	dnsRule := findDNSEgressRule(np.Spec.Egress)
	assert.Nil(t, dnsRule, "should not have DNS rule when Nameservers is empty")

	// Verify the only rule is our CIDR
	assert.Equal(t, "8.8.8.0/24", np.Spec.Egress[0].To[0].IPBlock.CIDR)
}

func TestBuildNetworkPolicy_WithNameservers_Invalid_ReturnsError(t *testing.T) {
	template := &sandboxv1alpha1.NetworkTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-template",
			Namespace: "default",
		},
		Spec: sandboxv1alpha1.NetworkTemplateSpec{
			Nameservers: []string{"not-an-ip"},
		},
	}

	_, err := BuildNetworkPolicy(template)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid DNS server IP")
}

func TestBuildNetworkPolicy_InvalidEgressCIDRReturnsError(t *testing.T) {
	template := &sandboxv1alpha1.NetworkTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-template",
			Namespace: "default",
		},
		Spec: sandboxv1alpha1.NetworkTemplateSpec{
			AllowedEgressCIDRs: []string{"also-invalid"},
		},
	}

	_, err := BuildNetworkPolicy(template)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid CIDR")
}

func TestBuildNetworkPolicy_BlockedEgressCIDRReturnsError(t *testing.T) {
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
		{
			name:          "IPv6 inside blocked",
			cidr:          "fc00::/8",
			errorContains: "inside blocked range",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			template := &sandboxv1alpha1.NetworkTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-template",
					Namespace: "default",
				},
				Spec: sandboxv1alpha1.NetworkTemplateSpec{
					AllowedEgressCIDRs: []string{tt.cidr},
				},
			}

			_, err := BuildNetworkPolicy(template)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errorContains)
		})
	}
}

func TestBuildNetworkPolicy_CanonicalizesNonCanonicalCIDR(t *testing.T) {
	// Non-canonical input: 1.2.3.4/16 should become 1.2.0.0/16
	template := &sandboxv1alpha1.NetworkTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-template",
			Namespace: "default",
		},
		Spec: sandboxv1alpha1.NetworkTemplateSpec{
			AllowedEgressCIDRs: []string{"1.2.3.4/16"},
		},
	}

	np, err := BuildNetworkPolicy(template)
	require.NoError(t, err)

	// Check egress CIDR is canonicalized
	require.Len(t, np.Spec.Egress, 1)
	assert.Equal(t, "1.2.0.0/16", np.Spec.Egress[0].To[0].IPBlock.CIDR)
}

func TestBuildNetworkPolicy_FullTemplate(t *testing.T) {
	template := &sandboxv1alpha1.NetworkTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "full-template",
			Namespace: "sandbox-ns",
		},
		Spec: sandboxv1alpha1.NetworkTemplateSpec{
			AllowedEgressCIDRs: []string{"0.0.0.0/0"},
			Nameservers:        []string{"8.8.8.8"},
		},
	}

	np, err := BuildNetworkPolicy(template)
	require.NoError(t, err)

	// Ingress is nil (handled by Helm-installed NetworkPolicy)
	assert.Nil(t, np.Spec.Ingress)

	// Verify all egress rules present: DNS + CIDR
	assert.Len(t, np.Spec.Egress, 2)

	// Verify DNS rule exists
	dnsRule := findDNSEgressRule(np.Spec.Egress)
	require.NotNil(t, dnsRule, "should have DNS egress rule")
	assert.Equal(t, "8.8.8.8/32", dnsRule.To[0].IPBlock.CIDR)

	// Verify egress CIDR has blocked exceptions
	var foundEgressCIDR bool
	for _, rule := range np.Spec.Egress {
		for _, to := range rule.To {
			if to.IPBlock != nil && to.IPBlock.CIDR == "0.0.0.0/0" {
				foundEgressCIDR = true
				assert.Contains(t, to.IPBlock.Except, "10.0.0.0/8")
				assert.Contains(t, to.IPBlock.Except, "169.254.0.0/16")
			}
		}
	}
	assert.True(t, foundEgressCIDR, "should have 0.0.0.0/0 egress rule with exceptions")
}

// =============================================================================
// AllowedEgressPods tests
// =============================================================================

func TestBuildNetworkPolicy_WithEgressPods_BasicPodSelector(t *testing.T) {
	template := &sandboxv1alpha1.NetworkTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-template",
			Namespace: "default",
		},
		Spec: sandboxv1alpha1.NetworkTemplateSpec{
			AllowedEgressPods: []sandboxv1alpha1.EgressPodRule{
				{
					Namespace: "kube-system",
					PodSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"k8s-app": "kube-dns",
						},
					},
				},
			},
		},
	}

	np, err := BuildNetworkPolicy(template)
	require.NoError(t, err)

	require.Len(t, np.Spec.Egress, 1)
	egressRule := np.Spec.Egress[0]

	require.Len(t, egressRule.To, 1)
	peer := egressRule.To[0]

	// Verify namespace selector
	require.NotNil(t, peer.NamespaceSelector)
	assert.Equal(t, "kube-system", peer.NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"])

	// Verify pod selector
	require.NotNil(t, peer.PodSelector)
	assert.Equal(t, "kube-dns", peer.PodSelector.MatchLabels["k8s-app"])

	// No ports specified = all ports allowed
	assert.Empty(t, egressRule.Ports)
}

func TestBuildNetworkPolicy_WithEgressPods_WithPorts(t *testing.T) {
	template := &sandboxv1alpha1.NetworkTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-template",
			Namespace: "default",
		},
		Spec: sandboxv1alpha1.NetworkTemplateSpec{
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
						{Protocol: corev1.ProtocolTCP, Port: 53},
					},
				},
			},
		},
	}

	np, err := BuildNetworkPolicy(template)
	require.NoError(t, err)

	require.Len(t, np.Spec.Egress, 1)
	egressRule := np.Spec.Egress[0]

	// Verify ports
	require.Len(t, egressRule.Ports, 2)

	var hasUDP, hasTCP bool
	for _, port := range egressRule.Ports {
		if *port.Protocol == corev1.ProtocolUDP && port.Port.IntVal == 53 {
			hasUDP = true
		}
		if *port.Protocol == corev1.ProtocolTCP && port.Port.IntVal == 53 {
			hasTCP = true
		}
	}
	assert.True(t, hasUDP, "should have UDP port 53")
	assert.True(t, hasTCP, "should have TCP port 53")
}

func TestBuildNetworkPolicy_WithEgressPods_DefaultProtocol(t *testing.T) {
	template := &sandboxv1alpha1.NetworkTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-template",
			Namespace: "default",
		},
		Spec: sandboxv1alpha1.NetworkTemplateSpec{
			AllowedEgressPods: []sandboxv1alpha1.EgressPodRule{
				{
					Namespace: "my-namespace",
					PodSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "my-service",
						},
					},
					Ports: []sandboxv1alpha1.NetworkPort{
						{Port: 8080}, // No protocol specified - should default to TCP
					},
				},
			},
		},
	}

	np, err := BuildNetworkPolicy(template)
	require.NoError(t, err)

	require.Len(t, np.Spec.Egress, 1)
	egressRule := np.Spec.Egress[0]

	require.Len(t, egressRule.Ports, 1)
	assert.Equal(t, corev1.ProtocolTCP, *egressRule.Ports[0].Protocol)
	assert.Equal(t, int32(8080), egressRule.Ports[0].Port.IntVal)
}

func TestBuildNetworkPolicy_WithEgressPods_MultipleRules(t *testing.T) {
	template := &sandboxv1alpha1.NetworkTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-template",
			Namespace: "default",
		},
		Spec: sandboxv1alpha1.NetworkTemplateSpec{
			AllowedEgressPods: []sandboxv1alpha1.EgressPodRule{
				{
					Namespace: "kube-system",
					PodSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{"k8s-app": "kube-dns"},
					},
					Ports: []sandboxv1alpha1.NetworkPort{
						{Protocol: corev1.ProtocolUDP, Port: 53},
					},
				},
				{
					Namespace: "localstack",
					PodSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{"app.kubernetes.io/name": "localstack"},
					},
					Ports: []sandboxv1alpha1.NetworkPort{
						{Protocol: corev1.ProtocolTCP, Port: 4566},
					},
				},
			},
		},
	}

	np, err := BuildNetworkPolicy(template)
	require.NoError(t, err)

	// Should have 2 egress rules (one per pod rule)
	require.Len(t, np.Spec.Egress, 2)

	// Find kube-dns rule
	var kubeDNSRule, localstackRule *networkingv1.NetworkPolicyEgressRule
	for i := range np.Spec.Egress {
		rule := &np.Spec.Egress[i]
		if len(rule.To) > 0 && rule.To[0].NamespaceSelector != nil {
			ns := rule.To[0].NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"]
			if ns == "kube-system" {
				kubeDNSRule = rule
			} else if ns == "localstack" {
				localstackRule = rule
			}
		}
	}

	require.NotNil(t, kubeDNSRule, "should have kube-dns egress rule")
	require.NotNil(t, localstackRule, "should have localstack egress rule")

	// Verify kube-dns rule
	assert.Equal(t, "kube-dns", kubeDNSRule.To[0].PodSelector.MatchLabels["k8s-app"])
	require.Len(t, kubeDNSRule.Ports, 1)
	assert.Equal(t, int32(53), kubeDNSRule.Ports[0].Port.IntVal)

	// Verify localstack rule
	assert.Equal(t, "localstack", localstackRule.To[0].PodSelector.MatchLabels["app.kubernetes.io/name"])
	require.Len(t, localstackRule.Ports, 1)
	assert.Equal(t, int32(4566), localstackRule.Ports[0].Port.IntVal)
}

func TestBuildNetworkPolicy_WithEgressPods_EmptyPodSelector(t *testing.T) {
	// Empty pod selector should match all pods in the namespace
	template := &sandboxv1alpha1.NetworkTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-template",
			Namespace: "default",
		},
		Spec: sandboxv1alpha1.NetworkTemplateSpec{
			AllowedEgressPods: []sandboxv1alpha1.EgressPodRule{
				{
					Namespace:   "my-namespace",
					PodSelector: metav1.LabelSelector{}, // Empty = all pods
				},
			},
		},
	}

	np, err := BuildNetworkPolicy(template)
	require.NoError(t, err)

	require.Len(t, np.Spec.Egress, 1)
	egressRule := np.Spec.Egress[0]

	require.Len(t, egressRule.To, 1)
	peer := egressRule.To[0]

	// Namespace should be set
	require.NotNil(t, peer.NamespaceSelector)
	assert.Equal(t, "my-namespace", peer.NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"])

	// Pod selector should be empty (matches all pods)
	require.NotNil(t, peer.PodSelector)
	assert.Empty(t, peer.PodSelector.MatchLabels)
	assert.Empty(t, peer.PodSelector.MatchExpressions)
}

func TestBuildNetworkPolicy_CombinedEgressRules(t *testing.T) {
	// Test combining Nameservers, AllowedEgressPods, and AllowedEgressCIDRs
	template := &sandboxv1alpha1.NetworkTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-template",
			Namespace: "default",
		},
		Spec: sandboxv1alpha1.NetworkTemplateSpec{
			Nameservers: []string{"8.8.8.8"},
			AllowedEgressPods: []sandboxv1alpha1.EgressPodRule{
				{
					Namespace: "kube-system",
					PodSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{"k8s-app": "kube-dns"},
					},
				},
			},
			AllowedEgressCIDRs: []string{"1.1.1.0/24"},
		},
	}

	np, err := BuildNetworkPolicy(template)
	require.NoError(t, err)

	// Should have 3 egress rules: DNS IP + pod selector + CIDR
	require.Len(t, np.Spec.Egress, 3)

	// Verify DNS rule exists (by port 53)
	dnsRule := findDNSEgressRule(np.Spec.Egress)
	require.NotNil(t, dnsRule)
	assert.Equal(t, "8.8.8.8/32", dnsRule.To[0].IPBlock.CIDR)

	// Verify pod selector rule exists
	var podRule *networkingv1.NetworkPolicyEgressRule
	for i := range np.Spec.Egress {
		rule := &np.Spec.Egress[i]
		if len(rule.To) > 0 && rule.To[0].PodSelector != nil {
			podRule = rule
			break
		}
	}
	require.NotNil(t, podRule, "should have pod selector egress rule")

	// Verify CIDR rule exists
	cidrRule := findEgressRuleWithCIDR(np.Spec.Egress, "1.1.1.0/24")
	require.NotNil(t, cidrRule, "should have CIDR egress rule")
}

// =============================================================================
// Additional test coverage
// =============================================================================

func TestBuildNetworkPolicy_IPv6InternetAllowYieldsExceptions(t *testing.T) {
	// IPv6 "internet allow" (::/0) should have IPv6 blocked ranges in Except
	template := &sandboxv1alpha1.NetworkTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-template",
			Namespace: "default",
		},
		Spec: sandboxv1alpha1.NetworkTemplateSpec{
			AllowedEgressCIDRs: []string{"::/0"},
		},
	}

	np, err := BuildNetworkPolicy(template)
	require.NoError(t, err)

	require.Len(t, np.Spec.Egress, 1)
	egressRule := np.Spec.Egress[0]
	require.Len(t, egressRule.To, 1)

	ipBlock := egressRule.To[0].IPBlock
	assert.Equal(t, "::/0", ipBlock.CIDR)

	// Should block IPv6 private/link-local CIDRs
	assert.Contains(t, ipBlock.Except, "fc00::/7", "should block ULA")
	assert.Contains(t, ipBlock.Except, "fe80::/10", "should block link-local")

	// Should NOT contain any IPv4 CIDRs
	for _, e := range ipBlock.Except {
		assert.NotContains(t, e, ".", "IPv6 except should not contain IPv4 CIDRs")
	}
}

func TestBuildNetworkPolicy_PartialSupersetContainment(t *testing.T) {
	// 10.0.0.0/7 contains 10.0.0.0/8 but NOT 172.16.0.0/12 or others
	// This tests that containment logic isn't hard-coded to 0/0
	template := &sandboxv1alpha1.NetworkTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-template",
			Namespace: "default",
		},
		Spec: sandboxv1alpha1.NetworkTemplateSpec{
			AllowedEgressCIDRs: []string{"10.0.0.0/7"},
		},
	}

	np, err := BuildNetworkPolicy(template)
	require.NoError(t, err)

	require.Len(t, np.Spec.Egress, 1)
	ipBlock := np.Spec.Egress[0].To[0].IPBlock
	assert.Equal(t, "10.0.0.0/7", ipBlock.CIDR)

	// Should only except 10.0.0.0/8 (contained within 10.0.0.0/7)
	assert.Contains(t, ipBlock.Except, "10.0.0.0/8")

	// Should NOT contain ranges outside 10.0.0.0/7
	assert.NotContains(t, ipBlock.Except, "172.16.0.0/12")
	assert.NotContains(t, ipBlock.Except, "192.168.0.0/16")
	assert.NotContains(t, ipBlock.Except, "169.254.0.0/16")
}

func TestBuildNetworkPolicy_DeduplicatesCIDRs(t *testing.T) {
	// Test that duplicate CIDRs result in single rules
	template := &sandboxv1alpha1.NetworkTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-template",
			Namespace: "default",
		},
		Spec: sandboxv1alpha1.NetworkTemplateSpec{
			AllowedEgressCIDRs: []string{"8.8.8.0/24", "8.8.8.0/24", "1.1.1.0/24"},
		},
	}

	np, err := BuildNetworkPolicy(template)
	require.NoError(t, err)

	// Egress: 2 rules (one per unique CIDR)
	require.Len(t, np.Spec.Egress, 2)

	// Verify both unique CIDRs exist (order-agnostic)
	rule1 := findEgressRuleWithCIDR(np.Spec.Egress, "8.8.8.0/24")
	rule2 := findEgressRuleWithCIDR(np.Spec.Egress, "1.1.1.0/24")
	assert.NotNil(t, rule1, "should have 8.8.8.0/24 egress rule")
	assert.NotNil(t, rule2, "should have 1.1.1.0/24 egress rule")
}

func TestBuildNetworkPolicy_DeduplicatesNonCanonicalDuplicates(t *testing.T) {
	// 1.2.3.4/16 and 1.2.0.0/16 should dedupe to single rule (same canonical form)
	template := &sandboxv1alpha1.NetworkTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-template",
			Namespace: "default",
		},
		Spec: sandboxv1alpha1.NetworkTemplateSpec{
			AllowedEgressCIDRs: []string{"1.2.3.4/16", "1.2.0.0/16"},
		},
	}

	np, err := BuildNetworkPolicy(template)
	require.NoError(t, err)

	// Egress: single rule
	require.Len(t, np.Spec.Egress, 1)
	assert.Equal(t, "1.2.0.0/16", np.Spec.Egress[0].To[0].IPBlock.CIDR)
}

// =============================================================================
// Helper functions for rule matching
// =============================================================================

// findEgressRuleWithCIDR finds an egress rule with the specified CIDR (order-agnostic).
func findEgressRuleWithCIDR(rules []networkingv1.NetworkPolicyEgressRule, cidr string) *networkingv1.NetworkPolicyEgressRule {
	for i := range rules {
		for _, to := range rules[i].To {
			if to.IPBlock != nil && to.IPBlock.CIDR == cidr {
				return &rules[i]
			}
		}
	}
	return nil
}

// findDNSEgressRule finds an egress rule with port 53 (DNS rule).
func findDNSEgressRule(rules []networkingv1.NetworkPolicyEgressRule) *networkingv1.NetworkPolicyEgressRule {
	for i := range rules {
		for _, port := range rules[i].Ports {
			if port.Port != nil && port.Port.IntVal == 53 {
				return &rules[i]
			}
		}
	}
	return nil
}
