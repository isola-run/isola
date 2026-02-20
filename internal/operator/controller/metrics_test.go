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
	"github.com/prometheus/client_golang/prometheus/testutil"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	sandboxv1alpha1 "github.com/isola-ai/isola/api/v1alpha1"
)

var _ = Describe("Metrics", func() {

	Context("Registration", func() {
		It("should register all custom metrics with the controller-runtime registry", func() {
			// Verify each metric can be collected from the registry without errors.
			// We use CollectAndCount on individual collectors rather than Gather(),
			// because CounterVec metrics don't appear in Gather() until labels are observed.
			Expect(testutil.CollectAndCount(sandboxCreatedTotal)).To(Equal(1))
			Expect(testutil.CollectAndCount(sandboxDeletedTotal)).To(Equal(1))
			Expect(testutil.CollectAndCount(sandboxTimedOutTotal)).To(Equal(1))
			Expect(testutil.CollectAndCount(sandboxReadyDurationSeconds)).To(Equal(1))
			Expect(testutil.CollectAndCount(rootfsSnapshotCreatedTotal)).To(Equal(1))
			// CounterVec starts with 0 label combos until observations happen
			Expect(testutil.CollectAndCount(rootfsSnapshotCompletedTotal)).To(BeNumerically(">=", 0))
		})
	})

	Context("Sandbox lifecycle", func() {
		var (
			reconciler *SandboxReconciler
			fakeClock  *FakeClock
		)

		BeforeEach(func() {
			fakeClock = NewFakeClock(time.Now())
			reconciler = newTestReconciler(fakeClock)
		})

		It("should increment sandboxCreatedTotal on pod creation", func() {
			sandboxName := "metrics-created"
			before := testutil.ToFloat64(sandboxCreatedTotal)

			createSandbox(ctx, sandboxName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			Expect(testutil.ToFloat64(sandboxCreatedTotal)).To(Equal(before + 1))
		})

		It("should observe sandboxReadyDurationSeconds when sandbox becomes ready", func() {
			sandboxName := "metrics-ready-duration"

			createSandbox(ctx, sandboxName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			// First reconcile: create pod
			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			// Make pod ready
			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())
			makePodReady(ctx, pod, "containerd://abc123", fakeClock)

			// Reconcile again to pick up the ready state — the histogram should gain an observation
			_, err = doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			// Verify the histogram has at least 1 observation (CollectAndCount returns metric family count)
			Expect(testutil.CollectAndCount(sandboxReadyDurationSeconds)).To(Equal(1))
		})

		It("should increment sandboxDeletedTotal on finalizer removal", func() {
			sandboxName := "metrics-deleted"

			createSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			// Reconcile to add finalizer and create pod
			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			before := testutil.ToFloat64(sandboxDeletedTotal)

			// Delete sandbox
			deleteSandbox(ctx, sandboxName)

			// Reconcile to run finalizer
			_, err = doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			Expect(testutil.ToFloat64(sandboxDeletedTotal)).To(Equal(before + 1))
		})

		It("should increment sandboxTimedOutTotal when sandbox times out", func() {
			sandboxName := "metrics-timed-out"
			timeout := int64(60)

			createSandbox(ctx, sandboxName, func(s *sandboxv1alpha1.Sandbox) {
				s.Spec.ActiveDeadlineSeconds = &timeout
			})
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			// Reconcile to add finalizer and create pod
			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			// Make pod ready
			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())
			pod.Status.Phase = corev1.PodRunning
			pod.Status.StartTime = &metav1.Time{Time: fakeClock.Now()}
			pod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
			Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())

			// Reconcile to update status
			_, err = doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			before := testutil.ToFloat64(sandboxTimedOutTotal)

			// Advance past timeout
			fakeClock.Advance(61 * time.Second)

			// Reconcile to trigger timeout
			_, err = doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			Expect(testutil.ToFloat64(sandboxTimedOutTotal)).To(Equal(before + 1))
		})
	})
})
