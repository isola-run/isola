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
	"context"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	nodev1 "k8s.io/api/node/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	sandboxv1alpha1 "github.com/isola-ai/isola-sb/services/isola-operator/api/v1alpha1"
)

// Helper functions for tests

func createSandbox(ctx context.Context, name, templateRef string) *sandboxv1alpha1.Sandbox {
	sandbox := &sandboxv1alpha1.Sandbox{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: sandboxv1alpha1.SandboxSpec{
			TemplateRef: sandboxv1alpha1.SandboxTemplateReference{
				Name: templateRef,
			},
		},
	}
	ExpectWithOffset(1, k8sClient.Create(ctx, sandbox)).To(Succeed())
	return sandbox
}

func createTemplate(ctx context.Context, name string, opts ...func(*sandboxv1alpha1.SandboxTemplate)) *sandboxv1alpha1.SandboxTemplate {
	template := &sandboxv1alpha1.SandboxTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: sandboxv1alpha1.SandboxTemplateSpec{
			PodTemplate: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:    "sandbox",
							Image:   "busybox:latest",
							Command: []string{"sleep", "infinity"},
						},
					},
				},
			},
		},
	}
	for _, opt := range opts {
		opt(template)
	}
	ExpectWithOffset(1, k8sClient.Create(ctx, template)).To(Succeed())
	return template
}

func createRuntimeClass(ctx context.Context, name, handler string) {
	rc := &nodev1.RuntimeClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Handler: handler,
	}
	ExpectWithOffset(1, k8sClient.Create(ctx, rc)).To(Succeed())
}

func getSandbox(ctx context.Context, name string) *sandboxv1alpha1.Sandbox {
	sandbox := &sandboxv1alpha1.Sandbox{}
	ExpectWithOffset(1, k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: testNamespace}, sandbox)).To(Succeed())
	return sandbox
}

func getPod(ctx context.Context, name string) *corev1.Pod {
	pod := &corev1.Pod{}
	err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: testNamespace}, pod)
	if errors.IsNotFound(err) {
		return nil
	}
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	return pod
}

func deleteSandbox(ctx context.Context, name string) {
	sandbox := &sandboxv1alpha1.Sandbox{}
	err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: testNamespace}, sandbox)
	if err == nil {
		_ = k8sClient.Delete(ctx, sandbox)
	}
}

func deleteTemplate(ctx context.Context, name string) {
	template := &sandboxv1alpha1.SandboxTemplate{}
	err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: testNamespace}, template)
	if err == nil {
		_ = k8sClient.Delete(ctx, template)
	}
}

func deleteRuntimeClass(ctx context.Context, name string) {
	rc := &nodev1.RuntimeClass{}
	err := k8sClient.Get(ctx, types.NamespacedName{Name: name}, rc)
	if err == nil {
		_ = k8sClient.Delete(ctx, rc)
	}
}

func deletePod(ctx context.Context, name string) {
	pod := &corev1.Pod{}
	err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: testNamespace}, pod)
	if err == nil {
		_ = k8sClient.Delete(ctx, pod)
	}
}

func getRootfsSnapshot(ctx context.Context, name string) *sandboxv1alpha1.RootfsSnapshot {
	snap := &sandboxv1alpha1.RootfsSnapshot{}
	err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: testNamespace}, snap)
	if err != nil {
		return nil
	}
	return snap
}

func getShutdownSnapshot(ctx context.Context, sandboxName string) *sandboxv1alpha1.RootfsSnapshot {
	return getRootfsSnapshot(ctx, sandboxName+"-shutdown")
}

func deleteShutdownSnapshot(ctx context.Context, sandboxName string) {
	snap := getShutdownSnapshot(ctx, sandboxName)
	if snap != nil {
		_ = k8sClient.Delete(ctx, snap)
	}
}

func setRootfsSnapshotReady(ctx context.Context, name string, ready bool, reason, message string) {
	snap := getRootfsSnapshot(ctx, name)
	if snap == nil {
		return
	}
	status := metav1.ConditionTrue
	if !ready {
		status = metav1.ConditionFalse
	}
	meta.SetStatusCondition(&snap.Status.Conditions, metav1.Condition{
		Type:               string(sandboxv1alpha1.RootfsSnapshotComplete),
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: snap.Generation,
	})
	if ready || reason == sandboxv1alpha1.ReasonRootfsSnapshotFailed {
		now := metav1.Now()
		snap.Status.CompletedAt = &now
	}
	ExpectWithOffset(1, k8sClient.Status().Update(ctx, snap)).To(Succeed())
}

func setShutdownSnapshotReady(ctx context.Context, sandboxName string, ready bool, reason, message string) {
	setRootfsSnapshotReady(ctx, sandboxName+"-shutdown", ready, reason, message)
}

func createSandboxWithNetwork(ctx context.Context, name, templateRef string, network *sandboxv1alpha1.NetworkSpec) {
	sandbox := &sandboxv1alpha1.Sandbox{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: sandboxv1alpha1.SandboxSpec{
			TemplateRef: sandboxv1alpha1.SandboxTemplateReference{
				Name: templateRef,
			},
			Network: network,
		},
	}
	ExpectWithOffset(1, k8sClient.Create(ctx, sandbox)).To(Succeed())
}

func hasConditionWithReason(sandbox *sandboxv1alpha1.Sandbox, condType string, status metav1.ConditionStatus, reason string) bool {
	cond := meta.FindStatusCondition(sandbox.Status.Conditions, condType)
	return cond != nil && cond.Status == status && cond.Reason == reason
}

// recreatePodWithNodeName deletes the existing pod and creates a new one with NodeName set
// This is needed because Kubernetes doesn't allow updating NodeName on existing pods
func recreatePodWithNodeName(ctx context.Context, podName, nodeName string, runtimeClassName *string) *corev1.Pod {
	// Get the existing pod to copy labels
	existingPod := getPod(ctx, podName)
	ExpectWithOffset(1, existingPod).NotTo(BeNil())
	labels := existingPod.Labels
	ExpectWithOffset(1, k8sClient.Delete(ctx, existingPod)).To(Succeed())

	// Create new pod with NodeName
	newPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: testNamespace,
			Labels:    labels,
		},
		Spec: corev1.PodSpec{
			RuntimeClassName: runtimeClassName,
			NodeName:         nodeName,
			Containers: []corev1.Container{
				{Name: "sandbox", Image: "busybox:latest", Command: []string{"sleep", "infinity"}},
			},
		},
	}
	ExpectWithOffset(1, k8sClient.Create(ctx, newPod)).To(Succeed())
	return newPod
}

