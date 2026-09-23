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
	LabelPool             = "isola.run/pool"
	LabelPoolState        = "isola.run/pool-state"
	LabelPoolTemplateHash = "isola.run/pool-template-hash"

	PoolStateWarmAvailable = "WarmAvailable"

	WarmSandboxPoolReadyCondition       = "Ready"
	WarmSandboxPoolProgressingCondition = "Progressing"
)

// EmbeddedObjectMeta is a strict subset of metav1.ObjectMeta safe to embed
// in templates. Mirrors the well-known kube convention used by VMPool /
// PodTemplateSpec metadata: only labels and annotations are honored.
type EmbeddedObjectMeta struct {
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
}

// SandboxTemplate describes the Sandbox that the pool controller stamps for
// each replica.
type SandboxTemplate struct {
	// +optional
	Metadata EmbeddedObjectMeta `json:"metadata,omitempty"`
	// +required
	Spec SandboxSpec `json:"spec"`
}

// WarmSandboxPoolSpec defines the desired state of a warm sandbox pool.
//
// CEL constraints reflect the v1 design:
//   - terminationPolicy.type, if set, must be Delete. SnapshotRootfs would race
//     the pre-adoption skip in the Sandbox controller.
//   - timeoutSeconds must be unset; adoption sets it.
//
// +kubebuilder:validation:XValidation:rule="!has(self.template.spec.terminationPolicy) || !has(self.template.spec.terminationPolicy.type) || self.template.spec.terminationPolicy.type == 'Delete'",message="warm pool template must use terminationPolicy.type=Delete",reason="FieldValueInvalid"
// +kubebuilder:validation:XValidation:rule="!has(self.template.spec.timeoutSeconds)",message="warm pool template must not set timeoutSeconds; it is set at adoption time",reason="FieldValueForbidden"
type WarmSandboxPoolSpec struct {
	// Replicas is the desired count of pooled Sandbox children. Allocated
	// members detach from the pool (ownerReference removed) and stop counting,
	// so this value is effectively the warm-floor size.
	// +optional
	// +kubebuilder:default=0
	// +kubebuilder:validation:Minimum=0
	Replicas *int32 `json:"replicas,omitempty"`

	// Selector matches pool children. Must match Template.Metadata.Labels.
	// +required
	Selector *metav1.LabelSelector `json:"selector"`

	// Template is stamped onto each child Sandbox.
	// +required
	Template SandboxTemplate `json:"template"`
}

// WarmSandboxPoolStatus defines the observed state of a WarmSandboxPool.
type WarmSandboxPoolStatus struct {
	// Replicas is the total number of pool-owned Sandbox children currently
	// observed. Detached (allocated) members are not counted.
	Replicas int32 `json:"replicas"`
	// ReadyReplicas is the subset whose Sandbox.Ready=True.
	ReadyReplicas int32 `json:"readyReplicas"`
	// AvailableReplicas is ReadyReplicas minus those being deleted; the count
	// immediately eligible for adoption.
	AvailableReplicas int32 `json:"availableReplicas"`
	// TemplateHash is the canonical hash of the current Spec.Template; child
	// Sandboxes are stamped with this value so stale-template stragglers can
	// be drained on rollout.
	TemplateHash string `json:"templateHash,omitempty"`
	// ObservedGeneration is the .metadata.generation last reconciled.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// LabelSelector is the serialized form of Spec.Selector required by
	// the /scale subresource.
	LabelSelector string `json:"labelSelector,omitempty"`
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:subresource:scale:specpath=.spec.replicas,statuspath=.status.replicas,selectorpath=.status.labelSelector
// +kubebuilder:resource:shortName=wsp
// +kubebuilder:printcolumn:name="Desired",type="integer",JSONPath=".spec.replicas"
// +kubebuilder:printcolumn:name="Replicas",type="integer",JSONPath=".status.replicas"
// +kubebuilder:printcolumn:name="Ready",type="integer",JSONPath=".status.readyReplicas"
// +kubebuilder:printcolumn:name="Available",type="integer",JSONPath=".status.availableReplicas"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// WarmSandboxPool is the Schema for the warmsandboxpools API.
type WarmSandboxPool struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              WarmSandboxPoolSpec   `json:"spec"`
	Status            WarmSandboxPoolStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// WarmSandboxPoolList contains a list of WarmSandboxPool.
type WarmSandboxPoolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []WarmSandboxPool `json:"items"`
}

func init() {
	SchemeBuilder.Register(&WarmSandboxPool{}, &WarmSandboxPoolList{})
}
