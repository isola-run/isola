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

// RootfsSnapshotConditionType defines condition types for RootfsSnapshot
type RootfsSnapshotConditionType string

const (
	// RootfsSnapshotComplete indicates all containers have been snapshotted.
	// True when all container snapshots succeeded.
	// False when any snapshot failed or is still in progress.
	RootfsSnapshotComplete RootfsSnapshotConditionType = "Complete"
)

// ContainerSnapshotConditionType defines condition types for per-container status
type ContainerSnapshotConditionType string

const (
	// ContainerSnapshotComplete indicates this container's snapshot status.
	// True when snapshot succeeded.
	// False when snapshot failed or is still in progress.
	ContainerSnapshotComplete ContainerSnapshotConditionType = "Complete"
)

// Condition reasons for RootfsSnapshot
const (
	// Ready condition reasons
	ReasonRootfsSnapshotPending    = "Pending"
	ReasonRootfsSnapshotInProgress = "InProgress"
	ReasonRootfsSnapshotSucceeded  = "Succeeded"
	ReasonRootfsSnapshotFailed     = "Failed"

	// RuntimeSupported condition reasons
	ReasonRuntimeSupported    = "Supported"
	ReasonRuntimeNotSupported = "NotSupported"

	// Per-container Ready condition reasons
	ReasonContainerJobCreated = "JobCreated"
	ReasonContainerJobRunning = "JobRunning"
	ReasonContainerSucceeded  = "Succeeded"
	ReasonContainerFailed     = "Failed"
)

// RootfsSnapshotSpec defines the desired state of RootfsSnapshot
type RootfsSnapshotSpec struct {
	// SandboxName is the name of the sandbox to snapshot.
	// The sandbox must be in the same namespace as this RootfsSnapshot.
	// +required
	// +kubebuilder:validation:MinLength=1
	SandboxName string `json:"sandboxName"`

	// ContainerNames optionally specifies which containers to snapshot.
	// Each name must match a non-init container in the pod.
	// If empty, all non-init containers in the pod are snapshotted.
	// +optional
	ContainerNames []string `json:"containerNames,omitempty"`

	// ActiveDeadlineSeconds specifies the duration in seconds for each snapshot job.
	// If a job does not complete within this time, it will be terminated.
	// +optional
	// +kubebuilder:default=300
	// +kubebuilder:validation:Minimum=1
	ActiveDeadlineSeconds *int64 `json:"activeDeadlineSeconds,omitempty"`

	// TTLSecondsAfterFinished limits the lifetime of a RootfsSnapshot that has
	// finished execution (either all containers succeeded or any failed).
	// If set, the RootfsSnapshot will be automatically deleted after this many
	// seconds after it finishes.
	// If not set, the RootfsSnapshot will be deleted after a default value.
	// 0 means immediate deletion upon completion.
	// +optional
	// +kubebuilder:default=300
	// +kubebuilder:validation:Minimum=0
	TTLSecondsAfterFinished *int32 `json:"ttlSecondsAfterFinished,omitempty"`
}

// ContainerSnapshotStatus tracks the snapshot status of a single container
type ContainerSnapshotStatus struct {
	// ContainerName is the name of the container being snapshotted
	// +required
	ContainerName string `json:"containerName"`

	// ContainerID is the container ID that is being snapshotted
	// +optional
	ContainerID string `json:"containerID,omitempty"`

	// SnapshotURI is the full bucket URI where the snapshot tarball is stored
	// +optional
	SnapshotURI string `json:"snapshotURI,omitempty"`

	// SnapshotKey is the object key within the bucket (without the bucket URL prefix)
	// +optional
	SnapshotKey string `json:"snapshotKey,omitempty"`

	// Conditions represent the status of this container's snapshot
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// RootfsSnapshotStatus defines the observed state of RootfsSnapshot
type RootfsSnapshotStatus struct {
	// Conditions represent the overall RootfsSnapshot state
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ContainerSnapshots tracks the status of each container being snapshotted
	// +optional
	ContainerSnapshots []ContainerSnapshotStatus `json:"containerSnapshots,omitempty"`

	// Revision is the snapshot revision number for this sandbox
	// +optional
	Revision int32 `json:"revision,omitempty"`

	// StartedAt is when the first snapshot job was created
	// +optional
	StartedAt *metav1.Time `json:"startedAt,omitempty"`

	// CompletedAt is when all snapshots finished (success or failure)
	// +optional
	CompletedAt *metav1.Time `json:"completedAt,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=rfs
// +kubebuilder:printcolumn:name="Complete",type="string",JSONPath=".status.conditions[?(@.type=='Complete')].status",description="All snapshots completed successfully"
// +kubebuilder:printcolumn:name="Reason",type="string",JSONPath=".status.conditions[?(@.type=='Complete')].reason",description="Reason for Complete condition"
// +kubebuilder:printcolumn:name="Sandbox",type="string",JSONPath=".spec.sandboxName",description="Sandbox being snapshotted"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// RootfsSnapshot represents a request to snapshot a sandbox's container root filesystems.
// The controller creates Jobs that use gvisor's runsc to tar the overlay2 upper layer.
// Each container in the sandbox gets its own rootfs snapshot Job.
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
