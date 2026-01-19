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

// No phases. The pattern of using phase is deprecated. Newer API types should use conditions instead.

type SandboxConditionType string

const (
	// The aggregate condition.
	SandboxReady SandboxConditionType = "Ready"
	// Sandbox pod is up and running.
	SandboxPodReady SandboxConditionType = "PodReady"
	// Network is configured
	SandboxNetworkConfigured SandboxConditionType = "NetworkConfigured"
	// set when sandbox is past its timeout
	// todo benl: necessary? helpful?
	SandboxTimedOut SandboxConditionType = "TimedOut"
	// Filesystem snapshotting is in progress
	SandboxSnapshottingFilesystem SandboxConditionType = "SnapshottingFilesystem"
)

// Network capability label keys - added to pods to trigger shared NetworkPolicies
const (
	LabelAllowInternet   = "isola.run/allow-internet"
	LabelAllowClusterDNS = "isola.run/allow-cluster-dns"
	LabelSandboxName     = "isola.run/sandbox"
)

// SandboxTemplateReference identifies a SandboxTemplate in the same namespace.
type SandboxTemplateReference struct {
	// Name of the SandboxTemplate in the same namespace.
	// The referenced SandboxTemplate provides the pod configuration (containers, initContainers, etc.)
	// and lifecycle settings (timeout, shutdown policy) for this Sandbox.
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// NetworkPort defines a port for network rules.
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

// NetworkConfig specifies the network isolation configuration for a Sandbox.
// By default, sandboxes have no network access (deny-all). Enable capabilities using the boolean flags.
// Custom CIDR rules generate per-sandbox NetworkPolicies owned by the sandbox.
type NetworkConfig struct {
	// AllowInternet enables outbound internet access.
	// When true, adds label isola.run/allow-internet=true which is selected by the
	// pre-installed isola-allow-internet NetworkPolicy.
	// This allows egress to 0.0.0.0/0 EXCEPT private ranges (10.0.0.0/8, 172.16.0.0/12,
	// 192.168.0.0/16), link-local (169.254.0.0/16), and CGNAT (100.64.0.0/10).
	// +optional
	AllowInternet bool `json:"allowInternet,omitempty"`

	// AllowClusterDNS enables access to cluster DNS (kube-dns/CoreDNS).
	// When true, adds label isola.run/allow-cluster-dns=true and sets DNSPolicy to ClusterFirst.
	// The pre-installed isola-allow-cluster-dns NetworkPolicy allows egress to kube-dns pods.
	// +optional
	AllowClusterDNS bool `json:"allowClusterDNS,omitempty"`

	// DNS specifies custom nameserver IPs for the sandbox.
	// When specified with AllowClusterDNS=false, sets DNSPolicy to None with these nameservers.
	// When specified with AllowClusterDNS=true, these are added as additional nameservers.
	// Egress to these IPs on port 53 is automatically allowed.
	// MaxItems=3 because Kubernetes allows at most 3 nameservers in pod DNS config.
	// +kubebuilder:validation:MaxItems=3
	// +kubebuilder:validation:XValidation:rule="self.all(s, isIP(s))",message="must be valid IP addresses"
	// +optional
	DNS []string `json:"dns,omitempty"`

	// AllowedCIDRs specifies custom CIDR-based egress rules.
	// Creates a per-sandbox NetworkPolicy owned by the sandbox (garbage-collected on deletion).
	// Use this for allowing access to specific internal services or IP ranges.
	// +optional
	AllowedCIDRs []CIDREgressRule `json:"allowedCIDRs,omitempty"`

	// AllowedPods specifies pod-selector based egress rules.
	// Creates per-sandbox NetworkPolicy rules for communicating with specific pods.
	// +optional
	AllowedPods []PodEgressRule `json:"allowedPods,omitempty"`
}

// CIDREgressRule defines an egress rule to a specific CIDR range.
type CIDREgressRule struct {
	// CIDR is the IP range to allow egress to (e.g., "10.200.0.0/24").
	// +kubebuilder:validation:Pattern=`^((([0-9]{1,3}\.){3}[0-9]{1,3})/(3[0-2]|[12]?[0-9]))|(([0-9a-fA-F:]+)/([0-9]|[1-9][0-9]|1[01][0-9]|12[0-8]))$`
	// +required
	CIDR string `json:"cidr"`

	// Ports restricts the rule to specific ports. If empty, all ports are allowed.
	// +optional
	Ports []NetworkPort `json:"ports,omitempty"`
}

// PodEgressRule defines an egress rule to pods matching a selector.
type PodEgressRule struct {
	// Namespace of the target pods.
	// +kubebuilder:validation:MinLength=1
	// +required
	Namespace string `json:"namespace"`

	// PodSelector selects pods in the namespace.
	// An empty selector ({}) matches all pods in the namespace.
	// +required
	PodSelector metav1.LabelSelector `json:"podSelector"`

	// Ports restricts the rule to specific ports. If empty, all ports are allowed.
	// +optional
	Ports []NetworkPort `json:"ports,omitempty"`
}

// SandboxSpec defines the desired state of Sandbox
type SandboxSpec struct {
	// TemplateRef references the SandboxTemplate to inherit pod configuration from.
	// The SandboxTemplate must exist in the same namespace as this Sandbox.
	// +required
	TemplateRef SandboxTemplateReference `json:"templateRef"`

	// Network specifies the network isolation configuration for this sandbox.
	// If not specified, the sandbox has no network access (default deny-all via base policy).
	// Use boolean flags (allowInternet, allowClusterDNS) to enable common capabilities.
	// Use allowedCIDRs and allowedPods for custom egress rules.
	// +optional
	Network *NetworkConfig `json:"network,omitempty"`
}

// HasCustomNetworkRules returns true if the sandbox has custom CIDR or pod egress rules
// that require a per-sandbox NetworkPolicy.
func (s *Sandbox) HasCustomNetworkRules() bool {
	if s.Spec.Network == nil {
		return false
	}
	return len(s.Spec.Network.AllowedCIDRs) > 0 ||
		len(s.Spec.Network.AllowedPods) > 0 ||
		len(s.Spec.Network.DNS) > 0
}

// GetCustomNetworkPolicyName returns the name for the per-sandbox NetworkPolicy.
func (s *Sandbox) GetCustomNetworkPolicyName() string {
	return s.Name + "-egress"
}

// GetNetworkLabels returns the labels that should be added to the sandbox pod
// based on the network configuration.
func (s *Sandbox) GetNetworkLabels() map[string]string {
	labels := map[string]string{
		LabelSandboxName: s.Name,
	}
	if s.Spec.Network == nil {
		return labels
	}
	if s.Spec.Network.AllowInternet {
		labels[LabelAllowInternet] = "true"
	}
	if s.Spec.Network.AllowClusterDNS {
		labels[LabelAllowClusterDNS] = "true"
	}
	return labels
}

// todo benl: for now, not storing sandbox pod or snapshotter pod info anywhere in the sandbox CRD
// SandboxStatus defines the observed state of Sandbox.
type SandboxStatus struct {
	// Important: Run "make" to regenerate code after modifying this file

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// TimeoutAt is the absolute time at which the sandbox should be considered timed out.
	// It is set by the controller (derived from template timeout + chosen start time).
	// +optional
	TimeoutAt *metav1.Time `json:"timeoutAt,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status",description="Aggregate readiness"
// +kubebuilder:printcolumn:name="Reason",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].reason",description="Reason for Ready condition"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// Sandbox is the Schema for the sandboxes API
type Sandbox struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec defines the desired state of Sandbox
	// +required
	Spec SandboxSpec `json:"spec"`

	// status defines the observed state of Sandbox
	// +optional
	Status SandboxStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// SandboxList contains a list of Sandbox
type SandboxList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Sandbox `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Sandbox{}, &SandboxList{})
}
