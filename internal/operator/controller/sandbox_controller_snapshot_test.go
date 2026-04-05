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
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	sandboxv1alpha1 "github.com/isola-run/isola/api/v1alpha1"
)

var _ = Describe("Sandbox Controller", func() {
	Context("Snapshotting", func() {
		var (
			reconciler *SandboxReconciler
			fakeClock  *FakeClock
		)

		BeforeEach(func() {
			fakeClock = NewFakeClock(time.Now())
			reconciler = newTestReconciler(fakeClock)
		})

		It("should skip snapshot when runtime handler is not supported", func() {
			sandboxName := "sandbox-unsupported-runtime"

			recorder := events.NewFakeRecorder(10)
			reconciler = newTestReconcilerWithRecorder(fakeClock, recorder)

			// Replace the suite-level "gvisor" RC with one using "runc" handler
			deleteRuntimeClass(ctx, "gvisor")
			createRuntimeClass(ctx, "gvisor", "runc")
			defer func() {
				deleteRuntimeClass(ctx, "gvisor")
				createRuntimeClass(ctx, "gvisor", "runsc")
			}()

			timeout := int64(1)
			createSandbox(ctx, sandboxName, func(s *sandboxv1alpha1.Sandbox) {
				s.Spec.TimeoutSeconds = &timeout
				s.Spec.TerminationPolicy = &sandboxv1alpha1.TerminationPolicy{
					Strategy:       sandboxv1alpha1.TerminationStrategySnapshotRootfs,
					SnapshotRootfs: &sandboxv1alpha1.SnapshotRootfsTermination{SnapshotName: "test-snapshot"},
				}
			})
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())
			makePodReady(ctx, pod, "containerd://abc123", fakeClock)

			fakeClock.Advance(2 * time.Second)

			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			Eventually(recorder.Events).Should(Receive(ContainSubstring("RuntimeNotSupported")))

			// Sandbox deleted after snapshot skipped
			err = k8sClient.Get(ctx, types.NamespacedName{Name: sandboxName, Namespace: testNamespace}, &sandboxv1alpha1.Sandbox{})
			Expect(err).To(Satisfy(errors.IsNotFound))
		})

		It("should create RootfsSnapshot for supported runtime (runsc)", func() {
			sandboxName := "sandbox-runsc-snapshot"

			timeout := int64(1)
			createSandbox(ctx, sandboxName, func(s *sandboxv1alpha1.Sandbox) {
				s.Spec.TimeoutSeconds = &timeout
				s.Spec.TerminationPolicy = &sandboxv1alpha1.TerminationPolicy{
					Strategy:       sandboxv1alpha1.TerminationStrategySnapshotRootfs,
					SnapshotRootfs: &sandboxv1alpha1.SnapshotRootfsTermination{SnapshotName: "test-snapshot"},
				}
			})
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)
			defer deleteTerminationSnapshot(ctx, sandboxName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := bindPodToNode(ctx, podName)
			makePodReady(ctx, pod, "containerd://abc123def456", fakeClock)

			fakeClock.Advance(2 * time.Second)

			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify RootfsSnapshot was created
			rootfsSnapshot := getTerminationSnapshot(ctx, sandboxName)
			Expect(rootfsSnapshot).NotTo(BeNil())
			Expect(rootfsSnapshot.Spec.SandboxName).To(Equal(sandboxName))
		})

		It("should set condition when sandbox pod is not found during snapshot", func() {
			sandboxName := "sandbox-no-pod-snapshot"

			timeout := int64(1)
			createSandbox(ctx, sandboxName, func(s *sandboxv1alpha1.Sandbox) {
				s.Spec.TimeoutSeconds = &timeout
				s.Spec.TerminationPolicy = &sandboxv1alpha1.TerminationPolicy{
					Strategy:       sandboxv1alpha1.TerminationStrategySnapshotRootfs,
					SnapshotRootfs: &sandboxv1alpha1.SnapshotRootfsTermination{SnapshotName: "test-snapshot"},
				}
			})
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Make pod ready so TimeoutAt gets persisted
			pod := getPod(ctx, sandboxName+"-pod")
			Expect(pod).NotTo(BeNil())
			makePodReady(ctx, pod, "containerd://abc123", fakeClock)

			_, err = doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			// Simulate pod being gone after timeout was set
			deletePod(ctx, sandboxName+"-pod")
			fakeClock.Advance(2 * time.Second)

			// Reconcile - sandbox deleted after snapshot skipped (pod missing)
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Get(ctx, types.NamespacedName{Name: sandboxName, Namespace: testNamespace}, &sandboxv1alpha1.Sandbox{})
			Expect(err).To(Satisfy(errors.IsNotFound))
		})

		It("should skip snapshot when pod exists but is not ready", func() {
			sandboxName := "sandbox-pod-notready-snapshot"

			timeout := int64(1)
			createSandbox(ctx, sandboxName, func(s *sandboxv1alpha1.Sandbox) {
				s.Spec.TimeoutSeconds = &timeout
				s.Spec.TerminationPolicy = &sandboxv1alpha1.TerminationPolicy{
					Strategy:       sandboxv1alpha1.TerminationStrategySnapshotRootfs,
					SnapshotRootfs: &sandboxv1alpha1.SnapshotRootfsTermination{SnapshotName: "test-snapshot"},
				}
			})
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			// Make pod ready so TimeoutAt gets persisted, then make it not-ready
			pod := bindPodToNode(ctx, sandboxName+"-pod")
			makePodReady(ctx, pod, "containerd://abc123", fakeClock)

			_, err = doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			// Simulate pod becoming not-ready (e.g. container crashed)
			pod = getPod(ctx, sandboxName+"-pod")
			pod.Status.Phase = corev1.PodRunning
			pod.Status.Conditions = []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionFalse},
			}
			Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())

			fakeClock.Advance(2 * time.Second)

			// Reconcile triggers timeout — snapshot should be skipped because pod not ready
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// No RootfsSnapshot should be created (pod not ready → skip)
			Expect(getTerminationSnapshot(ctx, sandboxName)).To(BeNil())

			// Sandbox should be deleted (snapshot skipped, cleanup done)
			err = k8sClient.Get(ctx, types.NamespacedName{Name: sandboxName, Namespace: testNamespace}, &sandboxv1alpha1.Sandbox{})
			Expect(err).To(Satisfy(errors.IsNotFound))
		})

		It("should mark snapshot complete when RootfsSnapshot Ready=True", func() {
			sandboxName := "sandbox-snapshot-success"

			recorder := events.NewFakeRecorder(10)
			reconciler = newTestReconcilerWithRecorder(fakeClock, recorder)

			timeout := int64(1)
			createSandbox(ctx, sandboxName, func(s *sandboxv1alpha1.Sandbox) {
				s.Spec.TimeoutSeconds = &timeout
				s.Spec.TerminationPolicy = &sandboxv1alpha1.TerminationPolicy{
					Strategy:       sandboxv1alpha1.TerminationStrategySnapshotRootfs,
					SnapshotRootfs: &sandboxv1alpha1.SnapshotRootfsTermination{SnapshotName: "test-snapshot"},
				}
			})
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)
			defer deleteTerminationSnapshot(ctx, sandboxName)

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			pod := bindPodToNode(ctx, podName)
			makePodReady(ctx, pod, "containerd://abc123", fakeClock)

			fakeClock.Advance(2 * time.Second)
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify RootfsSnapshot was created
			rootfsSnapshot := getTerminationSnapshot(ctx, sandboxName)
			Expect(rootfsSnapshot).NotTo(BeNil())

			// Set RootfsSnapshot Ready=True to simulate successful snapshot
			setTerminationSnapshotReady(ctx, sandboxName, true, sandboxv1alpha1.ReasonRootfsSnapshotSucceeded, "All snapshots completed")

			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			Eventually(recorder.Events).Should(Receive(ContainSubstring("Normal SnapshotSucceeded")))

			// Sandbox deleted after successful snapshot
			err = k8sClient.Get(ctx, types.NamespacedName{Name: sandboxName, Namespace: testNamespace}, &sandboxv1alpha1.Sandbox{})
			Expect(err).To(Satisfy(errors.IsNotFound))
		})

		It("should mark snapshot failed when RootfsSnapshot fails", func() {
			sandboxName := "sandbox-snapshot-fail"

			timeout := int64(1)
			createSandbox(ctx, sandboxName, func(s *sandboxv1alpha1.Sandbox) {
				s.Spec.TimeoutSeconds = &timeout
				s.Spec.TerminationPolicy = &sandboxv1alpha1.TerminationPolicy{
					Strategy:       sandboxv1alpha1.TerminationStrategySnapshotRootfs,
					SnapshotRootfs: &sandboxv1alpha1.SnapshotRootfsTermination{SnapshotName: "test-snapshot"},
				}
			})
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)
			defer deleteTerminationSnapshot(ctx, sandboxName)

			// Setup: reconcile to create pod, then bind it to a node (simulating the scheduler)
			_, _ = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace}})
			pod := bindPodToNode(ctx, podName)
			makePodReady(ctx, pod, "containerd://abc123", fakeClock)

			fakeClock.Advance(2 * time.Second)
			_, _ = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace}})

			// Verify RootfsSnapshot was created
			rootfsSnapshot := getTerminationSnapshot(ctx, sandboxName)
			Expect(rootfsSnapshot).NotTo(BeNil())

			// Set RootfsSnapshot Ready=False with failed reason to simulate failed snapshot
			setTerminationSnapshotReady(ctx, sandboxName, false, sandboxv1alpha1.ReasonRootfsSnapshotFailed, "Snapshot job failed")

			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace}})
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Get(ctx, types.NamespacedName{Name: sandboxName, Namespace: testNamespace}, &sandboxv1alpha1.Sandbox{})
			Expect(err).To(Satisfy(errors.IsNotFound))
		})

		It("should use default timeoutSeconds when not specified", func() {
			sandboxName := "sandbox-default-deadline"

			timeout := int64(1)
			createSandbox(ctx, sandboxName, func(s *sandboxv1alpha1.Sandbox) {
				s.Spec.TimeoutSeconds = &timeout
				s.Spec.TerminationPolicy = &sandboxv1alpha1.TerminationPolicy{
					Strategy:       sandboxv1alpha1.TerminationStrategySnapshotRootfs,
					SnapshotRootfs: &sandboxv1alpha1.SnapshotRootfsTermination{SnapshotName: "test-snapshot"},
				}
			})
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)
			defer deleteTerminationSnapshot(ctx, sandboxName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := bindPodToNode(ctx, podName)
			makePodReady(ctx, pod, "containerd://abc123", fakeClock)

			fakeClock.Advance(2 * time.Second)

			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify RootfsSnapshot was created with default timeoutSeconds (300)
			rootfsSnapshot := getTerminationSnapshot(ctx, sandboxName)
			Expect(rootfsSnapshot).NotTo(BeNil())
			Expect(rootfsSnapshot.Spec.TimeoutSeconds).NotTo(BeNil())
			Expect(*rootfsSnapshot.Spec.TimeoutSeconds).To(Equal(int64(300)), "Should use default timeoutSeconds of 300")
		})

		It("should fail pod creation when RuntimeClass not found", func() {
			sandboxName := "sandbox-rc-not-found"

			// Temporarily remove the suite-level "gvisor" RC to simulate misconfiguration
			deleteRuntimeClass(ctx, "gvisor")
			defer createRuntimeClass(ctx, "gvisor", "runsc")

			createSandbox(ctx, sandboxName, func(s *sandboxv1alpha1.Sandbox) {
				s.Spec.TerminationPolicy = &sandboxv1alpha1.TerminationPolicy{
					Strategy:       sandboxv1alpha1.TerminationStrategySnapshotRootfs,
					SnapshotRootfs: &sandboxv1alpha1.SnapshotRootfsTermination{SnapshotName: "test-snapshot"},
				}
			})
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("RuntimeClass"))
		})

		It("should create RootfsSnapshot exactly once even if reconciled multiple times", func() {
			sandboxName := "sandbox-snapshot-idempotent"

			timeout := int64(1)
			createSandbox(ctx, sandboxName, func(s *sandboxv1alpha1.Sandbox) {
				s.Spec.TimeoutSeconds = &timeout
				s.Spec.TerminationPolicy = &sandboxv1alpha1.TerminationPolicy{
					Strategy:       sandboxv1alpha1.TerminationStrategySnapshotRootfs,
					SnapshotRootfs: &sandboxv1alpha1.SnapshotRootfsTermination{SnapshotName: "test-snapshot"},
				}
			})
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)
			defer deleteTerminationSnapshot(ctx, sandboxName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := bindPodToNode(ctx, podName)
			makePodReady(ctx, pod, "containerd://abc123", fakeClock)

			fakeClock.Advance(2 * time.Second)

			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			rootfsSnapshot := getTerminationSnapshot(ctx, sandboxName)
			Expect(rootfsSnapshot).NotTo(BeNil())
			originalUID := rootfsSnapshot.UID

			// Second reconcile while snapshot running - should be idempotent
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			rootfsSnapshot = getTerminationSnapshot(ctx, sandboxName)
			Expect(rootfsSnapshot).NotTo(BeNil())
			Expect(rootfsSnapshot.UID).To(Equal(originalUID), "RootfsSnapshot should not be recreated")

			// Third reconcile - still idempotent
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			rootfsSnapshot = getTerminationSnapshot(ctx, sandboxName)
			Expect(rootfsSnapshot).NotTo(BeNil())
			Expect(rootfsSnapshot.UID).To(Equal(originalUID), "RootfsSnapshot should not be recreated on third reconcile")
		})

		It("should timeout snapshot even after multiple reconciles (deadline must not slide)", func() {
			sandboxName := "sandbox-snapshot-deadline-nosilde"

			snapshotDeadline := int64(10)
			timeout := int64(1) // sandbox times out quickly to enter finalization
			createSandbox(ctx, sandboxName, func(s *sandboxv1alpha1.Sandbox) {
				s.Spec.TimeoutSeconds = &timeout
				s.Spec.TerminationPolicy = &sandboxv1alpha1.TerminationPolicy{
					Strategy: sandboxv1alpha1.TerminationStrategySnapshotRootfs,
					SnapshotRootfs: &sandboxv1alpha1.SnapshotRootfsTermination{
						SnapshotName:   "test-snapshot",
						TimeoutSeconds: &snapshotDeadline,
					},
				}
			})
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)
			defer deleteTerminationSnapshot(ctx, sandboxName)

			// Reconcile to create pod
			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := bindPodToNode(ctx, podName)
			makePodReady(ctx, pod, "containerd://abc123", fakeClock)

			// Reconcile to persist TimeoutAt
			_, err = doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			// Advance past sandbox timeout to enter finalization
			fakeClock.Advance(2 * time.Second)

			// First reconcile in finalization: creates the RootfsSnapshot
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			rootfsSnapshot := getTerminationSnapshot(ctx, sandboxName)
			Expect(rootfsSnapshot).NotTo(BeNil(), "RootfsSnapshot should be created")

			// Simulate multiple reconciles over time, each 3s apart.
			// Total elapsed: 4 reconciles * 3s = 12s, which exceeds the 10s snapshot deadline.
			// With the bug, each reconcile resets the deadline to now+10s, so timeout never fires.
			for i := 0; i < 4; i++ {
				fakeClock.Advance(3 * time.Second)
				_, err = reconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
				})
				Expect(err).NotTo(HaveOccurred())
			}

			// The snapshot was never completed, and 12s > 10s deadline.
			// The sandbox should be deleted due to snapshot timeout.
			err = k8sClient.Get(ctx, types.NamespacedName{Name: sandboxName, Namespace: testNamespace}, &sandboxv1alpha1.Sandbox{})
			Expect(err).To(Satisfy(errors.IsNotFound), "Sandbox should be deleted after snapshot deadline exceeded")
		})
	})
})
