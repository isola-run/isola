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

// GvisorCheckpointConditionType defines condition types for GvisorCheckpoint
type GvisorCheckpointConditionType string

const (
	// GvisorCheckpointComplete indicates the checkpoint has completed.
	// True when checkpoint succeeded.
	// False when checkpoint failed or is still in progress.
	GvisorCheckpointComplete GvisorCheckpointConditionType = "Complete"
)

// Condition reasons for GvisorCheckpoint
const (
	ReasonGvisorCheckpointPending    = "Pending"
	ReasonGvisorCheckpointInProgress = "InProgress"
	ReasonGvisorCheckpointSucceeded  = "Succeeded"
	ReasonGvisorCheckpointFailed     = "Failed"
)

// GvisorCheckpointSpec defines the desired state of GvisorCheckpoint
type GvisorCheckpointSpec struct {
	// SandboxName is the name of the sandbox to checkpoint.
	// The sandbox must be in the same namespace as this GvisorCheckpoint.
	// +required
	// +kubebuilder:validation:MinLength=1
	SandboxName string `json:"sandboxName"`

	// ContainerName specifies which container to checkpoint.
	// Must match a non-init container in the pod.
	// +required
	// +kubebuilder:validation:MinLength=1
	ContainerName string `json:"containerName"`

	// ActiveDeadlineSeconds specifies the duration in seconds for the checkpoint job.
	// If the job does not complete within this time, it will be terminated.
	// +optional
	// +kubebuilder:default=300
	// +kubebuilder:validation:Minimum=1
	ActiveDeadlineSeconds *int64 `json:"activeDeadlineSeconds,omitempty"`

	// TTLSecondsAfterFinished limits the lifetime of a GvisorCheckpoint that has
	// finished execution (succeeded or failed).
	// If set, the GvisorCheckpoint will be automatically deleted after this many
	// seconds after it finishes.
	// If not set, the GvisorCheckpoint will be deleted after a default value.
	// 0 means immediate deletion upon completion.
	// +optional
	// +kubebuilder:default=300
	// +kubebuilder:validation:Minimum=0
	TTLSecondsAfterFinished *int32 `json:"ttlSecondsAfterFinished,omitempty"`
}

// GvisorCheckpointStatus defines the observed state of GvisorCheckpoint
type GvisorCheckpointStatus struct {
	// Conditions represent the overall GvisorCheckpoint state
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ContainerName is the name of the container being checkpointed
	// +optional
	ContainerName string `json:"containerName,omitempty"`

	// ContainerID is the container ID that was checkpointed
	// +optional
	ContainerID string `json:"containerID,omitempty"`

	// CheckpointKey is the object key within the bucket (without the bucket URL prefix)
	// +optional
	CheckpointKey string `json:"checkpointKey,omitempty"`

	// Revision is the checkpoint revision number for this sandbox
	// +optional
	Revision int32 `json:"revision,omitempty"`

	// StartedAt is when the checkpoint job was created
	// +optional
	StartedAt *metav1.Time `json:"startedAt,omitempty"`

	// CompletedAt is when the checkpoint finished (success or failure)
	// +optional
	CompletedAt *metav1.Time `json:"completedAt,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=gcp
// +kubebuilder:printcolumn:name="Complete",type="string",JSONPath=".status.conditions[?(@.type=='Complete')].status",description="Checkpoint completed successfully"
// +kubebuilder:printcolumn:name="Reason",type="string",JSONPath=".status.conditions[?(@.type=='Complete')].reason",description="Reason for Complete condition"
// +kubebuilder:printcolumn:name="Sandbox",type="string",JSONPath=".spec.sandboxName",description="Sandbox being checkpointed"
// +kubebuilder:printcolumn:name="Container",type="string",JSONPath=".spec.containerName",description="Container being checkpointed"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// GvisorCheckpoint represents a request to checkpoint a sandbox container's full state.
// The controller creates a Job that uses gvisor's runsc to checkpoint the container,
// capturing memory, CPU registers, and other process state for later restoration.
type GvisorCheckpoint struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec defines the checkpoint request
	// +required
	Spec GvisorCheckpointSpec `json:"spec"`

	// status defines the observed state
	// +optional
	Status GvisorCheckpointStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// GvisorCheckpointList contains a list of GvisorCheckpoint
type GvisorCheckpointList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GvisorCheckpoint `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GvisorCheckpoint{}, &GvisorCheckpointList{})
}
