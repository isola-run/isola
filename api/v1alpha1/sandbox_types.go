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

const (
	// SandboxSucceededCondition indicates whether the sandbox execution completed successfully.
	SandboxSucceededCondition = "Succeeded"

	// SandboxReadyCondition is the aggregate readiness condition.
	SandboxReadyCondition = "Ready"
)

// SandboxTerminationType defines the policy for handling sandbox termination
// +kubebuilder:validation:Enum=Delete;SnapshotRootfs
type SandboxTerminationType string

const (
	TerminationTypeDelete         SandboxTerminationType = "Delete"
	TerminationTypeSnapshotRootfs SandboxTerminationType = "SnapshotRootfs"
)

// SnapshotRootfsTermination configures the SnapshotRootfs termination type.
type SnapshotRootfsTermination struct {
	// SnapshotName is the name used for the snapshot storage key.
	// This is the value callers must pass as rootfsSnapshotSources[].snapshotName to restore from this snapshot.
	// Same semantic as RootfsSnapshotSpec.SnapshotName.
	// If omitted, the operator defaults it to the sandbox name.
	// Length and pattern are aligned with RootfsSnapshot.metadata.name (DNS-1123 subdomain, max 59).
	// +optional
	// +kubebuilder:validation:MaxLength=59
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`
	SnapshotName string `json:"snapshotName,omitempty"`

	// TimeoutSeconds specifies the duration in seconds for the snapshot job.
	// If the job does not complete within this time, it will be terminated.
	// Same semantic as RootfsSnapshotSpec.TimeoutSeconds.
	// +optional
	// +kubebuilder:validation:Minimum=1
	TimeoutSeconds *int64 `json:"timeoutSeconds,omitempty"`
}

// TerminationPolicy controls how the sandbox is handled before termination.
// +kubebuilder:validation:XValidation:rule="self.type != 'SnapshotRootfs' || has(self.snapshotRootfs)",message="snapshotRootfs is required when type is SnapshotRootfs",reason="FieldValueRequired",fieldPath=".snapshotRootfs"
// +kubebuilder:validation:XValidation:rule="self.type == 'SnapshotRootfs' || !has(self.snapshotRootfs)",message="snapshotRootfs is only valid when type is SnapshotRootfs",reason="FieldValueForbidden",fieldPath=".snapshotRootfs"
type TerminationPolicy struct {
	// Type determines the action taken when the sandbox terminates
	// +optional
	// +kubebuilder:default=Delete
	// +kubebuilder:validation:Enum=Delete;SnapshotRootfs
	Type SandboxTerminationType `json:"type,omitempty"`

	// SnapshotRootfs configures the SnapshotRootfs type.
	// +optional
	SnapshotRootfs *SnapshotRootfsTermination `json:"snapshotRootfs,omitempty"`
}

// EgressRateLimit configures token-bucket egress traffic shaping (gVisor --qdisc=tbf).
// Orthogonal to egress policy: it shapes whatever egress the NetworkPolicy allows.
type EgressRateLimit struct {
	// RateBytesPerSecond is the sustained egress rate in bytes per second.
	// When unset, egress is not rate limited.
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=1000000000000
	RateBytesPerSecond *int64 `json:"rateBytesPerSecond,omitempty"`
}

// Network defines network isolation for a sandbox.
// If not specified, the sandbox has deny-all egress with sink DNS (queries fail fast).
// +kubebuilder:validation:XValidation:rule="!has(self.nameservers) || (has(self.allowIPv6Egress) && self.allowIPv6Egress == true) || self.nameservers.all(s, ip(s).family() == 4)",message="IPv6 nameservers require allowIPv6Egress to be true"
// +kubebuilder:validation:XValidation:rule="!has(self.allowedEgressCIDRs) || (has(self.allowIPv6Egress) && self.allowIPv6Egress == true) || self.allowedEgressCIDRs.all(s, cidr(s).ip().family() == 4)",message="IPv6 CIDRs require allowIPv6Egress to be true"
type Network struct {
	// AllowInternetEgress allows egress to 0.0.0.0/0 (and ::/0 when allowIPv6Egress is true)
	// with blocked ranges (private IPs, cloud metadata, etc.) automatically excepted.
	// Adds label isola.run/allow-ipv4-internet-egress=true to the pod.
	// +optional
	AllowInternetEgress *bool `json:"allowInternetEgress,omitempty"`

	// AllowClusterDNS allows DNS queries to cluster DNS (kube-dns/CoreDNS).
	// When true: DNSPolicy=ClusterFirst, adds label isola.run/allow-cluster-dns=true
	// When false: DNSPolicy=None, uses nameservers field or sink (127.0.0.1)
	// +optional
	AllowClusterDNS *bool `json:"allowClusterDNS,omitempty"`

	// AllowIPv6Egress enables IPv6 in egress configuration.
	// When false (default): only IPv4 CIDRs and nameservers are accepted.
	// When true with allowInternetEgress: also enables IPv6 internet egress.
	// When true without allowInternetEgress: allows IPv6 in allowedEgressCIDRs and nameservers.
	// +optional
	AllowIPv6Egress *bool `json:"allowIPv6Egress,omitempty"`

	// AllowedEgressCIDRs specifies additional CIDRs the sandbox can reach.
	// Blocked ranges (private IPs, cloud metadata, etc.) are rejected.
	// When allowInternetEgress is true, these CIDRs are already reachable via the static
	// internet policy and do not produce additional NetworkPolicy rules.
	// Creates a custom NetworkPolicy only when allowInternetEgress is false or unset.
	// +kubebuilder:validation:MaxItems=16
	// +kubebuilder:validation:items:MaxLength=43
	// +kubebuilder:validation:XValidation:rule="self.all(s, isCIDR(s))",message="must be valid CIDR notation (e.g. 10.0.0.0/8 or 2001:db8::/32)"
	// +listType=atomic
	// +optional
	AllowedEgressCIDRs []string `json:"allowedEgressCIDRs,omitempty"`

	// Nameservers are DNS server IPs to inject into the pod.
	// When allowClusterDNS=false and no egress is configured: 127.0.0.1 sink (DNS fails fast).
	// When allowClusterDNS=false and allowInternetEgress or allowedEgressCIDRs is set: public resolvers are auto-injected.
	// When allowClusterDNS=true: Combined with cluster DNS.
	// Explicit nameservers override auto-injection in all cases.
	// When allowInternetEgress is true, nameservers do not produce additional NetworkPolicy rules.
	// MaxItems=3 because Kubernetes allows at most 3 nameservers in pod DNS config.
	// +kubebuilder:validation:MaxItems=3
	// +kubebuilder:validation:XValidation:rule="self.all(s, isIP(s))",message="must be valid IP addresses"
	// +listType=atomic
	// +optional
	Nameservers []string `json:"nameservers,omitempty"`

	// EgressRateLimit shapes (rate-limits) egress traffic using a gVisor token bucket.
	// +optional
	EgressRateLimit *EgressRateLimit `json:"egressRateLimit,omitempty"`
}

// RootfsSnapshotSource specifies a rootfs snapshot to restore into a container at creation time.
type RootfsSnapshotSource struct {
	// SnapshotName is the name of the rootfs snapshot to restore from.
	// This matches the snapshotName field from the RootfsSnapshot CR that created it.
	// Must be a valid RFC 1123 DNS subdomain (lowercase alphanumeric, hyphens, dots), max 59 chars.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=59
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`
	SnapshotName string `json:"snapshotName"`

	// ContainerName is the name of the container to apply the rootfs restore to.
	// If omitted and the sandbox has exactly one user container, that container is used.
	// Required when the sandbox has multiple user containers.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	ContainerName string `json:"containerName,omitempty"`
}

