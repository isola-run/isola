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

package utils

import (
	corev1 "k8s.io/api/core/v1"
	nodev1 "k8s.io/api/node/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	sandboxv1alpha1 "github.com/isola-ai/isola/api/v1alpha1"
)

// SandboxOption is a functional option for configuring Sandbox
type SandboxOption func(*sandboxv1alpha1.Sandbox)

// WithSandboxActiveDeadline sets the active deadline for the sandbox
func WithSandboxActiveDeadline(seconds int64) SandboxOption {
	return func(s *sandboxv1alpha1.Sandbox) {
		s.Spec.ActiveDeadlineSeconds = &seconds
	}
}

// WithSandboxShutdownStrategy sets the shutdown strategy for the sandbox
func WithSandboxShutdownStrategy(strategy sandboxv1alpha1.SandboxShutdownStrategy) SandboxOption {
	return func(s *sandboxv1alpha1.Sandbox) {
		s.Spec.ShutdownPolicy = &sandboxv1alpha1.ShutdownPolicy{
			Strategy: strategy,
		}
	}
}

// WithSandboxActiveDeadlineSeconds sets the active deadline seconds for the sandbox shutdown policy
func WithSandboxActiveDeadlineSeconds(seconds int64) SandboxOption {
	return func(s *sandboxv1alpha1.Sandbox) {
		if s.Spec.ShutdownPolicy == nil {
			s.Spec.ShutdownPolicy = &sandboxv1alpha1.ShutdownPolicy{
				Strategy: sandboxv1alpha1.ShutdownStrategySnapshotRootfs,
			}
		}
		s.Spec.ShutdownPolicy.ActiveDeadlineSeconds = &seconds
	}
}

// WithPodTemplate sets the pod template for the sandbox
func WithPodTemplate(spec corev1.PodTemplateSpec) SandboxOption {
	return func(s *sandboxv1alpha1.Sandbox) {
		s.Spec.PodTemplate = spec
	}
}

// WithSandboxRuntimeClass sets the runtime class for the sandbox pod
func WithSandboxRuntimeClass(name string) SandboxOption {
	return func(s *sandboxv1alpha1.Sandbox) {
		s.Spec.PodTemplate.Spec.RuntimeClassName = &name
	}
}

// WithNetworkSpec sets the network configuration for the sandbox
func WithNetworkSpec(spec *sandboxv1alpha1.NetworkSpec) SandboxOption {
	return func(s *sandboxv1alpha1.Sandbox) {
		s.Spec.Network = spec
	}
}

// WithInternetAccess enables internet egress for the sandbox
func WithInternetAccess() SandboxOption {
	return func(s *sandboxv1alpha1.Sandbox) {
		if s.Spec.Network == nil {
			s.Spec.Network = &sandboxv1alpha1.NetworkSpec{}
		}
		s.Spec.Network.AllowInternetEgress = ptr.To(true)
	}
}

// WithClusterDNS enables cluster DNS for the sandbox
func WithClusterDNS() SandboxOption {
	return func(s *sandboxv1alpha1.Sandbox) {
		if s.Spec.Network == nil {
			s.Spec.Network = &sandboxv1alpha1.NetworkSpec{}
		}
		s.Spec.Network.AllowClusterDNS = ptr.To(true)
	}
}

func NewTestSandbox(name, namespace string, opts ...SandboxOption) *sandboxv1alpha1.Sandbox {
	sandbox := &sandboxv1alpha1.Sandbox{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: sandboxv1alpha1.SandboxSpec{
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
		opt(sandbox)
	}

	return sandbox
}

// NetworkSpecOption is a functional option for configuring NetworkSpec
type NetworkSpecOption func(*sandboxv1alpha1.NetworkSpec)

// WithAllowedEgressCIDRs sets the allowed egress CIDRs
func WithAllowedEgressCIDRs(cidrs ...string) NetworkSpecOption {
	return func(ns *sandboxv1alpha1.NetworkSpec) {
		ns.AllowedEgressCIDRs = cidrs
	}
}

// WithNameservers sets the DNS server IPs for the network spec
func WithNameservers(servers ...string) NetworkSpecOption {
	return func(ns *sandboxv1alpha1.NetworkSpec) {
		ns.Nameservers = servers
	}
}

// NewTestNetworkSpec creates a new NetworkSpec for testing.
func NewTestNetworkSpec(opts ...NetworkSpecOption) *sandboxv1alpha1.NetworkSpec {
	ns := &sandboxv1alpha1.NetworkSpec{}

	for _, opt := range opts {
		opt(ns)
	}

	return ns
}

func NewTestRuntimeClass(name, handler string) *nodev1.RuntimeClass {
	return &nodev1.RuntimeClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Handler: handler,
	}
}

type PodOption func(*corev1.Pod)

func WithPodPhase(phase corev1.PodPhase) PodOption {
	return func(p *corev1.Pod) {
		p.Status.Phase = phase
	}
}

func WithPodCondition(condType corev1.PodConditionType, status corev1.ConditionStatus) PodOption {
	return func(p *corev1.Pod) {
		p.Status.Conditions = append(p.Status.Conditions, corev1.PodCondition{
			Type:   condType,
			Status: status,
		})
	}
}

func WithPodStartTime(t metav1.Time) PodOption {
	return func(p *corev1.Pod) {
		p.Status.StartTime = &t
	}
}

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

func WithPodRuntimeClass(name string) PodOption {
	return func(p *corev1.Pod) {
		p.Spec.RuntimeClassName = &name
	}
}

func WithNodeName(nodeName string) PodOption {
	return func(p *corev1.Pod) {
		p.Spec.NodeName = nodeName
	}
}

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

func MakeTestPodReady(pod *corev1.Pod) {
	pod.Status.Phase = corev1.PodRunning
	pod.Status.Conditions = append(pod.Status.Conditions, corev1.PodCondition{
		Type:   corev1.PodReady,
		Status: corev1.ConditionTrue,
	})
}

type UniqueNameGenerator struct {
	prefix  string
	counter int
}

func NewUniqueNameGenerator(prefix string) *UniqueNameGenerator {
	return &UniqueNameGenerator{prefix: prefix}
}

func (g *UniqueNameGenerator) Next() string {
	g.counter++
	return g.prefix + "-" + string(rune('a'+g.counter-1))
}
