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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/events"

	sandboxv1alpha1 "github.com/isola-run/isola/api/v1alpha1"
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
					{SnapshotName: "my-snapshot"},
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
				"/mnt/isola-snapshots/"+testNamespace+"/my-snapshot.tar",
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
					{SnapshotName: "snap1", ContainerName: "my-app"},
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
				"/mnt/isola-snapshots/"+testNamespace+"/snap1.tar",
			))
		})

		It("should inject annotations for multiple sources targeting different containers", func() {
			runtimeClassName := "gvisor-restore"

			createRuntimeClass(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClass(ctx, runtimeClassName)

			reconciler := newTestReconcilerWithRestore(fakeClock, runtimeClassName, "/mnt/isola-snapshots")

			sandboxName := "sb-restore-multi-src"
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
					{SnapshotName: "snap-a", ContainerName: "app1"},
					{SnapshotName: "snap-b", ContainerName: "app2"},
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
				"dev.gvisor.tar.rootfs.upper.app1",
				"/mnt/isola-snapshots/"+testNamespace+"/snap-a.tar",
			))
			Expect(pod.Annotations).To(HaveKeyWithValue(
				"dev.gvisor.tar.rootfs.upper.app2",
				"/mnt/isola-snapshots/"+testNamespace+"/snap-b.tar",
			))
		})

		// CRD-level CEL validation prevents duplicate container targets at the API layer.
		// This tests the defense-in-depth check in injectRootfsRestore directly.
		It("should reject duplicate container target in annotation injection", func() {
			reconciler := newTestReconcilerWithRestore(fakeClock, "gvisor", "/mnt/isola-snapshots")

			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "app1", Image: "busybox:latest"},
						{Name: "app2", Image: "busybox:latest"},
					},
				},
			}

			sources := []sandboxv1alpha1.RootfsSnapshotSource{
				{SnapshotName: "snap-a", ContainerName: "app1"},
				{SnapshotName: "snap-b", ContainerName: "app1"},
			}

			err := reconciler.injectRootfsRestore(sources, pod, testNamespace)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("duplicate restore target"))
			Expect(err.Error()).To(ContainSubstring("app1"))
		})

		It("should fail when container not found", func() {
			runtimeClassName := "gvisor-restore"

			createRuntimeClass(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClass(ctx, runtimeClassName)

			reconciler := newTestReconcilerWithRestore(fakeClock, runtimeClassName, "/mnt/isola-snapshots")

			sandboxName := "sb-restore-bad-ctr"
			createSandbox(ctx, sandboxName, func(s *sandboxv1alpha1.Sandbox) {
				s.Spec.RootfsSnapshotSources = []sandboxv1alpha1.RootfsSnapshotSource{
					{SnapshotName: "snap1", ContainerName: "nonexistent"},
				}
			})
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).To(HaveOccurred())

			sandbox := getSandbox(ctx, sandboxName)
			cond := meta.FindStatusCondition(sandbox.Status.Conditions, sandboxv1alpha1.SandboxReadyCondition)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Reason).To(Equal(CondReasonRootfsRestoreConfigError))
			Expect(cond.Message).To(ContainSubstring("nonexistent"))
			Expect(cond.Message).To(ContainSubstring("not found"))

			Expect(hasConditionWithReason(sandbox, sandboxv1alpha1.SandboxSucceededCondition, metav1.ConditionFalse, CondReasonRootfsRestoreConfigError)).To(BeTrue())
		})

		It("should fail when host mount path not configured", func() {
			runtimeClassName := "gvisor-restore"

			createRuntimeClass(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClass(ctx, runtimeClassName)

			reconciler := newTestReconcilerWithRestore(fakeClock, runtimeClassName, "")

			sandboxName := "sb-restore-no-mount"
			createSandbox(ctx, sandboxName, func(s *sandboxv1alpha1.Sandbox) {
				s.Spec.RootfsSnapshotSources = []sandboxv1alpha1.RootfsSnapshotSource{
					{SnapshotName: "snap1"},
				}
			})
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).To(HaveOccurred())

			sandbox := getSandbox(ctx, sandboxName)
			cond := meta.FindStatusCondition(sandbox.Status.Conditions, sandboxv1alpha1.SandboxReadyCondition)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Reason).To(Equal(CondReasonRootfsRestoreConfigError))
			Expect(cond.Message).To(ContainSubstring("--rootfssnapshot-host-mount-path"))

			Expect(hasConditionWithReason(sandbox, sandboxv1alpha1.SandboxSucceededCondition, metav1.ConditionFalse, CondReasonRootfsRestoreConfigError)).To(BeTrue())
		})

		It("should fail when runtime is not gVisor", func() {
			runtimeClassName := "runc-restore"

			createRuntimeClass(ctx, runtimeClassName, "runc")
			defer deleteRuntimeClass(ctx, runtimeClassName)

			reconciler := newTestReconcilerWithRestore(fakeClock, runtimeClassName, "/mnt/isola-snapshots")

			sandboxName := "sb-restore-wrong-rt"
			createSandbox(ctx, sandboxName, func(s *sandboxv1alpha1.Sandbox) {
				s.Spec.RootfsSnapshotSources = []sandboxv1alpha1.RootfsSnapshotSource{
					{SnapshotName: "snap1"},
				}
			})
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).To(HaveOccurred())

			sandbox := getSandbox(ctx, sandboxName)
			cond := meta.FindStatusCondition(sandbox.Status.Conditions, sandboxv1alpha1.SandboxReadyCondition)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Reason).To(Equal(CondReasonRootfsRestoreConfigError))
			Expect(cond.Message).To(ContainSubstring("not runsc/gvisor"))

			Expect(hasConditionWithReason(sandbox, sandboxv1alpha1.SandboxSucceededCondition, metav1.ConditionFalse, CondReasonRootfsRestoreConfigError)).To(BeTrue())
		})

		It("should inject restartPolicyRules on container with restore annotation", func() {
			runtimeClassName := "gvisor-restore"

			createRuntimeClass(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClass(ctx, runtimeClassName)

			reconciler := newTestReconcilerWithRestore(fakeClock, runtimeClassName, "/mnt/isola-snapshots")

			sandboxName := "sb-restore-retry-rules"
			createSandbox(ctx, sandboxName, func(s *sandboxv1alpha1.Sandbox) {
				s.Spec.RootfsSnapshotSources = []sandboxv1alpha1.RootfsSnapshotSource{
					{SnapshotName: "my-snapshot"},
				}
			})
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())

			// The default "sandbox" container should have restart rules
			ctr := pod.Spec.Containers[0]
			Expect(ctr.Name).To(Equal("sandbox"))
			rpNever := corev1.ContainerRestartPolicyNever
			Expect(ctr.RestartPolicy).To(Equal(&rpNever))
			Expect(ctr.RestartPolicyRules).To(HaveLen(1))
			Expect(ctr.RestartPolicyRules[0].Action).To(Equal(corev1.ContainerRestartRuleActionRestart))
			Expect(ctr.RestartPolicyRules[0].ExitCodes).NotTo(BeNil())
			Expect(ctr.RestartPolicyRules[0].ExitCodes.Operator).To(Equal(corev1.ContainerRestartRuleOnExitCodesOpIn))
			Expect(ctr.RestartPolicyRules[0].ExitCodes.Values).To(Equal([]int32{128}))
		})

		It("should inject restartPolicyRules only on restored containers", func() {
			runtimeClassName := "gvisor-restore"

			createRuntimeClass(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClass(ctx, runtimeClassName)

			reconciler := newTestReconcilerWithRestore(fakeClock, runtimeClassName, "/mnt/isola-snapshots")

			sandboxName := "sb-restore-partial-rules"
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
					{SnapshotName: "snap-a", ContainerName: "app1"},
				}
			})
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())

			// app1 should have restart rules
			var app1, app2 corev1.Container
			for _, c := range pod.Spec.Containers {
				switch c.Name {
				case "app1":
					app1 = c
				case "app2":
					app2 = c
				}
			}
			rpNever := corev1.ContainerRestartPolicyNever
			Expect(app1.RestartPolicy).To(Equal(&rpNever))
			Expect(app1.RestartPolicyRules).To(HaveLen(1))
			Expect(app1.RestartPolicyRules[0].ExitCodes.Values).To(Equal([]int32{128}))

			// app2 should NOT have restart rules
			Expect(app2.RestartPolicy).To(BeNil())
			Expect(app2.RestartPolicyRules).To(BeEmpty())
		})

		It("should not inject restartPolicyRules without restore sources", func() {
			runtimeClassName := "gvisor-restore"

			createRuntimeClass(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClass(ctx, runtimeClassName)

			reconciler := newTestReconcilerWithRestore(fakeClock, runtimeClassName, "/mnt/isola-snapshots")

			sandboxName := "sb-no-restore-rules"
			createSandbox(ctx, sandboxName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())

			for _, c := range pod.Spec.Containers {
				Expect(c.RestartPolicy).To(BeNil(), "container %q should not have per-container RestartPolicy", c.Name)
				Expect(c.RestartPolicyRules).To(BeEmpty(), "container %q should not have RestartPolicyRules", c.Name)
			}
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
					{SnapshotName: "snap1"},
				}
			})
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).To(HaveOccurred())

			sandbox := getSandbox(ctx, sandboxName)
			cond := meta.FindStatusCondition(sandbox.Status.Conditions, sandboxv1alpha1.SandboxReadyCondition)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Reason).To(Equal(CondReasonRootfsRestoreConfigError))
			Expect(cond.Message).To(ContainSubstring("must be specified"))
			Expect(cond.Message).To(ContainSubstring("2 containers"))

			Expect(hasConditionWithReason(sandbox, sandboxv1alpha1.SandboxSucceededCondition, metav1.ConditionFalse, CondReasonRootfsRestoreConfigError)).To(BeTrue())
		})

	})
})
