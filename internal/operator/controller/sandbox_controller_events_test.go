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
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	sandboxv1alpha1 "github.com/isola-ai/isola-sb/api/v1alpha1"
)

var _ = Describe("Sandbox Controller", func() {

	// ============================================
	// Event Recording Tests
	// ============================================
	Context("Event Recording", func() {
		var (
			reconciler *SandboxReconciler
			fakeClock  *FakeClock
			recorder   *events.FakeRecorder
		)

		BeforeEach(func() {
			fakeClock = NewFakeClock(time.Now())
			recorder = events.NewFakeRecorder(100)
			reconciler = newTestReconcilerWithRecorder(fakeClock, recorder)
		})

		It("should record PodCreated event when pod is created", func() {
			sandboxName := "sandbox-event-pod-created"

			createSandbox(ctx, sandboxName)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Check for PodCreated event
			Eventually(recorder.Events).Should(Receive(Equal("Normal PodCreated Sandbox Pod created")))
		})

		It("should record RootfsSnapshotCreated event", func() {
			sandboxName := "sandbox-event-snapshot-start"
			runtimeClassName := "gvisor-event"

			createRuntimeClass(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClass(ctx, runtimeClassName)

			timeout := int64(1)
			createSandbox(ctx, sandboxName, func(s *sandboxv1alpha1.Sandbox) {
				s.Spec.TimeoutSeconds = &timeout
				s.Spec.ShutdownPolicy = &sandboxv1alpha1.ShutdownPolicy{Policy: sandboxv1alpha1.ShutdownPolicySnapshotRootfs}
				s.Spec.PodTemplate.Spec.RuntimeClassName = &runtimeClassName
			})
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)
			defer deleteShutdownSnapshot(ctx, sandboxName)

			// Create and make pod ready (need to recreate with NodeName)
			_, _ = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace}})
			pod := recreatePodWithNodeName(ctx, podName, "test-node", &runtimeClassName)
			makePodReady(ctx, pod, "containerd://abc")

			// Drain PodCreated event
			<-recorder.Events

			// Trigger snapshotting
			fakeClock.Advance(2 * time.Second)
			_, _ = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace}})

			// Check for RootfsSnapshotCreated event (snapshot name is dynamic)
			Eventually(recorder.Events).Should(Receive(ContainSubstring("Normal RootfsSnapshotCreated Created RootfsSnapshot")))
		})

		It("should record SnapshotSucceeded event", func() {
			sandboxName := "sandbox-event-snapshot-success"
			runtimeClassName := "gvisor-event-success"

			createRuntimeClass(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClass(ctx, runtimeClassName)

			timeout := int64(1)
			createSandbox(ctx, sandboxName, func(s *sandboxv1alpha1.Sandbox) {
				s.Spec.TimeoutSeconds = &timeout
				s.Spec.ShutdownPolicy = &sandboxv1alpha1.ShutdownPolicy{Policy: sandboxv1alpha1.ShutdownPolicySnapshotRootfs}
				s.Spec.PodTemplate.Spec.RuntimeClassName = &runtimeClassName
			})
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)
			defer deleteShutdownSnapshot(ctx, sandboxName)

			// Setup - recreate pod with NodeName
			_, _ = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace}})
			pod := recreatePodWithNodeName(ctx, podName, "test-node", &runtimeClassName)
			makePodReady(ctx, pod, "containerd://abc")

			// Drain previous events
		drainEvents:
			for {
				select {
				case <-recorder.Events:
				default:
					break drainEvents
				}
			}

			fakeClock.Advance(2 * time.Second)
			_, _ = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace}})

			// Mark RootfsSnapshot succeeded
			rootfsSnapshot := getShutdownSnapshot(ctx, sandboxName)
			Expect(rootfsSnapshot).NotTo(BeNil())
			setShutdownSnapshotReady(ctx, sandboxName, true, sandboxv1alpha1.ReasonRootfsSnapshotSucceeded, "All snapshots completed")

		drainEvents2:
			for {
				select {
				case <-recorder.Events:
				default:
					break drainEvents2
				}
			}

			_, _ = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace}})

			Eventually(recorder.Events).Should(Receive(ContainSubstring("Normal SnapshotSucceeded Snapshot")))
		})

		It("should record SnapshotFailed event", func() {
			sandboxName := "sandbox-event-snapshot-fail"
			runtimeClassName := "gvisor-event-fail"

			createRuntimeClass(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClass(ctx, runtimeClassName)

			timeout := int64(1)
			createSandbox(ctx, sandboxName, func(s *sandboxv1alpha1.Sandbox) {
				s.Spec.TimeoutSeconds = &timeout
				s.Spec.ShutdownPolicy = &sandboxv1alpha1.ShutdownPolicy{Policy: sandboxv1alpha1.ShutdownPolicySnapshotRootfs}
				s.Spec.PodTemplate.Spec.RuntimeClassName = &runtimeClassName
			})
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)
			defer deleteShutdownSnapshot(ctx, sandboxName)

			// Setup - recreate pod with NodeName
			_, _ = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace}})
			pod := recreatePodWithNodeName(ctx, podName, "test-node", &runtimeClassName)
			makePodReady(ctx, pod, "containerd://abc")

			fakeClock.Advance(2 * time.Second)
			_, _ = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace}})

			// Mark RootfsSnapshot failed
			rootfsSnapshot := getShutdownSnapshot(ctx, sandboxName)
			Expect(rootfsSnapshot).NotTo(BeNil())
			setShutdownSnapshotReady(ctx, sandboxName, false, sandboxv1alpha1.ReasonRootfsSnapshotFailed, "Snapshot job failed")

			// Drain previous events
		drainEvents3:
			for {
				select {
				case <-recorder.Events:
				default:
					break drainEvents3
				}
			}

			_, _ = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace}})

			// Check for Warning SnapshotFailed event
			Eventually(recorder.Events).Should(Receive(ContainSubstring("Warning SnapshotFailed")))
		})
	})

	// ============================================
	// Error Handling Tests
	// ============================================
	Context("Error Handling", func() {
		var (
			reconciler *SandboxReconciler
			fakeClock  *FakeClock
		)

		BeforeEach(func() {
			fakeClock = NewFakeClock(time.Now())
			reconciler = newTestReconciler(fakeClock)
		})

		It("should return no error when sandbox is not found", func() {
			// Try to reconcile a non-existent sandbox
			result, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "nonexistent-sandbox", Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))
		})

		It("should skip reconciliation when sandbox has deletion timestamp", func() {
			sandboxName := "sandbox-deleting"

			sandbox := createSandbox(ctx, sandboxName)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Start deletion
			Expect(k8sClient.Delete(ctx, sandbox)).To(Succeed())

			// Refresh sandbox
			sandbox = &sandboxv1alpha1.Sandbox{}
			// Sandbox may or may not exist at this point depending on timing
			err = k8sClient.Get(ctx, types.NamespacedName{Name: sandboxName, Namespace: testNamespace}, sandbox)
			if err == nil && !sandbox.DeletionTimestamp.IsZero() {
				// Reconcile should skip
				_, err = reconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
				})
				Expect(err).NotTo(HaveOccurred())
			}
		})

		It("should handle simple concurrent modifications gracefully", func() {
			sandboxName := "sandbox-concurrent"

			createSandbox(ctx, sandboxName)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")

			// First reconcile
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Modify sandbox externally
			sandbox := getSandbox(ctx, sandboxName)
			if sandbox.Labels == nil {
				sandbox.Labels = make(map[string]string)
			}
			sandbox.Labels["external-modification"] = "true"
			Expect(k8sClient.Update(ctx, sandbox)).To(Succeed())

			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
