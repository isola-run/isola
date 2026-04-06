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
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	sandboxv1alpha1 "github.com/isola-run/isola/api/v1alpha1"
)

var _ = Describe("Sandbox Controller", func() {

	// ============================================
	// Finalizer Behavior Tests
	// ============================================
	Context("Finalizer Behavior", func() {
		var (
			reconciler *SandboxReconciler
			fakeClock  *FakeClock
		)

		BeforeEach(func() {
			fakeClock = NewFakeClock(time.Now())
			reconciler = newTestReconciler(fakeClock)
		})

		It("should add finalizer on reconcile", func() {
			sandboxName := "sandbox-finalizer-add"

			createSandbox(ctx, sandboxName)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify finalizer is present
			sandbox := getSandbox(ctx, sandboxName)
			Expect(sandbox.Finalizers).To(ContainElement(SandboxFinalizer))
		})

		It("should preserve all conditions after finalizer is added", func() {
			sandboxName := "sandbox-conditions-preserved"

			createSandbox(ctx, sandboxName)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify ALL expected conditions are present (not just finalizer)
			sandbox := getSandbox(ctx, sandboxName)
			Expect(sandbox.Finalizers).To(ContainElement(SandboxFinalizer))

			// PodReady should be set
			cond := meta.FindStatusCondition(sandbox.Status.Conditions, SandboxPodReadyCondition)
			Expect(cond).NotTo(BeNil(), "PodReady condition should be set")

			// Ready should be set
			cond = meta.FindStatusCondition(sandbox.Status.Conditions, sandboxv1alpha1.SandboxReadyCondition)
			Expect(cond).NotTo(BeNil(), "Ready condition should be set")
		})

		It("should execute Delete policy and remove finalizer on deletion", func() {
			sandboxName := "sandbox-delete-policy"

			createSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			sandbox := getSandbox(ctx, sandboxName)
			Expect(sandbox.Finalizers).To(ContainElement(SandboxFinalizer))

			Expect(k8sClient.Delete(ctx, sandbox)).To(Succeed())

			// Sandbox is being deleted (DeletionTimestamp set) — Succeeded should not be set
			sandbox = getSandbox(ctx, sandboxName)
			Expect(meta.FindStatusCondition(sandbox.Status.Conditions, sandboxv1alpha1.SandboxSucceededCondition)).To(BeNil())

			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Get(ctx, types.NamespacedName{Name: sandboxName, Namespace: testNamespace}, &sandboxv1alpha1.Sandbox{})
			Expect(err).To(Satisfy(errors.IsNotFound))
		})

		It("should reject SnapshotRootfs termination for multi-container sandboxes", func() {
			sandboxName := "sandbox-multi-container-snapshot"

			reconciler = newTestReconciler(fakeClock)

			createSandbox(ctx, sandboxName, func(s *sandboxv1alpha1.Sandbox) {
				s.Spec.PodTemplate.Spec.Containers = append(s.Spec.PodTemplate.Spec.Containers, corev1.Container{
					Name:    "sidecar",
					Image:   "busybox:latest",
					Command: []string{"sleep", "infinity"},
				})
				s.Spec.TerminationPolicy = &sandboxv1alpha1.TerminationPolicy{
					Type:           sandboxv1alpha1.TerminationTypeSnapshotRootfs,
					SnapshotRootfs: &sandboxv1alpha1.SnapshotRootfsTermination{SnapshotName: "test-snapshot"},
				}
			})

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			// First reconcile - creates pod and adds finalizer
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Bind pod and make it ready
			pod := bindPodToNode(ctx, podName)
			makePodReady(ctx, pod, "containerd://abc123", fakeClock)

			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Delete the sandbox to trigger finalization
			sandbox := getSandbox(ctx, sandboxName)
			Expect(k8sClient.Delete(ctx, sandbox)).To(Succeed())

			// Reconcile - should reject multi-container snapshot and proceed to deletion
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// No snapshot should have been created
			Expect(getTerminationSnapshot(ctx, sandboxName)).To(BeNil())

			// Sandbox should be deleted (rejection allows finalization to proceed)
			err = k8sClient.Get(ctx, types.NamespacedName{Name: sandboxName, Namespace: testNamespace}, &sandboxv1alpha1.Sandbox{})
			Expect(err).To(Satisfy(errors.IsNotFound))
		})

		It("should not recreate termination snapshot after TTL deletion", func() {
			sandboxName := "sandbox-ttl-zero"

			reconciler = newTestReconciler(fakeClock)

			createSandbox(ctx, sandboxName, func(s *sandboxv1alpha1.Sandbox) {
				s.Spec.TerminationPolicy = &sandboxv1alpha1.TerminationPolicy{
					Type: sandboxv1alpha1.TerminationTypeSnapshotRootfs,
					SnapshotRootfs: &sandboxv1alpha1.SnapshotRootfsTermination{
						SnapshotName:            "test-snapshot",
						TTLSecondsAfterFinished: ptr.To(int32(0)),
					},
				}
			})

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)
			defer deleteTerminationSnapshot(ctx, sandboxName)

			// First reconcile - creates pod and adds finalizer
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Bind pod and make it ready
			pod := bindPodToNode(ctx, podName)
			makePodReady(ctx, pod, "containerd://abc123", fakeClock)

			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Delete the sandbox to trigger finalization
			sandbox := getSandbox(ctx, sandboxName)
			Expect(k8sClient.Delete(ctx, sandbox)).To(Succeed())

			// Reconcile - should create termination snapshot
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify snapshot was created and status field was set
			snap := getTerminationSnapshot(ctx, sandboxName)
			Expect(snap).NotTo(BeNil())

			sandbox = getSandbox(ctx, sandboxName)
			Expect(sandbox.Status.TerminationSnapshotCreated).To(BeTrue())

			// Simulate TTL controller deleting the snapshot (TTL=0)
			deleteTerminationSnapshot(ctx, sandboxName)
			Expect(getTerminationSnapshot(ctx, sandboxName)).To(BeNil())

			// Reconcile again - should NOT recreate the snapshot, should proceed to cleanup
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Snapshot should still be nil (not recreated)
			Expect(getTerminationSnapshot(ctx, sandboxName)).To(BeNil())

			// Sandbox should be deleted (cleanup completed)
			err = k8sClient.Get(ctx, types.NamespacedName{Name: sandboxName, Namespace: testNamespace}, &sandboxv1alpha1.Sandbox{})
			Expect(err).To(Satisfy(errors.IsNotFound))
		})

		It("should execute SnapshotRootfs policy on deletion", func() {
			sandboxName := "sandbox-snapshot-delete"

			recorder := events.NewFakeRecorder(10)
			reconciler = newTestReconcilerWithRecorder(fakeClock, recorder)

			createSandbox(ctx, sandboxName, func(s *sandboxv1alpha1.Sandbox) {
				s.Spec.TerminationPolicy = &sandboxv1alpha1.TerminationPolicy{
					Type:           sandboxv1alpha1.TerminationTypeSnapshotRootfs,
					SnapshotRootfs: &sandboxv1alpha1.SnapshotRootfsTermination{SnapshotName: "test-snapshot"},
				}
			})

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)
			defer deleteTerminationSnapshot(ctx, sandboxName)

			// First reconcile - creates pod and adds finalizer
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Bind pod to node (simulating the scheduler) and make it ready
			pod := bindPodToNode(ctx, podName)
			makePodReady(ctx, pod, "containerd://abc123", fakeClock)

			// Reconcile to update status
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Delete the sandbox
			sandbox := getSandbox(ctx, sandboxName)
			Expect(k8sClient.Delete(ctx, sandbox)).To(Succeed())

			// Reconcile - should create RootfsSnapshot
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify RootfsSnapshot was created
			rootfsSnapshot := getTerminationSnapshot(ctx, sandboxName)
			Expect(rootfsSnapshot).NotTo(BeNil())
			Expect(rootfsSnapshot.Spec.SandboxName).To(Equal(sandboxName))

			// Mark RootfsSnapshot as complete
			setTerminationSnapshotReady(ctx, sandboxName, true, sandboxv1alpha1.ReasonRootfsSnapshotSucceeded, "All snapshots completed")

			// Reconcile - should complete deletion
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Sandbox should be gone
			err = k8sClient.Get(ctx, types.NamespacedName{Name: sandboxName, Namespace: testNamespace}, &sandboxv1alpha1.Sandbox{})
			Expect(err).To(Satisfy(errors.IsNotFound))
		})
	})
})