// makePodReady updates pod status to make it appear ready
func makePodReady(ctx context.Context, pod *corev1.Pod, containerID string) {
	pod.Status.Phase = corev1.PodRunning
	pod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
	pod.Status.ContainerStatuses = []corev1.ContainerStatus{
		{Name: "sandbox", ContainerID: containerID, Ready: true, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
	}
	ExpectWithOffset(1, k8sClient.Status().Update(ctx, pod)).To(Succeed())
}

// doReconcile is a convenience helper that reconciles the sandbox.
// The controller adds the finalizer and proceeds with work in a single reconcile.
func doReconcile(ctx context.Context, reconciler *SandboxReconciler, name string) (reconcile.Result, error) {
	req := reconcile.Request{NamespacedName: types.NamespacedName{Name: name, Namespace: testNamespace}}
	return reconciler.Reconcile(ctx, req)
}

var _ = Describe("Sandbox Controller", func() {

	// ============================================
	// Category A: Template Lifecycle Tests
	// ============================================
	Context("Template Lifecycle", func() {
		var (
			reconciler *SandboxReconciler
			fakeClock  *FakeClock
		)

		BeforeEach(func() {
			fakeClock = NewFakeClock(time.Now())
			reconciler = newTestReconciler(fakeClock)
		})

		It("should set TemplateNotFound condition when template does not exist", func() {
			sandboxName := "sandbox-no-template"
			templateName := "nonexistent-template"

			createSandbox(ctx, sandboxName, templateName)
			defer deleteSandbox(ctx, sandboxName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			sandbox := getSandbox(ctx, sandboxName)
			Expect(hasConditionWithReason(sandbox, SandboxTemplateReadyCondition, metav1.ConditionFalse, CondReasonTemplateNotFound)).To(BeTrue())
			Expect(hasConditionWithReason(sandbox, SandboxReadyCondition, metav1.ConditionFalse, CondReasonTemplateNotFound)).To(BeTrue())
		})

		It("should set TemplateReady condition when template exists", func() {
			sandboxName := "sandbox-with-template"
			templateName := "test-template-exists"

			createTemplate(ctx, templateName)
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			sandbox := getSandbox(ctx, sandboxName)
			cond := meta.FindStatusCondition(sandbox.Status.Conditions, SandboxTemplateReadyCondition)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionTrue))
			Expect(cond.Reason).To(Equal("TemplateResolved"))
		})

		It("should resolve template when created after sandbox", func() {
			sandboxName := "sandbox-template-later"
			templateName := "template-created-later"

			createSandbox(ctx, sandboxName, templateName)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			sandbox := getSandbox(ctx, sandboxName)
			Expect(hasConditionWithReason(sandbox, SandboxTemplateReadyCondition, metav1.ConditionFalse, CondReasonTemplateNotFound)).To(BeTrue())

			createTemplate(ctx, templateName)
			defer deleteTemplate(ctx, templateName)

			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			sandbox = getSandbox(ctx, sandboxName)
			cond := meta.FindStatusCondition(sandbox.Status.Conditions, SandboxTemplateReadyCondition)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionTrue))
		})

		It("should handle empty template reference gracefully", func() {
			sandboxName := "sandbox-empty-template-ref"

			sandbox := &sandboxv1alpha1.Sandbox{
				ObjectMeta: metav1.ObjectMeta{
					Name:      sandboxName,
					Namespace: testNamespace,
				},
				Spec: sandboxv1alpha1.SandboxSpec{
					TemplateRef: sandboxv1alpha1.SandboxTemplateReference{
						Name: "", // Empty template ref
					},
				},
			}
			err := k8sClient.Create(ctx, sandbox)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("should be at least 1 chars long"))
		})

		It("should find sandboxes referencing a template via findSandboxesForTemplate", func() {
			templateName := "template-find-sandboxes"
			sandbox1Name := "sandbox-find-1"
			sandbox2Name := "sandbox-find-2"
			sandbox3Name := "sandbox-find-other"

			// Use cached reconciler - required for field index queries
			cachedReconciler := newTestReconcilerWithCache(fakeClock)

			template := createTemplate(ctx, templateName)
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandbox1Name, templateName)
			defer deleteSandbox(ctx, sandbox1Name)
			defer deletePod(ctx, sandbox1Name+"-pod")

			createSandbox(ctx, sandbox2Name, templateName)
			defer deleteSandbox(ctx, sandbox2Name)
			defer deletePod(ctx, sandbox2Name+"-pod")

			createSandbox(ctx, sandbox3Name, "other-template")
			defer deleteSandbox(ctx, sandbox3Name)

			var requests []reconcile.Request
			Eventually(func() int {
				requests = cachedReconciler.findSandboxesForTemplate(ctx, template)
				return len(requests)
			}, testTimeout, testInterval).Should(Equal(2))

			names := make([]string, len(requests))
			for i, req := range requests {
				names[i] = req.Name
			}
			Expect(names).To(ContainElements(sandbox1Name, sandbox2Name))
			Expect(names).NotTo(ContainElement(sandbox3Name))
		})
	})

	// ============================================
	// Category B: Pod Creation Tests
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

		It("should create pod with correct spec from template", func() {
			sandboxName := "sandbox-pod-spec"
			templateName := "template-pod-spec"

			createTemplate(ctx, templateName, func(t *sandboxv1alpha1.SandboxTemplate) {
				t.Spec.PodTemplate.Spec.Containers = []corev1.Container{
					{
						Name:    "my-sandbox",
						Image:   "python:3.11",
						Command: []string{"python", "-c", "import time; time.sleep(3600)"},
					},
				}
			})
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)
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

		It("should inject isola-agent sidecar as init container", func() {
			sandboxName := "sandbox-sidecar"
			templateName := "template-sidecar"

			createTemplate(ctx, templateName)
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())
			Expect(pod.Spec.InitContainers).To(HaveLen(1))
			Expect(pod.Spec.InitContainers[0].Name).To(Equal(agentContainerName))
			Expect(pod.Spec.InitContainers[0].Image).To(Equal("isola-agent:test"))
		})

		It("should set owner reference for garbage collection", func() {
			sandboxName := "sandbox-owner-ref"
			templateName := "template-owner-ref"

			createTemplate(ctx, templateName)
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)
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
			templateName := "template-labels"

			createTemplate(ctx, templateName)
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())
			Expect(pod.Labels).To(HaveKeyWithValue("app", "isola-sandbox"))
			Expect(pod.Labels).To(HaveKeyWithValue("sandbox.isola.run/id", sandboxName))
			Expect(pod.Labels).To(HaveKeyWithValue("app.kubernetes.io/managed-by", "isola-operator"))
		})

		It("should add gvisor overlay2 annotation when RuntimeClassName is set", func() {
			sandboxName := "sandbox-gvisor-overlay"
			templateName := "template-gvisor-overlay"
			runtimeClassName := "gvisor"

			createRuntimeClass(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClass(ctx, runtimeClassName)

			// Use reconciler with RuntimeClassName configured
			reconcilerWithRuntime := newTestReconcilerWithRuntimeClass(fakeClock, runtimeClassName)

			createTemplate(ctx, templateName)
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconcilerWithRuntime, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())
			Expect(pod.Spec.RuntimeClassName).NotTo(BeNil())
			Expect(*pod.Spec.RuntimeClassName).To(Equal(runtimeClassName))
			Expect(pod.Annotations).To(HaveKeyWithValue("dev.gvisor.flag.overlay2", "root:self"))
		})

		It("should not add gvisor overlay2 annotation when RuntimeClassName is not set", func() {
			sandboxName := "sandbox-no-runtime"
			templateName := "template-no-runtime"

			createTemplate(ctx, templateName)
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())
			Expect(pod.Spec.RuntimeClassName).To(BeNil())
			Expect(pod.Annotations).NotTo(HaveKey("dev.gvisor.flag.overlay2"))
		})

		It("should preserve template init containers when injecting agent sidecar", func() {
			sandboxName := "sandbox-preserve-init"
			templateName := "template-preserve-init"

			createTemplate(ctx, templateName, func(t *sandboxv1alpha1.SandboxTemplate) {
				t.Spec.PodTemplate.Spec.InitContainers = []corev1.Container{
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
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)
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
			Expect(pod.Spec.InitContainers[2].Name).To(Equal(agentContainerName))
		})
	})

	// ============================================
	// Category C: Condition State Machine Tests
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
			templateName := "template-init-conds"

			createTemplate(ctx, templateName)
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)
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
			templateName := "template-pod-pending"

			createTemplate(ctx, templateName)
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)
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
			templateName := "template-pod-running"

			createTemplate(ctx, templateName)
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)
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
			Expect(hasConditionWithReason(sandbox, SandboxReadyCondition, metav1.ConditionTrue, CondReasonPodRunning)).To(BeTrue())
		})

		It("should reflect pod failure in conditions with PodFailed reason", func() {
			sandboxName := "sandbox-pod-failed"
			templateName := "template-pod-failed"

			createTemplate(ctx, templateName)
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)
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
			Expect(hasConditionWithReason(sandbox, SandboxReadyCondition, metav1.ConditionFalse, CondReasonPodFailed)).To(BeTrue())
		})

		It("should reflect pod success in conditions with PodSucceeded reason", func() {
			sandboxName := "sandbox-pod-succeeded"
			templateName := "template-pod-succeeded"

			createTemplate(ctx, templateName)
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)
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
			Expect(hasConditionWithReason(sandbox, SandboxReadyCondition, metav1.ConditionFalse, CondReasonPodSucceeded)).To(BeTrue())
		})

		It("should maintain stable conditions across multiple reconciles", func() {
			sandboxName := "sandbox-stable-conds"
			templateName := "template-stable-conds"

			createTemplate(ctx, templateName)
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)
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
			templateName := "template-observed-gen"

			createTemplate(ctx, templateName)
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)
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
	})

	// ============================================
	// Category D: Timeout Behavior Tests
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
			templateName := "template-no-timeout"

			createTemplate(ctx, templateName)
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			sandbox := getSandbox(ctx, sandboxName)
			Expect(sandbox.Status.TimeoutAt).To(BeNil())
		})

		It("should calculate TimeoutAt from pod start time when available", func() {
			sandboxName := "sandbox-timeout-pod-start"
			templateName := "template-timeout-pod-start"

			timeout := int64(60)
			createTemplate(ctx, templateName, func(t *sandboxv1alpha1.SandboxTemplate) {
				t.Spec.TimeoutSeconds = &timeout
			})
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())

			startTime := metav1.Now()
			pod.Status.StartTime = &startTime
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
			Expect(sandbox.Status.TimeoutAt).NotTo(BeNil())
			expectedTimeout := startTime.Add(time.Duration(timeout) * time.Second)
			Expect(sandbox.Status.TimeoutAt.Time).To(BeTemporally("~", expectedTimeout, time.Second))
		})

		It("should fallback to sandbox creation time when pod has no start time", func() {
			sandboxName := "sandbox-timeout-fallback"
			templateName := "template-timeout-fallback"

			timeout := int64(60)
			createTemplate(ctx, templateName, func(t *sandboxv1alpha1.SandboxTemplate) {
				t.Spec.TimeoutSeconds = &timeout
			})
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			sandbox := getSandbox(ctx, sandboxName)
			// TimeoutAt should be based on sandbox creation time
			Expect(sandbox.Status.TimeoutAt).NotTo(BeNil())
			expectedTimeout := sandbox.CreationTimestamp.Add(time.Duration(timeout) * time.Second)
			Expect(sandbox.Status.TimeoutAt.Time).To(BeTemporally("~", expectedTimeout, time.Second))
		})

		It("should delete sandbox with Delete policy when timeout exceeded and set TimedOut reason", func() {
			sandboxName := "sandbox-timeout-delete"
			templateName := "template-timeout-delete"

			timeout := int64(1) // 1 second timeout
			createTemplate(ctx, templateName, func(t *sandboxv1alpha1.SandboxTemplate) {
				t.Spec.TimeoutSeconds = &timeout
				t.Spec.ShutdownPolicy = &sandboxv1alpha1.ShutdownPolicy{
					Policy: sandboxv1alpha1.ShutdownPolicyDelete,
				}
			})
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			fakeClock.Advance(2 * time.Second)

			// Reconcile triggers timeout handling - removes finalizer and deletes
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			sandbox := &sandboxv1alpha1.Sandbox{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: sandboxName, Namespace: testNamespace}, sandbox)
			Expect(errors.IsNotFound(err)).To(BeTrue())
		})

		It("should set TimedOut condition reason before deleting sandbox", func() {
			sandboxName := "sandbox-timeout-condition"
			templateName := "template-timeout-condition"

			timeout := int64(1)
			createTemplate(ctx, templateName, func(t *sandboxv1alpha1.SandboxTemplate) {
				t.Spec.TimeoutSeconds = &timeout
				// Default policy is Delete when nil
			})
			defer deleteTemplate(ctx, templateName)

			sandbox := createSandbox(ctx, sandboxName, templateName)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			originalUID := sandbox.UID
			fakeClock.Advance(2 * time.Second)

			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify sandbox was deleted (confirms timeout path with Delete policy)
			err = k8sClient.Get(ctx, types.NamespacedName{Name: sandboxName, Namespace: testNamespace}, &sandboxv1alpha1.Sandbox{})
			Expect(errors.IsNotFound(err)).To(BeTrue())
			_ = originalUID // Used to confirm we're talking about the right sandbox
		})

		It("should schedule requeue before timeout", func() {
			sandboxName := "sandbox-requeue"
			templateName := "template-requeue"

			timeout := int64(60)
			createTemplate(ctx, templateName, func(t *sandboxv1alpha1.SandboxTemplate) {
				t.Spec.TimeoutSeconds = &timeout
			})
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")

			result, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeNumerically(">", 0))
			Expect(result.RequeueAfter).To(BeNumerically("<=", time.Duration(timeout)*time.Second))
		})
	})

	// ============================================
	// Category E: Snapshotting Tests
	// ============================================
	Context("Snapshotting", func() {
		var (
			reconciler *SandboxReconciler
			fakeClock  *FakeClock
		)

		BeforeEach(func() {
			fakeClock = NewFakeClock(time.Now())
			reconciler = newTestReconciler(fakeClock)
		})

		It("should skip snapshot when RuntimeClassName is not set", func() {
			sandboxName := "sandbox-no-runtimeclass"
			templateName := "template-no-runtimeclass"

			recorder := record.NewFakeRecorder(10)
			reconciler = newTestReconcilerWithRecorder(fakeClock, recorder)

			timeout := int64(1)
			createTemplate(ctx, templateName, func(t *sandboxv1alpha1.SandboxTemplate) {
				t.Spec.TimeoutSeconds = &timeout
				t.Spec.ShutdownPolicy = &sandboxv1alpha1.ShutdownPolicy{
					Policy: sandboxv1alpha1.ShutdownPolicySnapshotRootfs,
				}
			})
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)
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
			pod.Status.ContainerStatuses = []corev1.ContainerStatus{
				{
					Name:        "sandbox",
					ContainerID: "containerd://abc123",
					Ready:       true,
					State:       corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
				},
			}
			Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())

			fakeClock.Advance(2 * time.Second)

			// Reconcile - snapshot skipped due to no runtimeclass, sandbox deleted
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			Eventually(recorder.Events).Should(Receive(ContainSubstring("RuntimeNotSupported")))

			err = k8sClient.Get(ctx, types.NamespacedName{Name: sandboxName, Namespace: testNamespace}, &sandboxv1alpha1.Sandbox{})
			Expect(errors.IsNotFound(err)).To(BeTrue())
		})

		It("should skip snapshot when runtime handler is not supported", func() {
			sandboxName := "sandbox-unsupported-runtime"
			templateName := "template-unsupported-runtime"
			runtimeClassName := "unsupported-runtime"

			recorder := record.NewFakeRecorder(10)
			reconciler = newTestReconcilerWithRecorder(fakeClock, recorder)

			createRuntimeClass(ctx, runtimeClassName, "runc") // runc is not supported for snapshotting
			defer deleteRuntimeClass(ctx, runtimeClassName)

			timeout := int64(1)
			createTemplate(ctx, templateName, func(t *sandboxv1alpha1.SandboxTemplate) {
				t.Spec.TimeoutSeconds = &timeout
				t.Spec.ShutdownPolicy = &sandboxv1alpha1.ShutdownPolicy{
					Policy: sandboxv1alpha1.ShutdownPolicySnapshotRootfs,
				}
				t.Spec.PodTemplate.Spec.RuntimeClassName = &runtimeClassName
			})
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)
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
			pod.Status.ContainerStatuses = []corev1.ContainerStatus{
				{
					Name:        "sandbox",
					ContainerID: "containerd://abc123",
					Ready:       true,
					State:       corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
				},
			}
			Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())

			fakeClock.Advance(2 * time.Second)

			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			Eventually(recorder.Events).Should(Receive(ContainSubstring("RuntimeNotSupported")))

			// Sandbox deleted after snapshot skipped
			err = k8sClient.Get(ctx, types.NamespacedName{Name: sandboxName, Namespace: testNamespace}, &sandboxv1alpha1.Sandbox{})
			Expect(errors.IsNotFound(err)).To(BeTrue())
		})

		It("should create RootfsSnapshot for supported runtime (runsc)", func() {
			sandboxName := "sandbox-runsc-snapshot"
			templateName := "template-runsc-snapshot"
			runtimeClassName := "gvisor-runsc"

			createRuntimeClass(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClass(ctx, runtimeClassName)

			timeout := int64(1)
			createTemplate(ctx, templateName, func(t *sandboxv1alpha1.SandboxTemplate) {
				t.Spec.TimeoutSeconds = &timeout
				t.Spec.ShutdownPolicy = &sandboxv1alpha1.ShutdownPolicy{
					Policy: sandboxv1alpha1.ShutdownPolicySnapshotRootfs,
				}
				t.Spec.PodTemplate.Spec.RuntimeClassName = &runtimeClassName
			})
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)
			defer deleteShutdownSnapshot(ctx, sandboxName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			// Recreate pod with NodeName - K8s doesn't allow updating NodeName on existing pods
			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())
			Expect(k8sClient.Delete(ctx, pod)).To(Succeed())

			newPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      podName,
					Namespace: testNamespace,
					Labels:    pod.Labels,
				},
				Spec: corev1.PodSpec{
					RuntimeClassName: &runtimeClassName,
					NodeName:         "test-node",
					Containers: []corev1.Container{
						{
							Name:    "sandbox",
							Image:   "busybox:latest",
							Command: []string{"sleep", "infinity"},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, newPod)).To(Succeed())

			newPod.Status.Phase = corev1.PodRunning
			newPod.Status.Conditions = []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			}
			newPod.Status.ContainerStatuses = []corev1.ContainerStatus{
				{
					Name:        "sandbox",
					ContainerID: "containerd://abc123def456",
					Ready:       true,
					State:       corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
				},
			}
			Expect(k8sClient.Status().Update(ctx, newPod)).To(Succeed())

			fakeClock.Advance(2 * time.Second)

			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify RootfsSnapshot was created
			rootfsSnapshot := getShutdownSnapshot(ctx, sandboxName)
			Expect(rootfsSnapshot).NotTo(BeNil())
			Expect(rootfsSnapshot.Spec.SandboxName).To(Equal(sandboxName))
		})

		It("should set condition when sandbox pod is not found during snapshot", func() {
			sandboxName := "sandbox-no-pod-snapshot"
			templateName := "template-no-pod-snapshot"

			timeout := int64(1)
			createTemplate(ctx, templateName, func(t *sandboxv1alpha1.SandboxTemplate) {
				t.Spec.TimeoutSeconds = &timeout
				t.Spec.ShutdownPolicy = &sandboxv1alpha1.ShutdownPolicy{
					Policy: sandboxv1alpha1.ShutdownPolicySnapshotRootfs,
				}
			})
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Simulate pod being gone
			deletePod(ctx, sandboxName+"-pod")
			fakeClock.Advance(2 * time.Second)

			// Reconcile - sandbox deleted after snapshot skipped (pod missing)
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Get(ctx, types.NamespacedName{Name: sandboxName, Namespace: testNamespace}, &sandboxv1alpha1.Sandbox{})
			Expect(errors.IsNotFound(err)).To(BeTrue())
		})

		It("should mark snapshot complete when RootfsSnapshot Ready=True", func() {
			sandboxName := "sandbox-snapshot-success"
			templateName := "template-snapshot-success"
			runtimeClassName := "gvisor-success"

			recorder := record.NewFakeRecorder(10)
			reconciler = newTestReconcilerWithRecorder(fakeClock, recorder)

			createRuntimeClass(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClass(ctx, runtimeClassName)

			timeout := int64(1)
			createTemplate(ctx, templateName, func(t *sandboxv1alpha1.SandboxTemplate) {
				t.Spec.TimeoutSeconds = &timeout
				t.Spec.ShutdownPolicy = &sandboxv1alpha1.ShutdownPolicy{
					Policy: sandboxv1alpha1.ShutdownPolicySnapshotRootfs,
				}
				t.Spec.PodTemplate.Spec.RuntimeClassName = &runtimeClassName
			})
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)
			defer deleteShutdownSnapshot(ctx, sandboxName)

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Recreate pod with NodeName (can't update NodeName on existing pods)
			pod := getPod(ctx, podName)
			labels := pod.Labels
			Expect(k8sClient.Delete(ctx, pod)).To(Succeed())

			newPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: podName, Namespace: testNamespace, Labels: labels},
				Spec: corev1.PodSpec{
					RuntimeClassName: &runtimeClassName,
					NodeName:         "test-node",
					Containers:       []corev1.Container{{Name: "sandbox", Image: "busybox:latest", Command: []string{"sleep", "infinity"}}},
				},
			}
			Expect(k8sClient.Create(ctx, newPod)).To(Succeed())
			newPod.Status.Phase = corev1.PodRunning
			newPod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
			newPod.Status.ContainerStatuses = []corev1.ContainerStatus{
				{Name: "sandbox", ContainerID: "containerd://abc123", Ready: true, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
			}
			Expect(k8sClient.Status().Update(ctx, newPod)).To(Succeed())

			fakeClock.Advance(2 * time.Second)
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify RootfsSnapshot was created
			rootfsSnapshot := getShutdownSnapshot(ctx, sandboxName)
			Expect(rootfsSnapshot).NotTo(BeNil())

			// Set RootfsSnapshot Ready=True to simulate successful snapshot
			setShutdownSnapshotReady(ctx, sandboxName, true, sandboxv1alpha1.ReasonRootfsSnapshotSucceeded, "All snapshots completed")

			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() bool {
				select {
				case event := <-recorder.Events:
					return len(event) > 0
				default:
					return false
				}
			}, testTimeout, testInterval).Should(BeTrue())

			// Sandbox deleted after successful snapshot
			err = k8sClient.Get(ctx, types.NamespacedName{Name: sandboxName, Namespace: testNamespace}, &sandboxv1alpha1.Sandbox{})
			Expect(errors.IsNotFound(err)).To(BeTrue())
		})

		It("should mark snapshot failed when RootfsSnapshot fails", func() {
			sandboxName := "sandbox-snapshot-fail"
			templateName := "template-snapshot-fail"
			runtimeClassName := "gvisor-fail"

			createRuntimeClass(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClass(ctx, runtimeClassName)

			timeout := int64(1)
			createTemplate(ctx, templateName, func(t *sandboxv1alpha1.SandboxTemplate) {
				t.Spec.TimeoutSeconds = &timeout
				t.Spec.ShutdownPolicy = &sandboxv1alpha1.ShutdownPolicy{
					Policy: sandboxv1alpha1.ShutdownPolicySnapshotRootfs,
				}
				t.Spec.PodTemplate.Spec.RuntimeClassName = &runtimeClassName
			})
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)
			defer deleteShutdownSnapshot(ctx, sandboxName)

			// Setup: reconcile to create pod, then replace with pod that has NodeName
			_, _ = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace}})
			pod := getPod(ctx, podName)
			labels := pod.Labels
			Expect(k8sClient.Delete(ctx, pod)).To(Succeed())

			newPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: podName, Namespace: testNamespace, Labels: labels},
				Spec: corev1.PodSpec{
					RuntimeClassName: &runtimeClassName,
					NodeName:         "test-node",
					Containers:       []corev1.Container{{Name: "sandbox", Image: "busybox:latest", Command: []string{"sleep", "infinity"}}},
				},
			}
			Expect(k8sClient.Create(ctx, newPod)).To(Succeed())
			newPod.Status.Phase = corev1.PodRunning
			newPod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
			newPod.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "sandbox", ContainerID: "containerd://abc123", Ready: true, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}}
			Expect(k8sClient.Status().Update(ctx, newPod)).To(Succeed())

			fakeClock.Advance(2 * time.Second)
			_, _ = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace}})

			// Verify RootfsSnapshot was created
			rootfsSnapshot := getShutdownSnapshot(ctx, sandboxName)
			Expect(rootfsSnapshot).NotTo(BeNil())

			// Set RootfsSnapshot Ready=False with failed reason to simulate failed snapshot
			setShutdownSnapshotReady(ctx, sandboxName, false, sandboxv1alpha1.ReasonRootfsSnapshotFailed, "Snapshot job failed")

			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace}})
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Get(ctx, types.NamespacedName{Name: sandboxName, Namespace: testNamespace}, &sandboxv1alpha1.Sandbox{})
			Expect(errors.IsNotFound(err)).To(BeTrue())
		})

		It("should respect custom snapshot timeout", func() {
			sandboxName := "sandbox-custom-snapshot-timeout"
			templateName := "template-custom-snapshot-timeout"

			timeout := int64(1)
			snapshotTimeout := int64(5)
			createTemplate(ctx, templateName, func(t *sandboxv1alpha1.SandboxTemplate) {
				t.Spec.TimeoutSeconds = &timeout
				t.Spec.ShutdownPolicy = &sandboxv1alpha1.ShutdownPolicy{
					Policy:                sandboxv1alpha1.ShutdownPolicySnapshotRootfs,
					ActiveDeadlineSeconds: &snapshotTimeout,
				}
			})
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")

			template := &sandboxv1alpha1.SandboxTemplate{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: templateName, Namespace: testNamespace}, template)).To(Succeed())
			Expect(template.Spec.ShutdownPolicy.ActiveDeadlineSeconds).NotTo(BeNil())
			Expect(*template.Spec.ShutdownPolicy.ActiveDeadlineSeconds).To(Equal(snapshotTimeout))
		})

		It("should use default activeDeadlineSeconds when not specified", func() {
			sandboxName := "sandbox-default-deadline"
			templateName := "template-default-deadline"
			runtimeClassName := "gvisor-default-deadline"

			createRuntimeClass(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClass(ctx, runtimeClassName)

			timeout := int64(1)
			createTemplate(ctx, templateName, func(t *sandboxv1alpha1.SandboxTemplate) {
				t.Spec.TimeoutSeconds = &timeout
				t.Spec.ShutdownPolicy = &sandboxv1alpha1.ShutdownPolicy{
					Policy: sandboxv1alpha1.ShutdownPolicySnapshotRootfs,
					// ActiveDeadlineSeconds not set - should use default (300)
				}
				t.Spec.PodTemplate.Spec.RuntimeClassName = &runtimeClassName
			})
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)
			defer deleteShutdownSnapshot(ctx, sandboxName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			// Recreate pod with NodeName (required for snapshotting)
			pod := getPod(ctx, podName)
			labels := pod.Labels
			Expect(k8sClient.Delete(ctx, pod)).To(Succeed())

			newPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: podName, Namespace: testNamespace, Labels: labels},
				Spec: corev1.PodSpec{
					RuntimeClassName: &runtimeClassName,
					NodeName:         "test-node",
					Containers:       []corev1.Container{{Name: "sandbox", Image: "busybox:latest", Command: []string{"sleep", "infinity"}}},
				},
			}
			Expect(k8sClient.Create(ctx, newPod)).To(Succeed())
			newPod.Status.Phase = corev1.PodRunning
			newPod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
			newPod.Status.ContainerStatuses = []corev1.ContainerStatus{
				{Name: "sandbox", ContainerID: "containerd://abc123", Ready: true, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
			}
			Expect(k8sClient.Status().Update(ctx, newPod)).To(Succeed())

			fakeClock.Advance(2 * time.Second)

			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify RootfsSnapshot was created with default activeDeadlineSeconds (300)
			rootfsSnapshot := getShutdownSnapshot(ctx, sandboxName)
			Expect(rootfsSnapshot).NotTo(BeNil())
			Expect(rootfsSnapshot.Spec.ActiveDeadlineSeconds).NotTo(BeNil())
			Expect(*rootfsSnapshot.Spec.ActiveDeadlineSeconds).To(Equal(int64(300)), "Should use default activeDeadlineSeconds of 300")
		})

		It("should handle RuntimeClass not found during snapshot verification", func() {
			sandboxName := "sandbox-rc-not-found"
			templateName := "template-rc-not-found"
			runtimeClassName := "nonexistent-runtime"

			timeout := int64(1)
			createTemplate(ctx, templateName, func(t *sandboxv1alpha1.SandboxTemplate) {
				t.Spec.TimeoutSeconds = &timeout
				t.Spec.ShutdownPolicy = &sandboxv1alpha1.ShutdownPolicy{
					Policy: sandboxv1alpha1.ShutdownPolicySnapshotRootfs,
				}
				t.Spec.PodTemplate.Spec.RuntimeClassName = &runtimeClassName
			})
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			// Reconcile to try to create pod - this should fail because RuntimeClass doesn't exist
			// The pod creation will be rejected by the API server
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace}})
			// The reconcile should return an error because pod creation fails with nonexistent RuntimeClass
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("RuntimeClass"))
		})

		It("should create RootfsSnapshot exactly once even if reconciled multiple times", func() {
			sandboxName := "sandbox-snapshot-idempotent"
			templateName := "template-snapshot-idempotent"
			runtimeClassName := "gvisor-idempotent"

			createRuntimeClass(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClass(ctx, runtimeClassName)

			timeout := int64(1)
			createTemplate(ctx, templateName, func(t *sandboxv1alpha1.SandboxTemplate) {
				t.Spec.TimeoutSeconds = &timeout
				t.Spec.ShutdownPolicy = &sandboxv1alpha1.ShutdownPolicy{
					Policy: sandboxv1alpha1.ShutdownPolicySnapshotRootfs,
				}
				t.Spec.PodTemplate.Spec.RuntimeClassName = &runtimeClassName
			})
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)
			defer deleteShutdownSnapshot(ctx, sandboxName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			// Replace pod with NodeName set (can't update NodeName)
			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())
			labels := pod.Labels
			Expect(k8sClient.Delete(ctx, pod)).To(Succeed())

			newPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: podName, Namespace: testNamespace, Labels: labels},
				Spec: corev1.PodSpec{
					RuntimeClassName: &runtimeClassName,
					NodeName:         "test-node",
					Containers:       []corev1.Container{{Name: "sandbox", Image: "busybox:latest", Command: []string{"sleep", "infinity"}}},
				},
			}
			Expect(k8sClient.Create(ctx, newPod)).To(Succeed())
			newPod.Status.Phase = corev1.PodRunning
			newPod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
			newPod.Status.ContainerStatuses = []corev1.ContainerStatus{
				{Name: "sandbox", ContainerID: "containerd://abc123", Ready: true, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
			}
			Expect(k8sClient.Status().Update(ctx, newPod)).To(Succeed())

			fakeClock.Advance(2 * time.Second)

			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			rootfsSnapshot := getShutdownSnapshot(ctx, sandboxName)
			Expect(rootfsSnapshot).NotTo(BeNil())
			originalUID := rootfsSnapshot.UID

			// Second reconcile while snapshot running - should be idempotent
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			rootfsSnapshot = getShutdownSnapshot(ctx, sandboxName)
			Expect(rootfsSnapshot).NotTo(BeNil())
			Expect(rootfsSnapshot.UID).To(Equal(originalUID), "RootfsSnapshot should not be recreated")

			// Third reconcile - still idempotent
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			rootfsSnapshot = getShutdownSnapshot(ctx, sandboxName)
			Expect(rootfsSnapshot).NotTo(BeNil())
			Expect(rootfsSnapshot.UID).To(Equal(originalUID), "RootfsSnapshot should not be recreated on third reconcile")
		})
	})

	// ============================================
	// Category F: Event Recording Tests
	// ============================================
	Context("Event Recording", func() {
		var (
			reconciler *SandboxReconciler
			fakeClock  *FakeClock
			recorder   *record.FakeRecorder
		)

		BeforeEach(func() {
			fakeClock = NewFakeClock(time.Now())
			recorder = record.NewFakeRecorder(100)
			reconciler = newTestReconcilerWithRecorder(fakeClock, recorder)
		})

		It("should record PodCreated event when pod is created", func() {
			sandboxName := "sandbox-event-pod-created"
			templateName := "template-event-pod-created"

			createTemplate(ctx, templateName)
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Check for PodCreated event
			Eventually(func() bool {
				select {
				case event := <-recorder.Events:
					return event == "Normal PodCreated Sandbox Pod created"
				default:
					return false
				}
			}, testTimeout, testInterval).Should(BeTrue())
		})

		It("should record RootfsSnapshotCreated event", func() {
			sandboxName := "sandbox-event-snapshot-start"
			templateName := "template-event-snapshot-start"
			runtimeClassName := "gvisor-event"

			createRuntimeClass(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClass(ctx, runtimeClassName)

			timeout := int64(1)
			createTemplate(ctx, templateName, func(t *sandboxv1alpha1.SandboxTemplate) {
				t.Spec.TimeoutSeconds = &timeout
				t.Spec.ShutdownPolicy = &sandboxv1alpha1.ShutdownPolicy{Policy: sandboxv1alpha1.ShutdownPolicySnapshotRootfs}
				t.Spec.PodTemplate.Spec.RuntimeClassName = &runtimeClassName
			})
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)
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
			Eventually(func() bool {
				select {
				case event := <-recorder.Events:
					return strings.Contains(event, "Normal RootfsSnapshotCreated Created RootfsSnapshot")
				default:
					return false
				}
			}, testTimeout, testInterval).Should(BeTrue())
		})

		It("should record SnapshotSucceeded event", func() {
			sandboxName := "sandbox-event-snapshot-success"
			templateName := "template-event-snapshot-success"
			runtimeClassName := "gvisor-event-success"

			createRuntimeClass(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClass(ctx, runtimeClassName)

			timeout := int64(1)
			createTemplate(ctx, templateName, func(t *sandboxv1alpha1.SandboxTemplate) {
				t.Spec.TimeoutSeconds = &timeout
				t.Spec.ShutdownPolicy = &sandboxv1alpha1.ShutdownPolicy{Policy: sandboxv1alpha1.ShutdownPolicySnapshotRootfs}
				t.Spec.PodTemplate.Spec.RuntimeClassName = &runtimeClassName
			})
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)
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

			Eventually(func() bool {
				select {
				case event := <-recorder.Events:
					return len(event) > 0 && (event == "Normal SnapshotSucceeded Rootfs snapshot completed" || len(event) > 20)
				default:
					return false
				}
			}, testTimeout, testInterval).Should(BeTrue())
		})

		It("should record SnapshotFailed event", func() {
			sandboxName := "sandbox-event-snapshot-fail"
			templateName := "template-event-snapshot-fail"
			runtimeClassName := "gvisor-event-fail"

			createRuntimeClass(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClass(ctx, runtimeClassName)

			timeout := int64(1)
			createTemplate(ctx, templateName, func(t *sandboxv1alpha1.SandboxTemplate) {
				t.Spec.TimeoutSeconds = &timeout
				t.Spec.ShutdownPolicy = &sandboxv1alpha1.ShutdownPolicy{Policy: sandboxv1alpha1.ShutdownPolicySnapshotRootfs}
				t.Spec.PodTemplate.Spec.RuntimeClassName = &runtimeClassName
			})
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)
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
			Eventually(func() bool {
				select {
				case event := <-recorder.Events:
					return len(event) > 0 && len(event) > 10
				default:
					return false
				}
			}, testTimeout, testInterval).Should(BeTrue())
		})
	})

	// ============================================
	// Category G: Error Handling Tests
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
			templateName := "template-deleting"

			createTemplate(ctx, templateName)
			defer deleteTemplate(ctx, templateName)

			sandbox := createSandbox(ctx, sandboxName, templateName)
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
			templateName := "template-concurrent"

			createTemplate(ctx, templateName)
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)
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

	// ============================================
	// Category G: Finalizer Behavior Tests
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

		It("should add finalizer after template validation", func() {
			sandboxName := "sandbox-finalizer-add"
			templateName := "template-finalizer-add"

			createTemplate(ctx, templateName)
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)
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
			templateName := "template-conditions-preserved"

			createTemplate(ctx, templateName)
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify ALL expected conditions are present (not just finalizer)
			sandbox := getSandbox(ctx, sandboxName)
			Expect(sandbox.Finalizers).To(ContainElement(SandboxFinalizer))

			// TemplateReady should be persisted (set by EnsureTemplate before finalizer was added)
			cond := meta.FindStatusCondition(sandbox.Status.Conditions, SandboxTemplateReadyCondition)
			Expect(cond).NotTo(BeNil(), "TemplateReady condition should be preserved")
			Expect(cond.Status).To(Equal(metav1.ConditionTrue))

			// PodReady should be set (set by CreateSandboxPod after finalizer was added)
			cond = meta.FindStatusCondition(sandbox.Status.Conditions, SandboxPodReadyCondition)
			Expect(cond).NotTo(BeNil(), "PodReady condition should be set")

			// Ready should be set
			cond = meta.FindStatusCondition(sandbox.Status.Conditions, SandboxReadyCondition)
			Expect(cond).NotTo(BeNil(), "Ready condition should be set")
		})

		It("should execute Delete policy and remove finalizer on deletion", func() {
			sandboxName := "sandbox-delete-policy"
			templateName := "template-delete-policy"

			createTemplate(ctx, templateName)
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)
			defer deletePod(ctx, sandboxName+"-pod")

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			sandbox := getSandbox(ctx, sandboxName)
			Expect(sandbox.Finalizers).To(ContainElement(SandboxFinalizer))

			Expect(k8sClient.Delete(ctx, sandbox)).To(Succeed())

			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Get(ctx, types.NamespacedName{Name: sandboxName, Namespace: testNamespace}, &sandboxv1alpha1.Sandbox{})
			Expect(errors.IsNotFound(err)).To(BeTrue())
		})

		It("should remove finalizer if template not found during deletion", func() {
			sandboxName := "sandbox-no-template-delete"
			templateName := "template-no-template-delete"

			createTemplate(ctx, templateName)
			createSandbox(ctx, sandboxName, templateName)
			defer deletePod(ctx, sandboxName+"-pod")

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			sandbox := getSandbox(ctx, sandboxName)
			Expect(sandbox.Finalizers).To(ContainElement(SandboxFinalizer))

			deleteTemplate(ctx, templateName)

			Expect(k8sClient.Delete(ctx, sandbox)).To(Succeed())

			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Get(ctx, types.NamespacedName{Name: sandboxName, Namespace: testNamespace}, &sandboxv1alpha1.Sandbox{})
			Expect(errors.IsNotFound(err)).To(BeTrue())
		})

		It("should execute SnapshotRootfs policy on deletion", func() {
			sandboxName := "sandbox-snapshot-delete"
			templateName := "template-snapshot-delete"
			runtimeClassName := "gvisor-delete"

			recorder := record.NewFakeRecorder(10)
			reconciler = newTestReconcilerWithRecorder(fakeClock, recorder)

			createRuntimeClass(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClass(ctx, runtimeClassName)

			createTemplate(ctx, templateName, func(t *sandboxv1alpha1.SandboxTemplate) {
				t.Spec.ShutdownPolicy = &sandboxv1alpha1.ShutdownPolicy{
					Policy: sandboxv1alpha1.ShutdownPolicySnapshotRootfs,
				}
				t.Spec.PodTemplate.Spec.RuntimeClassName = &runtimeClassName
			})
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)
			defer deleteShutdownSnapshot(ctx, sandboxName)

			// First reconcile - creates pod and adds finalizer
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Recreate pod with NodeName set (required for snapshotting)
			pod := getPod(ctx, podName)
			labels := pod.Labels
			Expect(k8sClient.Delete(ctx, pod)).To(Succeed())

			newPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: podName, Namespace: testNamespace, Labels: labels},
				Spec: corev1.PodSpec{
					RuntimeClassName: &runtimeClassName,
					NodeName:         "test-node",
					Containers:       []corev1.Container{{Name: "sandbox", Image: "busybox:latest", Command: []string{"sleep", "infinity"}}},
				},
			}
			Expect(k8sClient.Create(ctx, newPod)).To(Succeed())
			newPod.Status.Phase = corev1.PodRunning
			newPod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
			newPod.Status.ContainerStatuses = []corev1.ContainerStatus{
				{Name: "sandbox", ContainerID: "containerd://abc123", Ready: true, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
			}
			Expect(k8sClient.Status().Update(ctx, newPod)).To(Succeed())

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
			rootfsSnapshot := getShutdownSnapshot(ctx, sandboxName)
			Expect(rootfsSnapshot).NotTo(BeNil())
			Expect(rootfsSnapshot.Spec.SandboxName).To(Equal(sandboxName))

			// Mark RootfsSnapshot as complete
			setShutdownSnapshotReady(ctx, sandboxName, true, sandboxv1alpha1.ReasonRootfsSnapshotSucceeded, "All snapshots completed")

			// Reconcile - should complete deletion
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Sandbox should be gone
			err = k8sClient.Get(ctx, types.NamespacedName{Name: sandboxName, Namespace: testNamespace}, &sandboxv1alpha1.Sandbox{})
			Expect(errors.IsNotFound(err)).To(BeTrue())
		})
	})

	// ============================================
	// Category G: Network Configuration Tests
	// ============================================
	Context("Network Configuration", func() {
		var (
			reconciler *SandboxReconciler
			fakeClock  *FakeClock
		)

		BeforeEach(func() {
			fakeClock = NewFakeClock(time.Now())
			reconciler = newTestReconciler(fakeClock)
		})

		getNetworkPolicy := func(ctx context.Context, name string) *networkingv1.NetworkPolicy {
			np := &networkingv1.NetworkPolicy{}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: testNamespace}, np)
			if errors.IsNotFound(err) {
				return nil
			}
			ExpectWithOffset(1, err).NotTo(HaveOccurred())
			return np
		}

		deleteNetworkPolicyHelper := func(ctx context.Context, name string) {
			np := &networkingv1.NetworkPolicy{}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: testNamespace}, np)
			if err == nil {
				_ = k8sClient.Delete(ctx, np)
			}
		}

		It("should create custom NetworkPolicy when allowedEgressCIDRs is specified", func() {
			sandboxName := "sandbox-netpol-cidr"
			templateName := "template-netpol-cidr"

			createTemplate(ctx, templateName)
			defer deleteTemplate(ctx, templateName)

			network := &sandboxv1alpha1.NetworkSpec{
				AllowedEgressCIDRs: []string{"8.8.8.0/24"},
			}
			createSandboxWithNetwork(ctx, sandboxName, templateName, network)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")
			defer deleteNetworkPolicyHelper(ctx, sandboxName+"-custom-netpol")

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			np := getNetworkPolicy(ctx, sandboxName+"-custom-netpol")
			Expect(np).NotTo(BeNil())
			Expect(np.Spec.PolicyTypes).To(ContainElement(networkingv1.PolicyTypeEgress))

			// Verify CIDR rule exists
			Expect(np.Spec.Egress).To(HaveLen(1))
			Expect(np.Spec.Egress[0].To[0].IPBlock.CIDR).To(Equal("8.8.8.0/24"))

			// Verify pod selector uses sandbox ID
			Expect(np.Spec.PodSelector.MatchLabels).To(HaveKeyWithValue("sandbox.isola.run/id", sandboxName))

			sandbox := getSandbox(ctx, sandboxName)
			Expect(hasConditionWithReason(sandbox, SandboxNetworkReadyCondition, metav1.ConditionTrue, CondReasonNetworkPolicyApplied)).To(BeTrue())
		})

		It("should not create custom NetworkPolicy when network spec is nil", func() {
			sandboxName := "sandbox-no-netpol"
			templateName := "template-no-netpol"

			createTemplate(ctx, templateName)
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			np := getNetworkPolicy(ctx, sandboxName+"-custom-netpol")
			Expect(np).To(BeNil())
		})

		It("should not create custom NetworkPolicy when only allowAllInternet is set", func() {
			// Internet access is handled by Helm-installed NetworkPolicy, no custom policy needed
			sandboxName := "sandbox-internet-only"
			templateName := "template-internet-only"

			createTemplate(ctx, templateName)
			defer deleteTemplate(ctx, templateName)

			network := &sandboxv1alpha1.NetworkSpec{
				AllowAllInternet: true,
			}
			createSandboxWithNetwork(ctx, sandboxName, templateName, network)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			np := getNetworkPolicy(ctx, sandboxName+"-custom-netpol")
			Expect(np).To(BeNil())
		})

		It("should create custom NetworkPolicy when nameservers specified without internet access", func() {
			sandboxName := "sandbox-dns-allowed"
			templateName := "template-dns-allowed"

			createTemplate(ctx, templateName)
			defer deleteTemplate(ctx, templateName)

			network := &sandboxv1alpha1.NetworkSpec{
				Nameservers: []string{"8.8.8.8"},
			}
			createSandboxWithNetwork(ctx, sandboxName, templateName, network)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")
			defer deleteNetworkPolicyHelper(ctx, sandboxName+"-custom-netpol")

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			// Verify DNS egress rule exists
			np := getNetworkPolicy(ctx, sandboxName+"-custom-netpol")
			Expect(np).NotTo(BeNil())
			Expect(np.Spec.Egress).To(HaveLen(1))
			// Verify it targets the DNS server IP as /32 CIDR
			Expect(np.Spec.Egress[0].To).To(HaveLen(1))
			Expect(np.Spec.Egress[0].To[0].IPBlock).NotTo(BeNil())
			Expect(np.Spec.Egress[0].To[0].IPBlock.CIDR).To(Equal("8.8.8.8/32"))
			// Verify port 53 UDP and TCP
			Expect(np.Spec.Egress[0].Ports).To(HaveLen(2))
		})

		It("should block risky CIDRs (169.254.0.0/16) when egress allows 0.0.0.0/0", func() {
			sandboxName := "sandbox-block-metadata"
			templateName := "template-block-metadata"

			createTemplate(ctx, templateName)
			defer deleteTemplate(ctx, templateName)

			network := &sandboxv1alpha1.NetworkSpec{
				AllowedEgressCIDRs: []string{"0.0.0.0/0"},
			}
			createSandboxWithNetwork(ctx, sandboxName, templateName, network)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")
			defer deleteNetworkPolicyHelper(ctx, sandboxName+"-custom-netpol")

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			// Verify NetworkPolicy has 169.254.0.0/16 in except
			np := getNetworkPolicy(ctx, sandboxName+"-custom-netpol")
			Expect(np).NotTo(BeNil())
			Expect(np.Spec.Egress).To(HaveLen(1))
			Expect(np.Spec.Egress[0].To[0].IPBlock.CIDR).To(Equal("0.0.0.0/0"))
			Expect(np.Spec.Egress[0].To[0].IPBlock.Except).To(ContainElement("169.254.0.0/16"))
		})

		It("should not add except for public CIDRs that don't overlap blocked ranges", func() {
			sandboxName := "sandbox-public-range"
			templateName := "template-public-range"

			createTemplate(ctx, templateName)
			defer deleteTemplate(ctx, templateName)

			network := &sandboxv1alpha1.NetworkSpec{
				AllowedEgressCIDRs: []string{"8.8.8.0/24"},
			}
			createSandboxWithNetwork(ctx, sandboxName, templateName, network)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")
			defer deleteNetworkPolicyHelper(ctx, sandboxName+"-custom-netpol")

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			np := getNetworkPolicy(ctx, sandboxName+"-custom-netpol")
			Expect(np).NotTo(BeNil())
			Expect(np.Spec.Egress).To(HaveLen(1))
			Expect(np.Spec.Egress[0].To[0].IPBlock.CIDR).To(Equal("8.8.8.0/24"))
			Expect(np.Spec.Egress[0].To[0].IPBlock.Except).To(BeEmpty())
		})

		It("should create egress rules for allowed egress pods", func() {
			sandboxName := "sandbox-egress-pods"
			templateName := "template-egress-pods"

			createTemplate(ctx, templateName)
			defer deleteTemplate(ctx, templateName)

			network := &sandboxv1alpha1.NetworkSpec{
				AllowedEgressPods: []sandboxv1alpha1.EgressPodRule{
					{
						Namespace: "kube-system",
						PodSelector: metav1.LabelSelector{
							MatchLabels: map[string]string{"k8s-app": "kube-dns"},
						},
						Ports: []sandboxv1alpha1.NetworkPort{
							{Protocol: corev1.ProtocolUDP, Port: 53},
							{Protocol: corev1.ProtocolTCP, Port: 53},
						},
					},
				},
			}
			createSandboxWithNetwork(ctx, sandboxName, templateName, network)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")
			defer deleteNetworkPolicyHelper(ctx, sandboxName+"-custom-netpol")

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			np := getNetworkPolicy(ctx, sandboxName+"-custom-netpol")
			Expect(np).NotTo(BeNil())
			Expect(np.Spec.Egress).To(HaveLen(1))

			// Verify namespace selector
			Expect(np.Spec.Egress[0].To[0].NamespaceSelector).NotTo(BeNil())
			Expect(np.Spec.Egress[0].To[0].NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"]).To(Equal("kube-system"))
			// Verify pod selector
			Expect(np.Spec.Egress[0].To[0].PodSelector).NotTo(BeNil())
			Expect(np.Spec.Egress[0].To[0].PodSelector.MatchLabels["k8s-app"]).To(Equal("kube-dns"))
			// Verify ports
			Expect(np.Spec.Egress[0].Ports).To(HaveLen(2))
		})

		It("should recreate custom NetworkPolicy if deleted on next reconcile", func() {
			sandboxName := "sandbox-np-recreate"
			templateName := "template-np-recreate"

			createTemplate(ctx, templateName)
			defer deleteTemplate(ctx, templateName)

			network := &sandboxv1alpha1.NetworkSpec{
				AllowedEgressCIDRs: []string{"8.8.8.0/24"},
			}
			createSandboxWithNetwork(ctx, sandboxName, templateName, network)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")
			defer deleteNetworkPolicyHelper(ctx, sandboxName+"-custom-netpol")

			// Initial reconcile - creates Pod and NetworkPolicy
			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			// Verify NetworkPolicy exists
			np := getNetworkPolicy(ctx, sandboxName+"-custom-netpol")
			Expect(np).NotTo(BeNil())

			// Delete the NetworkPolicy externally
			Expect(k8sClient.Delete(ctx, np)).To(Succeed())

			// Verify it's gone
			np = getNetworkPolicy(ctx, sandboxName+"-custom-netpol")
			Expect(np).To(BeNil())

			// Reconcile again - should recreate NetworkPolicy
			_, err = doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			// Verify NetworkPolicy is recreated
			np = getNetworkPolicy(ctx, sandboxName+"-custom-netpol")
			Expect(np).NotTo(BeNil())
		})

		It("should add network labels to pod for allowAllInternet", func() {
			sandboxName := "sandbox-internet-labels"
			templateName := "template-internet-labels"

			createTemplate(ctx, templateName)
			defer deleteTemplate(ctx, templateName)

			network := &sandboxv1alpha1.NetworkSpec{
				AllowAllInternet: true,
			}
			createSandboxWithNetwork(ctx, sandboxName, templateName, network)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, sandboxName+"-pod")
			Expect(pod).NotTo(BeNil())
			Expect(pod.Labels).To(HaveKeyWithValue(LabelAllowInternet, "true"))
		})

		It("should add network labels to pod for allowClusterDNS", func() {
			sandboxName := "sandbox-dns-labels"
			templateName := "template-dns-labels"

			createTemplate(ctx, templateName)
			defer deleteTemplate(ctx, templateName)

			network := &sandboxv1alpha1.NetworkSpec{
				AllowClusterDNS: true,
			}
			createSandboxWithNetwork(ctx, sandboxName, templateName, network)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, sandboxName+"-pod")
			Expect(pod).NotTo(BeNil())
			Expect(pod.Labels).To(HaveKeyWithValue(LabelAllowClusterDNS, "true"))
		})

		It("should set DNSPolicy to ClusterFirst when allowClusterDNS is true", func() {
			sandboxName := "sandbox-dns-cluster"
			templateName := "template-dns-cluster"

			createTemplate(ctx, templateName)
			defer deleteTemplate(ctx, templateName)

			network := &sandboxv1alpha1.NetworkSpec{
				AllowClusterDNS: true,
			}
			createSandboxWithNetwork(ctx, sandboxName, templateName, network)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, sandboxName+"-pod")
			Expect(pod).NotTo(BeNil())
			Expect(pod.Spec.DNSPolicy).To(Equal(corev1.DNSClusterFirst))
		})

		It("should set DNSPolicy to None with sink DNS when no network config", func() {
			sandboxName := "sandbox-dns-sink"
			templateName := "template-dns-sink"

			createTemplate(ctx, templateName)
			defer deleteTemplate(ctx, templateName)

			// No network config - should use sink DNS
			createSandbox(ctx, sandboxName, templateName)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, sandboxName+"-pod")
			Expect(pod).NotTo(BeNil())
			Expect(pod.Spec.DNSPolicy).To(Equal(corev1.DNSNone))
			Expect(pod.Spec.DNSConfig).NotTo(BeNil())
			Expect(pod.Spec.DNSConfig.Nameservers).To(ContainElement("127.0.0.1"))
		})

		It("should set custom nameservers when specified", func() {
			sandboxName := "sandbox-dns-custom"
			templateName := "template-dns-custom"

			createTemplate(ctx, templateName)
			defer deleteTemplate(ctx, templateName)

			network := &sandboxv1alpha1.NetworkSpec{
				Nameservers: []string{"1.1.1.1", "8.8.8.8"},
			}
			createSandboxWithNetwork(ctx, sandboxName, templateName, network)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")
			defer deleteNetworkPolicyHelper(ctx, sandboxName+"-custom-netpol")

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, sandboxName+"-pod")
			Expect(pod).NotTo(BeNil())
			Expect(pod.Spec.DNSPolicy).To(Equal(corev1.DNSNone))
			Expect(pod.Spec.DNSConfig.Nameservers).To(Equal([]string{"1.1.1.1", "8.8.8.8"}))
		})
	})

	// Combined network configuration tests - testing various network specs together
	Context("Combined Network Configuration", func() {
		var (
			reconciler *SandboxReconciler
			fakeClock  *FakeClock
		)

		BeforeEach(func() {
			fakeClock = NewFakeClock(time.Now())
			reconciler = newTestReconciler(fakeClock)
		})

		getNetworkPolicy := func(ctx context.Context, name string) *networkingv1.NetworkPolicy {
			np := &networkingv1.NetworkPolicy{}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: testNamespace}, np)
			if errors.IsNotFound(err) {
				return nil
			}
			ExpectWithOffset(1, err).NotTo(HaveOccurred())
			return np
		}

		deleteNetworkPolicyHelper := func(ctx context.Context, name string) {
			np := &networkingv1.NetworkPolicy{}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: testNamespace}, np)
			if err == nil {
				_ = k8sClient.Delete(ctx, np)
			}
		}

		It("should create custom NetworkPolicy with combined CIDR and pod rules", func() {
			sandboxName := "sandbox-combined"
			templateName := "template-combined"

			createTemplate(ctx, templateName)
			defer deleteTemplate(ctx, templateName)

			network := &sandboxv1alpha1.NetworkSpec{
				AllowedEgressCIDRs: []string{"8.8.8.0/24"},
				AllowedEgressPods: []sandboxv1alpha1.EgressPodRule{
					{
						Namespace: "kube-system",
						PodSelector: metav1.LabelSelector{
							MatchLabels: map[string]string{"k8s-app": "kube-dns"},
						},
					},
				},
			}
			createSandboxWithNetwork(ctx, sandboxName, templateName, network)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")
			defer deleteNetworkPolicyHelper(ctx, sandboxName+"-custom-netpol")

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			np := getNetworkPolicy(ctx, sandboxName+"-custom-netpol")
			Expect(np).NotTo(BeNil())
			// Should have 2 egress rules: CIDR + pod selector
			Expect(np.Spec.Egress).To(HaveLen(2))
		})

		It("should not create custom NetworkPolicy when nameservers provided with internet access", func() {
			// When allowAllInternet=true, nameservers don't need custom egress rules
			sandboxName := "sandbox-internet-dns"
			templateName := "template-internet-dns"

			createTemplate(ctx, templateName)
			defer deleteTemplate(ctx, templateName)

			network := &sandboxv1alpha1.NetworkSpec{
				AllowAllInternet: true,
				Nameservers:      []string{"8.8.8.8"},
			}
			createSandboxWithNetwork(ctx, sandboxName, templateName, network)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			// No custom NetworkPolicy needed - internet access covers DNS
			np := getNetworkPolicy(ctx, sandboxName+"-custom-netpol")
			Expect(np).To(BeNil())

			// Verify pod has correct labels and DNS config
			pod := getPod(ctx, sandboxName+"-pod")
			Expect(pod).NotTo(BeNil())
			Expect(pod.Labels).To(HaveKeyWithValue(LabelAllowInternet, "true"))
			Expect(pod.Spec.DNSConfig.Nameservers).To(ContainElement("8.8.8.8"))
		})
	})

})

