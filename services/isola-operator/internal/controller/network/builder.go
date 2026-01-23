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

/*
Package network provides custom NetworkPolicy building for sandboxes with advanced
network configurations (custom CIDRs, pod egress rules, or custom DNS).

# Architecture

Most sandboxes use static Helm-installed NetworkPolicies based on pod labels:
  - sandbox-default-deny: Denies all traffic for pods with app=isola-sandbox
  - sandbox-allow-internet: Allows internet egress for pods with isola.run/allow-internet=true
  - sandbox-allow-cluster-dns: Allows cluster DNS for pods with isola.run/allow-cluster-dns=true

This package builds custom NetworkPolicies only when needed:
  - Custom egress CIDRs are specified
  - Pod-based egress rules are specified
  - Custom nameservers are specified (may be private IPs)
*/
package network

import (
	"net/netip"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	sandboxv1alpha1 "github.com/isola-ai/isola-sb/services/isola-operator/api/v1alpha1"
	"github.com/isola-ai/isola-sb/services/isola-operator/internal/controller/network/cidr"
)

const (
	// SandboxIDLabelKey is the label used to identify the sandbox pod.
	SandboxIDLabelKey = "sandbox.isola.run/id"
)

// egressCIDR holds a validated egress prefix with its computed exceptions.
type egressCIDR struct {
	Prefix netip.Prefix
	Except []netip.Prefix
}

// BuildCustomNetworkPolicy creates a K8s NetworkPolicy for a sandbox with custom
// network configuration (CIDRs, pod rules, or nameservers).
//
// The policy selects the specific sandbox pod using {SandboxIDLabelKey}={sandboxName}.
//
// This is only called when the sandbox needs custom rules beyond the static
// Helm-installed policies. Returns nil if no custom policy is needed.
//
// Returns error if CIDRs are invalid or if egress CIDRs completely overlap with blocked ranges.
func BuildCustomNetworkPolicy(sandboxName, namespace string, network *sandboxv1alpha1.NetworkSpec) (*networkingv1.NetworkPolicy, error) {
	if network == nil {
		return nil, nil
	}

	// Parse and validate egress CIDRs, collecting canonical prefixes.
	// Deduplicate to avoid redundant rules (uses canonical string as key).
	egressCIDRs := make([]egressCIDR, 0, len(network.AllowedEgressCIDRs))
	seenEgress := make(map[string]bool)
	for _, cidrStr := range network.AllowedEgressCIDRs {
		prefix, err := cidr.ParsePrefix(cidrStr)
		if err != nil {
			return nil, err
		}
		key := prefix.String()
		if seenEgress[key] {
			continue
		}
		seenEgress[key] = true
		except, err := cidr.ComputeExcept(prefix)
		if err != nil {
			return nil, err
		}
		egressCIDRs = append(egressCIDRs, egressCIDR{Prefix: prefix, Except: except})
	}

	// Parse nameserver IPs - egress to these IPs is automatically allowed on port 53.
	// Always create rules even with allowAllInternet, since nameservers may be private IPs
	// that fall within blocked ranges (10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16).
	var dnsAddrs []netip.Addr
	for _, ipStr := range network.Nameservers {
		addr, err := cidr.ParseDNSServerIP(ipStr)
		if err != nil {
			return nil, err
		}
		dnsAddrs = append(dnsAddrs, addr)
	}

	// Check if we actually need a custom policy
	hasCustomRules := len(egressCIDRs) > 0 || len(dnsAddrs) > 0 || len(network.AllowedEgressPods) > 0
	if !hasCustomRules {
		return nil, nil
	}

	policyName := sandboxName + "-custom-netpol"

	np := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      policyName,
			Namespace: namespace,
			Labels: map[string]string{
				SandboxIDLabelKey:               sandboxName,
				"app.kubernetes.io/managed-by":  "isola-operator",
				"app.kubernetes.io/component":   "sandbox-network",
				"isola.run/custom-network-rule": "true",
			},
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					SandboxIDLabelKey: sandboxName,
				},
			},
			// Only set egress policy type - ingress is handled by default-deny and allow-gw policies
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeEgress,
			},
		},
	}

	np.Spec.Egress = buildEgressRules(egressCIDRs, dnsAddrs, network.AllowedEgressPods)

	return np, nil
}

