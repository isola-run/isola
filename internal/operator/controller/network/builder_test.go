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

	. "github.com/onsi/gomega"
	networkingv1 "k8s.io/api/networking/v1"

	sandboxv1alpha1 "github.com/isola-ai/isola-sb/api/v1alpha1"
	"k8s.io/utils/ptr"
)

func mustBuildCustomNetworkPolicy(t *testing.T, network *sandboxv1alpha1.NetworkSpec) *networkingv1.NetworkPolicy {
	t.Helper()
	np, err := BuildCustomNetworkPolicy("test-sandbox", "default", network)
	if err != nil {
		t.Fatalf("BuildCustomNetworkPolicy returned error: %v", err)
	}
	if np == nil {
		t.Fatal("BuildCustomNetworkPolicy returned nil")
	}
	return np
}

func TestBuildCustomNetworkPolicy_NilNetwork(t *testing.T) {
	g := NewWithT(t)
	np, err := BuildCustomNetworkPolicy("test-sandbox", "default", nil)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(np).To(BeNil())
}

func TestBuildCustomNetworkPolicy_EmptyNetwork(t *testing.T) {
	g := NewWithT(t)
	network := &sandboxv1alpha1.NetworkSpec{}

	np, err := BuildCustomNetworkPolicy("test-sandbox", "default", network)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(np).To(BeNil(), "empty network should not create policy")
}

func TestBuildCustomNetworkPolicy_WithAllowedEgressCIDRs(t *testing.T) {
	g := NewWithT(t)
	network := &sandboxv1alpha1.NetworkSpec{
		AllowedEgressCIDRs: []string{"8.8.8.0/24"},
	}

	np := mustBuildCustomNetworkPolicy(t, network)

	g.Expect(np.Name).To(Equal("test-sandbox-custom-netpol"))
	g.Expect(np.Namespace).To(Equal("default"))
	g.Expect(np.Spec.PodSelector.MatchLabels["app.kubernetes.io/instance"]).To(Equal("test-sandbox"))
	g.Expect(np.Spec.PolicyTypes).To(ContainElement(networkingv1.PolicyTypeEgress))

	g.Expect(np.Spec.Egress).To(HaveLen(1))
	g.Expect(np.Spec.Egress[0].To[0].IPBlock.CIDR).To(Equal("8.8.8.0/24"))
}

func TestBuildCustomNetworkPolicy_BlocksRiskyCIDRs(t *testing.T) {
	g := NewWithT(t)
	network := &sandboxv1alpha1.NetworkSpec{
		AllowedEgressCIDRs: []string{"0.0.0.0/0"},
	}

	np := mustBuildCustomNetworkPolicy(t, network)

	g.Expect(np.Spec.Egress).To(HaveLen(1))
	egressRule := np.Spec.Egress[0]
	g.Expect(egressRule.To).To(HaveLen(1))

	ipBlock := egressRule.To[0].IPBlock
	g.Expect(ipBlock.CIDR).To(Equal("0.0.0.0/0"))

	// Should block all private/internal CIDRs
	g.Expect(ipBlock.Except).To(ContainElement("10.0.0.0/8"))
	g.Expect(ipBlock.Except).To(ContainElement("172.16.0.0/12"))
	g.Expect(ipBlock.Except).To(ContainElement("192.168.0.0/16"))
	g.Expect(ipBlock.Except).To(ContainElement("169.254.0.0/16"))
}

func TestBuildCustomNetworkPolicy_DoesNotBlockNonOverlappingCIDRs(t *testing.T) {
	g := NewWithT(t)
	network := &sandboxv1alpha1.NetworkSpec{
		AllowedEgressCIDRs: []string{"8.8.0.0/16"},
	}

	np := mustBuildCustomNetworkPolicy(t, network)

	g.Expect(np.Spec.Egress).To(HaveLen(1))
	ipBlock := np.Spec.Egress[0].To[0].IPBlock
	g.Expect(ipBlock.CIDR).To(Equal("8.8.0.0/16"))
	g.Expect(ipBlock.Except).To(BeEmpty())
}

