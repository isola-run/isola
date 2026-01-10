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

// FilesystemSnapshotConditionType defines the condition types for FilesystemSnapshot
type FilesystemSnapshotConditionType string

const (
	// FilesystemSnapshotReady indicates whether the snapshot operation has completed
	FilesystemSnapshotReady FilesystemSnapshotConditionType = "Ready"
	// FilesystemSnapshotJobCreated indicates whether the snapshotter job has been created
	FilesystemSnapshotJobCreated FilesystemSnapshotConditionType = "JobCreated"
)

// FilesystemSnapshotSpec defines the desired state of FilesystemSnapshot
type FilesystemSnapshotSpec struct {
	// SandboxRef references the Sandbox being snapshotted
	// +required
	SandboxRef corev1.LocalObjectReference `json:"sandboxRef"`

	// PodName is the name of the pod to snapshot
	// +required
	PodName string `json:"podName"`

	// ContainerID is the container ID to snapshot (without containerd:// prefix)
	// +required
	ContainerID string `json:"containerId"`

	// NodeName is the node where the container is running
	// +required
	NodeName string `json:"nodeName"`

	// SnapshotPath is the output path for the snapshot tar file
	// +required
	SnapshotPath string `json:"snapshotPath"`

	// ActiveDeadlineSeconds specifies the duration in seconds for snapshot job timeout
	// +optional
	// +kubebuilder:default=300
	// +kubebuilder:validation:Minimum=1
	ActiveDeadlineSeconds int64 `json:"activeDeadlineSeconds,omitempty"`
}

// FilesystemSnapshotStatus defines the observed state of FilesystemSnapshot
type FilesystemSnapshotStatus struct {
	// Conditions represent the current state of the FilesystemSnapshot
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// JobName is the name of the snapshotter job created for this snapshot
	// +optional
	JobName string `json:"jobName,omitempty"`

	// StartTime is when the snapshot operation started
	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// CompletionTime is when the snapshot operation completed (success or failure)
	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`

	// Phase indicates the current phase of the snapshot operation
	// +optional
	Phase FilesystemSnapshotPhase `json:"phase,omitempty"`

	// Message provides additional information about the current state
	// +optional
	Message string `json:"message,omitempty"`
}

// FilesystemSnapshotPhase represents the phase of a FilesystemSnapshot
// +kubebuilder:validation:Enum=Pending;Running;Succeeded;Failed
type FilesystemSnapshotPhase string

const (
	// FilesystemSnapshotPhasePending indicates the snapshot is waiting to start
	FilesystemSnapshotPhasePending FilesystemSnapshotPhase = "Pending"
	// FilesystemSnapshotPhaseRunning indicates the snapshot job is running
	FilesystemSnapshotPhaseRunning FilesystemSnapshotPhase = "Running"
	// FilesystemSnapshotPhaseSucceeded indicates the snapshot completed successfully
	FilesystemSnapshotPhaseSucceeded FilesystemSnapshotPhase = "Succeeded"
	// FilesystemSnapshotPhaseFailed indicates the snapshot failed
	FilesystemSnapshotPhaseFailed FilesystemSnapshotPhase = "Failed"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase",description="Phase of the snapshot"
// +kubebuilder:printcolumn:name="Sandbox",type="string",JSONPath=".spec.sandboxRef.name",description="Target sandbox"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// FilesystemSnapshot is the Schema for the filesystemsnapshots API.
// It represents a request to snapshot a sandbox container's filesystem.
type FilesystemSnapshot struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec defines the desired state of FilesystemSnapshot
	// +required
	Spec FilesystemSnapshotSpec `json:"spec"`

	// status defines the observed state of FilesystemSnapshot
	// +optional
	Status FilesystemSnapshotStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// FilesystemSnapshotList contains a list of FilesystemSnapshot
type FilesystemSnapshotList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []FilesystemSnapshot `json:"items"`
}

func init() {
	SchemeBuilder.Register(&FilesystemSnapshot{}, &FilesystemSnapshotList{})
}

// GetSnapshotterJobName returns the name of the snapshotter job for this snapshot
func (fs *FilesystemSnapshot) GetSnapshotterJobName() string {
	return fs.Name + "-job"
}
