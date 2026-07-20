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
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	sandboxv1alpha1 "github.com/isola-run/isola/api/v1alpha1"
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
				s.Spec.TimeoutSeconds = &timeout
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
				s.Spec.TimeoutSeconds = &timeout
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
				s.Spec.TimeoutSeconds = &timeout
				s.Spec.TerminationPolicy = &sandboxv1alpha1.TerminationPolicy{
					Type: sandboxv1alpha1.TerminationTypeDelete,
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

		It("should set Succeeded=True with Timeout reason when sandbox times out", func() {
			sandboxName := "sandbox-timeout-condition"

			timeout := int64(1)
			createSandbox(ctx, sandboxName, func(s *sandboxv1alpha1.Sandbox) {
				s.Spec.TimeoutSeconds = &timeout
				s.Spec.TerminationPolicy = &sandboxv1alpha1.TerminationPolicy{
					Type:           sandboxv1alpha1.TerminationTypeSnapshotRootfs,
					SnapshotRootfs: &sandboxv1alpha1.SnapshotRootfsTermination{SnapshotName: "test-snapshot"},
				}
			})
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)
			defer deleteTerminationSnapshot(ctx, sandboxName)

			// First reconcile creates the pod
			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			// Make pod ready so TimeoutAt gets set
			pod := bindPodToNode(ctx, podName)
			makePodReady(ctx, pod, "containerd://abc123", fakeClock)

			// Reconcile to persist TimeoutAt
			_, err = doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			// Advance past timeout
			fakeClock.Advance(2 * time.Second)

			// Reconcile triggers timeout — sets Succeeded condition and begins
			// SnapshotRootfs finalization (sandbox survives while snapshot is pending)
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Sandbox still exists (snapshot not yet complete) — verify Succeeded=True
			// (timeout is normal lifecycle completion, not failure)
			sandbox := getSandbox(ctx, sandboxName)
			Expect(hasConditionWithReason(sandbox, sandboxv1alpha1.SandboxSucceededCondition, metav1.ConditionTrue, CondReasonTimeout)).To(BeTrue())
		})

		It("should schedule requeue before timeout", func() {
			sandboxName := "sandbox-requeue"

			timeout := int64(60)
			createSandbox(ctx, sandboxName, func(s *sandboxv1alpha1.Sandbox) {
				s.Spec.TimeoutSeconds = &timeout
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
				s.Spec.TimeoutSeconds = &timeout
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
				s.Spec.TimeoutSeconds = &timeout
				s.Spec.TerminationPolicy = &sandboxv1alpha1.TerminationPolicy{
					Type: sandboxv1alpha1.TerminationTypeDelete,
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

	// ============================================
	// Startup Timeout Tests
	// ============================================
	Context("Startup timeout", func() {
		var (
			reconciler *SandboxReconciler
			fakeClock  *FakeClock
		)

		BeforeEach(func() {
			fakeClock = NewFakeClock(time.Now())
			reconciler = newTestReconciler(fakeClock)
		})

		It("should delete sandbox when pod not ready after startup deadline", func() {
			sandboxName := "sandbox-startup-timeout-delete"

			createSandbox(ctx, sandboxName, func(s *sandboxv1alpha1.Sandbox) {
				s.Spec.StartupTimeoutSeconds = ptr.To(int64(10))
			})
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			// First reconcile creates the pod
			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())

			// Pod stays pending (don't make it ready)

			// Advance the fake clock past the pod's creation timestamp + startup deadline.
			// The pod's CreationTimestamp is set by the API server at real wall-clock time,
			// which may differ from the FakeClock's initial value.
			fakeClock.Set(pod.CreationTimestamp.Add(11 * time.Second))

			// Reconcile detects startup timeout and issues Delete (sets DeletionTimestamp
			// but the finalizer keeps the object around)
			_, err = doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			// Sandbox still exists (finalizer holds it) — verify Succeeded=False was set
			sandbox := getSandbox(ctx, sandboxName)
			Expect(hasConditionWithReason(sandbox, sandboxv1alpha1.SandboxSucceededCondition, metav1.ConditionFalse, CondReasonStartupTimeoutExceeded)).To(BeTrue())

			// Second reconcile runs the finalizer, removes it, and the object is deleted
			_, err = doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Get(ctx, types.NamespacedName{Name: sandboxName, Namespace: testNamespace}, &sandboxv1alpha1.Sandbox{})
			Expect(err).To(Satisfy(errors.IsNotFound))
		})

		It("should not fire startup timeout when pod becomes ready before deadline", func() {
			sandboxName := "sandbox-startup-ready-before"

			createSandbox(ctx, sandboxName, func(s *sandboxv1alpha1.Sandbox) {
				s.Spec.StartupTimeoutSeconds = ptr.To(int64(30))
			})
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			// First reconcile creates the pod
			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())

			// Make pod ready before deadline
			makePodReady(ctx, pod, "containerd://abc123", fakeClock)

			// Advance past the startup deadline relative to pod creation
			fakeClock.Set(pod.CreationTimestamp.Add(35 * time.Second))

			// Reconcile -- sandbox should still exist and be ready
			_, err = doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			sandbox := getSandbox(ctx, sandboxName)
			readyCond := meta.FindStatusCondition(sandbox.Status.Conditions, sandboxv1alpha1.SandboxReadyCondition)
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(readyCond.Reason).To(Equal(CondReasonPodRunning))
		})

		It("should not fire startup timeout when deadline is far in the future", func() {
			sandboxName := "sandbox-startup-nil-timeout"

			// The CRD has +kubebuilder:default=90 so the API server always sets
			// StartupTimeoutSeconds. Use a very large value to verify that the sandbox
			// survives well past a typical startup window without being timed out.
			createSandbox(ctx, sandboxName, func(s *sandboxv1alpha1.Sandbox) {
				s.Spec.StartupTimeoutSeconds = ptr.To(int64(86400))
			})
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			// First reconcile creates the pod
			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())

			// Pod stays pending

			// Advance 600 seconds -- well past typical startup, but not past 86400s
			fakeClock.Set(pod.CreationTimestamp.Add(600 * time.Second))

			// Reconcile -- sandbox should still exist with PodPending, not StartupTimeoutExceeded
			_, err = doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			sandbox := getSandbox(ctx, sandboxName)
			readyCond := meta.FindStatusCondition(sandbox.Status.Conditions, sandboxv1alpha1.SandboxReadyCondition)
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCond.Reason).To(Equal(CondReasonPodPending))
		})

		It("should use min of startup and lifetime for RequeueAfter", func() {
			sandboxName := "sandbox-startup-requeue-min"

			createSandbox(ctx, sandboxName, func(s *sandboxv1alpha1.Sandbox) {
				s.Spec.StartupTimeoutSeconds = ptr.To(int64(30))
				s.Spec.TimeoutSeconds = ptr.To(int64(120))
			})
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			// First reconcile creates the pod
			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())

			// Align the fake clock to the pod's creation timestamp so that
			// makePodReady sets StartTime at a known point relative to creation.
			fakeClock.Set(pod.CreationTimestamp.Time)

			// Make pod ready so ensureTimeout sets TimeoutAt
			makePodReady(ctx, pod, "containerd://abc123", fakeClock)

			_, err = doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			// Set pod back to not-ready so both startup and lifetime requeue are computed
			pod = getPod(ctx, podName)
			pod.Status.Phase = corev1.PodRunning
			pod.Status.Conditions = []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionFalse, LastTransitionTime: metav1.NewTime(fakeClock.Now())},
			}
			Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())

			result, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			// RequeueAfter should be approximately 30s (startup), not 120s (lifetime).
			// Startup deadline = CreationTimestamp + 30s; lifetime deadline = StartTime + 120s.
			// Since the clock is aligned to CreationTimestamp, both deadlines are in the future
			// and the startup one (30s) is shorter.
			Expect(result.RequeueAfter).To(BeNumerically(">", 0))
			Expect(result.RequeueAfter).To(BeNumerically("<=", 30*time.Second))
			Expect(result.RequeueAfter).To(BeNumerically("<", 120*time.Second))
		})

		It("should fail and clean up when the pod can never be created", func() {
			sandboxName := "sandbox-startup-pod-rejected"

			createSandbox(ctx, sandboxName, func(s *sandboxv1alpha1.Sandbox) {
				s.Spec.StartupTimeoutSeconds = ptr.To(int64(10))
				s.Spec.PodTemplate.Spec.Containers = []corev1.Container{
					{Name: "dup", Image: "busybox:latest", Command: []string{"sleep", "infinity"}},
					{Name: "dup", Image: "busybox:latest", Command: []string{"sleep", "infinity"}},
				}
			})
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")

			sandbox := getSandbox(ctx, sandboxName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).To(HaveOccurred())

			Expect(getPod(ctx, sandboxName+"-pod")).To(BeNil())

			fakeClock.Set(sandbox.CreationTimestamp.Add(11 * time.Second))

			_, err = doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			sandbox = getSandbox(ctx, sandboxName)
			Expect(hasConditionWithReason(sandbox, sandboxv1alpha1.SandboxSucceededCondition, metav1.ConditionFalse, CondReasonStartupTimeoutExceeded)).To(BeTrue())

			_, err = doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Get(ctx, types.NamespacedName{Name: sandboxName, Namespace: testNamespace}, &sandboxv1alpha1.Sandbox{})
			Expect(err).To(Satisfy(errors.IsNotFound))
		})

		It("should recreate the pod, not fail, when a running pod is deleted out-of-band after the startup window", func() {
			sandboxName := "sandbox-pod-vanished"

			createSandbox(ctx, sandboxName, func(s *sandboxv1alpha1.Sandbox) {
				s.Spec.StartupTimeoutSeconds = ptr.To(int64(10))
				s.Spec.TimeoutSeconds = ptr.To(int64(3600))
			})
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())

			makePodReady(ctx, pod, "containerd://abc123", reconciler.clock())
			_, err = doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			sandbox := getSandbox(ctx, sandboxName)
			Expect(sandbox.Status.PodCreatedAt).NotTo(BeNil())

			deletePod(ctx, podName)
			Expect(getPod(ctx, podName)).To(BeNil())

			fakeClock.Set(sandbox.CreationTimestamp.Add(20 * time.Second))

			_, err = doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			sandbox = getSandbox(ctx, sandboxName)
			Expect(meta.FindStatusCondition(sandbox.Status.Conditions, sandboxv1alpha1.SandboxSucceededCondition)).To(BeNil())
			Expect(getPod(ctx, podName)).NotTo(BeNil())
		})

		It("should not fire startup timeout at exact boundary", func() {
			sandboxName := "sandbox-startup-exact-boundary"

			createSandbox(ctx, sandboxName, func(s *sandboxv1alpha1.Sandbox) {
				s.Spec.StartupTimeoutSeconds = ptr.To(int64(10))
			})
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			// First reconcile creates the pod
			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())

			// Set the fake clock to exactly the startup deadline boundary
			// (CreationTimestamp + StartupTimeoutSeconds)
			fakeClock.Set(pod.CreationTimestamp.Add(10 * time.Second))

			// Reconcile -- sandbox should still exist (After is strictly >, not >=)
			_, err = doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Get(ctx, types.NamespacedName{Name: sandboxName, Namespace: testNamespace}, &sandboxv1alpha1.Sandbox{})
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