// SandboxSpec defines the desired state of Sandbox
// +kubebuilder:validation:XValidation:rule="has(self.network) == has(oldSelf.network) && (!has(self.network) || self.network == oldSelf.network)",message="network is immutable",reason="FieldValueForbidden",fieldPath=".network"
// +kubebuilder:validation:XValidation:rule="has(self.rootfsSnapshotSources) == has(oldSelf.rootfsSnapshotSources) && (!has(self.rootfsSnapshotSources) || self.rootfsSnapshotSources == oldSelf.rootfsSnapshotSources)",message="rootfsSnapshotSources is immutable",reason="FieldValueForbidden",fieldPath=".rootfsSnapshotSources"
// +kubebuilder:validation:XValidation:rule="has(self.terminationPolicy) == has(oldSelf.terminationPolicy) && (!has(self.terminationPolicy) || self.terminationPolicy == oldSelf.terminationPolicy)",message="terminationPolicy is immutable",reason="FieldValueForbidden",fieldPath=".terminationPolicy"
// +kubebuilder:validation:XValidation:rule="has(self.timeoutSeconds) == has(oldSelf.timeoutSeconds) && (!has(self.timeoutSeconds) || self.timeoutSeconds == oldSelf.timeoutSeconds)",message="timeoutSeconds is immutable",reason="FieldValueForbidden",fieldPath=".timeoutSeconds"
type SandboxSpec struct {
	// PodTemplate describes the pod that will be created to run the sandbox.
	// The Sandbox controller will override specific security settings (runtimeClassName, etc.)
	// but allows users to define containers, volumes, and env vars.
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Schemaless
	// +required
	PodTemplate corev1.PodTemplateSpec `json:"podTemplate"`

	// TimeoutSeconds defines how long the sandbox runs before the termination process begins
	// +kubebuilder:validation:Minimum=1
	// +optional
	TimeoutSeconds *int64 `json:"timeoutSeconds,omitempty"`

	// StartupTimeoutSeconds defines how long the sandbox has to become Ready
	// after pod creation. If the pod hasn't reached Ready within this time,
	// the sandbox is marked as failed.
	// +optional
	// +kubebuilder:default=90
	// +kubebuilder:validation:Minimum=1
	StartupTimeoutSeconds *int64 `json:"startupTimeoutSeconds,omitempty"`

	// TerminationPolicy defines what to do when the sandbox ends (defaults to Delete if unspecified)
	// +optional
	TerminationPolicy *TerminationPolicy `json:"terminationPolicy,omitempty"`

	// Network specifies the network isolation configuration for this sandbox.
	// If not specified, the sandbox has deny-all egress.
	// Once set, network configuration is immutable.
	// +optional
	Network *Network `json:"network,omitempty"`

	// RootfsSnapshotSources specifies rootfs snapshots to restore into containers at creation time.
	// Requires gVisor runtime and the snapshot-mounter NFS mount to be running on the node.
	// Only the overlay rootfs upper layer is captured/restored. Files on separately-mounted
	// filesystems (e.g. /tmp, which gVisor mounts as a separate tmpfs) are excluded.
	// Write data to paths on the root filesystem (e.g. /root, /home) to ensure it
	// survives snapshot and restore.
	// +optional
	// +listType=atomic
	// +kubebuilder:validation:MaxItems=16
	// +kubebuilder:validation:XValidation:rule="self.size() <= 1 || self.all(s, s.containerName != '')",message="containerName is required when multiple rootfsSnapshotSources are specified",reason="FieldValueRequired"
	// +kubebuilder:validation:XValidation:rule="self.size() <= 1 || self.all(i, self.all(j, i == j || i.containerName != j.containerName))",message="each rootfsSnapshotSource must target a different container",reason="FieldValueDuplicate"
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

	// TerminationDeadlineAt is the absolute time by which the termination policy must complete.
	// Set once by the controller when finalization begins (anchored to DeletionTimestamp).
	// +optional
	TerminationDeadlineAt *metav1.Time `json:"terminationDeadlineAt,omitempty"`

	// PodIP is the IP address of the sandbox pod.
	// +optional
	PodIP string `json:"podIP,omitempty"`

	// SidecarVersion mirrors the isola.run/sidecar-version annotation on the
	// sandbox pod, which is stamped with the isola-operator GitVersion at pod
	// creation time. It is a proxy for the sandbox-sidecar build shipped into
	// the pod: isola-operator and sandbox-sidecar are released together, so the
	// operator's own version identifies the sidecar build. Consumers (api-gateway)
	// use this for capability checks against long-running sandboxes whose sidecar
	// image predates later operator upgrades.
	// +optional
	SidecarVersion string `json:"sidecarVersion,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=sb
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status",description="Aggregate readiness"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Reason",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].reason",priority=1,description="Reason for Ready condition"
// +kubebuilder:validation:XValidation:rule="size(self.metadata.name) <= 47",message="metadata.name must be at most 47 characters: the operator writes it into label values (capped at 63) and derives <name>-termination RootfsSnapshot from it (itself capped at 59)",reason="FieldValueInvalid"
// Sandbox is the Schema for the sandboxes API.
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
	registerTypes(&Sandbox{}, &SandboxList{})
}
