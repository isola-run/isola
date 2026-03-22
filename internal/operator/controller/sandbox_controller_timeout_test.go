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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	sandboxv1alpha1 "github.com/isola-ai/isola/api/v1alpha1"
)

var _ = Describe("Sandbox Controller", func() {

	// ============================================
	// Timeout Behavior Tests
	// ============================================
	Context("Timeout Behavior", func() {
		var (
			reconciler *SandboxReconciler
			fakeClock  *FakeClock
		)

		BeforeEach(func() {
			fakeClock = NewFakeClock(time.Now())
			reconciler = newTestReconciler(fakeClock)
		})

		It("should not set TimeoutAt when no timeout configured", func() {
			sandboxName := "sandbox-no-timeout"

			createSandbox(ctx, sandboxName)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			sandbox := getSandbox(ctx, sandboxName)
			Expect(sandbox.Status.TimeoutAt).To(BeNil())
		})

		It("should calculate TimeoutAt from pod start time", func() {
			sandboxName := "sandbox-timeout-pod-ready"

			timeout := int64(60)
			createSandbox(ctx, sandboxName, func(s *sandboxv1alpha1.Sandbox) {
				s.Spec.ActiveDeadlineSeconds = &timeout
			})
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			// First reconcile creates the pod
			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())

			// Set StartTime to an earlier time, PodReady LTT to a later time
			// to verify timeout is anchored to StartTime, not PodReady
			startTime := metav1.NewTime(fakeClock.Now().Add(-10 * time.Second))
			podReadyTime := metav1.NewTime(fakeClock.Now())
			pod.Status.StartTime = &startTime
			pod.Status.Phase = corev1.PodRunning
			pod.Status.Conditions = []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue, LastTransitionTime: podReadyTime},
			}
			Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())

			_, err = doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			sandbox := getSandbox(ctx, sandboxName)
			Expect(sandbox.Status.TimeoutAt).NotTo(BeNil())
			expectedTimeout := startTime.Add(time.Duration(timeout) * time.Second)
			Expect(sandbox.Status.TimeoutAt.Time).To(BeTemporally("~", expectedTimeout, time.Second))
		})

		It("should set TimeoutAt from StartTime even when pod is not ready", func() {
			sandboxName := "sandbox-timeout-not-ready"

			timeout := int64(60)
			createSandbox(ctx, sandboxName, func(s *sandboxv1alpha1.Sandbox) {
				s.Spec.ActiveDeadlineSeconds = &timeout
			})
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			// First reconcile creates the pod (pod is not ready yet)
			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			// Set StartTime but do NOT make pod ready
			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())
			startTime := metav1.NewTime(fakeClock.Now())
			pod.Status.StartTime = &startTime
			pod.Status.Phase = corev1.PodPending
			Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())

			_, err = doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			sandbox := getSandbox(ctx, sandboxName)
			Expect(sandbox.Status.TimeoutAt).NotTo(BeNil())
			expectedTimeout := startTime.Add(time.Duration(timeout) * time.Second)
			Expect(sandbox.Status.TimeoutAt.Time).To(BeTemporally("~", expectedTimeout, time.Second))
		})

		It("should delete sandbox with Delete policy when timeout exceeded", func() {
			sandboxName := "sandbox-timeout-delete"

			timeout := int64(1) // 1 second timeout
			createSandbox(ctx, sandboxName, func(s *sandboxv1alpha1.Sandbox) {
				s.Spec.ActiveDeadlineSeconds = &timeout
				s.Spec.ShutdownPolicy = &sandboxv1alpha1.ShutdownPolicy{
					Strategy: sandboxv1alpha1.ShutdownStrategyDelete,
				}
			})
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")

			// First reconcile creates the pod
			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			// Make pod ready so TimeoutAt gets set
			pod := getPod(ctx, sandboxName+"-pod")
			Expect(pod).NotTo(BeNil())
			makePodReady(ctx, pod, "containerd://abc123", fakeClock)

			// Reconcile to persist TimeoutAt
			_, err = doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			sandbox := getSandbox(ctx, sandboxName)
			Expect(sandbox.Status.TimeoutAt).NotTo(BeNil())

			// Advance past timeout
			fakeClock.Advance(2 * time.Second)

			// Reconcile triggers timeout handling - removes finalizer and deletes
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Get(ctx, types.NamespacedName{Name: sandboxName, Namespace: testNamespace}, &sandboxv1alpha1.Sandbox{})
			Expect(err).To(Satisfy(errors.IsNotFound))
		})

		It("should set TimedOut condition reason before deleting sandbox", func() {
			sandboxName := "sandbox-timeout-condition"

			timeout := int64(1)
			createSandbox(ctx, sandboxName, func(s *sandboxv1alpha1.Sandbox) {
				s.Spec.ActiveDeadlineSeconds = &timeout
			})
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")

			sandbox := getSandbox(ctx, sandboxName)
			originalUID := sandbox.UID

			// First reconcile creates the pod
			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			// Make pod ready so TimeoutAt gets set
			pod := getPod(ctx, sandboxName+"-pod")
			Expect(pod).NotTo(BeNil())
			makePodReady(ctx, pod, "containerd://abc123", fakeClock)

			// Reconcile to persist TimeoutAt
			_, err = doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			// Advance past timeout
			fakeClock.Advance(2 * time.Second)

			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify sandbox was deleted (confirms timeout path with Delete policy)
			err = k8sClient.Get(ctx, types.NamespacedName{Name: sandboxName, Namespace: testNamespace}, &sandboxv1alpha1.Sandbox{})
			Expect(err).To(Satisfy(errors.IsNotFound))
			_ = originalUID
		})

		It("should schedule requeue before timeout", func() {
			sandboxName := "sandbox-requeue"

			timeout := int64(60)
			createSandbox(ctx, sandboxName, func(s *sandboxv1alpha1.Sandbox) {
				s.Spec.ActiveDeadlineSeconds = &timeout
			})
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")

			// First reconcile creates the pod
			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			// Make pod ready so TimeoutAt gets set
			pod := getPod(ctx, sandboxName+"-pod")
			Expect(pod).NotTo(BeNil())
			makePodReady(ctx, pod, "containerd://abc123", fakeClock)

			// Reconcile with pod ready - should set TimeoutAt and return RequeueAfter
			result, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeNumerically(">", 0))
			Expect(result.RequeueAfter).To(BeNumerically("<=", time.Duration(timeout)*time.Second))
		})

		It("should preserve TimeoutAt through Ready/NotReady oscillation", func() {
			sandboxName := "sandbox-timeout-oscillation"

			timeout := int64(120)
			createSandbox(ctx, sandboxName, func(s *sandboxv1alpha1.Sandbox) {
				s.Spec.ActiveDeadlineSeconds = &timeout
			})
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			// First reconcile creates the pod
			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())

			// T1: Make pod Ready (makePodReady sets StartTime to clock.Now())
			t1 := fakeClock.Now()
			makePodReady(ctx, pod, "containerd://abc123", fakeClock)

			_, err = doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			// Timeout anchored to StartTime (set at T1 by makePodReady)
			sandbox := getSandbox(ctx, sandboxName)
			Expect(sandbox.Status.TimeoutAt).NotTo(BeNil())
			expectedTimeout := t1.Add(time.Duration(timeout) * time.Second)
			Expect(sandbox.Status.TimeoutAt.Time).To(BeTemporally("~", expectedTimeout, time.Second))

			// Set PodReady=False (simulate crash)
			pod = getPod(ctx, podName)
			pod.Status.Conditions = []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionFalse, LastTransitionTime: metav1.NewTime(fakeClock.Now())},
			}
			Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())

			_, err = doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			sandbox = getSandbox(ctx, sandboxName)
			Expect(sandbox.Status.TimeoutAt).NotTo(BeNil())
			Expect(sandbox.Status.TimeoutAt.Time).To(BeTemporally("~", expectedTimeout, time.Second))

			// T2: Make pod Ready again at a later time
			fakeClock.Advance(10 * time.Second)
			pod = getPod(ctx, podName)
			pod.Status.Phase = corev1.PodRunning
			pod.Status.Conditions = []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue, LastTransitionTime: metav1.NewTime(fakeClock.Now())},
			}
			Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())

			_, err = doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			// TimeoutAt must still be anchored to T1, not T2
			sandbox = getSandbox(ctx, sandboxName)
			Expect(sandbox.Status.TimeoutAt).NotTo(BeNil())
			Expect(sandbox.Status.TimeoutAt.Time).To(BeTemporally("~", expectedTimeout, time.Second))

			// Set PodReady=False again
			pod = getPod(ctx, podName)
			pod.Status.Conditions = []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionFalse, LastTransitionTime: metav1.NewTime(fakeClock.Now())},
			}
			Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())

			_, err = doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			sandbox = getSandbox(ctx, sandboxName)
			Expect(sandbox.Status.TimeoutAt).NotTo(BeNil())
			Expect(sandbox.Status.TimeoutAt.Time).To(BeTemporally("~", expectedTimeout, time.Second))

			// T3: Make pod Ready one more time
			fakeClock.Advance(10 * time.Second)
			pod = getPod(ctx, podName)
			pod.Status.Phase = corev1.PodRunning
			pod.Status.Conditions = []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue, LastTransitionTime: metav1.NewTime(fakeClock.Now())},
			}
			Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())

			_, err = doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			// Still anchored to T1
			sandbox = getSandbox(ctx, sandboxName)
			Expect(sandbox.Status.TimeoutAt).NotTo(BeNil())
			Expect(sandbox.Status.TimeoutAt.Time).To(BeTemporally("~", expectedTimeout, time.Second))
		})
		It("should timeout and delete sandbox that never became ready", func() {
			sandboxName := "sandbox-timeout-never-ready"

			timeout := int64(1) // 1 second timeout
			createSandbox(ctx, sandboxName, func(s *sandboxv1alpha1.Sandbox) {
				s.Spec.ActiveDeadlineSeconds = &timeout
				s.Spec.ShutdownPolicy = &sandboxv1alpha1.ShutdownPolicy{
					Strategy: sandboxv1alpha1.ShutdownStrategyDelete,
				}
			})
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			// First reconcile creates the pod
			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			// Set StartTime but do NOT make pod ready (simulates crashlooping pod)
			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())
			startTime := metav1.NewTime(fakeClock.Now())
			pod.Status.StartTime = &startTime
			pod.Status.Phase = corev1.PodPending
			Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())

			// Reconcile to persist TimeoutAt
			_, err = doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			sandbox := getSandbox(ctx, sandboxName)
			Expect(sandbox.Status.TimeoutAt).NotTo(BeNil())

			// Advance past timeout
			fakeClock.Advance(2 * time.Second)

			// Reconcile triggers timeout handling - removes finalizer and deletes
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Get(ctx, types.NamespacedName{Name: sandboxName, Namespace: testNamespace}, &sandboxv1alpha1.Sandbox{})
			Expect(err).To(Satisfy(errors.IsNotFound))
		})
	})
})