func TestBuildCustomNetworkPolicy_WithNameservers(t *testing.T) {
	g := NewWithT(t)
	network := &sandboxv1alpha1.NetworkSpec{
		Nameservers: []string{"8.8.8.8", "1.1.1.1"},
	}

	np := mustBuildCustomNetworkPolicy(t, network)

	g.Expect(np.Spec.Egress).To(HaveLen(1))
	dnsRule := np.Spec.Egress[0]

	g.Expect(dnsRule.To).To(HaveLen(2))
	cidrs := []string{dnsRule.To[0].IPBlock.CIDR, dnsRule.To[1].IPBlock.CIDR}
	g.Expect(cidrs).To(ContainElement("8.8.8.8/32"))
	g.Expect(cidrs).To(ContainElement("1.1.1.1/32"))

	g.Expect(dnsRule.Ports).To(HaveLen(2))
}

func TestBuildCustomNetworkPolicy_NameserversWithInternetAccess(t *testing.T) {
	g := NewWithT(t)
	network := &sandboxv1alpha1.NetworkSpec{
		AllowAllInternet: ptr.To(true),
		Nameservers:      []string{"8.8.8.8"},
	}

	np, err := BuildCustomNetworkPolicy("test-sandbox", "default", network)
	g.Expect(err).ToNot(HaveOccurred())
	// Public nameserver already reachable via static allow-internet policy — no custom NP needed
	g.Expect(np).To(BeNil())
}

func TestBuildCustomNetworkPolicy_PrivateNameserverWithInternetAccess(t *testing.T) {
	g := NewWithT(t)
	network := &sandboxv1alpha1.NetworkSpec{
		AllowAllInternet: ptr.To(true),
		Nameservers:      []string{"10.0.0.53"},
	}

	np, err := BuildCustomNetworkPolicy("test-sandbox", "default", network)
	g.Expect(err).ToNot(HaveOccurred())
	// All nameservers skipped when internet is allowed — no custom NP
	g.Expect(np).To(BeNil())
}

func TestBuildCustomNetworkPolicy_MixedNameserversWithInternetAccess(t *testing.T) {
	g := NewWithT(t)
	network := &sandboxv1alpha1.NetworkSpec{
		AllowAllInternet: ptr.To(true),
		Nameservers:      []string{"8.8.8.8", "10.0.0.53", "1.1.1.1"},
	}

	np, err := BuildCustomNetworkPolicy("test-sandbox", "default", network)
	g.Expect(err).ToNot(HaveOccurred())
	// All nameservers skipped when internet is allowed — no custom NP
	g.Expect(np).To(BeNil())
}

func TestBuildCustomNetworkPolicy_IPv6PublicNameserverWithInternetAccess(t *testing.T) {
	g := NewWithT(t)
	network := &sandboxv1alpha1.NetworkSpec{
		AllowAllInternet: ptr.To(true),
		Nameservers:      []string{"2001:4860:4860::8888"},
	}

	np, err := BuildCustomNetworkPolicy("test-sandbox", "default", network)
	g.Expect(err).ToNot(HaveOccurred())
	// Public IPv6 nameserver already reachable — no custom NP needed
	g.Expect(np).To(BeNil())
}

func TestBuildCustomNetworkPolicy_IPv6PrivateNameserverWithInternetAccess(t *testing.T) {
	g := NewWithT(t)
	network := &sandboxv1alpha1.NetworkSpec{
		AllowAllInternet: ptr.To(true),
		Nameservers:      []string{"fd00::53"},
	}

	np, err := BuildCustomNetworkPolicy("test-sandbox", "default", network)
	g.Expect(err).ToNot(HaveOccurred())
	// All nameservers skipped when internet is allowed — no custom NP
	g.Expect(np).To(BeNil())
}

func TestBuildCustomNetworkPolicy_NameserversWithoutInternetAccess(t *testing.T) {
	g := NewWithT(t)
	network := &sandboxv1alpha1.NetworkSpec{
		AllowAllInternet: ptr.To(false),
		Nameservers:      []string{"8.8.8.8", "10.0.0.53"},
	}

	// Without internet access, ALL nameservers need explicit rules
	np := mustBuildCustomNetworkPolicy(t, network)

	g.Expect(np.Spec.Egress).To(HaveLen(1))
	g.Expect(np.Spec.Egress[0].To).To(HaveLen(2))
	cidrs := []string{np.Spec.Egress[0].To[0].IPBlock.CIDR, np.Spec.Egress[0].To[1].IPBlock.CIDR}
	g.Expect(cidrs).To(ContainElement("8.8.8.8/32"))
	g.Expect(cidrs).To(ContainElement("10.0.0.53/32"))
}

