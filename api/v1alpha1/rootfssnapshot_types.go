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

// RootfsSnapshotConditionType defines condition types for RootfsSnapshot
type RootfsSnapshotConditionType string

const (
	// RootfsSnapshotComplete indicates the snapshot completed successfully.
	RootfsSnapshotComplete RootfsSnapshotConditionType = "Complete"

	// RootfsSnapshotFailed indicates the snapshot operation failed.
	RootfsSnapshotFailed RootfsSnapshotConditionType = "Failed"
)

// Condition reasons for RootfsSnapshot
const (
	ReasonRootfsSnapshotSucceeded = "Succeeded"
	ReasonRootfsSnapshotFailed    = "Failed"

	// RuntimeSupported condition reasons
	ReasonRuntimeSupported    = "Supported"
	ReasonRuntimeNotSupported = "NotSupported"
)

// RootfsSnapshotSpec defines the desired state of RootfsSnapshot
type RootfsSnapshotSpec struct {
	// SandboxName is the name of the sandbox to snapshot.
	// The sandbox must be in the same namespace as this RootfsSnapshot.
	// +required
	// +kubebuilder:validation:MinLength=1
	SandboxName string `json:"sandboxName"`

	// SnapshotName is the name used for the snapshot storage key.
	// This is the value callers must pass as rootfsSnapshotSources[].snapshotKey to restore from this snapshot.
	// The SnapshotName validation is crucial as paths may be constructed from it.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	SnapshotName string `json:"snapshotName"`

	// Container is the name of the container to snapshot.
	// If empty, defaults to the first container in the sandbox pod.
	// +optional
	Container string `json:"container,omitempty"`

	// ActiveDeadlineSeconds specifies the duration in seconds for the snapshot job.
	// If the job does not complete within this time, it will be terminated.
	// +optional
	// +kubebuilder:default=300
	// +kubebuilder:validation:Minimum=1
	ActiveDeadlineSeconds *int64 `json:"activeDeadlineSeconds,omitempty"`

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

	// SnapshotKey is the object key within the bucket (e.g. "rootfssnapshots/<name>.tar")
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
// +kubebuilder:printcolumn:name="Complete",type="string",JSONPath=".status.conditions[?(@.type=='Complete')].status",description="Snapshot completed successfully"
// +kubebuilder:printcolumn:name="Failed",type="string",JSONPath=".status.conditions[?(@.type=='Failed')].status",description="Snapshot failed"
// +kubebuilder:printcolumn:name="Sandbox",type="string",JSONPath=".spec.sandboxName",description="Sandbox being snapshotted"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// RootfsSnapshot represents a request to snapshot a sandbox's root filesystem.
// The controller creates a Job that uses gvisor's runsc to tar the overlay2 upper layer.
//
// Only changes to the overlay rootfs are captured. Files written to separate mounts
// (e.g. /tmp, which gVisor mounts as an internal tmpfs for performance) are NOT included
// in the snapshot. To persist data across snapshots, write to directories on the root
// filesystem such as /root or /home.
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
