// Copyright The Isola Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

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

// SandboxShutdownStrategy defines the policy for handling sandbox termination
// +kubebuilder:validation:Enum=Delete;SnapshotRootfs
type SandboxShutdownStrategy string

const (
	ShutdownStrategyDelete         SandboxShutdownStrategy = "Delete"
	ShutdownStrategySnapshotRootfs SandboxShutdownStrategy = "SnapshotRootfs"
)

// ShutdownPolicy controls how the sandbox is handled when it ends
type ShutdownPolicy struct {
	// Strategy determines the action taken when the sandbox shuts down
	// +optional
	// +kubebuilder:default=Delete
	// +kubebuilder:validation:Enum=Delete;SnapshotRootfs
	Strategy SandboxShutdownStrategy `json:"strategy,omitempty"`

	// ActiveDeadlineSeconds specifies the duration in seconds relative to the startTime
	// that the snapshot job may be active before the system tries to terminate it.
	// Only used when Strategy is SnapshotRootfs.
	// +optional
	// +kubebuilder:default=300
	// +kubebuilder:validation:Minimum=1
	ActiveDeadlineSeconds *int64 `json:"activeDeadlineSeconds,omitempty"`
}

// NetworkSpec defines network isolation for a sandbox.
// If not specified, the sandbox has deny-all egress with sink DNS (queries fail fast).
type NetworkSpec struct {
	// AllowInternetEgress allows egress to 0.0.0.0/0 and ::/0 with blocked ranges
	// (private IPs, cloud metadata, etc.) automatically excepted.
	// Adds label isola.run/allow-internet-egress=true to the pod.
	// +optional
	AllowInternetEgress *bool `json:"allowInternetEgress,omitempty"`

	// AllowClusterDNS allows DNS queries to cluster DNS (kube-dns/CoreDNS).
	// When true: DNSPolicy=ClusterFirst, adds label isola.run/allow-cluster-dns=true
	// When false: DNSPolicy=None, uses nameservers field or sink (127.0.0.1)
	// +optional
	AllowClusterDNS *bool `json:"allowClusterDNS,omitempty"`

	// AllowedEgressCIDRs specifies additional CIDRs the sandbox can reach.
	// Blocked ranges (private IPs, cloud metadata, etc.) are rejected — see ComputeExcept.
	// When allowInternetEgress is true, these CIDRs are already reachable via the static
	// internet policy and do not produce additional NetworkPolicy rules.
	// Creates a custom NetworkPolicy only when allowInternetEgress is false or unset.
	// +kubebuilder:validation:items:Pattern=`^((([0-9]{1,3}\.){3}[0-9]{1,3})/(3[0-2]|[12]?[0-9]))|(([0-9a-fA-F:]+)/([0-9]|[1-9][0-9]|1[01][0-9]|12[0-8]))$`
	// +optional
	AllowedEgressCIDRs []string `json:"allowedEgressCIDRs,omitempty"`

	// Nameservers are DNS server IPs to inject into the pod.
	// When allowClusterDNS=false: These are the only nameservers (or 127.0.0.1 sink if empty).
	// When allowClusterDNS=true: Combined with cluster DNS.
	// When allowInternetEgress is true, nameservers do not produce additional NetworkPolicy rules.
	// MaxItems=3 because Kubernetes allows at most 3 nameservers in pod DNS config.
	// +kubebuilder:validation:MaxItems=3
	// +kubebuilder:validation:XValidation:rule="self.all(s, isIP(s))",message="must be valid IP addresses"
	// +optional
	Nameservers []string `json:"nameservers,omitempty"`
}

// RootfsSnapshotSource specifies a rootfs snapshot to restore into a container at creation time.
type RootfsSnapshotSource struct {
	// SnapshotKey is the storage key of the rootfs snapshot to restore from.
	// This matches the snapshotName field from the RootfsSnapshot CR that created it.
	// Must be a valid RFC 1123 DNS label (lowercase alphanumeric and hyphens only).
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	SnapshotKey string `json:"snapshotKey"`

	// Container is the name of the container to apply the rootfs restore to.
	// If omitted and the sandbox has exactly one user container, that container is used.
	// Required when the sandbox has multiple user containers.
	// +optional
	Container string `json:"container,omitempty"`
}

// SandboxSpec defines the desired state of Sandbox
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.network) || has(self.network)",message="network cannot be removed once set"
// +kubebuilder:validation:XValidation:rule="!has(self.network) || !has(oldSelf.network) || self.network == oldSelf.network",message="network is immutable once set"
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.rootfsSnapshotSources) || has(self.rootfsSnapshotSources)",message="rootfsSnapshotSources cannot be removed once set"
// +kubebuilder:validation:XValidation:rule="!has(self.rootfsSnapshotSources) || !has(oldSelf.rootfsSnapshotSources) || self.rootfsSnapshotSources == oldSelf.rootfsSnapshotSources",message="rootfsSnapshotSources is immutable once set"
type SandboxSpec struct {
	// PodTemplate describes the pod that will be created to run the sandbox.
	// The Sandbox controller will override specific security settings (runtimeClassName, etc.)
	// but allows users to define containers, volumes, and env vars.
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Schemaless
	// +required
	PodTemplate corev1.PodTemplateSpec `json:"podTemplate"`

	// ActiveDeadlineSeconds defines how long the sandbox runs before being shut down
	// +kubebuilder:validation:Minimum=1
	// +optional
	ActiveDeadlineSeconds *int64 `json:"activeDeadlineSeconds,omitempty"`

	// ShutdownPolicy defines what to do when the sandbox ends (defaults to Delete if unspecified)
	// +optional
	ShutdownPolicy *ShutdownPolicy `json:"shutdownPolicy,omitempty"`

	// Network specifies the network isolation configuration for this sandbox.
	// If not specified, the sandbox has deny-all egress.
	// Network configuration is immutable after sandbox creation.
	// +optional
	Network *NetworkSpec `json:"network,omitempty"`

	// RootfsSnapshotSources specifies rootfs snapshots to restore into containers at creation time.
	// Requires gVisor runtime and the rootfs snapshot FUSE mount to be running on the node.
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:XValidation:rule="self.size() <= 1 || self.all(s, s.container != '')",message="container is required when multiple rootfsSnapshotSources are specified"
	RootfsSnapshotSources []RootfsSnapshotSource `json:"rootfsSnapshotSources,omitempty"`
}

// SandboxStatus defines the observed state of Sandbox.
type SandboxStatus struct {
	// Important: Run "make" to regenerate code after modifying this file

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// TimeoutAt is the absolute time at which the sandbox should be considered timed out.
	// It is set by the controller (derived from sandbox timeout).
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
