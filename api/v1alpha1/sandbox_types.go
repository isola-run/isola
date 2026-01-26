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

// EgressPodRule defines a pod-based egress rule.
// This allows sandboxes to communicate with specific pods in the cluster.
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

// NetworkSpec defines network isolation for a sandbox.
// If not specified, the sandbox has deny-all egress with sink DNS (queries fail fast).
type NetworkSpec struct {
	// AllowAllInternet allows egress to 0.0.0.0/0 and ::/0 with blocked ranges
	// (private IPs, cloud metadata, etc.) automatically excepted.
	// Adds label isola.run/allow-internet=true to the pod.
	// +kubebuilder:default=false
	AllowAllInternet bool `json:"allowAllInternet,omitempty"`

	// AllowClusterDNS allows DNS queries to cluster DNS (kube-dns/CoreDNS).
	// When true: DNSPolicy=ClusterFirst, adds label isola.run/allow-cluster-dns=true
	// When false: DNSPolicy=None, uses nameservers field or sink (127.0.0.1)
	// +kubebuilder:default=false
	AllowClusterDNS bool `json:"allowClusterDNS,omitempty"`

	// AllowedEgressCIDRs specifies additional CIDRs the sandbox can reach.
	// Blocked ranges are automatically excepted. Creates a custom NetworkPolicy.
	// +kubebuilder:validation:items:Pattern=`^((([0-9]{1,3}\.){3}[0-9]{1,3})/(3[0-2]|[12]?[0-9]))|(([0-9a-fA-F:]+)/([0-9]|[1-9][0-9]|1[01][0-9]|12[0-8]))$`
	// +optional
	AllowedEgressCIDRs []string `json:"allowedEgressCIDRs,omitempty"`

	// AllowedEgressPods specifies pods the sandbox can reach via selectors.
	// Uses full LabelSelector (matchLabels + matchExpressions) for flexibility.
	// Creates a custom NetworkPolicy.
	// +optional
	AllowedEgressPods []EgressPodRule `json:"allowedEgressPods,omitempty"`

	// Nameservers are DNS server IPs to inject into the pod.
	// When allowClusterDNS=false: These are the only nameservers (or 127.0.0.1 sink if empty).
	// When allowClusterDNS=true: Combined with cluster DNS.
	// When specified with allowAllInternet=false, creates custom policy for DNS egress.
	// MaxItems=3 because Kubernetes allows at most 3 nameservers in pod DNS config.
	// +kubebuilder:validation:MaxItems=3
	// +kubebuilder:validation:XValidation:rule="self.all(s, isIP(s))",message="must be valid IP addresses"
	// +optional
	Nameservers []string `json:"nameservers,omitempty"`
}

// SandboxSpec defines the desired state of Sandbox
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.network) || has(self.network)",message="network cannot be removed once set"
// +kubebuilder:validation:XValidation:rule="!has(self.network) || !has(oldSelf.network) || self.network == oldSelf.network",message="network is immutable once set"
type SandboxSpec struct {
	// TemplateRef references the SandboxTemplate to inherit pod configuration from.
	// The SandboxTemplate must exist in the same namespace as this Sandbox.
	// +required
	TemplateRef SandboxTemplateReference `json:"templateRef"`

	// Network specifies the network isolation configuration for this sandbox.
	// If not specified, the sandbox has deny-all egress with sink DNS (queries fail fast).
	// Network configuration is immutable after sandbox creation.
	// +optional
	Network *NetworkSpec `json:"network,omitempty"`
}

// NeedsCustomNetworkPolicy returns true if this sandbox requires a custom NetworkPolicy
// (beyond the Helm-installed static policies).
func (s *Sandbox) NeedsCustomNetworkPolicy() bool {
	if s.Spec.Network == nil {
		return false
	}
	n := s.Spec.Network
	// Custom policy needed for:
	// - Custom CIDR rules
	// - Pod egress rules
	// - Custom nameservers (even with allowAllInternet, nameservers may be in blocked private ranges)
	return len(n.AllowedEgressCIDRs) > 0 ||
		len(n.AllowedEgressPods) > 0 ||
		len(n.Nameservers) > 0
}

// GetCustomNetworkPolicyName returns the name for this sandbox's custom NetworkPolicy.
func (s *Sandbox) GetCustomNetworkPolicyName() string {
	return s.Name + "-custom-netpol"
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

	// PodIP is the IP address of the sandbox pod.
	// +optional
	PodIP string `json:"podIP,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=sb
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status",description="Aggregate readiness"
// +kubebuilder:printcolumn:name="Template",type="string",JSONPath=".spec.templateRef.name",description="SandboxTemplate reference"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Reason",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].reason",priority=1,description="Reason for Ready condition"
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
