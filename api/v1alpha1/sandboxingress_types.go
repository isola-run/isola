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

// SandboxIngressConditionType defines the types of conditions for SandboxIngress.
type SandboxIngressConditionType string

const (
	// SandboxIngressReady indicates the ingress is fully configured and accessible.
	SandboxIngressReady SandboxIngressConditionType = "Ready"
	// SandboxIngressSandboxReady indicates the referenced sandbox exists and is ready.
	SandboxIngressSandboxReady SandboxIngressConditionType = "SandboxReady"
	// SandboxIngressHTTPRouteReady indicates the HTTPRoute has been created.
	SandboxIngressHTTPRouteReady SandboxIngressConditionType = "HTTPRouteReady"
	// SandboxIngressServiceReady indicates the Service has been created.
	SandboxIngressServiceReady SandboxIngressConditionType = "ServiceReady"
)

// SandboxIngressSpec defines the desired state of SandboxIngress.
type SandboxIngressSpec struct {
	// SandboxRef is the name of the Sandbox to expose.
	// The Sandbox must exist in the same namespace.
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	SandboxRef string `json:"sandboxRef"`

	// ContainerPort is the port on the sandbox container to expose.
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	ContainerPort int32 `json:"containerPort"`
}

// SandboxIngressStatus defines the observed state of SandboxIngress.
type SandboxIngressStatus struct {
	// URL is the public URL where the sandbox can be accessed.
	// Format: https://<sandbox-id>.sandboxes.<domain>
	// +optional
	URL string `json:"url,omitempty"`

	// Conditions represent the latest available observations of the SandboxIngress's state.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=sbi
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status",description="Ingress readiness"
// +kubebuilder:printcolumn:name="Sandbox",type="string",JSONPath=".spec.sandboxRef",description="Referenced sandbox"
// +kubebuilder:printcolumn:name="Port",type="integer",JSONPath=".spec.containerPort",description="Container port"
// +kubebuilder:printcolumn:name="URL",type="string",JSONPath=".status.url",description="Public URL"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// SandboxIngress enables external HTTP(S) access to a Sandbox.
// When created, the operator provisions a Service and HTTPRoute that expose the sandbox
// at a unique URL. The URL itself acts as the authentication mechanism (presigned URL pattern).
type SandboxIngress struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +required
	Spec SandboxIngressSpec `json:"spec"`

	// +optional
	Status SandboxIngressStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// SandboxIngressList contains a list of SandboxIngress.
type SandboxIngressList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SandboxIngress `json:"items"`
}

// GetServiceName returns the name of the Service created for this ingress.
func (s *SandboxIngress) GetServiceName() string {
	return s.Name + "-svc"
}

// GetHTTPRouteName returns the name of the HTTPRoute created for this ingress.
func (s *SandboxIngress) GetHTTPRouteName() string {
	return s.Name + "-route"
}

func init() {
	SchemeBuilder.Register(&SandboxIngress{}, &SandboxIngressList{})
}
