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
Package network provides NetworkPolicy building from NetworkTemplate specifications.

# Architecture: Thin Builder with Future Extensibility

This package implements a "thin builder" pattern that directly translates NetworkTemplate
specs into Kubernetes NetworkPolicy objects. This is intentionally simple for v1.

# Future Enforcement Backend Support

The current implementation generates standard Kubernetes NetworkPolicy objects.
To support alternative enforcement backends (e.g., Cilium, Calico), the architecture
can be extended as follows:

1. Define an Enforcer interface:

	type Enforcer interface {
	    // IsAvailable checks if this enforcer is available in the cluster
	    IsAvailable(ctx context.Context) bool

	    // BuildPolicy creates the backend-specific policy object(s)
	    BuildPolicy(template *NetworkTemplate, opts PolicyOptions) (client.Object, error)

	    // CanHandleFQDN returns true if this enforcer supports FQDN-based rules
	    CanHandleFQDN() bool
	}

2. Implement enforcers for each backend:

  - K8sNetworkPolicyEnforcer (current implementation)
  - CiliumNetworkPolicyEnforcer (generates CiliumNetworkPolicy with FQDN support)
  - CalicoNetworkPolicyEnforcer (generates Calico GlobalNetworkPolicy)

3. Add enforcer detection and selection:

	func DetectEnforcer(ctx context.Context, client client.Client) Enforcer {
	    // Check for Cilium CRDs
	    if hasCRD(ctx, client, "ciliumnetworkpolicies.cilium.io") {
	        return &CiliumEnforcer{}
	    }
	    // Check for Calico CRDs
	    if hasCRD(ctx, client, "networkpolicies.crd.projectcalico.org") {
	        return &CalicoEnforcer{}
	    }
	    // Default to standard K8s NetworkPolicy
	    return &K8sEnforcer{}
	}

4. Extend NetworkTemplateSpec for FQDN support (requires Cilium):

	type NetworkTemplateSpec struct {
	    // ... existing fields ...

	    // AllowedEgressFQDNs specifies FQDN-based egress rules (requires Cilium)
	    // +optional
	    AllowedEgressFQDNs []FQDNSelector `json:"allowedEgressFQDNs,omitempty"`
	}

	type FQDNSelector struct {
	    // MatchName matches an exact FQDN (e.g., "api.openai.com")
	    MatchName string `json:"matchName,omitempty"`
	    // MatchPattern matches FQDNs with wildcards (e.g., "*.github.com")
	    MatchPattern string `json:"matchPattern,omitempty"`
	}

This abstraction is deferred until FQDN/Cilium support is actually needed.
The current thin builder approach minimizes complexity while maintaining extensibility.
*/
package network

import (
	"net/netip"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	sandboxv1alpha1 "github.com/omereli/dev-isola/services/isola-operator/api/v1alpha1"
	"github.com/omereli/dev-isola/services/isola-operator/internal/controller/network/cidr"
)

const (
	NetworkPolicySuffix = "-netpol"

	NetworkTemplateLabelKey = "sandbox.isola.run/network-template"

	// the TLS port used by the isola-agent sidecar
	isolaAgentIngressPort = 8443
)

func GetNetworkPolicyName(templateName string) string {
	return templateName + NetworkPolicySuffix
}

// egressCIDR holds a validated egress prefix with its computed exceptions.
type egressCIDR struct {
	Prefix netip.Prefix
	Except []netip.Prefix
}

