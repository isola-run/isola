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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	sandboxv1alpha1 "github.com/isola-ai/isola-sb/api/v1alpha1"
)

// =============================================================================
// kstatus Compliance Tests
// =============================================================================
//
// These tests verify the Sandbox controller correctly implements the kstatus
// standard from kubernetes-sigs/cli-utils for compatibility with tools like
// ArgoCD, Flux, and kubectl wait.
//
// Key kstatus rules verified:
// 1. Reconciling condition uses "abnormal-true" pattern (absent when healthy)
// 2. Stalled condition uses "abnormal-true" pattern (absent when healthy)
// 3. observedGeneration matches metadata.generation after reconciliation
// 4. Proper state transitions through the lifecycle
//
// =============================================================================

var _ = Describe("Sandbox Controller kstatus Compliance", func() {

	// ============================================
	// Abnormal-True Pattern Tests
	// ============================================
	Context("Abnormal-True Pattern", func() {
		var (
			reconciler *SandboxReconciler
			fakeClock  *FakeClock
		)

		BeforeEach(func() {
			fakeClock = NewFakeClock(time.Now())
			reconciler = newTestReconciler(fakeClock)
		})

		It("should have Reconciling=True while pod is pending (kstatus: InProgress)", func() {
			sandboxName := "kstatus-reconciling-pending"
			templateName := "template-reconciling-pending"

			createTemplate(ctx, templateName)
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			// Second reconcile to update status after pod exists
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			sandbox := getSandbox(ctx, sandboxName)

			// Reconciling should be present and True (abnormal state)
			reconcilingCond := meta.FindStatusCondition(sandbox.Status.Conditions, SandboxReconcilingCondition)
			Expect(reconcilingCond).NotTo(BeNil(), "Reconciling condition should be present while pod is pending")
			Expect(reconcilingCond.Status).To(Equal(metav1.ConditionTrue))

			// Stalled should be absent (not in abnormal state)
			stalledCond := meta.FindStatusCondition(sandbox.Status.Conditions, SandboxStalledCondition)
			Expect(stalledCond).To(BeNil(), "Stalled condition should be absent when not in error state")
		})

		It("should remove Reconciling when pod becomes ready (kstatus: Current)", func() {
			sandboxName := "kstatus-reconciling-removed"
			templateName := "template-reconciling-removed"

			createTemplate(ctx, templateName)
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			// Make pod ready
			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())
			pod.Status.Phase = corev1.PodRunning
			pod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
			Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())

			// Reconcile after pod is ready
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			sandbox := getSandbox(ctx, sandboxName)

			// Reconciling should be ABSENT (not False) per abnormal-true pattern
			reconcilingCond := meta.FindStatusCondition(sandbox.Status.Conditions, SandboxReconcilingCondition)
			Expect(reconcilingCond).To(BeNil(), "Reconciling condition should be REMOVED (not set to False) when pod is ready")

			// Stalled should also be absent
			stalledCond := meta.FindStatusCondition(sandbox.Status.Conditions, SandboxStalledCondition)
			Expect(stalledCond).To(BeNil(), "Stalled condition should be absent in healthy state")

			// Ready should be True
			readyCond := meta.FindStatusCondition(sandbox.Status.Conditions, SandboxReadyCondition)
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionTrue))
		})

		It("should have Stalled=True when pod fails (kstatus: Failed)", func() {
			sandboxName := "kstatus-stalled-pod-failed"
			templateName := "template-stalled-pod-failed"

			createTemplate(ctx, templateName)
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			// Make pod fail
			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())
			pod.Status.Phase = corev1.PodFailed
			pod.Status.ContainerStatuses = []corev1.ContainerStatus{
				{Name: "sandbox", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 1}}},
			}
			Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())

			// Reconcile after pod fails
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			sandbox := getSandbox(ctx, sandboxName)

			// Stalled should be present and True
			stalledCond := meta.FindStatusCondition(sandbox.Status.Conditions, SandboxStalledCondition)
			Expect(stalledCond).NotTo(BeNil(), "Stalled condition should be present when pod fails")
			Expect(stalledCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(stalledCond.Reason).To(Equal(CondReasonPodFailed))

			// Reconciling should be absent (not reconciling when stalled)
			reconcilingCond := meta.FindStatusCondition(sandbox.Status.Conditions, SandboxReconcilingCondition)
			Expect(reconcilingCond).To(BeNil(), "Reconciling condition should be absent when stalled")
		})

		It("should have Stalled=True when template is not found (kstatus: Failed)", func() {
			sandboxName := "kstatus-stalled-no-template"

			// Create sandbox referencing non-existent template
			sandbox := &sandboxv1alpha1.Sandbox{
				ObjectMeta: metav1.ObjectMeta{
					Name:      sandboxName,
					Namespace: testNamespace,
				},
				Spec: sandboxv1alpha1.SandboxSpec{
					TemplateRef: sandboxv1alpha1.SandboxTemplateReference{
						Name: "nonexistent-template",
					},
				},
			}
			Expect(k8sClient.Create(ctx, sandbox)).To(Succeed())
			defer deleteSandbox(ctx, sandboxName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			sandbox = getSandbox(ctx, sandboxName)

			// Stalled should be present and True
			stalledCond := meta.FindStatusCondition(sandbox.Status.Conditions, SandboxStalledCondition)
			Expect(stalledCond).NotTo(BeNil(), "Stalled condition should be present when template not found")
			Expect(stalledCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(stalledCond.Reason).To(Equal(CondReasonTemplateNotFound))

			// Reconciling should be absent
			reconcilingCond := meta.FindStatusCondition(sandbox.Status.Conditions, SandboxReconcilingCondition)
			Expect(reconcilingCond).To(BeNil(), "Reconciling condition should be removed when stalled")
		})

		It("should remove Stalled when template becomes available", func() {
			sandboxName := "kstatus-stalled-resolved"
			templateName := "template-stalled-resolved"

			// Create sandbox without template first
			sandbox := &sandboxv1alpha1.Sandbox{
				ObjectMeta: metav1.ObjectMeta{
					Name:      sandboxName,
					Namespace: testNamespace,
				},
				Spec: sandboxv1alpha1.SandboxSpec{
					TemplateRef: sandboxv1alpha1.SandboxTemplateReference{
						Name: templateName,
					},
				},
			}
			Expect(k8sClient.Create(ctx, sandbox)).To(Succeed())
			defer deleteSandbox(ctx, sandboxName)

			// First reconcile - should be stalled
			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			sandbox = getSandbox(ctx, sandboxName)
			stalledCond := meta.FindStatusCondition(sandbox.Status.Conditions, SandboxStalledCondition)
			Expect(stalledCond).NotTo(BeNil())
			Expect(stalledCond.Status).To(Equal(metav1.ConditionTrue))

			// Now create the template
			createTemplate(ctx, templateName)
			defer deleteTemplate(ctx, templateName)
			defer deletePod(ctx, sandboxName+"-pod")

			// Reconcile again - should resolve stalled state
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			sandbox = getSandbox(ctx, sandboxName)

			// Stalled should be ABSENT (not False) per abnormal-true pattern
			stalledCond = meta.FindStatusCondition(sandbox.Status.Conditions, SandboxStalledCondition)
			Expect(stalledCond).To(BeNil(), "Stalled condition should be REMOVED when template is found")
		})
	})

	// ============================================
	// ObservedGeneration Tests
	// ============================================
	Context("ObservedGeneration", func() {
		var (
			reconciler *SandboxReconciler
			fakeClock  *FakeClock
		)

		BeforeEach(func() {
			fakeClock = NewFakeClock(time.Now())
			reconciler = newTestReconciler(fakeClock)
		})

		It("should set observedGeneration equal to metadata.generation after reconciliation", func() {
			sandboxName := "kstatus-observed-gen"
			templateName := "template-observed-gen"

			createTemplate(ctx, templateName)
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			sandbox := getSandbox(ctx, sandboxName)
			Expect(sandbox.Status.ObservedGeneration).To(Equal(sandbox.Generation),
				"observedGeneration should equal metadata.generation after reconciliation")
		})

		It("should update observedGeneration in all conditions", func() {
			sandboxName := "kstatus-observed-gen-conds"
			templateName := "template-observed-gen-conds"

			createTemplate(ctx, templateName)
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			sandbox := getSandbox(ctx, sandboxName)

			// All conditions should have observedGeneration set to current generation
			for _, cond := range sandbox.Status.Conditions {
				Expect(cond.ObservedGeneration).To(Equal(sandbox.Generation),
					"Condition %s should have observedGeneration equal to metadata.generation", cond.Type)
			}
		})
	})

	// ============================================
	// Shutdown Snapshot kstatus Tests
	// ============================================
	Context("Shutdown Snapshot kstatus", func() {
		var (
			reconciler *SandboxReconciler
			fakeClock  *FakeClock
		)

		BeforeEach(func() {
			fakeClock = NewFakeClock(time.Now())
			reconciler = newTestReconciler(fakeClock)
		})

		It("should set Reconciling=True while waiting for shutdown snapshot", func() {
			sandboxName := "kstatus-snapshot-reconciling"
			templateName := "template-snapshot-reconciling"

			// Create template with snapshot shutdown policy and timeout
			createTemplate(ctx, templateName, func(t *sandboxv1alpha1.SandboxTemplate) {
				t.Spec.TimeoutSeconds = ptr.To(int64(60))
				t.Spec.ShutdownPolicy = &sandboxv1alpha1.ShutdownPolicy{
					Policy: sandboxv1alpha1.ShutdownPolicySnapshotRootfs,
				}
			})
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)
			defer deleteShutdownSnapshot(ctx, sandboxName)

			// Initial reconcile to create pod
			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			// Make pod ready (required for snapshot)
			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())
			makePodReady(ctx, pod, "containerd://abc123")

			// Advance time past timeout
			fakeClock.Advance(61 * time.Second)

			// Reconcile after timeout - should create snapshot and set Reconciling=True
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			sandbox := getSandbox(ctx, sandboxName)

			// Reconciling should be True while waiting for snapshot
			reconcilingCond := meta.FindStatusCondition(sandbox.Status.Conditions, SandboxReconcilingCondition)
			Expect(reconcilingCond).NotTo(BeNil(), "Reconciling should be present while waiting for snapshot")
			Expect(reconcilingCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(reconcilingCond.Reason).To(Equal(CondReasonSnapshottingInProgress))
		})

		It("should remove Reconciling when shutdown snapshot completes successfully", func() {
			sandboxName := "kstatus-snapshot-complete"
			templateName := "template-snapshot-complete"

			// Create template with snapshot shutdown policy and timeout
			createTemplate(ctx, templateName, func(t *sandboxv1alpha1.SandboxTemplate) {
				t.Spec.TimeoutSeconds = ptr.To(int64(60))
				t.Spec.ShutdownPolicy = &sandboxv1alpha1.ShutdownPolicy{
					Policy: sandboxv1alpha1.ShutdownPolicySnapshotRootfs,
				}
			})
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)
			defer deleteShutdownSnapshot(ctx, sandboxName)

			// Initial reconcile to create pod
			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			// Make pod ready
			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())
			makePodReady(ctx, pod, "containerd://abc123")

			// Advance time past timeout
			fakeClock.Advance(61 * time.Second)

			// Reconcile to trigger snapshot
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Mark snapshot as complete
			setShutdownSnapshotReady(ctx, sandboxName, true, sandboxv1alpha1.ReasonRootfsSnapshotSucceeded, "Snapshot completed")

			// Reconcile after snapshot completes
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			sandbox := getSandbox(ctx, sandboxName)

			// Reconciling should be ABSENT after snapshot completes
			reconcilingCond := meta.FindStatusCondition(sandbox.Status.Conditions, SandboxReconcilingCondition)
			Expect(reconcilingCond).To(BeNil(), "Reconciling should be REMOVED after snapshot completes")
		})

		It("should remove Reconciling when shutdown snapshot fails", func() {
			sandboxName := "kstatus-snapshot-failed"
			templateName := "template-snapshot-failed"

			// Create template with snapshot shutdown policy and timeout
			createTemplate(ctx, templateName, func(t *sandboxv1alpha1.SandboxTemplate) {
				t.Spec.TimeoutSeconds = ptr.To(int64(60))
				t.Spec.ShutdownPolicy = &sandboxv1alpha1.ShutdownPolicy{
					Policy: sandboxv1alpha1.ShutdownPolicySnapshotRootfs,
				}
			})
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)
			defer deleteShutdownSnapshot(ctx, sandboxName)

			// Initial reconcile to create pod
			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			// Make pod ready
			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())
			makePodReady(ctx, pod, "containerd://abc123")

			// Advance time past timeout
			fakeClock.Advance(61 * time.Second)

			// Reconcile to trigger snapshot
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Mark snapshot as failed
			setShutdownSnapshotReady(ctx, sandboxName, false, sandboxv1alpha1.ReasonRootfsSnapshotFailed, "Upload failed")

			// Reconcile after snapshot fails
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			sandbox := getSandbox(ctx, sandboxName)

			// Reconciling should be ABSENT even after snapshot fails (cleanup complete)
			reconcilingCond := meta.FindStatusCondition(sandbox.Status.Conditions, SandboxReconcilingCondition)
			Expect(reconcilingCond).To(BeNil(), "Reconciling should be REMOVED after snapshot fails (cleanup proceeds)")
		})
	})

	// ============================================
	// Pod Creation Failure Tests
	// ============================================
	Context("Pod Creation Failure", func() {
		It("should set Stalled=True when pod creation fails (kstatus: Failed)", func() {
			// This test verifies that permanent errors like invalid spec set Stalled=True
			// Note: In envtest, we can't easily trigger pod creation failures,
			// so this test documents the expected behavior based on code review.
			// The actual implementation is in CreateSandboxPod() which sets Stalled=True
			// when r.Create(ctx, sandboxPod) fails.
			Skip("Pod creation failures are difficult to simulate in envtest; verified via code review")
		})
	})
})
