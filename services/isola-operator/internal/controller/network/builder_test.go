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

	controllerNS := "isola-system"
	controllerLabels := map[string]string{"app": "isola-controller"}

	np, err := BuildNetworkPolicy(template, controllerNS, controllerLabels)
	require.NoError(t, err)

	assert.Equal(t, "test-template-netpol", np.Name)
	assert.Equal(t, "default", np.Namespace)
	assert.Equal(t, "test-template", np.Labels[NetworkTemplateLabelKey])
	assert.Equal(t, "test-template", np.Spec.PodSelector.MatchLabels[NetworkTemplateLabelKey])
	assert.Contains(t, np.Spec.PolicyTypes, networkingv1.PolicyTypeIngress)
	assert.Contains(t, np.Spec.PolicyTypes, networkingv1.PolicyTypeEgress)
}

func TestBuildNetworkPolicy_AlwaysIncludesControllerIngress(t *testing.T) {
	template := &sandboxv1alpha1.NetworkTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-template",
			Namespace: "default",
		},
		Spec: sandboxv1alpha1.NetworkTemplateSpec{},
	}

	controllerNS := "isola-system"
	controllerLabels := map[string]string{"app.kubernetes.io/name": "isola-controller"}

	np, err := BuildNetworkPolicy(template, controllerNS, controllerLabels)
	require.NoError(t, err)

	// Should have exactly one ingress rule (controller ingress)
	require.Len(t, np.Spec.Ingress, 1)

	ingressRule := np.Spec.Ingress[0]
	require.Len(t, ingressRule.From, 1)
	require.NotNil(t, ingressRule.From[0].NamespaceSelector)
	assert.Equal(t, "isola-system", ingressRule.From[0].NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"])
	require.NotNil(t, ingressRule.From[0].PodSelector)
	assert.Equal(t, "isola-controller", ingressRule.From[0].PodSelector.MatchLabels["app.kubernetes.io/name"])
	require.Len(t, ingressRule.Ports, 1)
	assert.Equal(t, int32(8080), ingressRule.Ports[0].Port.IntVal)
	assert.Equal(t, corev1.ProtocolTCP, *ingressRule.Ports[0].Protocol)
}

func TestBuildNetworkPolicy_WithAllowedIngressCIDRs(t *testing.T) {
	template := &sandboxv1alpha1.NetworkTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-template",
			Namespace: "default",
		},
		Spec: sandboxv1alpha1.NetworkTemplateSpec{
			AllowedIngress: []string{"10.0.0.0/8", "192.168.0.0/16"},
		},
	}

	np, err := BuildNetworkPolicy(template, "isola-system", map[string]string{"app": "controller"})
	require.NoError(t, err)

	// Should have controller ingress + CIDR ingress
	require.Len(t, np.Spec.Ingress, 2)

	// Second rule should be the CIDR rules
	cidrRule := np.Spec.Ingress[1]
	require.Len(t, cidrRule.From, 2)
	assert.Equal(t, "10.0.0.0/8", cidrRule.From[0].IPBlock.CIDR)
	assert.Equal(t, "192.168.0.0/16", cidrRule.From[1].IPBlock.CIDR)
}

func TestBuildNetworkPolicy_WithAllowedEgress(t *testing.T) {
	// Use public CIDR since private ranges are blocked
	template := &sandboxv1alpha1.NetworkTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-template",
			Namespace: "default",
		},
		Spec: sandboxv1alpha1.NetworkTemplateSpec{
			AllowedEgress: []string{"8.8.8.0/24"},
		},
	}

	np, err := BuildNetworkPolicy(template, "isola-system", map[string]string{"app": "controller"})
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
			AllowedEgress: []string{"0.0.0.0/0"},
		},
	}

	np, err := BuildNetworkPolicy(template, "isola-system", map[string]string{"app": "controller"})
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
			AllowedEgress: []string{"8.8.0.0/16"}, // Google DNS range - public, no overlap
		},
	}

	np, err := BuildNetworkPolicy(template, "isola-system", map[string]string{"app": "controller"})
	require.NoError(t, err)

	require.Len(t, np.Spec.Egress, 1)
	ipBlock := np.Spec.Egress[0].To[0].IPBlock
	assert.Equal(t, "8.8.0.0/16", ipBlock.CIDR)
	assert.Empty(t, ipBlock.Except)
}

func TestBuildNetworkPolicy_WithDNSServers_SingleIP(t *testing.T) {
	template := &sandboxv1alpha1.NetworkTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-template",
			Namespace: "default",
		},
		Spec: sandboxv1alpha1.NetworkTemplateSpec{
			DNSServers: []string{"8.8.8.8"},
		},
	}

	np, err := BuildNetworkPolicy(template, "isola-system", map[string]string{"app": "controller"})
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

func TestBuildNetworkPolicy_WithDNSServers_MultipleIPs(t *testing.T) {
	template := &sandboxv1alpha1.NetworkTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-template",
			Namespace: "default",
		},
		Spec: sandboxv1alpha1.NetworkTemplateSpec{
			DNSServers: []string{"8.8.8.8", "1.1.1.1"},
		},
	}

	np, err := BuildNetworkPolicy(template, "isola-system", map[string]string{"app": "controller"})
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

