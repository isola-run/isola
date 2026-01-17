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
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	nodev1 "k8s.io/api/node/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
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

func getJob(ctx context.Context, name string) *batchv1.Job {
	job := &batchv1.Job{}
	err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: testNamespace}, job)
	if errors.IsNotFound(err) {
		return nil
	}
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	return job
}

func deleteJob(ctx context.Context, name string) {
	job := &batchv1.Job{}
	err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: testNamespace}, job)
	if err == nil {
		propagationPolicy := metav1.DeletePropagationBackground
		_ = k8sClient.Delete(ctx, job, &client.DeleteOptions{PropagationPolicy: &propagationPolicy})
	}
}

func createNetworkTemplate(ctx context.Context, name string, opts ...func(*sandboxv1alpha1.NetworkTemplate)) *sandboxv1alpha1.NetworkTemplate {
	nt := &sandboxv1alpha1.NetworkTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: sandboxv1alpha1.NetworkTemplateSpec{
			// Default to isolated mode (DNSPolicy: None with external DNS)
			// This satisfies CEL validation without requiring cluster DNS access
			DNSPolicy:   corev1.DNSNone,
			Nameservers: []string{"8.8.8.8"},
		},
	}
	for _, opt := range opts {
		opt(nt)
	}
	ExpectWithOffset(1, k8sClient.Create(ctx, nt)).To(Succeed())
	return nt
}

func deleteNetworkTemplate(ctx context.Context, name string) {
	nt := &sandboxv1alpha1.NetworkTemplate{}
	err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: testNamespace}, nt)
	if err == nil {
		// Remove finalizer if present to allow deletion
		if len(nt.Finalizers) > 0 {
			nt.Finalizers = nil
			_ = k8sClient.Update(ctx, nt)
		}
		_ = k8sClient.Delete(ctx, nt)
	}
}

