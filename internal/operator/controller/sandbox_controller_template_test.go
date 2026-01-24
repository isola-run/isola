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
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	sandboxv1alpha1 "github.com/isola-ai/isola-sb/api/v1alpha1"
)

var _ = Describe("Sandbox Controller", func() {

	// ============================================
	// Template Lifecycle Tests
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
})