func TestBuildCustomNetworkPolicy_CIDRsAndPublicNameserversWithInternetAccess(t *testing.T) {
	g := NewWithT(t)
	network := &sandboxv1alpha1.NetworkSpec{
		AllowAllInternet:   ptr.To(true),
		Nameservers:        []string{"8.8.8.8"},
		AllowedEgressCIDRs: []string{"1.1.1.0/24"},
	}

	np, err := BuildCustomNetworkPolicy("test-sandbox", "default", network)
	g.Expect(err).ToNot(HaveOccurred())
	// Both public NS and public CIDR already reachable via static internet policy
	g.Expect(np).To(BeNil())
}

func TestBuildCustomNetworkPolicy_CIDRsOnlyWithInternetAccess(t *testing.T) {
	g := NewWithT(t)
	network := &sandboxv1alpha1.NetworkSpec{
		AllowAllInternet:   ptr.To(true),
		AllowedEgressCIDRs: []string{"8.8.8.0/24", "1.1.1.0/24"},
	}

	np, err := BuildCustomNetworkPolicy("test-sandbox", "default", network)
	g.Expect(err).ToNot(HaveOccurred())
	// All CIDRs already reachable via static internet policy
	g.Expect(np).To(BeNil())
}

func TestBuildCustomNetworkPolicy_CIDRsAndPrivateNameserverWithInternetAccess(t *testing.T) {
	g := NewWithT(t)
	network := &sandboxv1alpha1.NetworkSpec{
		AllowAllInternet:   ptr.To(true),
		Nameservers:        []string{"10.0.0.53"},
		AllowedEgressCIDRs: []string{"1.1.1.0/24"},
	}

	np, err := BuildCustomNetworkPolicy("test-sandbox", "default", network)
	g.Expect(err).ToNot(HaveOccurred())
	// Both CIDRs and nameservers skipped when internet is allowed
	g.Expect(np).To(BeNil())
}

func TestBuildCustomNetworkPolicy_InvalidNameserver(t *testing.T) {
	g := NewWithT(t)
	network := &sandboxv1alpha1.NetworkSpec{
		Nameservers: []string{"not-an-ip"},
	}

	_, err := BuildCustomNetworkPolicy("test-sandbox", "default", network)
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("invalid DNS server IP"))
}

func TestBuildCustomNetworkPolicy_InvalidEgressCIDR(t *testing.T) {
	g := NewWithT(t)
	network := &sandboxv1alpha1.NetworkSpec{
		AllowedEgressCIDRs: []string{"also-invalid"},
	}

	_, err := BuildCustomNetworkPolicy("test-sandbox", "default", network)
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("invalid CIDR"))
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
			g := NewWithT(t)
			network := &sandboxv1alpha1.NetworkSpec{
				AllowedEgressCIDRs: []string{tt.cidr},
			}

			_, err := BuildCustomNetworkPolicy("test-sandbox", "default", network)
			g.Expect(err).To(HaveOccurred())
			g.Expect(err.Error()).To(ContainSubstring(tt.errorContains))
		})
	}
}

func TestBuildCustomNetworkPolicy_CombinedRules(t *testing.T) {
	g := NewWithT(t)
	network := &sandboxv1alpha1.NetworkSpec{
		Nameservers:        []string{"8.8.8.8"},
		AllowedEgressCIDRs: []string{"1.1.1.0/24"},
	}

	np := mustBuildCustomNetworkPolicy(t, network)

	// Should have 2 egress rules: DNS IP + CIDR
	g.Expect(np.Spec.Egress).To(HaveLen(2))
}

func TestBuildCustomNetworkPolicy_DeduplicatesCIDRs(t *testing.T) {
	g := NewWithT(t)
	network := &sandboxv1alpha1.NetworkSpec{
		AllowedEgressCIDRs: []string{"8.8.8.0/24", "8.8.8.0/24", "1.1.1.0/24"},
	}

	np := mustBuildCustomNetworkPolicy(t, network)

	// Should have 2 egress rules (deduplicated)
	g.Expect(np.Spec.Egress).To(HaveLen(2))
}

// IPv6 Tests

