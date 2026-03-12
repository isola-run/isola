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
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	sandboxv1alpha1 "github.com/isola-ai/isola/api/v1alpha1"
)

func newTestReconcilerWithRestore(clock Clock, runtimeClassName, hostMountPath string) *SandboxReconciler {
	rec := events.NewFakeRecorder(100)
	return &SandboxReconciler{
		Client:                      k8sClient,
		Scheme:                      scheme.Scheme,
		Recorder:                    rec,
		SandboxSidecarImage:         "sandbox-sidecar:test",
		Clock:                       clock,
		RuntimeClassName:            runtimeClassName,
		RootfsSnapshotHostMountPath: hostMountPath,
	}
}

var _ = Describe("Sandbox Controller", func() {
	Context("Rootfs Restore", func() {
		var fakeClock *FakeClock

		BeforeEach(func() {
			fakeClock = NewFakeClock(time.Now())
		})

		It("should inject gVisor restore annotation with valid config", func() {
			runtimeClassName := "gvisor-restore"

			createRuntimeClass(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClass(ctx, runtimeClassName)

			reconciler := newTestReconcilerWithRestore(fakeClock, runtimeClassName, "/mnt/isola-snapshots")

			sandboxName := "sb-restore-basic"
			createSandbox(ctx, sandboxName, func(s *sandboxv1alpha1.Sandbox) {
				s.Spec.RootfsSnapshotSources = []sandboxv1alpha1.RootfsSnapshotSource{
					{SnapshotKey: "my-snapshot"},
				}
			})
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())
			Expect(pod.Annotations).To(HaveKeyWithValue(
				"dev.gvisor.tar.rootfs.upper.sandbox",
				"/mnt/isola-snapshots/my-snapshot.tar",
			))
		})

		It("should use explicit container name in restore annotation key", func() {
			runtimeClassName := "gvisor-restore"

			createRuntimeClass(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClass(ctx, runtimeClassName)

			reconciler := newTestReconcilerWithRestore(fakeClock, runtimeClassName, "/mnt/isola-snapshots")

			sandboxName := "sb-restore-explicit-ctr"
			createSandbox(ctx, sandboxName, func(s *sandboxv1alpha1.Sandbox) {
				s.Spec.PodTemplate.Spec.Containers = []corev1.Container{
					{
						Name:    "my-app",
						Image:   "busybox:latest",
						Command: []string{"sleep", "infinity"},
					},
				}
				s.Spec.RootfsSnapshotSources = []sandboxv1alpha1.RootfsSnapshotSource{
					{SnapshotKey: "snap1", Container: "my-app"},
				}
			})
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())
			Expect(pod.Annotations).To(HaveKeyWithValue(
				"dev.gvisor.tar.rootfs.upper.my-app",
				"/mnt/isola-snapshots/snap1.tar",
			))
		})

		It("should fail when container not found", func() {
			runtimeClassName := "gvisor-restore"

			createRuntimeClass(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClass(ctx, runtimeClassName)

			reconciler := newTestReconcilerWithRestore(fakeClock, runtimeClassName, "/mnt/isola-snapshots")

			sandboxName := "sb-restore-bad-ctr"
			createSandbox(ctx, sandboxName, func(s *sandboxv1alpha1.Sandbox) {
				s.Spec.RootfsSnapshotSources = []sandboxv1alpha1.RootfsSnapshotSource{
					{SnapshotKey: "snap1", Container: "nonexistent"},
				}
			})
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).To(HaveOccurred())

			sandbox := getSandbox(ctx, sandboxName)
			cond := meta.FindStatusCondition(sandbox.Status.Conditions, SandboxReadyCondition)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Reason).To(Equal(CondReasonRootfsRestoreConfigError))
			Expect(cond.Message).To(ContainSubstring("nonexistent"))
			Expect(cond.Message).To(ContainSubstring("not found"))
		})

		It("should fail when no RuntimeClassName configured", func() {
			reconciler := newTestReconcilerWithRestore(fakeClock, "", "/mnt/isola-snapshots")

			sandboxName := "sb-restore-no-runtime"
			createSandbox(ctx, sandboxName, func(s *sandboxv1alpha1.Sandbox) {
				s.Spec.RootfsSnapshotSources = []sandboxv1alpha1.RootfsSnapshotSource{
					{SnapshotKey: "snap1"},
				}
			})
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).To(HaveOccurred())

			sandbox := getSandbox(ctx, sandboxName)
			cond := meta.FindStatusCondition(sandbox.Status.Conditions, SandboxReadyCondition)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Reason).To(Equal(CondReasonRootfsRestoreConfigError))
			Expect(cond.Message).To(ContainSubstring("no RuntimeClassName configured"))
		})

		It("should fail when host mount path not configured", func() {
			runtimeClassName := "gvisor-restore"

			createRuntimeClass(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClass(ctx, runtimeClassName)

			reconciler := newTestReconcilerWithRestore(fakeClock, runtimeClassName, "")

			sandboxName := "sb-restore-no-mount"
			createSandbox(ctx, sandboxName, func(s *sandboxv1alpha1.Sandbox) {
				s.Spec.RootfsSnapshotSources = []sandboxv1alpha1.RootfsSnapshotSource{
					{SnapshotKey: "snap1"},
				}
			})
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).To(HaveOccurred())

			sandbox := getSandbox(ctx, sandboxName)
			cond := meta.FindStatusCondition(sandbox.Status.Conditions, SandboxReadyCondition)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Reason).To(Equal(CondReasonRootfsRestoreConfigError))
			Expect(cond.Message).To(ContainSubstring("--rootfssnapshot-host-mount-path"))
		})

		It("should fail when runtime is not gVisor", func() {
			runtimeClassName := "runc-restore"

			createRuntimeClass(ctx, runtimeClassName, "runc")
			defer deleteRuntimeClass(ctx, runtimeClassName)

			reconciler := newTestReconcilerWithRestore(fakeClock, runtimeClassName, "/mnt/isola-snapshots")

			sandboxName := "sb-restore-wrong-rt"
			createSandbox(ctx, sandboxName, func(s *sandboxv1alpha1.Sandbox) {
				s.Spec.RootfsSnapshotSources = []sandboxv1alpha1.RootfsSnapshotSource{
					{SnapshotKey: "snap1"},
				}
			})
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).To(HaveOccurred())

			sandbox := getSandbox(ctx, sandboxName)
			cond := meta.FindStatusCondition(sandbox.Status.Conditions, SandboxReadyCondition)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Reason).To(Equal(CondReasonRootfsRestoreConfigError))
			Expect(cond.Message).To(ContainSubstring("not runsc/gvisor"))
		})

		It("should fail when multiple containers and no container specified", func() {
			runtimeClassName := "gvisor-restore"

			createRuntimeClass(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClass(ctx, runtimeClassName)

			reconciler := newTestReconcilerWithRestore(fakeClock, runtimeClassName, "/mnt/isola-snapshots")

			sandboxName := "sb-restore-multi-ctr"
			createSandbox(ctx, sandboxName, func(s *sandboxv1alpha1.Sandbox) {
				s.Spec.PodTemplate.Spec.Containers = []corev1.Container{
					{
						Name:    "app1",
						Image:   "busybox:latest",
						Command: []string{"sleep", "infinity"},
					},
					{
						Name:    "app2",
						Image:   "busybox:latest",
						Command: []string{"sleep", "infinity"},
					},
				}
				s.Spec.RootfsSnapshotSources = []sandboxv1alpha1.RootfsSnapshotSource{
					{SnapshotKey: "snap1"},
				}
			})
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).To(HaveOccurred())

			sandbox := getSandbox(ctx, sandboxName)
			cond := meta.FindStatusCondition(sandbox.Status.Conditions, SandboxReadyCondition)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Reason).To(Equal(CondReasonRootfsRestoreConfigError))
			Expect(cond.Message).To(ContainSubstring("must be specified"))
			Expect(cond.Message).To(ContainSubstring("2 containers"))
		})

		It("should include restore context in pod failure message", func() {
			runtimeClassName := "gvisor-restore"

			createRuntimeClass(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClass(ctx, runtimeClassName)

			reconciler := newTestReconcilerWithRestore(fakeClock, runtimeClassName, "/mnt/isola-snapshots")

			sandboxName := "sb-restore-pod-fail"
			createSandbox(ctx, sandboxName, func(s *sandboxv1alpha1.Sandbox) {
				s.Spec.RootfsSnapshotSources = []sandboxv1alpha1.RootfsSnapshotSource{
					{SnapshotKey: "my-snap"},
				}
			})
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())

			pod.Status.Phase = corev1.PodFailed
			pod.Status.ContainerStatuses = []corev1.ContainerStatus{
				{Name: "sandbox", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 1, Reason: "Error"}}},
			}
			Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())

			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			sandbox := getSandbox(ctx, sandboxName)

			podReadyCond := meta.FindStatusCondition(sandbox.Status.Conditions, SandboxPodReadyCondition)
			Expect(podReadyCond).NotTo(BeNil())
			Expect(podReadyCond.Message).To(ContainSubstring("rootfs restore from snapshot"))
			Expect(podReadyCond.Message).To(ContainSubstring("my-snap"))

			readyCond := meta.FindStatusCondition(sandbox.Status.Conditions, SandboxReadyCondition)
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Message).To(ContainSubstring("rootfs restore from snapshot"))
			Expect(readyCond.Message).To(ContainSubstring("my-snap"))
		})
	})
})
