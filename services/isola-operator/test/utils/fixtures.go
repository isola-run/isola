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

	sandboxv1alpha1 "github.com/isola-ai/isola-sb/services/isola-operator/api/v1alpha1"
)

// TemplateOption is a functional option for configuring SandboxTemplate
type TemplateOption func(*sandboxv1alpha1.SandboxTemplate)

func WithTimeout(seconds int64) TemplateOption {
	return func(t *sandboxv1alpha1.SandboxTemplate) {
		t.Spec.TimeoutSeconds = &seconds
	}
}

func WithShutdownPolicy(policy sandboxv1alpha1.SandboxShutdownPolicy) TemplateOption {
	return func(t *sandboxv1alpha1.SandboxTemplate) {
		t.Spec.ShutdownPolicy = &sandboxv1alpha1.ShutdownPolicy{
			Policy: policy,
		}
	}
}

func WithActiveDeadlineSeconds(seconds int64) TemplateOption {
	return func(t *sandboxv1alpha1.SandboxTemplate) {
		if t.Spec.ShutdownPolicy == nil {
			t.Spec.ShutdownPolicy = &sandboxv1alpha1.ShutdownPolicy{
				Policy: sandboxv1alpha1.ShutdownPolicySnapshotFilesystem,
			}
		}
		t.Spec.ShutdownPolicy.ActiveDeadlineSeconds = &seconds
	}
}

func WithPodSpec(spec corev1.PodSpec) TemplateOption {
	return func(t *sandboxv1alpha1.SandboxTemplate) {
		t.Spec.PodTemplate.Spec = spec
	}
}

func WithRuntimeClass(name string) TemplateOption {
	return func(t *sandboxv1alpha1.SandboxTemplate) {
		t.Spec.PodTemplate.Spec.RuntimeClassName = &name
	}
}

// SandboxOption is a functional option for configuring Sandbox
type SandboxOption func(*sandboxv1alpha1.Sandbox)

// WithNetworkTemplateRef adds a NetworkTemplate reference to the sandbox
func WithNetworkTemplateRef(name string) SandboxOption {
	return func(s *sandboxv1alpha1.Sandbox) {
		s.Spec.Network = &sandboxv1alpha1.NetworkConfig{
			TemplateRef: &sandboxv1alpha1.NetworkTemplateReference{
				Name: name,
			},
		}
	}
}

// WithNetworkSpec adds an embedded NetworkTemplateSpec to the sandbox
func WithNetworkSpec(spec sandboxv1alpha1.NetworkTemplateSpec) SandboxOption {
	return func(s *sandboxv1alpha1.Sandbox) {
		s.Spec.Network = &sandboxv1alpha1.NetworkConfig{
			Spec: &spec,
		}
	}
}

// WithSandboxTemplateSpec replaces the template reference with an embedded spec
func WithSandboxTemplateSpec(spec sandboxv1alpha1.SandboxTemplateSpec) SandboxOption {
	return func(s *sandboxv1alpha1.Sandbox) {
		s.Spec.Template = sandboxv1alpha1.TemplateConfig{
			Spec: &spec,
		}
	}
}

// NewTestSandbox creates a new Sandbox with a template reference.
// Use WithSandboxTemplateSpec() to override with an embedded spec instead.
func NewTestSandbox(name, namespace, templateRef string, opts ...SandboxOption) *sandboxv1alpha1.Sandbox {
	sandbox := &sandboxv1alpha1.Sandbox{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: sandboxv1alpha1.SandboxSpec{
			Template: sandboxv1alpha1.TemplateConfig{
				TemplateRef: &sandboxv1alpha1.SandboxTemplateReference{
					Name: templateRef,
				},
			},
		},
	}

	for _, opt := range opts {
		opt(sandbox)
	}

	return sandbox
}

// NewTestSandboxWithSpec creates a new Sandbox with an embedded template spec (no external template reference).
func NewTestSandboxWithSpec(name, namespace string, spec sandboxv1alpha1.SandboxTemplateSpec, opts ...SandboxOption) *sandboxv1alpha1.Sandbox {
	sandbox := &sandboxv1alpha1.Sandbox{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: sandboxv1alpha1.SandboxSpec{
			Template: sandboxv1alpha1.TemplateConfig{
				Spec: &spec,
			},
		},
	}

	for _, opt := range opts {
		opt(sandbox)
	}

	return sandbox
}

func NewTestSandboxTemplate(name, namespace string, opts ...TemplateOption) *sandboxv1alpha1.SandboxTemplate {
	template := &sandboxv1alpha1.SandboxTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: NewTestSandboxTemplateSpec(opts...),
	}

	return template
}

// NewTestSandboxTemplateSpec creates a default SandboxTemplateSpec for testing.
// This can be used both for standalone SandboxTemplate objects and for embedding in Sandbox specs.
func NewTestSandboxTemplateSpec(opts ...TemplateOption) sandboxv1alpha1.SandboxTemplateSpec {
	// Create a temporary template to apply options
	template := &sandboxv1alpha1.SandboxTemplate{
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

	return template.Spec
}

// NetworkTemplateOption is a functional option for configuring NetworkTemplate
type NetworkTemplateOption func(*sandboxv1alpha1.NetworkTemplate)

// WithAllowedEgressCIDRs sets the allowed egress CIDRs
func WithAllowedEgressCIDRs(cidrs ...string) NetworkTemplateOption {
	return func(nt *sandboxv1alpha1.NetworkTemplate) {
		nt.Spec.AllowedEgressCIDRs = cidrs
	}
}

// WithNameservers sets the DNS server IPs for the network template
func WithNameservers(servers ...string) NetworkTemplateOption {
	return func(nt *sandboxv1alpha1.NetworkTemplate) {
		nt.Spec.Nameservers = servers
	}
}

// WithDNSPolicy sets the DNS policy for the network template
func WithDNSPolicy(policy corev1.DNSPolicy) NetworkTemplateOption {
	return func(nt *sandboxv1alpha1.NetworkTemplate) {
		nt.Spec.DNSPolicy = policy
	}
}

// WithAllowedEgressPods sets the allowed egress pod rules
func WithAllowedEgressPods(rules ...sandboxv1alpha1.EgressPodRule) NetworkTemplateOption {
	return func(nt *sandboxv1alpha1.NetworkTemplate) {
		nt.Spec.AllowedEgressPods = rules
	}
}

// NewTestNetworkTemplate creates a new NetworkTemplate for testing.
// By default, uses DNSPolicy: None with external DNS.
// Use WithNameservers() to override or omit nameservers (empty = sink nameserver).
func NewTestNetworkTemplate(name, namespace string, opts ...NetworkTemplateOption) *sandboxv1alpha1.NetworkTemplate {
	nt := &sandboxv1alpha1.NetworkTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: sandboxv1alpha1.NetworkTemplateSpec{
			DNSPolicy:   corev1.DNSNone,
			Nameservers: []string{"8.8.8.8"},
		},
	}

	for _, opt := range opts {
		opt(nt)
	}

	return nt
}

// NewTestNetworkTemplateDenyAll creates a NetworkTemplate that denies all traffic
func NewTestNetworkTemplateDenyAll(name, namespace string) *sandboxv1alpha1.NetworkTemplate {
	return NewTestNetworkTemplate(name, namespace)
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
