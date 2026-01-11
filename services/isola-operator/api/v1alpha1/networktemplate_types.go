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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type NetworkTemplateConditionType string

const (
	// NetworkTemplateReady indicates whether the NetworkPolicy for this template
	// has been successfully created/updated. Sandboxes should not create pods
	// until this condition is True.
	NetworkTemplateReady NetworkTemplateConditionType = "Ready"
)

// NetworkPort defines a port for network rules.
// Using a custom type instead of networkingv1.NetworkPolicyPort for a simpler API
type NetworkPort struct {
	// Protocol (TCP or UDP). Defaults to TCP.
	// +kubebuilder:validation:Enum=TCP;UDP
	// +kubebuilder:default=TCP
	// +optional
	Protocol corev1.Protocol `json:"protocol,omitempty"`

	// Port number.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +required
	Port int32 `json:"port"`
}

// EgressPodRule defines a pod-based egress rule.
// This allows sandboxes to communicate with specific pods in the cluster,
// such as in-cluster DNS (kube-dns/CoreDNS) when using ClusterFirst DNS policy.
type EgressPodRule struct {
	// Namespace of the target pods.
	// +kubebuilder:validation:MinLength=1
	// +required
	Namespace string `json:"namespace"`

	// PodSelector selects pods in the namespace.
	// An empty selector ({}) matches all pods in the namespace.
	// +required
	PodSelector metav1.LabelSelector `json:"podSelector"`

	// Ports to allow. If empty, all ports are allowed to the selected pods.
	// +optional
	Ports []NetworkPort `json:"ports,omitempty"`
}

// NetworkTemplateSpec defines network isolation configuration that can be shared across sandboxes.
// When a Sandbox references a NetworkTemplate, a NetworkPolicy is created to enforce these rules.
// The sandbox pod will not be created until the NetworkPolicy is successfully applied.
// Note: This spec is immutable after creation - updates are ignored by the controller.
// To change network rules, create a new NetworkTemplate.
//
// +kubebuilder:validation:XValidation:rule="self.dnsPolicy != 'ClusterFirst' || size(self.allowedEgressPods) > 0",message="allowedEgressPods is required when dnsPolicy is ClusterFirst (must allow egress to cluster DNS)"
type NetworkTemplateSpec struct {
	// DNSPolicy specifies the DNS policy for sandbox pods.
	// - "None": Pod uses only the nameservers from the nameservers field (fully isolated from cluster DNS).
	//   If no nameservers are specified, a sink nameserver (127.0.0.1) is used and DNS queries will fail.
	//   The pod's resolv.conf will have ndots:1 for faster resolution of external domains.
	// - "ClusterFirst": Pod uses cluster DNS, with nameservers combined (duplicates removed).
	//   When using ClusterFirst, you must also add an egress rule via allowedEgressPods to permit
	//   traffic to kube-dns (typically namespace=kube-system, label k8s-app=kube-dns or k8s-app=coredns).
	//
	// TODO: Reconsider the default - ClusterFirst is convenient but None is more secure by default.
	//
	// +kubebuilder:validation:Enum=None;ClusterFirst
	// +kubebuilder:default=ClusterFirst
	// +optional
	DNSPolicy corev1.DNSPolicy `json:"dnsPolicy,omitempty"`

	// Nameservers is a list of DNS server IP addresses.
	// - When dnsPolicy is "None": optional. If empty, 127.0.0.1 is used as a sink (DNS queries fail).
	//   If specified, these are the only nameservers available, and egress to their IPs is automatically allowed.
	// - When dnsPolicy is "ClusterFirst": optional, combined with cluster DNS (duplicates removed by k8s).
	//   If specified, egress to their IPs is automatically allowed.
	// MaxItems=3 because Kubernetes allows at most 3 nameservers in pod DNS config.
	// +kubebuilder:validation:MaxItems=3
	// +kubebuilder:validation:XValidation:rule="self.all(s, isIP(s))",message="must be valid IP addresses"
	// +optional
	Nameservers []string `json:"nameservers,omitempty"`

	// AllowedEgressCIDRs is a list of CIDRs the sandbox is allowed to connect to (outbound traffic).
	// If empty, no CIDR-based egress is allowed (but allowedEgressPods rules may still permit traffic).
	// Risky IPs (cloud metadata 169.254.0.0/16, private ranges, IPv6 link-local fe80::/10) are
	// automatically blocked when the specified CIDR would otherwise allow them.
	// +kubebuilder:validation:items:Pattern=`^((([0-9]{1,3}\.){3}[0-9]{1,3})/(3[0-2]|[12]?[0-9]))|(([0-9a-fA-F:]+)/([0-9]|[1-9][0-9]|1[01][0-9]|12[0-8]))$`
	// +optional
	AllowedEgressCIDRs []string `json:"allowedEgressCIDRs,omitempty"`

	// AllowedEgressPods specifies pods the sandbox can connect to via label selectors.
	// Useful for allowing access to in-cluster services like kube-dns.
	// When using ClusterFirst DNS policy, you must add a rule here to allow egress to DNS.
	//
	// Example for kube-dns/CoreDNS:
	//   - namespace: kube-system
	//     podSelector:
	//       matchLabels:
	//         k8s-app: kube-dns  # or k8s-app: coredns depending on cluster
	//     ports:
	//       - port: 53
	//         protocol: UDP
	//       - port: 53
	//         protocol: TCP
	//
	// +optional
	AllowedEgressPods []EgressPodRule `json:"allowedEgressPods,omitempty"`
}

// NetworkTemplateStatus defines the observed state of NetworkTemplate.
type NetworkTemplateStatus struct {
	// Conditions represent the current state of the NetworkTemplate.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status",description="NetworkPolicy applied"
// +kubebuilder:printcolumn:name="Reason",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].reason",description="Reason for Ready condition"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// NetworkTemplate defines network isolation rules that can be shared across multiple Sandboxes.
// When a Sandbox references a NetworkTemplate, a Kubernetes NetworkPolicy is created that selects
// pods with the label sandbox.isola.run/network-template={template-name}.
type NetworkTemplate struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec defines the network isolation rules
	// +required
	Spec NetworkTemplateSpec `json:"spec"`

	// status defines the observed state of NetworkTemplate
	// +optional
	Status NetworkTemplateStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// NetworkTemplateList contains a list of NetworkTemplate
type NetworkTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NetworkTemplate `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NetworkTemplate{}, &NetworkTemplateList{})
}