func TestBuildNetworkPolicy_WithDNSServers_IPv6(t *testing.T) {
	template := &sandboxv1alpha1.NetworkTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-template",
			Namespace: "default",
		},
		Spec: sandboxv1alpha1.NetworkTemplateSpec{
			DNSServers: []string{"2001:4860:4860::8888"},
		},
	}

	np, err := BuildNetworkPolicy(template, "isola-system", map[string]string{"app": "controller"})
	require.NoError(t, err)

	require.Len(t, np.Spec.Egress, 1)
	dnsRule := np.Spec.Egress[0]

	require.Len(t, dnsRule.To, 1)
	// IPv6 should use /128 prefix
	assert.Equal(t, "2001:4860:4860::8888/128", dnsRule.To[0].IPBlock.CIDR)
}

func TestBuildNetworkPolicy_WithDNSServers_Empty_NoDNSRule(t *testing.T) {
	template := &sandboxv1alpha1.NetworkTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-template",
			Namespace: "default",
		},
		Spec: sandboxv1alpha1.NetworkTemplateSpec{
			DNSServers:    []string{}, // Explicitly empty
			AllowedEgress: []string{"8.8.8.0/24"},
		},
	}

	np, err := BuildNetworkPolicy(template, "isola-system", map[string]string{"app": "controller"})
	require.NoError(t, err)

	// Should have exactly 1 egress rule (CIDR only, no DNS)
	require.Len(t, np.Spec.Egress, 1)

	// Verify no DNS rule exists (no port 53)
	dnsRule := findDNSEgressRule(np.Spec.Egress)
	assert.Nil(t, dnsRule, "should not have DNS rule when DNSServers is empty")

	// Verify the only rule is our CIDR
	assert.Equal(t, "8.8.8.0/24", np.Spec.Egress[0].To[0].IPBlock.CIDR)
}

func TestBuildNetworkPolicy_WithDNSServers_Invalid_ReturnsError(t *testing.T) {
	template := &sandboxv1alpha1.NetworkTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-template",
			Namespace: "default",
		},
		Spec: sandboxv1alpha1.NetworkTemplateSpec{
			DNSServers: []string{"not-an-ip"},
		},
	}

	_, err := BuildNetworkPolicy(template, "isola-system", map[string]string{"app": "controller"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid DNS server IP")
}

func TestBuildNetworkPolicy_InvalidCIDRReturnsError(t *testing.T) {
	template := &sandboxv1alpha1.NetworkTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-template",
			Namespace: "default",
		},
		Spec: sandboxv1alpha1.NetworkTemplateSpec{
			AllowedIngress: []string{"invalid-cidr"},
		},
	}

	_, err := BuildNetworkPolicy(template, "isola-system", map[string]string{"app": "controller"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid CIDR")
}

func TestBuildNetworkPolicy_InvalidEgressCIDRReturnsError(t *testing.T) {
	template := &sandboxv1alpha1.NetworkTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-template",
			Namespace: "default",
		},
		Spec: sandboxv1alpha1.NetworkTemplateSpec{
			AllowedEgress: []string{"also-invalid"},
		},
	}

	_, err := BuildNetworkPolicy(template, "isola-system", map[string]string{"app": "controller"})
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
					AllowedEgress: []string{tt.cidr},
				},
			}

			_, err := BuildNetworkPolicy(template, "isola-system", map[string]string{"app": "controller"})
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errorContains)
		})
	}
}

func TestBuildNetworkPolicy_CanonicalizesNonCanonicalCIDR(t *testing.T) {
	// Non-canonical input: 8.8.8.8/24 should become 8.8.8.0/24
	template := &sandboxv1alpha1.NetworkTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-template",
			Namespace: "default",
		},
		Spec: sandboxv1alpha1.NetworkTemplateSpec{
			AllowedIngress: []string{"8.8.8.8/24"},
			AllowedEgress:  []string{"1.2.3.4/16"},
		},
	}

	np, err := BuildNetworkPolicy(template, "isola-system", map[string]string{"app": "controller"})
	require.NoError(t, err)

	// Check ingress CIDR is canonicalized
	require.Len(t, np.Spec.Ingress, 2)
	assert.Equal(t, "8.8.8.0/24", np.Spec.Ingress[1].From[0].IPBlock.CIDR)

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
			AllowedIngress: []string{"10.0.0.0/8"},
			AllowedEgress:  []string{"0.0.0.0/0"},
			DNSServers:     []string{"8.8.8.8"},
		},
	}

	np, err := BuildNetworkPolicy(template, "isola-system", map[string]string{"app": "controller"})
	require.NoError(t, err)

	// Verify all ingress rules present: controller + CIDR
	assert.Len(t, np.Spec.Ingress, 2)

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
// Order-agnostic helper functions for rule matching
// =============================================================================