// buildEgressRules creates NetworkPolicy egress rules from pre-computed CIDRs and pod selectors.
// If dnsServers is non-empty, adds a rule to allow DNS traffic to those IPs.
// If egressPodRules is non-empty, adds rules to allow traffic to those pods.
// Accepts fully validated and computed egressCIDRs from BuildNetworkPolicy.
func buildEgressRules(
	egressCIDRs []egressCIDR,
	dnsServers []netip.Addr,
	egressPodRules []sandboxv1alpha1.EgressPodRule,
) []networkingv1.NetworkPolicyEgressRule {
	var rules []networkingv1.NetworkPolicyEgressRule

	if len(dnsServers) > 0 {
		rules = append(rules, buildDNSServerEgressRule(dnsServers))
	}

	for _, podRule := range egressPodRules {
		rules = append(rules, buildPodSelectorEgressRule(podRule))
	}

	for _, ecidr := range egressCIDRs {
		ipBlock := &networkingv1.IPBlock{
			CIDR: ecidr.Prefix.String(),
		}
		for _, e := range ecidr.Except {
			ipBlock.Except = append(ipBlock.Except, e.String())
		}

		rules = append(rules, networkingv1.NetworkPolicyEgressRule{
			To: []networkingv1.NetworkPolicyPeer{
				{
					IPBlock: ipBlock,
				},
			},
		})
	}

	return rules
}

func buildPodSelectorEgressRule(rule sandboxv1alpha1.EgressPodRule) networkingv1.NetworkPolicyEgressRule {
	egressRule := networkingv1.NetworkPolicyEgressRule{
		To: []networkingv1.NetworkPolicyPeer{
			{
				// Select namespace by the standard kubernetes.io/metadata.name label
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"kubernetes.io/metadata.name": rule.Namespace,
					},
				},
				PodSelector: &rule.PodSelector,
			},
		},
	}

	if len(rule.Ports) > 0 {
		egressRule.Ports = make([]networkingv1.NetworkPolicyPort, 0, len(rule.Ports))
		for _, p := range rule.Ports {
			protocol := p.Protocol
			if protocol == "" {
				protocol = corev1.ProtocolTCP
			}
			port := intstr.FromInt32(p.Port)
			egressRule.Ports = append(egressRule.Ports, networkingv1.NetworkPolicyPort{
				Protocol: &protocol,
				Port:     &port,
			})
		}
	}

	return egressRule
}

// buildDNSServerEgressRule creates an egress rule allowing traffic to DNS server IPs on port 53.
func buildDNSServerEgressRule(dnsServers []netip.Addr) networkingv1.NetworkPolicyEgressRule {
	udpProtocol := corev1.ProtocolUDP
	tcpProtocol := corev1.ProtocolTCP
	port53 := intstr.FromInt(53)

	peers := make([]networkingv1.NetworkPolicyPeer, 0, len(dnsServers))
	for _, addr := range dnsServers {
		// Convert IP to /32 (IPv4) or /128 (IPv6) CIDR
		bits := 32
		if addr.Is6() {
			bits = 128
		}
		prefix := netip.PrefixFrom(addr, bits)
		peers = append(peers, networkingv1.NetworkPolicyPeer{
			IPBlock: &networkingv1.IPBlock{
				CIDR: prefix.String(),
			},
		})
	}

	return networkingv1.NetworkPolicyEgressRule{
		To: peers,
		Ports: []networkingv1.NetworkPolicyPort{
			{Protocol: &udpProtocol, Port: &port53},
			{Protocol: &tcpProtocol, Port: &port53},
		},
	}
}
