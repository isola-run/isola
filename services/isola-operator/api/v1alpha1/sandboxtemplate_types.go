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

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

//todo benl: I am not sure how to implement NetworkConfig / NetworkPolicy
// should we use a NetworkPolicy spec or create our own IsolaNetworkPolicy that may or may not be translated into a NetworkPolicy object?
// what to do with possible races between pod creation and NetworkPolicy application (if the policy is from outside and not enforced by, say, a gVisor patch)? init container that waits?

// NetworkConfig configure network isolation and limits
type NetworkConfig struct {
	// AllowedIncoming is a list of CIDRs allowed to connect to the sandbox
	// +optional
	AllowedIncoming []string `json:"allowedIncoming,omitempty"`

	// AllowedOutgoing is a list of CIDRs the sandbox is allowed to connect to
	// +optional
	AllowedOutgoing []string `json:"allowedOutgoing,omitempty"`
}

// SandboxShutdownPolicy defines the policy for handling sandbox termination
// +kubebuilder:validation:Enum=Delete;SnapshotFilesystem
type SandboxShutdownPolicy string

const (
	ShutdownPolicyDelete             SandboxShutdownPolicy = "Delete"
	ShutdownPolicySnapshotFilesystem SandboxShutdownPolicy = "SnapshotFilesystem"
)

// ShutdownPolicy controls how the sandbox is handled when it ends
type ShutdownPolicy struct {
	// Policy determines the action taken when the sandbox shuts down
	// +required
	// +kubebuilder:default=Delete
	// +kubebuilder:validation:Enum=Delete;SnapshotFilesystem
	Policy SandboxShutdownPolicy `json:"policy"`

	// SnapshotTimeoutSeconds sets how long to wait for filesystem snapshotting to finish
	// before giving up. Only used when Policy is SnapshotFilesystem.
	// +optional
	// +kubebuilder:default=300
	// +kubebuilder:validation:Minimum=1
	SnapshotTimeoutSeconds *int64 `json:"snapshotTimeoutSeconds,omitempty"`
}


// SandboxTemplateSpec defines the desired state of SandboxTemplate
type SandboxTemplateSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	// The following markers will use OpenAPI v3 schema to validate the value
	// More info: https://book.kubebuilder.io/reference/markers/crd-validation.html

	// PodTemplate describes the pod that will be created to run the sandbox.
	// The Sandbox controller will override specific security settings (runtimeClassName, etc.)
	// but allows users to define containers, volumes, and env vars.
	// TODO benl: allow defining volumes? everything?
	//
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Schemaless
	// + required
	// todo benl: I am extremly unsure about the kubebuilder attributes above
	//todo benl: agent-sandbox use PodTemplate instead of PodTemplateSpec but from my research the PodTemplateSpec is far more popular and suitable
	// todo benl: enforce PreemptionPolicy never in validating webhook?
	// todo benl: verify total resources make sense, etc
	PodTemplate corev1.PodTemplateSpec `json:"podTemplate"`

	// todo benl: is this the right place for it? should we allow / ready to change this policy at runtime?
	// Network configures network isolation and limits
	// +optional
	Network *NetworkConfig `json:"network,omitempty"`

	// TimeoutSeconds defines how long the sandbox runs before being terminated
	// +kubebuilder:validation:Minimum=1
	// +optional
	TimeoutSeconds *int64 `json:"timeoutSeconds,omitempty"`

	// todo benl: utilize TerminationGracePeriodSeconds like RabbitMQ operator to allow shutdown hooks to execute
	//todo benl: think on how to implement shutdown policy to allow multiple toggles
	// ShutdownPolicy defines what to do when the sandbox ends
	// +optional
	ShutdownPolicy *ShutdownPolicy `json:"shutdownPolicy,omitempty"`

	//todo benl: add runtime class here? (runc / runsc)
}

// SandboxTemplateStatus defines the observed state of SandboxTemplate.
type SandboxTemplateStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// For Kubernetes API conventions, see:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties

	// conditions represent the current state of the SandboxTemplate resource.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	//
	// Standard condition types include:
	// - "Available": the resource is fully functional
	// - "Progressing": the resource is being created or updated
	// - "Degraded": the resource failed to reach or maintain its desired state
	//
	// The status of each condition is one of True, False, or Unknown.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// SandboxTemplate is the Schema for the sandboxtemplates API
type SandboxTemplate struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec defines the desired state of SandboxTemplate
	// +required
	Spec SandboxTemplateSpec `json:"spec"`

	// status defines the observed state of SandboxTemplate
	// +optional
	Status SandboxTemplateStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// SandboxTemplateList contains a list of SandboxTemplate
type SandboxTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SandboxTemplate `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SandboxTemplate{}, &SandboxTemplateList{})
}