// findIngressRuleWithCIDRs finds an ingress rule containing all specified CIDRs (order-agnostic).
func findIngressRuleWithCIDRs(rules []networkingv1.NetworkPolicyIngressRule, cidrs ...string) *networkingv1.NetworkPolicyIngressRule {
	cidrSet := make(map[string]bool)
	for _, c := range cidrs {
		cidrSet[c] = true
	}

	for i := range rules {
		rule := &rules[i]
		found := make(map[string]bool)
		for _, from := range rule.From {
			if from.IPBlock != nil {
				found[from.IPBlock.CIDR] = true
			}
		}
		if len(found) == len(cidrSet) {
			match := true
			for c := range cidrSet {
				if !found[c] {
					match = false
					break
				}
			}
			if match {
				return rule
			}
		}
	}
	return nil
}

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
			AllowedEgress: []string{"::/0"},
		},
	}

	np, err := BuildNetworkPolicy(template, "isola-system", map[string]string{"app": "controller"})
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
			AllowedEgress: []string{"10.0.0.0/7"},
		},
	}

	np, err := BuildNetworkPolicy(template, "isola-system", map[string]string{"app": "controller"})
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
			AllowedIngress: []string{"8.8.8.0/24", "8.8.8.0/24", "1.1.1.0/24", "8.8.8.0/24"},
			AllowedEgress:  []string{"8.8.8.0/24", "8.8.8.0/24", "1.1.1.0/24"},
		},
	}

	np, err := BuildNetworkPolicy(template, "isola-system", map[string]string{"app": "controller"})
	require.NoError(t, err)

	// Ingress: controller rule + 1 CIDR rule with 2 unique CIDRs
	require.Len(t, np.Spec.Ingress, 2)
	cidrIngressRule := findIngressRuleWithCIDRs(np.Spec.Ingress, "8.8.8.0/24", "1.1.1.0/24")
	require.NotNil(t, cidrIngressRule, "should have ingress rule with both unique CIDRs")
	assert.Len(t, cidrIngressRule.From, 2, "should have exactly 2 unique CIDRs, not 4")

	// Egress: 2 rules (one per unique CIDR)
	require.Len(t, np.Spec.Egress, 2)

	// Verify both unique CIDRs exist (order-agnostic)
	rule1 := findEgressRuleWithCIDR(np.Spec.Egress, "8.8.8.0/24")
	rule2 := findEgressRuleWithCIDR(np.Spec.Egress, "1.1.1.0/24")
	assert.NotNil(t, rule1, "should have 8.8.8.0/24 egress rule")
	assert.NotNil(t, rule2, "should have 1.1.1.0/24 egress rule")
}

func TestBuildNetworkPolicy_DeduplicatesNonCanonicalDuplicates(t *testing.T) {
	// 8.8.8.8/24 and 8.8.8.0/24 should dedupe to single rule (same canonical form)
	template := &sandboxv1alpha1.NetworkTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-template",
			Namespace: "default",
		},
		Spec: sandboxv1alpha1.NetworkTemplateSpec{
			AllowedIngress: []string{"8.8.8.8/24", "8.8.8.0/24"},
			AllowedEgress:  []string{"1.2.3.4/16", "1.2.0.0/16"},
		},
	}

	np, err := BuildNetworkPolicy(template, "isola-system", map[string]string{"app": "controller"})
	require.NoError(t, err)

	// Ingress: controller + 1 CIDR rule with single unique CIDR
	require.Len(t, np.Spec.Ingress, 2)
	var cidrRule *networkingv1.NetworkPolicyIngressRule
	for i := range np.Spec.Ingress {
		for _, from := range np.Spec.Ingress[i].From {
			if from.IPBlock != nil {
				cidrRule = &np.Spec.Ingress[i]
				break
			}
		}
	}
	require.NotNil(t, cidrRule)
	assert.Len(t, cidrRule.From, 1, "non-canonical duplicates should be deduped")
	assert.Equal(t, "8.8.8.0/24", cidrRule.From[0].IPBlock.CIDR)

	// Egress: single rule
	require.Len(t, np.Spec.Egress, 1)
	assert.Equal(t, "1.2.0.0/16", np.Spec.Egress[0].To[0].IPBlock.CIDR)
}

func TestBuildNetworkPolicy_WithIngressCIDRs_OrderAgnostic(t *testing.T) {
	// Refactored version of TestBuildNetworkPolicy_WithAllowedIngressCIDRs
	// that doesn't assume rule ordering
	template := &sandboxv1alpha1.NetworkTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-template",
			Namespace: "default",
		},
		Spec: sandboxv1alpha1.NetworkTemplateSpec{
			AllowedIngress: []string{"10.0.0.0/8", "192.168.0.0/16"},
		},
	}

	np, err := BuildNetworkPolicy(template, "isola-system", map[string]string{"app": "controller"})
	require.NoError(t, err)

	// Should have controller ingress + CIDR ingress
	require.Len(t, np.Spec.Ingress, 2)

	// Find CIDR rule (order-agnostic)
	cidrRule := findIngressRuleWithCIDRs(np.Spec.Ingress, "10.0.0.0/8", "192.168.0.0/16")
	require.NotNil(t, cidrRule, "should have ingress rule with both CIDRs")
	assert.Len(t, cidrRule.From, 2)
}
