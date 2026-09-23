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

package controller

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	sandboxv1alpha1 "github.com/isola-run/isola/api/v1alpha1"
)

var _ = Describe("Sandbox Controller", func() {

	Describe("TimeoutAt anchoring with adopted-at", func() {
		var (
			reconciler *SandboxReconciler
			fakeClock  *FakeClock
		)

		BeforeEach(func() {
			fakeClock = NewFakeClock(time.Now())
			reconciler = newTestReconciler(fakeClock)
		})

		It("anchors TimeoutAt to adopted-at when adopted-at is later than pod start", func() {
			sandboxName := "sandbox-adopted-later"

			now := fakeClock.Now()
			adoptedAt := now.Add(time.Hour)
			timeout := int64(60)

			createSandbox(ctx, sandboxName, func(s *sandboxv1alpha1.Sandbox) {
				s.Spec.TimeoutSeconds = &timeout
				if s.Annotations == nil {
					s.Annotations = map[string]string{}
				}
				s.Annotations[AdoptedAtAnnotation] = adoptedAt.Format(time.RFC3339)
			})
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())
			startTime := metav1.NewTime(now)
			pod.Status.StartTime = &startTime
			Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())

			_, err = doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			sandbox := getSandbox(ctx, sandboxName)
			Expect(sandbox.Status.TimeoutAt).NotTo(BeNil())

			expected := adoptedAt.Add(time.Duration(timeout) * time.Second)
			Expect(sandbox.Status.TimeoutAt.Time).To(BeTemporally("~", expected, time.Second))
		})
	})

	Describe("termination policy skipped for unadopted", func() {
		var (
			reconciler *SandboxReconciler
			fakeClock  *FakeClock
		)

		BeforeEach(func() {
			fakeClock = NewFakeClock(time.Now())
			reconciler = newTestReconciler(fakeClock)
		})

		It("does not create RootfsSnapshot for a sandbox without adopted-at annotation", func() {
			sandboxName := "sandbox-unadopted-snapshot-skip"

			createSandbox(ctx, sandboxName, func(s *sandboxv1alpha1.Sandbox) {
				s.Spec.TerminationPolicy = &sandboxv1alpha1.TerminationPolicy{
					Type:           sandboxv1alpha1.TerminationTypeSnapshotRootfs,
					SnapshotRootfs: &sandboxv1alpha1.SnapshotRootfsTermination{SnapshotName: "test-snapshot"},
				}
			})
			defer deletePod(ctx, sandboxName+"-pod")
			defer deleteTerminationSnapshot(ctx, sandboxName)

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			pod := bindPodToNode(ctx, sandboxName+"-pod")
			makePodReady(ctx, pod, "containerd://abc123", fakeClock)

			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			sandbox := getSandbox(ctx, sandboxName)
			Expect(k8sClient.Delete(ctx, sandbox)).To(Succeed())

			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Pool churn must not fire the snapshot policy.
			Expect(getTerminationSnapshot(ctx, sandboxName)).To(BeNil())

			err = k8sClient.Get(ctx, types.NamespacedName{Name: sandboxName, Namespace: testNamespace}, &sandboxv1alpha1.Sandbox{})
			Expect(err).To(Satisfy(errors.IsNotFound))
		})
	})
})