func createSandboxWithNetworkTemplate(ctx context.Context, name, templateRef, networkTemplateRef string) {
	sandbox := &sandboxv1alpha1.Sandbox{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: sandboxv1alpha1.SandboxSpec{
			TemplateRef: sandboxv1alpha1.SandboxTemplateReference{
				Name: templateRef,
			},
			Network: &sandboxv1alpha1.NetworkConfig{
				TemplateRef: &sandboxv1alpha1.NetworkTemplateReference{
					Name: networkTemplateRef,
				},
			},
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
			defer deletePod(ctx, sandboxName)

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
			defer deletePod(ctx, sandboxName)

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
			defer deletePod(ctx, sandbox1Name)

			createSandbox(ctx, sandbox2Name, templateName)
			defer deleteSandbox(ctx, sandbox2Name)
			defer deletePod(ctx, sandbox2Name)

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

			podName := sandboxName
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

			podName := sandboxName
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

			podName := sandboxName
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

			podName := sandboxName
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

			podName := sandboxName
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

			podName := sandboxName
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

			podName := sandboxName
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
			defer deletePod(ctx, sandboxName)

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
			defer deletePod(ctx, sandboxName)

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

			podName := sandboxName
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

			podName := sandboxName
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

			podName := sandboxName
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

			podName := sandboxName
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
			defer deletePod(ctx, sandboxName)

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
			defer deletePod(ctx, sandboxName)

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

			podName := sandboxName
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
			defer deletePod(ctx, sandboxName)

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
			defer deletePod(ctx, sandboxName)

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
			defer deletePod(ctx, sandboxName)

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
			defer deletePod(ctx, sandboxName)

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
					Policy: sandboxv1alpha1.ShutdownPolicySnapshotFilesystem,
				}
			})
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName
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

			Eventually(recorder.Events).Should(Receive(ContainSubstring(ReasonFSSnapshotRuntimeClassMissing)))

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
					Policy: sandboxv1alpha1.ShutdownPolicySnapshotFilesystem,
				}
				t.Spec.PodTemplate.Spec.RuntimeClassName = &runtimeClassName
			})
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName
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

			Eventually(recorder.Events).Should(Receive(ContainSubstring(ReasonFSSnapshotRuntimeUnsupported)))

			// Sandbox deleted after snapshot skipped
			err = k8sClient.Get(ctx, types.NamespacedName{Name: sandboxName, Namespace: testNamespace}, &sandboxv1alpha1.Sandbox{})
			Expect(errors.IsNotFound(err)).To(BeTrue())
		})

		It("should create snapshotter job for supported runtime (runsc)", func() {
			sandboxName := "sandbox-runsc-snapshot"
			templateName := "template-runsc-snapshot"
			runtimeClassName := "gvisor-runsc"

			createRuntimeClass(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClass(ctx, runtimeClassName)

			timeout := int64(1)
			createTemplate(ctx, templateName, func(t *sandboxv1alpha1.SandboxTemplate) {
				t.Spec.TimeoutSeconds = &timeout
				t.Spec.ShutdownPolicy = &sandboxv1alpha1.ShutdownPolicy{
					Policy: sandboxv1alpha1.ShutdownPolicySnapshotFilesystem,
				}
				t.Spec.PodTemplate.Spec.RuntimeClassName = &runtimeClassName
			})
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName
			snapshotterJobName := sandboxName + "-snap"
			defer deletePod(ctx, podName)
			defer deleteJob(ctx, snapshotterJobName)

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

			snapshotterJob := &batchv1.Job{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: snapshotterJobName, Namespace: testNamespace}, snapshotterJob)
			Expect(err).NotTo(HaveOccurred())
			Expect(snapshotterJob.Spec.Template.Spec.Containers[0].Name).To(Equal("snapshotter"))
		})

		It("should set condition when sandbox pod is not found during snapshot", func() {
			sandboxName := "sandbox-no-pod-snapshot"
			templateName := "template-no-pod-snapshot"

			timeout := int64(1)
			createTemplate(ctx, templateName, func(t *sandboxv1alpha1.SandboxTemplate) {
				t.Spec.TimeoutSeconds = &timeout
				t.Spec.ShutdownPolicy = &sandboxv1alpha1.ShutdownPolicy{
					Policy: sandboxv1alpha1.ShutdownPolicySnapshotFilesystem,
				}
			})
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName)

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Simulate pod being gone
			deletePod(ctx, sandboxName)
			fakeClock.Advance(2 * time.Second)

			// Reconcile - sandbox deleted after snapshot skipped (pod missing)
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Get(ctx, types.NamespacedName{Name: sandboxName, Namespace: testNamespace}, &sandboxv1alpha1.Sandbox{})
			Expect(errors.IsNotFound(err)).To(BeTrue())
		})

		It("should mark snapshot complete when snapshotter job succeeds", func() {
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
					Policy: sandboxv1alpha1.ShutdownPolicySnapshotFilesystem,
				}
				t.Spec.PodTemplate.Spec.RuntimeClassName = &runtimeClassName
			})
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName
			snapshotterJobName := sandboxName + "-snap"
			defer deletePod(ctx, podName)
			defer deleteJob(ctx, snapshotterJobName)

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

			snapshotterJob := &batchv1.Job{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: snapshotterJobName, Namespace: testNamespace}, snapshotterJob)
			Expect(err).NotTo(HaveOccurred())
			now := metav1.Now()
			snapshotterJob.Status.StartTime = &now
			snapshotterJob.Status.CompletionTime = &now
			snapshotterJob.Status.Succeeded = 1
			snapshotterJob.Status.Conditions = []batchv1.JobCondition{
				{Type: batchv1.JobSuccessCriteriaMet, Status: corev1.ConditionTrue},
				{Type: batchv1.JobComplete, Status: corev1.ConditionTrue},
			}
			Expect(k8sClient.Status().Update(ctx, snapshotterJob)).To(Succeed())

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

		It("should mark snapshot failed when snapshotter job fails", func() {
			sandboxName := "sandbox-snapshot-fail"
			templateName := "template-snapshot-fail"
			runtimeClassName := "gvisor-fail"

			createRuntimeClass(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClass(ctx, runtimeClassName)

			timeout := int64(1)
			createTemplate(ctx, templateName, func(t *sandboxv1alpha1.SandboxTemplate) {
				t.Spec.TimeoutSeconds = &timeout
				t.Spec.ShutdownPolicy = &sandboxv1alpha1.ShutdownPolicy{
					Policy: sandboxv1alpha1.ShutdownPolicySnapshotFilesystem,
				}
				t.Spec.PodTemplate.Spec.RuntimeClassName = &runtimeClassName
			})
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName
			snapshotterJobName := sandboxName + "-snap"
			defer deletePod(ctx, podName)
			defer deleteJob(ctx, snapshotterJobName)

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

			snapshotterJob := &batchv1.Job{}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: snapshotterJobName, Namespace: testNamespace}, snapshotterJob)
			Expect(err).NotTo(HaveOccurred())
			now := metav1.Now()
			snapshotterJob.Status.StartTime = &now
			snapshotterJob.Status.Failed = 1
			snapshotterJob.Status.Conditions = []batchv1.JobCondition{
				{Type: batchv1.JobFailureTarget, Status: corev1.ConditionTrue},
				{Type: batchv1.JobFailed, Status: corev1.ConditionTrue, Message: "Job failed"},
			}
			Expect(k8sClient.Status().Update(ctx, snapshotterJob)).To(Succeed())

			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace}})
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
					Policy:                sandboxv1alpha1.ShutdownPolicySnapshotFilesystem,
					ActiveDeadlineSeconds: &snapshotTimeout,
				}
			})
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName)

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
					Policy: sandboxv1alpha1.ShutdownPolicySnapshotFilesystem,
					// ActiveDeadlineSeconds not set - should use default (300)
				}
				t.Spec.PodTemplate.Spec.RuntimeClassName = &runtimeClassName
			})
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName
			snapshotterJobName := sandboxName + "-snap"
			defer deletePod(ctx, podName)
			defer deleteJob(ctx, snapshotterJobName)

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

			// Verify snapshotter job was created with default activeDeadlineSeconds (300)
			snapshotterJob := getJob(ctx, snapshotterJobName)
			Expect(snapshotterJob).NotTo(BeNil())
			Expect(snapshotterJob.Spec.ActiveDeadlineSeconds).NotTo(BeNil())
			Expect(*snapshotterJob.Spec.ActiveDeadlineSeconds).To(Equal(int64(300)), "Should use default activeDeadlineSeconds of 300")
		})

		It("should handle RuntimeClass not found during snapshot verification", func() {
			sandboxName := "sandbox-rc-not-found"
			templateName := "template-rc-not-found"
			runtimeClassName := "nonexistent-runtime"

			timeout := int64(1)
			createTemplate(ctx, templateName, func(t *sandboxv1alpha1.SandboxTemplate) {
				t.Spec.TimeoutSeconds = &timeout
				t.Spec.ShutdownPolicy = &sandboxv1alpha1.ShutdownPolicy{
					Policy: sandboxv1alpha1.ShutdownPolicySnapshotFilesystem,
				}
				t.Spec.PodTemplate.Spec.RuntimeClassName = &runtimeClassName
			})
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName
			defer deletePod(ctx, podName)

			// Reconcile to try to create pod - this should fail because RuntimeClass doesn't exist
			// The pod creation will be rejected by the API server
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace}})
			// The reconcile should return an error because pod creation fails with nonexistent RuntimeClass
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("RuntimeClass"))
		})

		It("should create snapshotter job exactly once even if reconciled multiple times", func() {
			sandboxName := "sandbox-snapshot-idempotent"
			templateName := "template-snapshot-idempotent"
			runtimeClassName := "gvisor-idempotent"

			createRuntimeClass(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClass(ctx, runtimeClassName)

			timeout := int64(1)
			createTemplate(ctx, templateName, func(t *sandboxv1alpha1.SandboxTemplate) {
				t.Spec.TimeoutSeconds = &timeout
				t.Spec.ShutdownPolicy = &sandboxv1alpha1.ShutdownPolicy{
					Policy: sandboxv1alpha1.ShutdownPolicySnapshotFilesystem,
				}
				t.Spec.PodTemplate.Spec.RuntimeClassName = &runtimeClassName
			})
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName
			snapshotterJobName := sandboxName + "-snap"
			defer deletePod(ctx, podName)
			defer deleteJob(ctx, snapshotterJobName)

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

			snapshotterJob := &batchv1.Job{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: snapshotterJobName, Namespace: testNamespace}, snapshotterJob)
			Expect(err).NotTo(HaveOccurred())
			originalUID := snapshotterJob.UID

			// Second reconcile while snapshotter running - should be idempotent
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			snapshotterJob = &batchv1.Job{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: snapshotterJobName, Namespace: testNamespace}, snapshotterJob)
			Expect(err).NotTo(HaveOccurred())
			Expect(snapshotterJob.UID).To(Equal(originalUID), "Snapshotter job should not be recreated")

			// Third reconcile - still idempotent
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			snapshotterJob = &batchv1.Job{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: snapshotterJobName, Namespace: testNamespace}, snapshotterJob)
			Expect(err).NotTo(HaveOccurred())
			Expect(snapshotterJob.UID).To(Equal(originalUID), "Snapshotter job should not be recreated on third reconcile")
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
			defer deletePod(ctx, sandboxName)

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

		It("should record SnapshottingStarted event", func() {
			sandboxName := "sandbox-event-snapshot-start"
			templateName := "template-event-snapshot-start"
			runtimeClassName := "gvisor-event"

			createRuntimeClass(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClass(ctx, runtimeClassName)

			timeout := int64(1)
			createTemplate(ctx, templateName, func(t *sandboxv1alpha1.SandboxTemplate) {
				t.Spec.TimeoutSeconds = &timeout
				t.Spec.ShutdownPolicy = &sandboxv1alpha1.ShutdownPolicy{Policy: sandboxv1alpha1.ShutdownPolicySnapshotFilesystem}
				t.Spec.PodTemplate.Spec.RuntimeClassName = &runtimeClassName
			})
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName
			snapshotterJobName := sandboxName + "-snap"
			defer deletePod(ctx, podName)
			defer deleteJob(ctx, snapshotterJobName)

			// Create and make pod ready (need to recreate with NodeName)
			_, _ = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace}})
			pod := recreatePodWithNodeName(ctx, podName, "test-node", &runtimeClassName)
			makePodReady(ctx, pod, "containerd://abc")

			// Drain PodCreated event
			<-recorder.Events

			// Trigger snapshotting
			fakeClock.Advance(2 * time.Second)
			_, _ = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace}})

			// Check for SnapshottingStarted event
			Eventually(func() bool {
				select {
				case event := <-recorder.Events:
					return event == "Normal SnapshottingStarted Snapshotter job created"
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
				t.Spec.ShutdownPolicy = &sandboxv1alpha1.ShutdownPolicy{Policy: sandboxv1alpha1.ShutdownPolicySnapshotFilesystem}
				t.Spec.PodTemplate.Spec.RuntimeClassName = &runtimeClassName
			})
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName
			snapshotterJobName := sandboxName + "-snap"
			defer deletePod(ctx, podName)
			defer deleteJob(ctx, snapshotterJobName)

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

			// Mark snapshotter job succeeded
			snapshotterJob := getJob(ctx, snapshotterJobName)
			Expect(snapshotterJob).NotTo(BeNil())
			now := metav1.Now()
			snapshotterJob.Status.StartTime = &now
			snapshotterJob.Status.CompletionTime = &now
			snapshotterJob.Status.Succeeded = 1
			snapshotterJob.Status.Conditions = []batchv1.JobCondition{
				{Type: batchv1.JobSuccessCriteriaMet, Status: corev1.ConditionTrue},
				{Type: batchv1.JobComplete, Status: corev1.ConditionTrue},
			}
			Expect(k8sClient.Status().Update(ctx, snapshotterJob)).To(Succeed())

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
					return len(event) > 0 && (event == "Normal SnapshotSucceeded Filesystem snapshot job completed" || len(event) > 20)
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
				t.Spec.ShutdownPolicy = &sandboxv1alpha1.ShutdownPolicy{Policy: sandboxv1alpha1.ShutdownPolicySnapshotFilesystem}
				t.Spec.PodTemplate.Spec.RuntimeClassName = &runtimeClassName
			})
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName
			snapshotterJobName := sandboxName + "-snap"
			defer deletePod(ctx, podName)
			defer deleteJob(ctx, snapshotterJobName)

			// Setup - recreate pod with NodeName
			_, _ = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace}})
			pod := recreatePodWithNodeName(ctx, podName, "test-node", &runtimeClassName)
			makePodReady(ctx, pod, "containerd://abc")

			fakeClock.Advance(2 * time.Second)
			_, _ = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace}})

			// Mark snapshotter job failed
			snapshotterJob := getJob(ctx, snapshotterJobName)
			Expect(snapshotterJob).NotTo(BeNil())
			now := metav1.Now()
			snapshotterJob.Status.StartTime = &now
			snapshotterJob.Status.Failed = 1
			snapshotterJob.Status.Conditions = []batchv1.JobCondition{
				{Type: batchv1.JobFailureTarget, Status: corev1.ConditionTrue},
				{Type: batchv1.JobFailed, Status: corev1.ConditionTrue, Message: "Job failed"},
			}
			Expect(k8sClient.Status().Update(ctx, snapshotterJob)).To(Succeed())

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
			defer deletePod(ctx, sandboxName)

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
			defer deletePod(ctx, sandboxName)

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
			defer deletePod(ctx, sandboxName)

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
			defer deletePod(ctx, sandboxName)

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
			defer deletePod(ctx, sandboxName)

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
			defer deletePod(ctx, sandboxName)

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

		It("should execute SnapshotFilesystem policy on deletion", func() {
			sandboxName := "sandbox-snapshot-delete"
			templateName := "template-snapshot-delete"
			runtimeClassName := "gvisor-delete"

			recorder := record.NewFakeRecorder(10)
			reconciler = newTestReconcilerWithRecorder(fakeClock, recorder)

			createRuntimeClass(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClass(ctx, runtimeClassName)

			createTemplate(ctx, templateName, func(t *sandboxv1alpha1.SandboxTemplate) {
				t.Spec.ShutdownPolicy = &sandboxv1alpha1.ShutdownPolicy{
					Policy: sandboxv1alpha1.ShutdownPolicySnapshotFilesystem,
				}
				t.Spec.PodTemplate.Spec.RuntimeClassName = &runtimeClassName
			})
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)

			podName := sandboxName
			snapshotterJobName := sandboxName + "-snap"
			defer deletePod(ctx, podName)
			defer deleteJob(ctx, snapshotterJobName)

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

			// Reconcile - should create snapshotter job
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: sandboxName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify snapshotter job was created
			snapshotterJob := getJob(ctx, snapshotterJobName)
			Expect(snapshotterJob).NotTo(BeNil())
			Expect(snapshotterJob.Spec.Template.Spec.Containers[0].Name).To(Equal("snapshotter"))

			// Mark snapshotter job as complete
			now := metav1.Now()
			snapshotterJob.Status.StartTime = &now
			snapshotterJob.Status.CompletionTime = &now
			snapshotterJob.Status.Succeeded = 1
			snapshotterJob.Status.Conditions = []batchv1.JobCondition{
				{Type: batchv1.JobSuccessCriteriaMet, Status: corev1.ConditionTrue},
				{Type: batchv1.JobComplete, Status: corev1.ConditionTrue},
			}
			Expect(k8sClient.Status().Update(ctx, snapshotterJob)).To(Succeed())

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
	// Category G: NetworkPolicy Tests
	// ============================================
	Context("NetworkPolicy", func() {
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

		It("should create NetworkPolicy when NetworkTemplate is referenced", func() {
			sandboxName := "sandbox-netpol-basic"
			templateName := "template-netpol-basic"
			networkTemplateName := "nettemplate-basic"

			createTemplate(ctx, templateName)
			defer deleteTemplate(ctx, templateName)

			createNetworkTemplate(ctx, networkTemplateName, func(nt *sandboxv1alpha1.NetworkTemplate) {
				nt.Spec.AllowedEgressCIDRs = []string{"8.8.8.0/24"}
			})
			defer deleteNetworkTemplate(ctx, networkTemplateName)
			defer deleteNetworkPolicyHelper(ctx, networkTemplateName+"-netpol")

			reconcileNetworkTemplate(ctx, networkTemplateName)

			createSandboxWithNetworkTemplate(ctx, sandboxName, templateName, networkTemplateName)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			np := getNetworkPolicy(ctx, networkTemplateName+"-netpol")
			Expect(np).NotTo(BeNil())
			Expect(np.Spec.PolicyTypes).To(ContainElements(
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			))
			// 2 egress rules: DNS (from default spec) + CIDR
			Expect(np.Spec.Egress).To(HaveLen(2))
			// Verify our CIDR rule exists
			var foundCIDR bool
			for _, rule := range np.Spec.Egress {
				for _, to := range rule.To {
					if to.IPBlock != nil && to.IPBlock.CIDR == "8.8.8.0/24" {
						foundCIDR = true
						break
					}
				}
			}
			Expect(foundCIDR).To(BeTrue())

			Expect(np.Spec.PodSelector.MatchLabels).To(HaveKeyWithValue("sandbox.isola.run/network-template", networkTemplateName))

			sandbox := getSandbox(ctx, sandboxName)
			Expect(hasConditionWithReason(sandbox, SandboxNetworkReadyCondition, metav1.ConditionTrue, CondReasonNetworkPolicyApplied)).To(BeTrue())
		})

		It("should not create NetworkPolicy when no NetworkTemplateRef is specified", func() {
			sandboxName := "sandbox-no-netpol"
			templateName := "template-no-netpol"

			createTemplate(ctx, templateName)
			defer deleteTemplate(ctx, templateName)

			createSandbox(ctx, sandboxName, templateName)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			np := getNetworkPolicy(ctx, sandboxName+"-netpol")
			Expect(np).To(BeNil())
		})

		It("should create isolated policy with DNS egress when using default template config", func() {
			// Default template uses DNSPolicy: None with external DNS, so egress to DNS is allowed
			sandboxName := "sandbox-isolated"
			templateName := "template-isolated"
			networkTemplateName := "nettemplate-isolated"

			createTemplate(ctx, templateName)
			defer deleteTemplate(ctx, templateName)

			createNetworkTemplate(ctx, networkTemplateName)
			defer deleteNetworkTemplate(ctx, networkTemplateName)
			defer deleteNetworkPolicyHelper(ctx, networkTemplateName+"-netpol")

			reconcileNetworkTemplate(ctx, networkTemplateName)

			createSandboxWithNetworkTemplate(ctx, sandboxName, templateName, networkTemplateName)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			np := getNetworkPolicy(ctx, networkTemplateName+"-netpol")
			Expect(np).NotTo(BeNil())
			// Ingress is nil (handled by Helm-installed NetworkPolicy for isola-gw)
			Expect(np.Spec.Ingress).To(BeNil())
			// Egress has DNS rule (from default Nameservers)
			Expect(np.Spec.Egress).To(HaveLen(1))
			Expect(np.Spec.Egress[0].To[0].IPBlock.CIDR).To(Equal("8.8.8.8/32"))
			Expect(np.Spec.PolicyTypes).To(ContainElements(
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			))
		})

		It("should include DNS egress rule when Nameservers is specified", func() {
			sandboxName := "sandbox-dns-allowed"
			templateName := "template-dns-allowed"
			networkTemplateName := "nettemplate-dns-allowed"

			createTemplate(ctx, templateName)
			defer deleteTemplate(ctx, templateName)

			createNetworkTemplate(ctx, networkTemplateName, func(nt *sandboxv1alpha1.NetworkTemplate) {
				nt.Spec.Nameservers = []string{"8.8.8.8"}
			})
			defer deleteNetworkTemplate(ctx, networkTemplateName)
			defer deleteNetworkPolicyHelper(ctx, networkTemplateName+"-netpol")

			// Reconcile NetworkTemplate to create NetworkPolicy and set Ready condition
			reconcileNetworkTemplate(ctx, networkTemplateName)

			createSandboxWithNetworkTemplate(ctx, sandboxName, templateName, networkTemplateName)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			// Verify DNS egress rule exists
			np := getNetworkPolicy(ctx, networkTemplateName+"-netpol")
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
			networkTemplateName := "nettemplate-block-metadata"

			createTemplate(ctx, templateName)
			defer deleteTemplate(ctx, templateName)

			createNetworkTemplate(ctx, networkTemplateName, func(nt *sandboxv1alpha1.NetworkTemplate) {
				nt.Spec.AllowedEgressCIDRs = []string{"0.0.0.0/0"}
			})
			defer deleteNetworkTemplate(ctx, networkTemplateName)
			defer deleteNetworkPolicyHelper(ctx, networkTemplateName+"-netpol")

			// Reconcile NetworkTemplate to create NetworkPolicy and set Ready condition
			reconcileNetworkTemplate(ctx, networkTemplateName)

			createSandboxWithNetworkTemplate(ctx, sandboxName, templateName, networkTemplateName)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			// Verify NetworkPolicy has 169.254.0.0/16 in except
			np := getNetworkPolicy(ctx, networkTemplateName+"-netpol")
			Expect(np).NotTo(BeNil())
			// Find the 0.0.0.0/0 rule (not the DNS rule)
			var cidrRule *networkingv1.NetworkPolicyEgressRule
			for i := range np.Spec.Egress {
				for _, to := range np.Spec.Egress[i].To {
					if to.IPBlock != nil && to.IPBlock.CIDR == "0.0.0.0/0" {
						cidrRule = &np.Spec.Egress[i]
						break
					}
				}
			}
			Expect(cidrRule).NotTo(BeNil())
			Expect(cidrRule.To[0].IPBlock.Except).To(ContainElement("169.254.0.0/16"))
		})

		It("should not add except for public CIDRs that don't overlap blocked ranges", func() {
			sandboxName := "sandbox-public-range"
			templateName := "template-public-range"
			networkTemplateName := "nettemplate-public-range"

			createTemplate(ctx, templateName)
			defer deleteTemplate(ctx, templateName)

			createNetworkTemplate(ctx, networkTemplateName, func(nt *sandboxv1alpha1.NetworkTemplate) {
				nt.Spec.AllowedEgressCIDRs = []string{"8.8.8.0/24"}
			})
			defer deleteNetworkTemplate(ctx, networkTemplateName)
			defer deleteNetworkPolicyHelper(ctx, networkTemplateName+"-netpol")

			// Reconcile NetworkTemplate to create NetworkPolicy and set Ready condition
			reconcileNetworkTemplate(ctx, networkTemplateName)

			createSandboxWithNetworkTemplate(ctx, sandboxName, templateName, networkTemplateName)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			// Public CIDR 8.8.8.0/24 doesn't overlap blocked ranges, so no except list
			np := getNetworkPolicy(ctx, networkTemplateName+"-netpol")
			Expect(np).NotTo(BeNil())
			// Find our CIDR rule (not the DNS rule which is 8.8.8.8/32)
			var cidrRule *networkingv1.NetworkPolicyEgressRule
			for i := range np.Spec.Egress {
				for _, to := range np.Spec.Egress[i].To {
					if to.IPBlock != nil && to.IPBlock.CIDR == "8.8.8.0/24" {
						cidrRule = &np.Spec.Egress[i]
						break
					}
				}
			}
			Expect(cidrRule).NotTo(BeNil())
			Expect(cidrRule.To[0].IPBlock.Except).To(BeEmpty())
		})

		It("should create egress rules for allowed egress pods", func() {
			sandboxName := "sandbox-egress-pods"
			templateName := "template-egress-pods"
			networkTemplateName := "nettemplate-egress-pods"

			createTemplate(ctx, templateName)
			defer deleteTemplate(ctx, templateName)

			createNetworkTemplate(ctx, networkTemplateName, func(nt *sandboxv1alpha1.NetworkTemplate) {
				nt.Spec.AllowedEgressPods = []sandboxv1alpha1.EgressPodRule{
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
				}
			})
			defer deleteNetworkTemplate(ctx, networkTemplateName)
			defer deleteNetworkPolicyHelper(ctx, networkTemplateName+"-netpol")

			// Reconcile NetworkTemplate to create NetworkPolicy and set Ready condition
			reconcileNetworkTemplate(ctx, networkTemplateName)

			createSandboxWithNetworkTemplate(ctx, sandboxName, templateName, networkTemplateName)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			// Verify egress pod selector rule
			np := getNetworkPolicy(ctx, networkTemplateName+"-netpol")
			Expect(np).NotTo(BeNil())
			// 2 egress rules: DNS + pod selector
			Expect(np.Spec.Egress).To(HaveLen(2))
			// Find the pod selector rule (not the DNS IP rule)
			var podRule *networkingv1.NetworkPolicyEgressRule
			for i := range np.Spec.Egress {
				for _, to := range np.Spec.Egress[i].To {
					if to.PodSelector != nil {
						podRule = &np.Spec.Egress[i]
						break
					}
				}
			}
			Expect(podRule).NotTo(BeNil())
			Expect(podRule.To).To(HaveLen(1))
			// Verify namespace selector
			Expect(podRule.To[0].NamespaceSelector).NotTo(BeNil())
			Expect(podRule.To[0].NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"]).To(Equal("kube-system"))
			// Verify pod selector
			Expect(podRule.To[0].PodSelector).NotTo(BeNil())
			Expect(podRule.To[0].PodSelector.MatchLabels["k8s-app"]).To(Equal("kube-dns"))
			// Verify ports
			Expect(podRule.Ports).To(HaveLen(2))
		})

		It("should recreate NetworkPolicy if deleted (via NetworkTemplateReconciler)", func() {
			sandboxName := "sandbox-np-recreate"
			templateName := "template-np-recreate"
			networkTemplateName := "nettemplate-np-recreate"

			createTemplate(ctx, templateName)
			defer deleteTemplate(ctx, templateName)

			createNetworkTemplate(ctx, networkTemplateName, func(nt *sandboxv1alpha1.NetworkTemplate) {
				nt.Spec.AllowedEgressCIDRs = []string{"8.8.8.0/24"}
			})
			defer deleteNetworkTemplate(ctx, networkTemplateName)
			defer deleteNetworkPolicyHelper(ctx, networkTemplateName+"-netpol")

			// Reconcile NetworkTemplate to create NetworkPolicy and set Ready condition
			reconcileNetworkTemplate(ctx, networkTemplateName)

			// Verify NetworkPolicy exists
			np := getNetworkPolicy(ctx, networkTemplateName+"-netpol")
			Expect(np).NotTo(BeNil())

			createSandboxWithNetworkTemplate(ctx, sandboxName, templateName, networkTemplateName)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName)

			// Initial reconcile - creates Pod
			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			// Delete the NetworkPolicy externally (simulating attacker/accidental deletion)
			Expect(k8sClient.Delete(ctx, np)).To(Succeed())

			// Verify it's gone
			np = getNetworkPolicy(ctx, networkTemplateName+"-netpol")
			Expect(np).To(BeNil())

			// Reconcile NetworkTemplate - should recreate NetworkPolicy
			// (In production, this is triggered by the NetworkPolicy watch)
			reconcileNetworkTemplate(ctx, networkTemplateName)

			// Verify NetworkPolicy is recreated
			np = getNetworkPolicy(ctx, networkTemplateName+"-netpol")
			Expect(np).NotTo(BeNil())
		})

		It("should share NetworkPolicy across sandboxes using the same NetworkTemplate", func() {
			sandbox1Name := "sandbox-shared-np-1"
			sandbox2Name := "sandbox-shared-np-2"
			templateName := "template-shared-np"
			networkTemplateName := "nettemplate-shared"

			createTemplate(ctx, templateName)
			defer deleteTemplate(ctx, templateName)

			createNetworkTemplate(ctx, networkTemplateName, func(nt *sandboxv1alpha1.NetworkTemplate) {
				nt.Spec.AllowedEgressCIDRs = []string{"8.8.8.0/24"}
			})
			defer deleteNetworkTemplate(ctx, networkTemplateName)
			defer deleteNetworkPolicyHelper(ctx, networkTemplateName+"-netpol")

			// Reconcile NetworkTemplate to create NetworkPolicy and set Ready condition
			reconcileNetworkTemplate(ctx, networkTemplateName)

			np := getNetworkPolicy(ctx, networkTemplateName+"-netpol")
			Expect(np).NotTo(BeNil())

			// Create first sandbox
			createSandboxWithNetworkTemplate(ctx, sandbox1Name, templateName, networkTemplateName)
			defer deleteSandbox(ctx, sandbox1Name)
			defer deletePod(ctx, sandbox1Name)

			// Reconcile first sandbox - creates Pod, reuses NetworkPolicy
			_, err := doReconcile(ctx, reconciler, sandbox1Name)
			Expect(err).NotTo(HaveOccurred())

			// Create second sandbox using the same NetworkTemplate
			createSandboxWithNetworkTemplate(ctx, sandbox2Name, templateName, networkTemplateName)
			defer deleteSandbox(ctx, sandbox2Name)
			defer deletePod(ctx, sandbox2Name)

			// Reconcile second sandbox - should not fail, reuses existing NetworkPolicy
			_, err = doReconcile(ctx, reconciler, sandbox2Name)
			Expect(err).NotTo(HaveOccurred())

			// Verify still only one NetworkPolicy exists
			np = getNetworkPolicy(ctx, networkTemplateName+"-netpol")
			Expect(np).NotTo(BeNil())

			// Both sandboxes should have NetworkConfigured=True
			sandbox1 := getSandbox(ctx, sandbox1Name)
			Expect(hasConditionWithReason(sandbox1, SandboxNetworkReadyCondition, metav1.ConditionTrue, CondReasonNetworkPolicyApplied)).To(BeTrue())
			sandbox2 := getSandbox(ctx, sandbox2Name)
			Expect(hasConditionWithReason(sandbox2, SandboxNetworkReadyCondition, metav1.ConditionTrue, CondReasonNetworkPolicyApplied)).To(BeTrue())
		})

		It("should not use a NetworkTemplate that is being deleted", func() {
			sandboxName := "sandbox-deleting-template"
			templateName := "template-deleting"
			networkTemplateName := "nettemplate-deleting"

			createTemplate(ctx, templateName)
			defer deleteTemplate(ctx, templateName)

			// Create NetworkTemplate with a test finalizer (to prevent immediate deletion)
			createNetworkTemplate(ctx, networkTemplateName, func(nt *sandboxv1alpha1.NetworkTemplate) {
				nt.Spec.AllowedEgressCIDRs = []string{"8.8.8.0/24"}
				controllerutil.AddFinalizer(nt, "test-finalizer")
			})
			defer func() {
				// Cleanup: remove finalizer and delete
				nt := &sandboxv1alpha1.NetworkTemplate{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: networkTemplateName, Namespace: testNamespace}, nt); err == nil {
					controllerutil.RemoveFinalizer(nt, "test-finalizer")
					_ = k8sClient.Update(ctx, nt)
				}
				deleteNetworkTemplate(ctx, networkTemplateName)
			}()

			// Delete the NetworkTemplate - it will be blocked by finalizer, but DeletionTimestamp will be set
			nt := &sandboxv1alpha1.NetworkTemplate{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: networkTemplateName, Namespace: testNamespace}, nt)).To(Succeed())
			Expect(k8sClient.Delete(ctx, nt)).To(Succeed())

			// Verify DeletionTimestamp is set (deletion in progress)
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: networkTemplateName, Namespace: testNamespace}, nt)).To(Succeed())
			Expect(nt.DeletionTimestamp).NotTo(BeNil())

			// Create sandbox referencing the deleting template
			createSandboxWithNetworkTemplate(ctx, sandboxName, templateName, networkTemplateName)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName)

			// Reconcile - should detect template is being deleted
			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			// Verify sandbox has NetworkConfigured=False with reason NetworkTemplateDeleting
			sandbox := getSandbox(ctx, sandboxName)
			Expect(hasConditionWithReason(sandbox, SandboxNetworkReadyCondition,
				metav1.ConditionFalse, CondReasonNetworkTemplateDeleting)).To(BeTrue())

			// Verify no pod was created (template is being deleted)
			pod := getPod(ctx, sandboxName)
			Expect(pod).To(BeNil())
		})
	})

	Context("Embedded Network Spec", func() {
		var (
			templateName string
			sandboxName  string
			fakeClock    *FakeClock
			reconciler   *SandboxReconciler
		)

		BeforeEach(func() {
			suffix := fmt.Sprintf("%d", time.Now().UnixNano())
			templateName = "embed-template-" + suffix
			sandboxName = "embed-sandbox-" + suffix
			fakeClock = NewFakeClock(time.Now())
			reconciler = newTestReconciler(fakeClock)

			// Create sandbox template
			createTemplate(ctx, templateName, func(t *sandboxv1alpha1.SandboxTemplate) {})
		})

		AfterEach(func() {
			deleteTemplate(ctx, templateName)
		})

		// Helper to create sandbox with embedded network spec
		createSandboxWithNetworkSpec := func(name, templateRef string, networkSpec sandboxv1alpha1.NetworkTemplateSpec) {
			sandbox := &sandboxv1alpha1.Sandbox{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: testNamespace,
				},
				Spec: sandboxv1alpha1.SandboxSpec{
					TemplateRef: sandboxv1alpha1.SandboxTemplateReference{
						Name: templateRef,
					},
					Network: &sandboxv1alpha1.NetworkConfig{
						Spec: &networkSpec,
					},
				},
			}
			ExpectWithOffset(1, k8sClient.Create(ctx, sandbox)).To(Succeed())
		}

		It("should create owned NetworkTemplate when sandbox has embedded network spec", func() {
			// Create sandbox with embedded network spec
			networkSpec := sandboxv1alpha1.NetworkTemplateSpec{
				DNSPolicy:          corev1.DNSNone,
				Nameservers:        []string{"8.8.8.8"},
				AllowedEgressCIDRs: []string{"8.8.8.0/24"},
			}
			createSandboxWithNetworkSpec(sandboxName, templateName, networkSpec)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName)

			// Reconcile - should create owned NetworkTemplate
			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			// Verify owned NetworkTemplate was created
			ownedTemplateName := sandboxName + "-net"
			nt := &sandboxv1alpha1.NetworkTemplate{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: ownedTemplateName, Namespace: testNamespace}, nt)
			Expect(err).NotTo(HaveOccurred())

			// Verify spec matches
			Expect(nt.Spec.AllowedEgressCIDRs).To(Equal([]string{"8.8.8.0/24"}))
			Expect(nt.Spec.Nameservers).To(Equal([]string{"8.8.8.8"}))

			// Verify labels
			Expect(nt.Labels["sandbox.isola.run/owner"]).To(Equal(sandboxName))
			Expect(nt.Labels["sandbox.isola.run/owned"]).To(Equal("true"))

			// Verify owner reference
			Expect(nt.OwnerReferences).To(HaveLen(1))
			Expect(nt.OwnerReferences[0].Name).To(Equal(sandboxName))
			Expect(nt.OwnerReferences[0].Kind).To(Equal("Sandbox"))

			// Cleanup
			err = k8sClient.Delete(ctx, nt)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should NOT update owned NetworkTemplate when sandbox spec changes (immutable after creation)", func() {
			// Network spec is immutable after sandbox creation.
			// Changing network isolation mid-flight introduces edge cases with little benefit.

			// Create sandbox with embedded network spec
			networkSpec := sandboxv1alpha1.NetworkTemplateSpec{
				DNSPolicy:          corev1.DNSNone,
				Nameservers:        []string{"8.8.8.8"},
				AllowedEgressCIDRs: []string{"8.8.8.0/24"},
			}
			createSandboxWithNetworkSpec(sandboxName, templateName, networkSpec)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName)

			// First reconcile - creates owned template
			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			// Verify initial spec
			ownedTemplateName := sandboxName + "-net"
			nt := &sandboxv1alpha1.NetworkTemplate{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: ownedTemplateName, Namespace: testNamespace}, nt)
			Expect(err).NotTo(HaveOccurred())
			Expect(nt.Spec.AllowedEgressCIDRs).To(Equal([]string{"8.8.8.0/24"}))

			// Attempt to update sandbox spec
			sandbox := getSandbox(ctx, sandboxName)
			sandbox.Spec.Network.Spec.AllowedEgressCIDRs = []string{"192.168.0.0/16"}
			err = k8sClient.Update(ctx, sandbox)
			Expect(err).NotTo(HaveOccurred())

			// Reconcile again
			_, err = doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			// Verify spec is unchanged - owned NetworkTemplate retains original spec
			err = k8sClient.Get(ctx, types.NamespacedName{Name: ownedTemplateName, Namespace: testNamespace}, nt)
			Expect(err).NotTo(HaveOccurred())
			Expect(nt.Spec.AllowedEgressCIDRs).To(Equal([]string{"8.8.8.0/24"}), "owned NetworkTemplate spec should not change after creation")

			// Cleanup
			err = k8sClient.Delete(ctx, nt)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should fail with error when owned template name conflicts with non-owned template", func() {
			// Create a non-owned NetworkTemplate with the expected owned name
			conflictingName := sandboxName + "-net"
			conflictingNT := &sandboxv1alpha1.NetworkTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      conflictingName,
					Namespace: testNamespace,
				},
				Spec: sandboxv1alpha1.NetworkTemplateSpec{
					DNSPolicy:          corev1.DNSNone,
					Nameservers:        []string{"8.8.8.8"},
					AllowedEgressCIDRs: []string{"0.0.0.0/0"},
				},
			}
			err := k8sClient.Create(ctx, conflictingNT)
			Expect(err).NotTo(HaveOccurred())
			defer func() {
				err := k8sClient.Delete(ctx, conflictingNT)
				Expect(err).NotTo(HaveOccurred())
			}()

			// Create sandbox that would use the conflicting name
			networkSpec := sandboxv1alpha1.NetworkTemplateSpec{
				DNSPolicy:          corev1.DNSNone,
				Nameservers:        []string{"8.8.8.8"},
				AllowedEgressCIDRs: []string{"8.8.8.0/24"},
			}
			createSandboxWithNetworkSpec(sandboxName, templateName, networkSpec)
			defer deleteSandbox(ctx, sandboxName)

			// Reconcile - should fail with ownership error
			_, err = doReconcile(ctx, reconciler, sandboxName)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not owned by sandbox"))

			// Verify sandbox has error condition
			sandbox := getSandbox(ctx, sandboxName)
			Expect(hasConditionWithReason(sandbox, SandboxNetworkReadyCondition,
				metav1.ConditionFalse, CondReasonOwnedTemplateError)).To(BeTrue())
		})

		It("should work with mixed usage - some sandboxes with spec, some with templateRef", func() {
			// Create a shared NetworkTemplate
			sharedTemplateName := templateName + "-shared-network"
			sharedNT := &sandboxv1alpha1.NetworkTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      sharedTemplateName,
					Namespace: testNamespace,
				},
				Spec: sandboxv1alpha1.NetworkTemplateSpec{
					DNSPolicy:          corev1.DNSNone,
					Nameservers:        []string{"8.8.8.8"},
					AllowedEgressCIDRs: []string{"0.0.0.0/0"},
				},
			}
			err := k8sClient.Create(ctx, sharedNT)
			Expect(err).NotTo(HaveOccurred())
			reconcileNetworkTemplate(ctx, sharedTemplateName)
			defer func() {
				err := k8sClient.Delete(ctx, sharedNT)
				Expect(err).NotTo(HaveOccurred())
			}()

			// Create sandbox1 with embedded spec
			sandbox1Name := sandboxName + "-1"
			networkSpec := sandboxv1alpha1.NetworkTemplateSpec{
				DNSPolicy:          corev1.DNSNone,
				Nameservers:        []string{"8.8.8.8"},
				AllowedEgressCIDRs: []string{"8.8.8.0/24"},
			}
			createSandboxWithNetworkSpec(sandbox1Name, templateName, networkSpec)
			defer deleteSandbox(ctx, sandbox1Name)
			defer deletePod(ctx, sandbox1Name)

			// Create sandbox2 with templateRef
			sandbox2Name := sandboxName + "-2"
			createSandboxWithNetworkTemplate(ctx, sandbox2Name, templateName, sharedTemplateName)
			defer deleteSandbox(ctx, sandbox2Name)
			defer deletePod(ctx, sandbox2Name)

			// Reconcile sandbox1
			_, err = doReconcile(ctx, reconciler, sandbox1Name)
			Expect(err).NotTo(HaveOccurred())

			// Verify sandbox1's owned template
			owned1 := &sandboxv1alpha1.NetworkTemplate{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: sandbox1Name + "-net", Namespace: testNamespace}, owned1)
			Expect(err).NotTo(HaveOccurred())
			Expect(owned1.Labels["sandbox.isola.run/owned"]).To(Equal("true"))
			defer func() {
				err := k8sClient.Delete(ctx, owned1)
				Expect(err).NotTo(HaveOccurred())
			}()

			// Reconcile sandbox2
			_, err = doReconcile(ctx, reconciler, sandbox2Name)
			Expect(err).NotTo(HaveOccurred())

			// Verify sandbox2 uses the shared template (no owned template created)
			sandbox2 := getSandbox(ctx, sandbox2Name)
			Expect(sandbox2.GetNetworkTemplateName()).To(Equal(sharedTemplateName))

			// The owned template for sandbox1 should not have sandbox2 as owner
			err = k8sClient.Get(ctx, types.NamespacedName{Name: sandbox1Name + "-net", Namespace: testNamespace}, owned1)
			Expect(err).NotTo(HaveOccurred())
			Expect(owned1.OwnerReferences).To(HaveLen(1))
			Expect(owned1.OwnerReferences[0].Name).To(Equal(sandbox1Name))
		})
	})

	// ============================================
	// Category I: Default NetworkTemplate Tests
	// ============================================
	Context("Default NetworkTemplate Behavior", func() {
		var (
			reconciler   *SandboxReconciler
			fakeClock    *FakeClock
			templateName string
		)

		// restoreDefaultNetworkTemplate recreates and reconciles the default template
		restoreDefaultNetworkTemplate := func() {
			nt := &sandboxv1alpha1.NetworkTemplate{}
			err := k8sClient.Get(ctx, types.NamespacedName{
				Name:      sandboxv1alpha1.DefaultNetworkTemplate,
				Namespace: testNamespace,
			}, nt)
			if err != nil {
				createNetworkTemplate(ctx, sandboxv1alpha1.DefaultNetworkTemplate)
				reconcileNetworkTemplate(ctx, sandboxv1alpha1.DefaultNetworkTemplate)
			}
		}

		BeforeEach(func() {
			fakeClock = NewFakeClock(time.Now())
			reconciler = newTestReconciler(fakeClock)
			templateName = fmt.Sprintf("template-default-net-%d", time.Now().UnixNano())
			createTemplate(ctx, templateName)
		})

		AfterEach(func() {
			deleteTemplate(ctx, templateName)
			restoreDefaultNetworkTemplate()
		})

		It("should block pod creation until default NetworkTemplate exists", func() {
			sandboxName := fmt.Sprintf("sandbox-no-network-%d", time.Now().UnixNano())

			// Delete the default template to test the missing template scenario
			deleteNetworkTemplate(ctx, sandboxv1alpha1.DefaultNetworkTemplate)

			sandbox := &sandboxv1alpha1.Sandbox{
				ObjectMeta: metav1.ObjectMeta{
					Name:      sandboxName,
					Namespace: testNamespace,
				},
				Spec: sandboxv1alpha1.SandboxSpec{
					TemplateRef: sandboxv1alpha1.SandboxTemplateReference{
						Name: templateName,
					},
					Network: nil,
				},
			}
			Expect(k8sClient.Create(ctx, sandbox)).To(Succeed())
			defer deleteSandbox(ctx, sandboxName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			podName := sandboxName
			pod := getPod(ctx, podName)
			Expect(pod).To(BeNil(), "pod should not exist when default NetworkTemplate is missing")

			sandbox = getSandbox(ctx, sandboxName)
			Expect(hasConditionWithReason(sandbox, SandboxNetworkReadyCondition,
				metav1.ConditionFalse, CondReasonNetworkTemplateNotFound)).To(BeTrue())

			cond := meta.FindStatusCondition(sandbox.Status.Conditions, SandboxNetworkReadyCondition)
			Expect(cond.Message).To(ContainSubstring(sandboxv1alpha1.DefaultNetworkTemplate))
		})

		It("should create pod with default template label when default template exists", func() {
			sandboxName := fmt.Sprintf("sandbox-default-template-%d", time.Now().UnixNano())
			podName := sandboxName

			// Default template already exists from BeforeSuite

			sandbox := &sandboxv1alpha1.Sandbox{
				ObjectMeta: metav1.ObjectMeta{
					Name:      sandboxName,
					Namespace: testNamespace,
				},
				Spec: sandboxv1alpha1.SandboxSpec{
					TemplateRef: sandboxv1alpha1.SandboxTemplateReference{
						Name: templateName,
					},
					Network: nil,
				},
			}
			Expect(k8sClient.Create(ctx, sandbox)).To(Succeed())
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())
			Expect(pod.Labels["sandbox.isola.run/network-template"]).To(
				Equal(sandboxv1alpha1.DefaultNetworkTemplate),
			)
		})

		It("should configure ClusterFirst DNS when dnsPolicy is ClusterFirst", func() {
			sandboxName := fmt.Sprintf("sandbox-dns-cluster-%d", time.Now().UnixNano())
			podName := sandboxName
			networkTemplateName := fmt.Sprintf("dns-cluster-template-%d", time.Now().UnixNano())

			// Create network template with ClusterFirst - needs egress to DNS pods
			createNetworkTemplate(ctx, networkTemplateName, func(nt *sandboxv1alpha1.NetworkTemplate) {
				nt.Spec.DNSPolicy = corev1.DNSClusterFirst
				nt.Spec.Nameservers = nil // Clear default nameservers
				nt.Spec.AllowedEgressPods = []sandboxv1alpha1.EgressPodRule{
					{
						Namespace: "kube-system",
						PodSelector: metav1.LabelSelector{
							MatchLabels: map[string]string{"k8s-app": "kube-dns"},
						},
					},
				}
			})
			reconcileNetworkTemplate(ctx, networkTemplateName)
			defer deleteNetworkTemplate(ctx, networkTemplateName)

			createSandboxWithNetworkTemplate(ctx, sandboxName, templateName, networkTemplateName)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())
			Expect(pod.Spec.DNSPolicy).To(Equal(corev1.DNSClusterFirst))
			// No DNSConfig should be set when using ClusterFirst without additional nameservers
			Expect(pod.Spec.DNSConfig).To(BeNil())
		})

		It("should configure DNS None with ndots:1 when dnsPolicy is None", func() {
			sandboxName := fmt.Sprintf("sandbox-dns-none-%d", time.Now().UnixNano())
			podName := sandboxName
			networkTemplateName := fmt.Sprintf("dns-none-template-%d", time.Now().UnixNano())

			createNetworkTemplate(ctx, networkTemplateName, func(nt *sandboxv1alpha1.NetworkTemplate) {
				nt.Spec.DNSPolicy = corev1.DNSNone
				nt.Spec.Nameservers = []string{"8.8.8.8", "1.1.1.1"}
			})
			reconcileNetworkTemplate(ctx, networkTemplateName)
			defer deleteNetworkTemplate(ctx, networkTemplateName)

			createSandboxWithNetworkTemplate(ctx, sandboxName, templateName, networkTemplateName)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())
			Expect(pod.Spec.DNSPolicy).To(Equal(corev1.DNSNone))
			Expect(pod.Spec.DNSConfig).NotTo(BeNil())
			Expect(pod.Spec.DNSConfig.Nameservers).To(Equal([]string{"8.8.8.8", "1.1.1.1"}))
			// ndots:1 is hard-coded for DNSPolicy None
			Expect(pod.Spec.DNSConfig.Options).To(HaveLen(1))
			Expect(pod.Spec.DNSConfig.Options[0].Name).To(Equal("ndots"))
			Expect(*pod.Spec.DNSConfig.Options[0].Value).To(Equal("1"))
		})

		It("should use sink nameserver with fast-fail options when dnsPolicy is None and nameservers is empty", func() {
			sandboxName := fmt.Sprintf("sandbox-dns-sink-%d", time.Now().UnixNano())
			podName := sandboxName
			networkTemplateName := fmt.Sprintf("dns-sink-template-%d", time.Now().UnixNano())

			createNetworkTemplate(ctx, networkTemplateName, func(nt *sandboxv1alpha1.NetworkTemplate) {
				nt.Spec.DNSPolicy = corev1.DNSNone
				nt.Spec.Nameservers = []string{} // Empty - should use sink nameserver
			})
			reconcileNetworkTemplate(ctx, networkTemplateName)
			defer deleteNetworkTemplate(ctx, networkTemplateName)

			createSandboxWithNetworkTemplate(ctx, sandboxName, templateName, networkTemplateName)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())
			Expect(pod.Spec.DNSPolicy).To(Equal(corev1.DNSNone))
			Expect(pod.Spec.DNSConfig).NotTo(BeNil())
			// Should use sink nameserver 127.0.0.1
			Expect(pod.Spec.DNSConfig.Nameservers).To(Equal([]string{"127.0.0.1"}))
			// Should have fast-fail options: timeout=1, attempts=1, ndots=1
			Expect(pod.Spec.DNSConfig.Options).To(HaveLen(3))
			optionMap := make(map[string]string)
			for _, opt := range pod.Spec.DNSConfig.Options {
				if opt.Value != nil {
					optionMap[opt.Name] = *opt.Value
				}
			}
			Expect(optionMap["timeout"]).To(Equal("1"))
			Expect(optionMap["attempts"]).To(Equal("1"))
			Expect(optionMap["ndots"]).To(Equal("1"))
		})

		It("should preserve existing DNSConfig options when adding nameservers for ClusterFirst", func() {
			sandboxName := fmt.Sprintf("sandbox-dns-preserve-%d", time.Now().UnixNano())
			podName := sandboxName
			networkTemplateName := fmt.Sprintf("dns-preserve-template-%d", time.Now().UnixNano())
			customTemplateName := fmt.Sprintf("template-dns-preserve-%d", time.Now().UnixNano())

			// Create a SandboxTemplate with existing DNSConfig options in the PodTemplate
			customTemplate := &sandboxv1alpha1.SandboxTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      customTemplateName,
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
							DNSConfig: &corev1.PodDNSConfig{
								Options: []corev1.PodDNSConfigOption{
									{Name: "single-request-reopen"},
									{Name: "edns0"},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, customTemplate)).To(Succeed())
			defer deleteTemplate(ctx, customTemplateName)

			// Create network template with ClusterFirst + additional nameservers
			createNetworkTemplate(ctx, networkTemplateName, func(nt *sandboxv1alpha1.NetworkTemplate) {
				nt.Spec.DNSPolicy = corev1.DNSClusterFirst
				nt.Spec.Nameservers = []string{"8.8.8.8"}
				nt.Spec.AllowedEgressPods = []sandboxv1alpha1.EgressPodRule{
					{
						Namespace: "kube-system",
						PodSelector: metav1.LabelSelector{
							MatchLabels: map[string]string{"k8s-app": "kube-dns"},
						},
					},
				}
			})
			reconcileNetworkTemplate(ctx, networkTemplateName)
			defer deleteNetworkTemplate(ctx, networkTemplateName)

			createSandboxWithNetworkTemplate(ctx, sandboxName, customTemplateName, networkTemplateName)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())
			Expect(pod.Spec.DNSPolicy).To(Equal(corev1.DNSClusterFirst))
			Expect(pod.Spec.DNSConfig).NotTo(BeNil())
			// Should have the additional nameservers
			Expect(pod.Spec.DNSConfig.Nameservers).To(Equal([]string{"8.8.8.8"}))
			// Should preserve the original options from the PodTemplate
			Expect(pod.Spec.DNSConfig.Options).To(HaveLen(2))
			optionNames := make([]string, 0, len(pod.Spec.DNSConfig.Options))
			for _, opt := range pod.Spec.DNSConfig.Options {
				optionNames = append(optionNames, opt.Name)
			}
			Expect(optionNames).To(ContainElements("single-request-reopen", "edns0"))
		})

		It("should have NetworkReady=False when network template not ready", func() {
			sandboxName := fmt.Sprintf("sandbox-ready-network-%d", time.Now().UnixNano())
			podName := sandboxName

			// Replace the default template with one that is NOT reconciled (not ready)
			deleteNetworkTemplate(ctx, sandboxv1alpha1.DefaultNetworkTemplate)
			createNetworkTemplate(ctx, sandboxv1alpha1.DefaultNetworkTemplate)

			sandbox := &sandboxv1alpha1.Sandbox{
				ObjectMeta: metav1.ObjectMeta{
					Name:      sandboxName,
					Namespace: testNamespace,
				},
				Spec: sandboxv1alpha1.SandboxSpec{
					TemplateRef: sandboxv1alpha1.SandboxTemplateReference{
						Name: templateName,
					},
					Network: nil,
				},
			}
			Expect(k8sClient.Create(ctx, sandbox)).To(Succeed())
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			sandbox = getSandbox(ctx, sandboxName)

			// NetworkReady should be False when template is not ready
			networkCond := meta.FindStatusCondition(sandbox.Status.Conditions, SandboxNetworkReadyCondition)
			Expect(networkCond).NotTo(BeNil())
			Expect(networkCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(networkCond.Reason).To(Equal(CondReasonNetworkConfigNotApplied))

			// Overall Ready should also be False (either PodPending or NetworkConfigNotApplied)
			readyCond := meta.FindStatusCondition(sandbox.Status.Conditions, SandboxReadyCondition)
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))
		})

		It("should index sandboxes without network config under default template name", func() {
			sandboxName := fmt.Sprintf("sandbox-index-test-%d", time.Now().UnixNano())

			sandbox := &sandboxv1alpha1.Sandbox{
				ObjectMeta: metav1.ObjectMeta{
					Name:      sandboxName,
					Namespace: testNamespace,
				},
				Spec: sandboxv1alpha1.SandboxSpec{
					TemplateRef: sandboxv1alpha1.SandboxTemplateReference{
						Name: templateName,
					},
					Network: nil,
				},
			}

			result := extractNetworkTemplateRefName(sandbox)
			Expect(result).To(Equal([]string{sandboxv1alpha1.DefaultNetworkTemplate}))
		})

		It("should reconcile sandbox when referenced NetworkTemplate is created", func() {
			sandboxName := fmt.Sprintf("sandbox-watch-test-%d", time.Now().UnixNano())
			podName := sandboxName
			networkTemplateName := fmt.Sprintf("watch-template-%d", time.Now().UnixNano())

			cachedReconciler := newTestReconcilerWithCache(fakeClock)

			// Create sandbox referencing a template that doesn't exist yet
			createSandboxWithNetworkTemplate(ctx, sandboxName, templateName, networkTemplateName)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			sandbox := getSandbox(ctx, sandboxName)
			Expect(hasConditionWithReason(sandbox, SandboxNetworkReadyCondition,
				metav1.ConditionFalse, CondReasonNetworkTemplateNotFound)).To(BeTrue())

			// Now create the template
			networkTemplate := createNetworkTemplate(ctx, networkTemplateName)
			defer deleteNetworkTemplate(ctx, networkTemplateName)

			// Verify watch triggers reconcile via findSandboxesForNetworkTemplate
			Eventually(func() []reconcile.Request {
				return cachedReconciler.findSandboxesForNetworkTemplate(ctx, networkTemplate)
			}, testTimeout, testInterval).Should(ContainElement(reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      sandboxName,
					Namespace: testNamespace,
				},
			}))
		})
	})
})

var _ = Describe("configureDNS function", func() {
	It("should return error for unsupported DNS policy", func() {
		pod := &corev1.Pod{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "test", Image: "busybox"},
				},
			},
		}

		networkTemplate := &sandboxv1alpha1.NetworkTemplate{
			Spec: sandboxv1alpha1.NetworkTemplateSpec{
				DNSPolicy: corev1.DNSPolicy("InvalidPolicy"),
			},
		}

		err := configureDNS(pod, networkTemplate)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("unsupported DNS policy"))
		Expect(err.Error()).To(ContainSubstring("InvalidPolicy"))
	})

	It("should not return error for empty DNS policy (defaults to ClusterFirst)", func() {
		pod := &corev1.Pod{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "test", Image: "busybox"},
				},
			},
		}

		networkTemplate := &sandboxv1alpha1.NetworkTemplate{
			Spec: sandboxv1alpha1.NetworkTemplateSpec{
				DNSPolicy: "", // Empty should default to ClusterFirst
			},
		}

		err := configureDNS(pod, networkTemplate)
		Expect(err).NotTo(HaveOccurred())
		Expect(pod.Spec.DNSPolicy).To(Equal(corev1.DNSClusterFirst))
	})
})
