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

package controller

import (
	"context"
	"fmt"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	sandboxv1alpha1 "github.com/isola-ai/isola-sb/api/v1alpha1"
	netbuilder "github.com/isola-ai/isola-sb/internal/operator/controller/network"
)

// BenchmarkNetworkPolicyBuilder benchmarks the NetworkPolicy builder
// with various network spec configurations.
func BenchmarkNetworkPolicyBuilder(b *testing.B) {
	testCases := []struct {
		name string
		spec *sandboxv1alpha1.NetworkSpec
	}{
		{
			name: "EmptySpec",
			spec: &sandboxv1alpha1.NetworkSpec{},
		},
		{
			name: "AllowAllInternet",
			spec: &sandboxv1alpha1.NetworkSpec{
				AllowAllInternet: true,
			},
		},
		{
			name: "SingleCIDR",
			spec: &sandboxv1alpha1.NetworkSpec{
				AllowedEgressCIDRs: []string{"10.0.0.0/8"},
			},
		},
		{
			name: "MultipleCIDRs",
			spec: &sandboxv1alpha1.NetworkSpec{
				AllowedEgressCIDRs: []string{
					"10.0.0.0/8",
					"172.16.0.0/12",
					"192.168.0.0/16",
					"8.8.8.0/24",
					"1.1.1.0/24",
				},
			},
		},
		{
			name: "PodEgressRules",
			spec: &sandboxv1alpha1.NetworkSpec{
				AllowedEgressPods: []sandboxv1alpha1.PodEgressRule{
					{
						Namespace: "default",
						LabelSelector: metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "db"},
						},
					},
					{
						Namespace: "monitoring",
						LabelSelector: metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "prometheus"},
						},
					},
				},
			},
		},
		{
			name: "ComplexSpec",
			spec: &sandboxv1alpha1.NetworkSpec{
				AllowAllInternet: true,
				AllowClusterDNS:  true,
				Nameservers:      []string{"8.8.8.8", "1.1.1.1"},
				AllowedEgressCIDRs: []string{
					"10.0.0.0/8",
					"172.16.0.0/12",
				},
				AllowedEgressPods: []sandboxv1alpha1.PodEgressRule{
					{
						Namespace: "default",
						LabelSelector: metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "db"},
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_, _ = netbuilder.BuildCustomNetworkPolicy(
					fmt.Sprintf("sandbox-%d", i),
					"default",
					tc.spec,
				)
			}
		})
	}
}

// BenchmarkBuildNetworkLabels benchmarks the network label builder.
func BenchmarkBuildNetworkLabels(b *testing.B) {
	specs := []*sandboxv1alpha1.NetworkSpec{
		nil,
		{},
		{AllowAllInternet: true},
		{AllowClusterDNS: true},
		{AllowAllInternet: true, AllowClusterDNS: true},
	}

	for i, spec := range specs {
		name := fmt.Sprintf("Spec%d", i)
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for j := 0; j < b.N; j++ {
				_ = buildNetworkLabels(spec)
			}
		})
	}
}

// BenchmarkConditionHelpers benchmarks status condition operations.
func BenchmarkConditionHelpers(b *testing.B) {
	b.Run("SetCondition", func(b *testing.B) {
		conditions := make([]metav1.Condition, 0, 10)
		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			conditions = append(conditions[:0], metav1.Condition{
				Type:   "Ready",
				Status: metav1.ConditionTrue,
			})
		}
	})
}

// BenchmarkTimeoutCalculation benchmarks timeout calculation logic.
func BenchmarkTimeoutCalculation(b *testing.B) {
	r := &SandboxReconciler{
		Clock: RealClock{},
	}

	sandbox := &sandboxv1alpha1.Sandbox{
		ObjectMeta: metav1.ObjectMeta{
			CreationTimestamp: metav1.Now(),
		},
	}

	timeout := int32(600)
	template := &sandboxv1alpha1.SandboxTemplate{
		Spec: sandboxv1alpha1.SandboxTemplateSpec{
			TimeoutSeconds: &timeout,
		},
	}

	pod := &corev1.Pod{
		Status: corev1.PodStatus{
			StartTime: &metav1.Time{Time: time.Now()},
		},
	}

	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = r.calculateTimeout(ctx, sandbox, template, pod)
	}
}

// BenchmarkPodTemplateConstruction benchmarks pod template construction
// which is a key operation in sandbox creation.
func BenchmarkPodTemplateConstruction(b *testing.B) {
	template := &sandboxv1alpha1.SandboxTemplate{
		Spec: sandboxv1alpha1.SandboxTemplateSpec{
			PodTemplate: sandboxv1alpha1.PodTemplateSpec{
				Labels: map[string]string{
					"app": "test",
					"env": "benchmark",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "main",
							Image: "alpine:latest",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("128Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("500m"),
									corev1.ResourceMemory: resource.MustParse("512Mi"),
								},
							},
						},
					},
				},
			},
		},
	}

	b.Run("DeepCopy", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = template.Spec.PodTemplate.Spec.DeepCopy()
		}
	})
}

// BenchmarkSandboxNameGeneration benchmarks sandbox pod name generation.
func BenchmarkSandboxNameGeneration(b *testing.B) {
	sandboxes := make([]*sandboxv1alpha1.Sandbox, 100)
	for i := range sandboxes {
		sandboxes[i] = &sandboxv1alpha1.Sandbox{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("sandbox-%d", i),
				Namespace: "default",
			},
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = getSandboxPodName(sandboxes[i%100])
	}
}

// BenchmarkTypeNamespacedName benchmarks types.NamespacedName construction
// which is used extensively in reconciliation.
func BenchmarkTypeNamespacedName(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = types.NamespacedName{
			Name:      fmt.Sprintf("sandbox-%d", i),
			Namespace: "default",
		}
	}
}

// BenchmarkReconcileRequestConstruction benchmarks reconcile request construction.
func BenchmarkReconcileRequestConstruction(b *testing.B) {
	names := make([]string, 100)
	for i := range names {
		names[i] = fmt.Sprintf("sandbox-%d", i)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = types.NamespacedName{
			Name:      names[i%100],
			Namespace: "default",
		}
	}
}
