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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// RootfsSnapshotSucceededCondition indicates whether the snapshot completed successfully.
	RootfsSnapshotSucceededCondition = "Succeeded"
)

// Condition reasons for RootfsSnapshot
const (
	ReasonRootfsSnapshotSucceeded = "Succeeded"
	ReasonRootfsSnapshotFailed    = "Failed"
)

// RootfsSnapshotSpec defines the desired state of RootfsSnapshot
// +kubebuilder:validation:XValidation:rule="self.sandboxName == oldSelf.sandboxName",message="sandboxName is immutable"
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.snapshotName) || has(self.snapshotName)",message="snapshotName cannot be removed once set"
// +kubebuilder:validation:XValidation:rule="!has(self.snapshotName) || !has(oldSelf.snapshotName) || self.snapshotName == oldSelf.snapshotName",message="snapshotName is immutable once set"
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.containerName) || has(self.containerName)",message="containerName cannot be removed once set"
// +kubebuilder:validation:XValidation:rule="!has(self.containerName) || !has(oldSelf.containerName) || self.containerName == oldSelf.containerName",message="containerName is immutable once set"
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.timeoutSeconds) || has(self.timeoutSeconds)",message="timeoutSeconds cannot be removed once set"
// +kubebuilder:validation:XValidation:rule="!has(self.timeoutSeconds) || !has(oldSelf.timeoutSeconds) || self.timeoutSeconds == oldSelf.timeoutSeconds",message="timeoutSeconds is immutable once set"
type RootfsSnapshotSpec struct {
	// SandboxName is the name of the sandbox to snapshot.
	// The sandbox must be in the same namespace as this RootfsSnapshot.
	// Pattern and length match Sandbox metadata.name (DNS-1123 subdomain, max 47).
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=47
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`
	SandboxName string `json:"sandboxName"`

	// SnapshotName is the name used for the snapshot storage key.
	// This is the value callers must pass as rootfsSnapshotSources[].snapshotName to restore from this snapshot.
	// If omitted, the operator defaults it to the sandbox name.
	// Length and pattern are aligned with RootfsSnapshot.metadata.name (DNS-1123 subdomain, max 59).
	// +optional
	// +kubebuilder:validation:MaxLength=59
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`
	SnapshotName string `json:"snapshotName,omitempty"`

	// ContainerName is the name of the container to snapshot.
	// If empty, defaults to the first container in the sandbox pod.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	ContainerName string `json:"containerName,omitempty"`

	// TimeoutSeconds specifies the duration in seconds for the snapshot job.
	// If the job does not complete within this time, it will be terminated.
	// +optional
	// +kubebuilder:default=300
	// +kubebuilder:validation:Minimum=1
	TimeoutSeconds *int64 `json:"timeoutSeconds,omitempty"`

	// TTLSecondsAfterFinished limits the lifetime of a RootfsSnapshot that has
	// finished execution (succeeded or failed).
	// If set, the RootfsSnapshot will be automatically deleted after this many
	// seconds after it finishes.
	// If not set, the RootfsSnapshot will be deleted after a default value.
	// 0 means immediate deletion upon completion.
	// +optional
	// +kubebuilder:default=300
	// +kubebuilder:validation:Minimum=0
	TTLSecondsAfterFinished *int32 `json:"ttlSecondsAfterFinished,omitempty"`
}

// RootfsSnapshotStatus defines the observed state of RootfsSnapshot
type RootfsSnapshotStatus struct {
	// Conditions represent the overall RootfsSnapshot state
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ContainerID is the container ID that was snapshotted
	// +optional
	ContainerID string `json:"containerID,omitempty"`

	// SnapshotKey is the object key within the bucket (e.g. "rootfssnapshots/<namespace>/<name>.tar")
	// +optional
	SnapshotKey string `json:"snapshotKey,omitempty"`

	// StartTime is when the snapshot job was created
	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// CompletionTime is when the snapshot finished (success or failure).
	// Set on both success and failure (unlike K8s Job which only sets it on success)
	// because it is used for TTL calculation.
	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=rfs
// +kubebuilder:printcolumn:name="Succeeded",type="string",JSONPath=".status.conditions[?(@.type=='Succeeded')].status",description="Snapshot succeeded"
// +kubebuilder:printcolumn:name="Sandbox",type="string",JSONPath=".spec.sandboxName",description="Sandbox being snapshotted"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// RootfsSnapshot represents a request to snapshot a sandbox's root filesystem.
// The controller creates a Job that uses gvisor's runsc to tar the overlay2 upper layer.
//
// Only changes to the overlay rootfs upper layer are captured. Files on separately-mounted
// filesystems (e.g. /tmp, which gVisor mounts as a separate tmpfs) are not included.
// To persist data across snapshots, write to directories on the root filesystem
// (e.g. /root, /home).
//
// metadata.name is capped at 59 characters. The reason is one level deeper
// than the label-value cap that constrains Sandbox. The operator creates a
// snapshot Job named "<rootfsSnapshot.Name>-job", and Kubernetes auto-injects
// the batch.kubernetes.io/job-name label on the Job's pod template whose
// value is the Job name. Label values are capped at 63 characters, so the
// Job name must be at most 63, which means rootfsSnapshot.Name must be at
// most 63 - 4 = 59. Charset is already enforced by Kubernetes' default
// DNS-1123 subdomain validation. See the equivalent comment on Sandbox for
// the kmeta-based path that would let us lift this cap.
//
// This 59-char cap also indirectly constrains Sandbox.metadata.name to 47,
// because the operator derives a "<sandbox.Name>-termination" RootfsSnapshot
// when terminationPolicy.type is SnapshotRootfs. If you raise this cap,
// re-derive the Sandbox cap as well.
// +kubebuilder:validation:XValidation:rule="size(self.metadata.name) <= 59",message="metadata.name must be at most 59 characters because the operator creates a Job named <name>-job whose name is auto-injected as a Kubernetes label value"
type RootfsSnapshot struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec defines the snapshot request
	// +required
	Spec RootfsSnapshotSpec `json:"spec"`

	// status defines the observed state
	// +optional
	Status RootfsSnapshotStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// RootfsSnapshotList contains a list of RootfsSnapshot
type RootfsSnapshotList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RootfsSnapshot `json:"items"`
}

func init() {
	SchemeBuilder.Register(&RootfsSnapshot{}, &RootfsSnapshotList{})
}