// BuildNetworkPolicy creates a K8s NetworkPolicy from a NetworkTemplate.
// The policy selects pods with the label {NetworkTemplateLabelKey}={template-name}.
//
// Parameters:
//   - template: The NetworkTemplate to build the policy from
//   - isolaGatewayNamespace: The namespace where the isola isola-gateway runs
//   - isolaGatewayLabels: Labels to select isola-gateway pods for ingress
//
// Returns error if CIDRs are invalid or if egress CIDRs completely overlap with blocked ranges.
func BuildNetworkPolicy(
	template *sandboxv1alpha1.NetworkTemplate,
	isolaGatewayNamespace string,
	isolaGatewayLabels map[string]string,
) (*networkingv1.NetworkPolicy, error) {
	spec := &template.Spec

	// Parse and validate all CIDRs upfront, collecting canonical prefixes.
	// Deduplicate to avoid redundant rules (uses canonical string as key).
	ingressPrefixes := make([]netip.Prefix, 0, len(spec.AllowedIngress))
	seenIngress := make(map[string]bool)
	for _, cidrStr := range spec.AllowedIngress {
		prefix, err := cidr.ParsePrefix(cidrStr)
		if err != nil {
			return nil, err
		}
		key := prefix.String()
		if seenIngress[key] {
			continue
		}
		seenIngress[key] = true
		ingressPrefixes = append(ingressPrefixes, prefix)
	}

	egressCIDRs := make([]egressCIDR, 0, len(spec.AllowedEgress))
	seenEgress := make(map[string]bool)
	for _, cidrStr := range spec.AllowedEgress {
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

	var dnsAddrs []netip.Addr
	for _, ipStr := range spec.DNSServers {
		addr, err := cidr.ParseDNSServerIP(ipStr)
		if err != nil {
			return nil, err
		}
		dnsAddrs = append(dnsAddrs, addr)
	}

	np := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetNetworkPolicyName(template.Name),
			Namespace: template.Namespace,
			Labels: map[string]string{
				NetworkTemplateLabelKey:        template.Name,
				"app.kubernetes.io/managed-by": "isola-operator",
			},
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					NetworkTemplateLabelKey: template.Name,
				},
			},
			// Always set both policy types for default-deny behavior
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			},
		},
	}

	np.Spec.Ingress = buildIngressRules(ingressPrefixes, isolaGatewayNamespace, isolaGatewayLabels)
	np.Spec.Egress = buildEgressRules(egressCIDRs, dnsAddrs)

	return np, nil
}

// buildIngressRules creates NetworkPolicy ingress rules.
// Accepts pre-validated, canonical prefixes from BuildNetworkPolicy.
func buildIngressRules(
	allowedPrefixes []netip.Prefix,
	isolaGatewayNamespace string,
	isolaGatewayLabels map[string]string,
) []networkingv1.NetworkPolicyIngressRule {
	var rules []networkingv1.NetworkPolicyIngressRule

	// add isola-gateway allow ingress rule - This is required for isola-gateway->sandbox communication
	rules = append(rules, buildAllowIsolaGatewayIngressRule(isolaGatewayNamespace, isolaGatewayLabels))

	// Add CIDR-based rules
	if len(allowedPrefixes) > 0 {
		var peers []networkingv1.NetworkPolicyPeer
		for _, prefix := range allowedPrefixes {
			peers = append(peers, networkingv1.NetworkPolicyPeer{
				IPBlock: &networkingv1.IPBlock{
					CIDR: prefix.String(),
				},
			})
		}
		rules = append(rules, networkingv1.NetworkPolicyIngressRule{
			From: peers,
		})
	}

	return rules
}

func buildAllowIsolaGatewayIngressRule(controllerNamespace string, controllerLabels map[string]string) networkingv1.NetworkPolicyIngressRule {
	tcpProtocol := corev1.ProtocolTCP
	port := intstr.FromInt(isolaAgentIngressPort)

	return networkingv1.NetworkPolicyIngressRule{
		From: []networkingv1.NetworkPolicyPeer{
			{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"kubernetes.io/metadata.name": controllerNamespace,
					},
				},
				PodSelector: &metav1.LabelSelector{
					MatchLabels: controllerLabels,
				},
			},
		},
		Ports: []networkingv1.NetworkPolicyPort{
			{Protocol: &tcpProtocol, Port: &port},
		},
	}
}

// buildEgressRules creates NetworkPolicy egress rules from pre-computed CIDRs.
// If dnsServers is non-empty, adds a rule to allow DNS traffic to those IPs.
// Accepts fully validated and computed egressCIDRs from BuildNetworkPolicy.
func buildEgressRules(
	egressCIDRs []egressCIDR,
	dnsServers []netip.Addr,
) []networkingv1.NetworkPolicyEgressRule {
	var rules []networkingv1.NetworkPolicyEgressRule

	if len(dnsServers) > 0 {
		rules = append(rules, buildDNSServerEgressRule(dnsServers))
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

// buildDNSServerEgressRule creates an egress rule allowing traffic to DNS server IPs on port 53.
func buildDNSServerEgressRule(dnsServers []netip.Addr) networkingv1.NetworkPolicyEgressRule {
	udpProtocol := corev1.ProtocolUDP
	tcpProtocol := corev1.ProtocolTCP
	port53 := intstr.FromInt(53)

	var peers []networkingv1.NetworkPolicyPeer
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