func TestBuildCustomNetworkPolicy_IPv6Nameservers(t *testing.T) {
	g := NewWithT(t)
	network := &sandboxv1alpha1.NetworkSpec{
		Nameservers: []string{"2001:4860:4860::8888", "2001:4860:4860::8844"},
	}

	np := mustBuildCustomNetworkPolicy(t, network)

	g.Expect(np.Spec.Egress).To(HaveLen(1))
	dnsRule := np.Spec.Egress[0]

	g.Expect(dnsRule.To).To(HaveLen(2))
	cidrs := []string{dnsRule.To[0].IPBlock.CIDR, dnsRule.To[1].IPBlock.CIDR}
	// IPv6 addresses should use /128 prefix
	g.Expect(cidrs).To(ContainElement("2001:4860:4860::8888/128"))
	g.Expect(cidrs).To(ContainElement("2001:4860:4860::8844/128"))

	g.Expect(dnsRule.Ports).To(HaveLen(2))
}

func TestBuildCustomNetworkPolicy_MixedIPv4IPv6Nameservers(t *testing.T) {
	g := NewWithT(t)
	network := &sandboxv1alpha1.NetworkSpec{
		Nameservers: []string{"8.8.8.8", "2001:4860:4860::8888"},
	}

	np := mustBuildCustomNetworkPolicy(t, network)

	g.Expect(np.Spec.Egress).To(HaveLen(1))
	dnsRule := np.Spec.Egress[0]

	g.Expect(dnsRule.To).To(HaveLen(2))
	cidrs := []string{dnsRule.To[0].IPBlock.CIDR, dnsRule.To[1].IPBlock.CIDR}
	g.Expect(cidrs).To(ContainElement("8.8.8.8/32"))
	g.Expect(cidrs).To(ContainElement("2001:4860:4860::8888/128"))
}

func TestBuildCustomNetworkPolicy_IPv6AllowedEgressCIDR(t *testing.T) {
	g := NewWithT(t)
	network := &sandboxv1alpha1.NetworkSpec{
		AllowedEgressCIDRs: []string{"2001:4860::/32"},
	}

	np := mustBuildCustomNetworkPolicy(t, network)

	g.Expect(np.Spec.Egress).To(HaveLen(1))
	ipBlock := np.Spec.Egress[0].To[0].IPBlock
	g.Expect(ipBlock.CIDR).To(Equal("2001:4860::/32"))
	// Public IPv6 range should have no exceptions
	g.Expect(ipBlock.Except).To(BeEmpty())
}

func TestBuildCustomNetworkPolicy_IPv6AllInternet(t *testing.T) {
	g := NewWithT(t)
	network := &sandboxv1alpha1.NetworkSpec{
		AllowedEgressCIDRs: []string{"::/0"},
	}

	np := mustBuildCustomNetworkPolicy(t, network)

	g.Expect(np.Spec.Egress).To(HaveLen(1))
	ipBlock := np.Spec.Egress[0].To[0].IPBlock
	g.Expect(ipBlock.CIDR).To(Equal("::/0"))

	// Should block IPv6 private/internal ranges
	g.Expect(ipBlock.Except).To(ContainElement("fc00::/7"))  // ULA
	g.Expect(ipBlock.Except).To(ContainElement("fe80::/10")) // Link-local
	g.Expect(ipBlock.Except).To(ContainElement("ff00::/8"))  // Multicast
}

func TestBuildCustomNetworkPolicy_BlockedIPv6CIDRs(t *testing.T) {
	tests := []struct {
		name          string
		cidr          string
		errorContains string
	}{
		{
			name:          "ULA inside blocked range",
			cidr:          "fd00::/8",
			errorContains: "inside blocked range",
		},
		{
			name:          "ULA equals blocked range",
			cidr:          "fc00::/7",
			errorContains: "equals blocked range",
		},
		{
			name:          "link-local address",
			cidr:          "fe80::1/128",
			errorContains: "inside blocked range",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			network := &sandboxv1alpha1.NetworkSpec{
				AllowedEgressCIDRs: []string{tt.cidr},
			}

			_, err := BuildCustomNetworkPolicy("test-sandbox", "default", network)
			g.Expect(err).To(HaveOccurred())
			g.Expect(err.Error()).To(ContainSubstring(tt.errorContains))
		})
	}
}
