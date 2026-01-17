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

const (
	DefaultNetworkTemplate string = "isola-isolated"
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

// NetworkTemplateReference identifies a NetworkTemplate in the same namespace.
type NetworkTemplateReference struct {
	// Name of the NetworkTemplate in the same namespace.
	// The referenced NetworkTemplate defines the network isolation rules.
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// todo benl: neither specific
// NetworkConfig specifies the network isolation configuration for a Sandbox.
// Exactly one of TemplateRef or Spec must be specified.
// +kubebuilder:validation:XValidation:rule="has(self.templateRef) || has(self.spec)",message="At least one of 'templateRef' or 'spec' is required."
type NetworkConfig struct {
	// TemplateRef references an existing NetworkTemplate in the same namespace.
	// The referenced NetworkTemplate is not owned by this sandbox and will persist
	// independently of sandbox lifecycle.
	// +optional
	TemplateRef *NetworkTemplateReference `json:"templateRef,omitempty"`

	// Spec embeds network isolation rules directly in the sandbox.
	// When specified, the controller creates a NetworkTemplate CR
	// that is owned by this sandbox and garbage-collected
	// when the sandbox is deleted.
	// Note: This spec is immutable after sandbox creation - changes are ignored.
	// +optional
	Spec *NetworkTemplateSpec `json:"spec,omitempty"`
}

// SandboxSpec defines the desired state of Sandbox
type SandboxSpec struct {
	// TemplateRef references the SandboxTemplate to inherit pod configuration from.
	// The SandboxTemplate must exist in the same namespace as this Sandbox.
	// +required
	TemplateRef SandboxTemplateReference `json:"templateRef"`

	// Network specifies the network isolation configuration for this sandbox.
	// Can either reference a shared NetworkTemplate or embed network rules directly.
	// If not specified, no NetworkPolicy is created (unrestricted network access).
	// When specified, must contain exactly one of templateRef or spec.
	// +optional
	Network *NetworkConfig `json:"network,omitempty"`
}

// GetNetworkTemplateName returns the effective NetworkTemplate name for this sandbox.
// - For templateRef: returns the referenced template name
// - For spec: returns "{sandbox-name}-net"
// - otherwise defaults to DefaultNetworkTemplate
func (s *Sandbox) GetNetworkTemplateName() string {
	if s.Spec.Network == nil {
		return DefaultNetworkTemplate
	}
	if s.Spec.Network.TemplateRef != nil {
		return s.Spec.Network.TemplateRef.Name
	}
	return s.GetOwnedNetworkTemplateName()
}

func (s *Sandbox) GetOwnedNetworkTemplateName() string {
	return s.Name + "-net"
}

func (s *Sandbox) HasNetworkSpec() bool {
	return s.Spec.Network != nil && s.Spec.Network.Spec != nil
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
