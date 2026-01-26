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
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	sandboxv1alpha1 "github.com/isola-ai/isola-sb/api/v1alpha1"
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
			Expect(err).To(Satisfy(errors.IsNotFound))
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
			Expect(err).To(Satisfy(errors.IsNotFound))
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
			Expect(err).To(Satisfy(errors.IsNotFound))
		})
	})
})
