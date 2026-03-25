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
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	sandboxv1alpha1 "github.com/isola-ai/isola/api/v1alpha1"
)

// drainRecorderEvents consumes all pending events from a FakeRecorder channel.
func drainRecorderEvents(ch <-chan string) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

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
				s.Spec.ShutdownPolicy = &sandboxv1alpha1.ShutdownPolicy{Strategy: sandboxv1alpha1.ShutdownStrategySnapshotRootfs}
				s.Spec.PodTemplate.Spec.RuntimeClassName = &runtimeClassName
			})
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)
			defer deleteShutdownSnapshot(ctx, sandboxName)

			// Create pod via reconcile, then bind to a node (simulating the scheduler)
			_, _ = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace}})
			pod := bindPodToNode(ctx, podName)
			makePodReady(ctx, pod, "containerd://abc", fakeClock)

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
				s.Spec.ShutdownPolicy = &sandboxv1alpha1.ShutdownPolicy{Strategy: sandboxv1alpha1.ShutdownStrategySnapshotRootfs}
				s.Spec.PodTemplate.Spec.RuntimeClassName = &runtimeClassName
			})
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)
			defer deleteShutdownSnapshot(ctx, sandboxName)

			// Setup - bind pod to node (simulating the scheduler)
			_, _ = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace}})
			pod := bindPodToNode(ctx, podName)
			makePodReady(ctx, pod, "containerd://abc", fakeClock)

			drainRecorderEvents(recorder.Events)

			fakeClock.Advance(2 * time.Second)
			_, _ = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace}})

			// Mark RootfsSnapshot succeeded
			rootfsSnapshot := getShutdownSnapshot(ctx, sandboxName)
			Expect(rootfsSnapshot).NotTo(BeNil())
			setShutdownSnapshotReady(ctx, sandboxName, true, sandboxv1alpha1.ReasonRootfsSnapshotSucceeded, "All snapshots completed")

			drainRecorderEvents(recorder.Events)

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
				s.Spec.ShutdownPolicy = &sandboxv1alpha1.ShutdownPolicy{Strategy: sandboxv1alpha1.ShutdownStrategySnapshotRootfs}
				s.Spec.PodTemplate.Spec.RuntimeClassName = &runtimeClassName
			})
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)
			defer deleteShutdownSnapshot(ctx, sandboxName)

			// Setup - bind pod to node (simulating the scheduler)
			_, _ = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace}})
			pod := bindPodToNode(ctx, podName)
			makePodReady(ctx, pod, "containerd://abc", fakeClock)

			fakeClock.Advance(2 * time.Second)
			_, _ = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace}})

			// Mark RootfsSnapshot failed
			rootfsSnapshot := getShutdownSnapshot(ctx, sandboxName)
			Expect(rootfsSnapshot).NotTo(BeNil())
			setShutdownSnapshotReady(ctx, sandboxName, false, sandboxv1alpha1.ReasonRootfsSnapshotFailed, "Snapshot job failed")

			drainRecorderEvents(recorder.Events)

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
	})
})
