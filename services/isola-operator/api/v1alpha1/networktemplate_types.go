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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NetworkTemplate condition types
type NetworkTemplateConditionType string

const (
	// NetworkTemplateReady indicates whether the NetworkPolicy for this template
	// has been successfully created/updated. Sandboxes should not create pods
	// until this condition is True.
	NetworkTemplateReady NetworkTemplateConditionType = "Ready"
)

// NetworkTemplateSpec defines network isolation configuration that can be shared across sandboxes.
// When a Sandbox references a NetworkTemplate, a NetworkPolicy is created to enforce these rules.
// The sandbox pod will not be created until the NetworkPolicy is successfully applied.
// Note: This spec is immutable after creation - updates are ignored by the controller.
// To change network rules, create a new NetworkTemplate.
type NetworkTemplateSpec struct {
	// AllowedIngress is a list of CIDRs allowed to connect to the sandbox (inbound traffic).
	// If empty, all ingress traffic is blocked (default-deny) unless some other NetworkPolicy in the cluster allows traffic.
	// todo benl: consider removing AllowedIngress
	// +kubebuilder:validation:UniqueItems=true
	// +kubebuilder:validation:items:Pattern=`^((([0-9]{1,3}\.){3}[0-9]{1,3})/(3[0-2]|[12]?[0-9]))|(([0-9a-fA-F:]+)/([0-9]|[1-9][0-9]|1[01][0-9]|12[0-8]))$`
	// +optional
	AllowedIngress []string `json:"allowedIngress,omitempty"`

	// AllowedEgress is a list of CIDRs the sandbox is allowed to connect to (outbound traffic).
	// If empty, all egress traffic is blocked (default-deny) unless some other NetworkPolicy in the cluster allows traffic.
	// Risky IPs (cloud metadata 169.254.0.0/16, IPv6 link-local fe80::/10) are automatically
	// blocked when the specified CIDR would otherwise allow them.
	// +kubebuilder:validation:UniqueItems=true
	// +kubebuilder:validation:items:Pattern=`^((([0-9]{1,3}\.){3}[0-9]{1,3})/(3[0-2]|[12]?[0-9]))|(([0-9a-fA-F:]+)/([0-9]|[1-9][0-9]|1[01][0-9]|12[0-8]))$`
	// +optional
	AllowedEgress []string `json:"allowedEgress,omitempty"`

	// AllowClusterDNS allows the sandbox to resolve DNS names via the cluster DNS server.
	// When true, egress to the cluster DNS service on port 53 (UDP/TCP) is allowed.
	// Uses DNSSelector to target DNS pods (defaults to k8s-app=kube-dns in kube-system).
	// +optional
	// +kubebuilder:default=false
	AllowClusterDNS *bool `json:"allowClusterDNS,omitempty"`

	// DNSSelector specifies how to select DNS pods when AllowClusterDNS is true.
	// If not specified, defaults to namespace=kube-system with label k8s-app=kube-dns.
	// +optional
	DNSSelector *DNSSelector `json:"dnsSelector,omitempty"`
}

// DNSSelector specifies the namespace and pod labels for cluster DNS.
type DNSSelector struct {
	// Namespace is the namespace where DNS pods run.
	// +kubebuilder:default="kube-system"
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// PodLabels are the labels to match DNS pods.
	// Common values: {"k8s-app": "kube-dns"} or {"app.kubernetes.io/name": "coredns"}
	// +optional
	PodLabels map[string]string `json:"podLabels,omitempty"`
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
