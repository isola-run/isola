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

package v1alpha1_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	sandboxv1alpha1 "github.com/isola-run/isola/api/v1alpha1"
)

// minimalPool returns the smallest WarmSandboxPool that passes CREATE
// validation. Callers mutate before Create.
func minimalPool(name string) *sandboxv1alpha1.WarmSandboxPool {
	return &sandboxv1alpha1.WarmSandboxPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: sandboxv1alpha1.WarmSandboxPoolSpec{
			Replicas: ptr.To[int32](0),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{sandboxv1alpha1.LabelPool: name},
			},
			Template: sandboxv1alpha1.SandboxTemplate{
				Metadata: sandboxv1alpha1.EmbeddedObjectMeta{
					Labels: map[string]string{sandboxv1alpha1.LabelPool: name},
				},
				Spec: sandboxv1alpha1.SandboxSpec{
					PodTemplate: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "c1", Image: "nginx"},
							},
						},
					},
				},
			},
		},
	}
}

var _ = Describe("WarmSandboxPool CRD validation", func() {

	Describe("minimal pool", func() {
		It("accepts a minimal valid pool", func() {
			pool := minimalPool("p-minimal")
			Expect(k8sClient.Create(ctx, pool)).To(Succeed())
		})
	})

	Describe("template.spec.timeoutSeconds is forbidden", func() {
		It("rejects a pool whose template sets timeoutSeconds", func() {
			pool := minimalPool("p-timeout")
			pool.Spec.Template.Spec.TimeoutSeconds = ptr.To[int64](60)
			Expect(k8sClient.Create(ctx, pool)).ToNot(Succeed())
		})
	})

	Describe("template.spec.terminationPolicy restrictions", func() {
		It("rejects terminationPolicy.type=SnapshotRootfs", func() {
			pool := minimalPool("p-snap")
			pool.Spec.Template.Spec.TerminationPolicy = &sandboxv1alpha1.TerminationPolicy{
				Type:           sandboxv1alpha1.TerminationTypeSnapshotRootfs,
				SnapshotRootfs: &sandboxv1alpha1.SnapshotRootfsTermination{},
			}
			Expect(k8sClient.Create(ctx, pool)).ToNot(Succeed())
		})
		It("accepts terminationPolicy.type=Delete", func() {
			pool := minimalPool("p-delete")
			pool.Spec.Template.Spec.TerminationPolicy = &sandboxv1alpha1.TerminationPolicy{
				Type: sandboxv1alpha1.TerminationTypeDelete,
			}
			Expect(k8sClient.Create(ctx, pool)).To(Succeed())
		})
		It("accepts terminationPolicy omitted", func() {
			pool := minimalPool("p-noterm")
			pool.Spec.Template.Spec.TerminationPolicy = nil
			Expect(k8sClient.Create(ctx, pool)).To(Succeed())
		})
	})

	Describe("spec.replicas bounds", func() {
		It("rejects replicas: -1", func() {
			pool := minimalPool("p-neg")
			pool.Spec.Replicas = ptr.To[int32](-1)
			Expect(k8sClient.Create(ctx, pool)).ToNot(Succeed())
		})
	})

	Describe("spec.selector required", func() {
		It("rejects when selector is omitted", func() {
			pool := minimalPool("p-nosel")
			pool.Spec.Selector = nil
			Expect(k8sClient.Create(ctx, pool)).ToNot(Succeed())
		})
	})
})
