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

package utils

import (
	corev1 "k8s.io/api/core/v1"
	nodev1 "k8s.io/api/node/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	sandboxv1alpha1 "github.com/omereli/dev-isola/services/isola-operator/api/v1alpha1"
)

// TemplateOption is a functional option for configuring SandboxTemplate
type TemplateOption func(*sandboxv1alpha1.SandboxTemplate)

// WithTimeout sets the timeout in seconds for the template
func WithTimeout(seconds int64) TemplateOption {
	return func(t *sandboxv1alpha1.SandboxTemplate) {
		t.Spec.TimeoutSeconds = &seconds
	}
}

// WithShutdownPolicy sets the shutdown policy for the template
func WithShutdownPolicy(policy sandboxv1alpha1.SandboxShutdownPolicy) TemplateOption {
	return func(t *sandboxv1alpha1.SandboxTemplate) {
		t.Spec.ShutdownPolicy = &sandboxv1alpha1.ShutdownPolicy{
			Policy: policy,
		}
	}
}

// WithSnapshotTimeout sets the snapshot timeout for the template
func WithSnapshotTimeout(seconds int64) TemplateOption {
	return func(t *sandboxv1alpha1.SandboxTemplate) {
		if t.Spec.ShutdownPolicy == nil {
			t.Spec.ShutdownPolicy = &sandboxv1alpha1.ShutdownPolicy{
				Policy: sandboxv1alpha1.ShutdownPolicySnapshotFilesystem,
			}
		}
		t.Spec.ShutdownPolicy.SnapshotTimeoutSeconds = &seconds
	}
}

// WithPodSpec sets a custom pod spec for the template
func WithPodSpec(spec corev1.PodSpec) TemplateOption {
	return func(t *sandboxv1alpha1.SandboxTemplate) {
		t.Spec.PodTemplate.Spec = spec
	}
}

// WithRuntimeClass sets the RuntimeClassName for the template's pod
func WithRuntimeClass(name string) TemplateOption {
	return func(t *sandboxv1alpha1.SandboxTemplate) {
		t.Spec.PodTemplate.Spec.RuntimeClassName = &name
	}
}

// WithNetwork sets the network configuration for the template
func WithNetwork(allowedIncoming, allowedOutgoing []string) TemplateOption {
	return func(t *sandboxv1alpha1.SandboxTemplate) {
		t.Spec.Network = &sandboxv1alpha1.NetworkConfig{
			AllowedIncoming: allowedIncoming,
			AllowedOutgoing: allowedOutgoing,
		}
	}
}

// NewTestSandbox creates a Sandbox with sensible defaults for testing
func NewTestSandbox(name, namespace, templateRef string) *sandboxv1alpha1.Sandbox {
	return &sandboxv1alpha1.Sandbox{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: sandboxv1alpha1.SandboxSpec{
			TemplateRef: &corev1.LocalObjectReference{
				Name: templateRef,
			},
		},
	}
}

// NewTestSandboxTemplate creates a SandboxTemplate with configurable options
func NewTestSandboxTemplate(name, namespace string, opts ...TemplateOption) *sandboxv1alpha1.SandboxTemplate {
	template := &sandboxv1alpha1.SandboxTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: sandboxv1alpha1.SandboxTemplateSpec{
			PodTemplate: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:    "sandbox",
							Image:   "busybox:latest",
							Command: []string{"sleep", "infinity"},
						},
					},
				},
			},
		},
	}

	for _, opt := range opts {
		opt(template)
	}

	return template
}

// NewTestRuntimeClass creates a RuntimeClass for testing snapshotting
func NewTestRuntimeClass(name, handler string) *nodev1.RuntimeClass {
	return &nodev1.RuntimeClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Handler: handler,
	}
}

// PodOption is a functional option for configuring Pod
type PodOption func(*corev1.Pod)

// WithPodPhase sets the pod phase
func WithPodPhase(phase corev1.PodPhase) PodOption {
	return func(p *corev1.Pod) {
		p.Status.Phase = phase
	}
}

// WithPodCondition adds a condition to the pod
func WithPodCondition(condType corev1.PodConditionType, status corev1.ConditionStatus) PodOption {
	return func(p *corev1.Pod) {
		p.Status.Conditions = append(p.Status.Conditions, corev1.PodCondition{
			Type:   condType,
			Status: status,
		})
	}
}

// WithPodStartTime sets the pod start time
func WithPodStartTime(t metav1.Time) PodOption {
	return func(p *corev1.Pod) {
		p.Status.StartTime = &t
	}
}

// WithContainerStatus adds a container status to the pod
func WithContainerStatus(name, containerID string, ready bool) PodOption {
	return func(p *corev1.Pod) {
		p.Status.ContainerStatuses = append(p.Status.ContainerStatuses, corev1.ContainerStatus{
			Name:        name,
			ContainerID: containerID,
			Ready:       ready,
			State: corev1.ContainerState{
				Running: &corev1.ContainerStateRunning{},
			},
		})
	}
}

// WithPodRuntimeClass sets the RuntimeClassName for the pod
func WithPodRuntimeClass(name string) PodOption {
	return func(p *corev1.Pod) {
		p.Spec.RuntimeClassName = &name
	}
}

// WithNodeName sets the node name for the pod
func WithNodeName(nodeName string) PodOption {
	return func(p *corev1.Pod) {
		p.Spec.NodeName = nodeName
	}
}

// NewTestPod creates a Pod with configurable options
func NewTestPod(name, namespace string, opts ...PodOption) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:    "sandbox",
					Image:   "busybox:latest",
					Command: []string{"sleep", "infinity"},
				},
			},
		},
	}

	for _, opt := range opts {
		opt(pod)
	}

	return pod
}

// MakeTestPodReady configures a pod to appear ready
func MakeTestPodReady(pod *corev1.Pod) {
	pod.Status.Phase = corev1.PodRunning
	pod.Status.Conditions = append(pod.Status.Conditions, corev1.PodCondition{
		Type:   corev1.PodReady,
		Status: corev1.ConditionTrue,
	})
}

// UniqueNameGenerator generates unique names for test resources
type UniqueNameGenerator struct {
	prefix  string
	counter int
}

// NewUniqueNameGenerator creates a new name generator with the given prefix
func NewUniqueNameGenerator(prefix string) *UniqueNameGenerator {
	return &UniqueNameGenerator{prefix: prefix}
}

// Next returns the next unique name
func (g *UniqueNameGenerator) Next() string {
	g.counter++
	return g.prefix + "-" + string(rune('a'+g.counter-1))
}