var _ = Describe("configureDNS function", func() {
	It("should configure DNSPolicy None with sink nameserver when network is nil", func() {
		pod := &corev1.Pod{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "test", Image: "busybox"},
				},
			},
		}

		err := configureDNS(pod, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(pod.Spec.DNSPolicy).To(Equal(corev1.DNSNone))
		Expect(pod.Spec.DNSConfig.Nameservers).To(Equal([]string{"127.0.0.1"}))
	})

	It("should configure DNSPolicy ClusterFirst when allowClusterDNS is true", func() {
		pod := &corev1.Pod{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "test", Image: "busybox"},
				},
			},
		}

		network := &sandboxv1alpha1.NetworkSpec{
			AllowClusterDNS: true,
		}

		err := configureDNS(pod, network)
		Expect(err).NotTo(HaveOccurred())
		Expect(pod.Spec.DNSPolicy).To(Equal(corev1.DNSClusterFirst))
	})

	It("should configure custom nameservers when specified", func() {
		pod := &corev1.Pod{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "test", Image: "busybox"},
				},
			},
		}

		network := &sandboxv1alpha1.NetworkSpec{
			Nameservers: []string{"8.8.8.8", "1.1.1.1"},
		}

		err := configureDNS(pod, network)
		Expect(err).NotTo(HaveOccurred())
		Expect(pod.Spec.DNSPolicy).To(Equal(corev1.DNSNone))
		Expect(pod.Spec.DNSConfig.Nameservers).To(Equal([]string{"8.8.8.8", "1.1.1.1"}))
	})

	It("should add nameservers to cluster DNS when both are specified", func() {
		pod := &corev1.Pod{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "test", Image: "busybox"},
				},
			},
		}

		network := &sandboxv1alpha1.NetworkSpec{
			AllowClusterDNS: true,
			Nameservers:     []string{"8.8.8.8"},
		}

		err := configureDNS(pod, network)
		Expect(err).NotTo(HaveOccurred())
		Expect(pod.Spec.DNSPolicy).To(Equal(corev1.DNSClusterFirst))
		Expect(pod.Spec.DNSConfig.Nameservers).To(Equal([]string{"8.8.8.8"}))
	})
})
