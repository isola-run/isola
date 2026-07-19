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
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	sandboxv1alpha1 "github.com/isola-run/isola/api/v1alpha1"
	"github.com/isola-run/isola/internal/constants"
)

var _ = Describe("Sandbox Controller", func() {

	// ============================================
	// Pod Creation Tests
	// ============================================
	Context("Pod Creation", func() {
		var (
			reconciler *SandboxReconciler
			fakeClock  *FakeClock
		)

		BeforeEach(func() {
			fakeClock = NewFakeClock(time.Now())
			reconciler = newTestReconciler(fakeClock)
		})

		It("should create pod with correct spec from sandbox", func() {
			sandboxName := "sandbox-pod-spec"

			createSandbox(ctx, sandboxName, func(s *sandboxv1alpha1.Sandbox) {
				s.Spec.PodTemplate.Spec.Containers = []corev1.Container{
					{
						Name:    "my-sandbox",
						Image:   "python:3.11",
						Command: []string{"python", "-c", "import time; time.sleep(3600)"},
					},
				}
			})
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())
			Expect(pod.Spec.Containers).To(HaveLen(1))
			Expect(pod.Spec.Containers[0].Name).To(Equal("my-sandbox"))
			Expect(pod.Spec.Containers[0].Image).To(Equal("python:3.11"))
		})

		It("should inject sandbox-sidecar as init container", func() {
			sandboxName := "sandbox-sidecar"

			createSandbox(ctx, sandboxName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())
			Expect(pod.Spec.InitContainers).To(HaveLen(1))
			Expect(pod.Spec.InitContainers[0].Name).To(Equal(sandboxSidecarContainerName))
			Expect(pod.Spec.InitContainers[0].Image).To(Equal("sandbox-sidecar:test"))

			sidecarResources := pod.Spec.InitContainers[0].Resources
			Expect(sidecarResources.Requests.Cpu().String()).To(Equal("1m"))
			Expect(sidecarResources.Requests.Memory().String()).To(Equal("1Mi"))
			Expect(sidecarResources.Limits.Cpu().String()).To(Equal("1m"))
			Expect(sidecarResources.Limits.Memory().String()).To(Equal("1Mi"))
		})

		It("should back the sidecar command output dir with a memory emptyDir", func() {
			// Per-command stdout/stderr must land in RAM-backed tmpfs, not the sidecar
			// container's writable overlay layer, so command output never touches disk
			// and stays out of any rootfs snapshot.
			sandboxName := "sandbox-sidecar-cmd-output"

			createSandbox(ctx, sandboxName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())
			Expect(pod.Spec.InitContainers).To(HaveLen(1))

			sidecar := pod.Spec.InitContainers[0]
			var mount *corev1.VolumeMount
			for i := range sidecar.VolumeMounts {
				if sidecar.VolumeMounts[i].MountPath == constants.SidecarCommandOutputDir {
					mount = &sidecar.VolumeMounts[i]
					break
				}
			}
			Expect(mount).NotTo(BeNil(),
				"sidecar must mount a volume at the command output dir %q", constants.SidecarCommandOutputDir)

			var vol *corev1.Volume
			for i := range pod.Spec.Volumes {
				if pod.Spec.Volumes[i].Name == mount.Name {
					vol = &pod.Spec.Volumes[i]
					break
				}
			}
			Expect(vol).NotTo(BeNil(), "pod must declare the volume referenced by the sidecar mount")
			Expect(vol.EmptyDir).NotTo(BeNil(), "command output volume must be an emptyDir")
			Expect(vol.EmptyDir.Medium).To(Equal(corev1.StorageMediumMemory),
				"command output emptyDir must be memory-backed (tmpfs)")
		})

		It("should set sidecar ImagePullPolicy from reconciler config", func() {
			sandboxName := "sandbox-sidecar-pullpolicy"

			reconciler.SandboxSidecarImagePullPolicy = corev1.PullAlways

			createSandbox(ctx, sandboxName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, podName)
			Expect(pod.Spec.InitContainers[0].ImagePullPolicy).To(Equal(corev1.PullAlways))
		})

		It("should set owner reference for garbage collection", func() {
			sandboxName := "sandbox-owner-ref"

			createSandbox(ctx, sandboxName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			sandbox := getSandbox(ctx, sandboxName)
			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())
			Expect(pod.OwnerReferences).To(HaveLen(1))
			Expect(pod.OwnerReferences[0].Name).To(Equal(sandboxName))
			Expect(pod.OwnerReferences[0].UID).To(Equal(sandbox.UID))
			Expect(*pod.OwnerReferences[0].Controller).To(BeTrue())
		})

		It("should apply controller labels to pod", func() {
			sandboxName := "sandbox-labels"

			createSandbox(ctx, sandboxName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())
			// Standard Kubernetes recommended labels
			Expect(pod.Labels).To(HaveKeyWithValue("app.kubernetes.io/name", "isola-sandbox"))
			Expect(pod.Labels).To(HaveKeyWithValue("app.kubernetes.io/instance", sandboxName))
			Expect(pod.Labels).To(HaveKeyWithValue("app.kubernetes.io/component", "sandbox"))
			Expect(pod.Labels).To(HaveKeyWithValue("app.kubernetes.io/part-of", "isola"))
			Expect(pod.Labels).To(HaveKeyWithValue("app.kubernetes.io/managed-by", "isola-operator"))
			// Trust boundary label for NetworkPolicy selection
			Expect(pod.Labels).To(HaveKeyWithValue("isola.run/sandbox", "true"))
		})

		It("should add gvisor overlay2 annotation", func() {
			sandboxName := "sandbox-gvisor-overlay"

			createSandbox(ctx, sandboxName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())
			Expect(pod.Spec.RuntimeClassName).NotTo(BeNil())
			Expect(*pod.Spec.RuntimeClassName).To(Equal("gvisor"))
			Expect(pod.Annotations).To(HaveKeyWithValue("dev.gvisor.flag.overlay2", "root:self"))
		})

		It("should inject sleep infinity when no command is specified", func() {
			sandboxName := "sandbox-default-cmd"

			createSandbox(ctx, sandboxName, func(s *sandboxv1alpha1.Sandbox) {
				s.Spec.PodTemplate.Spec.Containers = []corev1.Container{
					{
						Name:  "sandbox",
						Image: "ubuntu:22.04",
					},
				}
			})
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())
			Expect(pod.Spec.Containers).To(HaveLen(1))
			Expect(pod.Spec.Containers[0].Command).To(Equal([]string{"sleep", "infinity"}))
		})

		It("should preserve explicit command and not inject sleep infinity", func() {
			sandboxName := "sandbox-explicit-cmd"

			createSandbox(ctx, sandboxName, func(s *sandboxv1alpha1.Sandbox) {
				s.Spec.PodTemplate.Spec.Containers = []corev1.Container{
					{
						Name:    "sandbox",
						Image:   "python:3.11",
						Command: []string{"python", "-c", "import time; time.sleep(3600)"},
					},
				}
			})
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())
			Expect(pod.Spec.Containers[0].Command).To(Equal([]string{"python", "-c", "import time; time.sleep(3600)"}))
		})

		It("should set restartPolicy to Never so sandbox pods do not restart", func() {
			sandboxName := "sandbox-restart-policy"

			createSandbox(ctx, sandboxName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())
			Expect(pod.Spec.RestartPolicy).To(Equal(corev1.RestartPolicyNever))
		})

		It("should disable service account token automount", func() {
			sandboxName := "sandbox-no-sa-token"

			createSandbox(ctx, sandboxName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())
			Expect(pod.Spec.AutomountServiceAccountToken).NotTo(BeNil())
			Expect(*pod.Spec.AutomountServiceAccountToken).To(BeFalse())
		})

		It("should configure sidecar with minimal required capabilities", func() {
			// CAP_SYS_PTRACE is the minimal capability required for the sidecar to access
			// /proc/<pid>/root, /proc/<pid>/cwd, and /proc/<pid>/environ of other containers
			// in the shared PID namespace. gVisor gates these paths behind ContextCanTrace
			// (task_files.go), which checks CAP_SYS_PTRACE via canTraceStandard (ptrace.go:205).
			//
			// CAP_SYS_CHROOT (in the default container cap set) covers the chroot(2) call
			// made by SysProcAttr.Chroot in the forked child (gVisor sys_file.go:368).
			//
			// CAP_SYS_ADMIN is NOT required: it was previously needed for nsenter's
			// setns(CLONE_NEWNS), which has been replaced by SysProcAttr.Chroot.
			sandboxName := "sandbox-sidecar-caps"

			createSandbox(ctx, sandboxName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())
			Expect(pod.Spec.InitContainers).To(HaveLen(1))

			sidecar := pod.Spec.InitContainers[0]
			Expect(sidecar.Name).To(Equal(sandboxSidecarContainerName))
			Expect(sidecar.SecurityContext).NotTo(BeNil())
			Expect(sidecar.SecurityContext.RunAsUser).To(HaveValue(BeEquivalentTo(0)))
			Expect(sidecar.SecurityContext.Capabilities).NotTo(BeNil())
			Expect(sidecar.SecurityContext.Capabilities.Add).To(ConsistOf(
				corev1.Capability("SYS_PTRACE"),
			))
		})

		It("should preserve sandbox init containers when injecting sidecar", func() {
			sandboxName := "sandbox-preserve-init"

			createSandbox(ctx, sandboxName, func(s *sandboxv1alpha1.Sandbox) {
				s.Spec.PodTemplate.Spec.InitContainers = []corev1.Container{
					{
						Name:    "init-setup",
						Image:   "busybox:latest",
						Command: []string{"sh", "-c", "echo setup"},
					},
					{
						Name:    "init-config",
						Image:   "alpine:latest",
						Command: []string{"sh", "-c", "echo config"},
					},
				}
			})
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())
			Expect(pod.Spec.InitContainers).To(HaveLen(3))
			Expect(pod.Spec.InitContainers[0].Name).To(Equal("init-setup"))
			Expect(pod.Spec.InitContainers[1].Name).To(Equal("init-config"))
			Expect(pod.Spec.InitContainers[2].Name).To(Equal(sandboxSidecarContainerName))
		})
	})

	// ============================================
	// Pod Lifecycle Edge Cases
	// ============================================
	Context("Pod Lifecycle Edge Cases", func() {
		var (
			reconciler *SandboxReconciler
			fakeClock  *FakeClock
		)

		BeforeEach(func() {
			fakeClock = NewFakeClock(time.Now())
			reconciler = newTestReconciler(fakeClock)
		})

		It("should re-create pod when externally deleted after running", func() {
			sandboxName := "sandbox-pod-extern-delete"

			createSandbox(ctx, sandboxName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			// First reconcile creates the pod
			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())

			// Externally delete the pod (e.g. node drain)
			Expect(k8sClient.Delete(ctx, pod)).To(Succeed())
			Expect(getPod(ctx, podName)).To(BeNil())

			// Next reconcile should re-create the pod
			_, err = doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod = getPod(ctx, podName)
			Expect(pod).NotTo(BeNil(), "pod should be re-created after external deletion")

			// Sandbox should not be terminal
			sandbox := getSandbox(ctx, sandboxName)
			Expect(meta.FindStatusCondition(sandbox.Status.Conditions, sandboxv1alpha1.SandboxSucceededCondition)).To(BeNil())
		})
	})

	// ============================================
	// Condition State Machine Tests
	// ============================================
	Context("Condition State Machine", func() {
		var (
			reconciler *SandboxReconciler
			fakeClock  *FakeClock
		)

		BeforeEach(func() {
			fakeClock = NewFakeClock(time.Now())
			reconciler = newTestReconciler(fakeClock)
		})

		It("should initialize conditions array on new sandbox", func() {
			sandboxName := "sandbox-init-conds"

			createSandbox(ctx, sandboxName)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			sandbox := getSandbox(ctx, sandboxName)
			Expect(sandbox.Status.Conditions).NotTo(BeNil())
			Expect(sandbox.Status.Conditions).ToNot(BeEmpty())
		})

		It("should set PodPending condition when pod is not ready", func() {
			sandboxName := "sandbox-pod-pending"

			createSandbox(ctx, sandboxName)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			sandbox := getSandbox(ctx, sandboxName)
			cond := meta.FindStatusCondition(sandbox.Status.Conditions, SandboxPodReadyCondition)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		})

		It("should set Ready condition when pod is running", func() {
			sandboxName := "sandbox-pod-running"

			createSandbox(ctx, sandboxName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())

			pod.Status.Phase = corev1.PodRunning
			pod.Status.Conditions = append(pod.Status.Conditions, corev1.PodCondition{
				Type:   corev1.PodReady,
				Status: corev1.ConditionTrue,
			})
			Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())

			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			sandbox := getSandbox(ctx, sandboxName)
			Expect(hasConditionWithReason(sandbox, SandboxPodReadyCondition, metav1.ConditionTrue, CondReasonPodRunning)).To(BeTrue())
			Expect(hasConditionWithReason(sandbox, sandboxv1alpha1.SandboxReadyCondition, metav1.ConditionTrue, CondReasonPodRunning)).To(BeTrue())
			Expect(meta.FindStatusCondition(sandbox.Status.Conditions, sandboxv1alpha1.SandboxSucceededCondition)).To(BeNil())
		})

		It("should reflect pod failure in conditions with PodFailed reason", func() {
			sandboxName := "sandbox-pod-failed"

			createSandbox(ctx, sandboxName)
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
			Expect(hasConditionWithReason(sandbox, SandboxPodReadyCondition, metav1.ConditionFalse, CondReasonPodFailed)).To(BeTrue())
			Expect(hasConditionWithReason(sandbox, sandboxv1alpha1.SandboxReadyCondition, metav1.ConditionFalse, CondReasonPodFailed)).To(BeTrue())
			Expect(hasConditionWithReason(sandbox, sandboxv1alpha1.SandboxSucceededCondition, metav1.ConditionFalse, CondReasonPodFailed)).To(BeTrue())
		})

		It("should reflect pod success in conditions with PodSucceeded reason", func() {
			sandboxName := "sandbox-pod-succeeded"

			createSandbox(ctx, sandboxName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())

			pod.Status.Phase = corev1.PodSucceeded
			pod.Status.ContainerStatuses = []corev1.ContainerStatus{
				{Name: "sandbox", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0, Reason: "Completed"}}},
			}
			Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())

			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			sandbox := getSandbox(ctx, sandboxName)
			Expect(hasConditionWithReason(sandbox, SandboxPodReadyCondition, metav1.ConditionFalse, CondReasonPodSucceeded)).To(BeTrue())
			Expect(hasConditionWithReason(sandbox, sandboxv1alpha1.SandboxReadyCondition, metav1.ConditionFalse, CondReasonPodSucceeded)).To(BeTrue())
			Expect(hasConditionWithReason(sandbox, sandboxv1alpha1.SandboxSucceededCondition, metav1.ConditionTrue, CondReasonPodSucceeded)).To(BeTrue())
		})

		It("should maintain stable conditions across multiple reconciles", func() {
			sandboxName := "sandbox-stable-conds"

			createSandbox(ctx, sandboxName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			sandbox2 := getSandbox(ctx, sandboxName)
			conds2 := sandbox2.Status.Conditions

			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			sandbox3 := getSandbox(ctx, sandboxName)
			conds3 := sandbox3.Status.Conditions

			Expect(conds3).To(HaveLen(len(conds2)))
			for _, c2 := range conds2 {
				c3 := meta.FindStatusCondition(conds3, c2.Type)
				Expect(c3).NotTo(BeNil())
				Expect(c3.Status).To(Equal(c2.Status))
				Expect(c3.Reason).To(Equal(c2.Reason))
			}
		})

		It("should update ObservedGeneration in conditions", func() {
			sandboxName := "sandbox-observed-gen"

			createSandbox(ctx, sandboxName)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			sandbox := getSandbox(ctx, sandboxName)
			for _, cond := range sandbox.Status.Conditions {
				Expect(cond.ObservedGeneration).To(Equal(sandbox.Generation))
			}
		})

		It("should not revert Succeeded=False on subsequent reconcile", func() {
			sandboxName := "sandbox-terminal-sticky"

			createSandbox(ctx, sandboxName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			// Pod fails (terminal)
			pod := getPod(ctx, podName)
			pod.Status.Phase = corev1.PodFailed
			pod.Status.ContainerStatuses = []corev1.ContainerStatus{
				{Name: "sandbox", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 1}}},
			}
			Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())

			_, err = doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			sandbox := getSandbox(ctx, sandboxName)
			Expect(hasConditionWithReason(sandbox, sandboxv1alpha1.SandboxSucceededCondition, metav1.ConditionFalse, CondReasonPodFailed)).To(BeTrue())

			// Simulate another reconcile — Succeeded must remain False (one-way latch)
			_, err = doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			sandbox = getSandbox(ctx, sandboxName)
			Expect(hasConditionWithReason(sandbox, sandboxv1alpha1.SandboxSucceededCondition, metav1.ConditionFalse, CondReasonPodFailed)).To(BeTrue())
		})

		It("should not revert Succeeded=True on subsequent reconcile", func() {
			sandboxName := "sandbox-terminal-true-sticky"

			createSandbox(ctx, sandboxName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			// Pod succeeds (terminal)
			pod := getPod(ctx, podName)
			pod.Status.Phase = corev1.PodSucceeded
			pod.Status.ContainerStatuses = []corev1.ContainerStatus{
				{Name: "sandbox", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0, Reason: "Completed"}}},
			}
			Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())

			_, err = doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			sandbox := getSandbox(ctx, sandboxName)
			Expect(hasConditionWithReason(sandbox, sandboxv1alpha1.SandboxSucceededCondition, metav1.ConditionTrue, CondReasonPodSucceeded)).To(BeTrue())

			// Simulate another reconcile — Succeeded must remain True (one-way latch)
			_, err = doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			sandbox = getSandbox(ctx, sandboxName)
			Expect(hasConditionWithReason(sandbox, sandboxv1alpha1.SandboxSucceededCondition, metav1.ConditionTrue, CondReasonPodSucceeded)).To(BeTrue())
		})

		It("should set PodIP in sandbox status when pod has IP", func() {
			sandboxName := "sandbox-pod-ip"

			createSandbox(ctx, sandboxName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())

			pod.Status.Phase = corev1.PodRunning
			pod.Status.PodIP = "10.244.0.42"
			pod.Status.Conditions = append(pod.Status.Conditions, corev1.PodCondition{
				Type:   corev1.PodReady,
				Status: corev1.ConditionTrue,
			})
			Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())

			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			sandbox := getSandbox(ctx, sandboxName)
			Expect(sandbox.Status.PodIP).To(Equal("10.244.0.42"))
		})

		It("should stamp sidecar version annotation on pod and mirror to status", func() {
			sandboxName := "sandbox-sidecar-version"

			createSandbox(ctx, sandboxName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			// First reconcile creates the pod (stamping the annotation).
			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())

			annotationValue := pod.Annotations[SidecarVersionAnnotation]
			Expect(annotationValue).NotTo(BeEmpty(), "pod must carry the sidecar-version annotation")

			// Second reconcile runs reconcileSandboxStatus, mirroring annotation → status.
			_, err = doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			sandbox := getSandbox(ctx, sandboxName)
			Expect(sandbox.Status.SidecarVersion).To(Equal(annotationValue),
				"Status.SidecarVersion must mirror the pod annotation")
		})

		It("should restore Status.SidecarVersion from pod annotation after status is lost", func() {
			// Regression test: if Status.SidecarVersion is ever lost (transient
			// apiserver error, manual edit, etc.), the next reconcile must re-derive
			// it from the pod annotation rather than leaving it empty for the
			// sandbox's lifetime. The pod annotation is the durable source of
			// truth; status is a cache.
			sandboxName := "sandbox-sidecar-version-recover"

			createSandbox(ctx, sandboxName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			// Reconcile twice: once to create the pod+annotation, once to mirror.
			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())
			_, err = doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())
			annotationValue := pod.Annotations[SidecarVersionAnnotation]
			Expect(annotationValue).NotTo(BeEmpty())

			sandbox := getSandbox(ctx, sandboxName)
			Expect(sandbox.Status.SidecarVersion).To(Equal(annotationValue))

			sandbox.Status.SidecarVersion = ""
			Expect(k8sClient.Status().Update(ctx, sandbox)).To(Succeed())

			_, err = doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			sandbox = getSandbox(ctx, sandboxName)
			Expect(sandbox.Status.SidecarVersion).To(Equal(annotationValue),
				"Status.SidecarVersion must be restored from the pod annotation")
		})
	})
})
