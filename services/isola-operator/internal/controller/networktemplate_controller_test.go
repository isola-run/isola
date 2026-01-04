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
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	sandboxv1alpha1 "github.com/omereli/dev-isola/services/isola-operator/api/v1alpha1"
	"github.com/omereli/dev-isola/services/isola-operator/internal/controller/network"
	"github.com/omereli/dev-isola/services/isola-operator/test/utils"
)

var _ = Describe("NetworkTemplate Controller", func() {
	var (
		reconciler *NetworkTemplateReconciler
	)

	BeforeEach(func() {
		reconciler = newTestNetworkTemplateReconciler()
	})

	doReconcile := func(name string) (ctrl.Result, error) {
		return reconciler.Reconcile(ctx, ctrl.Request{
			NamespacedName: types.NamespacedName{Name: name, Namespace: testNamespace},
		})
	}

	getNetworkTemplate := func(name string) *sandboxv1alpha1.NetworkTemplate {
		nt := &sandboxv1alpha1.NetworkTemplate{}
		err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: testNamespace}, nt)
		if err != nil {
			return nil
		}
		return nt
	}

	getNetworkPolicy := func(templateName string) *networkingv1.NetworkPolicy {
		np := &networkingv1.NetworkPolicy{}
		err := k8sClient.Get(ctx, types.NamespacedName{
			Name:      network.GetNetworkPolicyName(templateName),
			Namespace: testNamespace,
		}, np)
		if err != nil {
			return nil
		}
		return np
	}

	createNetworkTemplate := func(name string, opts ...utils.NetworkTemplateOption) {
		nt := utils.NewTestNetworkTemplate(name, testNamespace, opts...)
		ExpectWithOffset(1, k8sClient.Create(ctx, nt)).To(Succeed())
		EventuallyWithOffset(1, func() error {
			return k8sCache.Get(ctx, types.NamespacedName{Name: name, Namespace: testNamespace}, &sandboxv1alpha1.NetworkTemplate{})
		}, "5s", "100ms").Should(Succeed())
	}

	deleteNetworkTemplate := func(name string) {
		nt := &sandboxv1alpha1.NetworkTemplate{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: testNamespace}, nt); err == nil {
			_ = k8sClient.Delete(ctx, nt)
		}
	}

	deleteNetworkPolicy := func(templateName string) {
		np := &networkingv1.NetworkPolicy{}
		name := network.GetNetworkPolicyName(templateName)
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: testNamespace}, np); err == nil {
			_ = k8sClient.Delete(ctx, np)
		}
	}

	Context("Finalizer Behavior", func() {
		It("should add finalizer on first reconcile", func() {
			name := "nt-finalizer-test"
			createNetworkTemplate(name)
			defer deleteNetworkTemplate(name)
			defer deleteNetworkPolicy(name)

			nt := getNetworkTemplate(name)
			Expect(controllerutil.ContainsFinalizer(nt, NetworkTemplateFinalizer)).To(BeFalse())

			_, err := doReconcile(name)
			Expect(err).NotTo(HaveOccurred())

			nt = getNetworkTemplate(name)
			Expect(controllerutil.ContainsFinalizer(nt, NetworkTemplateFinalizer)).To(BeTrue())
		})
	})

	Context("NetworkPolicy Creation", func() {
		It("should create NetworkPolicy and set Ready=True on first reconcile", func() {
			name := "nt-create-policy"
			createNetworkTemplate(name, utils.WithAllowedEgress("8.8.8.0/24"))
			defer deleteNetworkTemplate(name)
			defer deleteNetworkPolicy(name)

			_, err := doReconcile(name)
			Expect(err).NotTo(HaveOccurred())

			np := getNetworkPolicy(name)
			Expect(np).NotTo(BeNil())
			Expect(np.Spec.Egress).To(HaveLen(1))
			Expect(np.Spec.Egress[0].To[0].IPBlock.CIDR).To(Equal("8.8.8.0/24"))

			nt := getNetworkTemplate(name)
			readyCond := meta.FindStatusCondition(nt.Status.Conditions, string(sandboxv1alpha1.NetworkTemplateReady))
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionTrue))
		})

		It("should set Ready=False for invalid CIDR", func() {
			name := "nt-invalid-cidr"

			nt := &sandboxv1alpha1.NetworkTemplate{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
				Spec: sandboxv1alpha1.NetworkTemplateSpec{
					AllowedEgress: []string{"not-a-valid-cidr"},
				},
			}
			Expect(k8sClient.Create(ctx, nt)).To(Succeed())
			defer deleteNetworkTemplate(name)

			Eventually(func() error {
				return k8sCache.Get(ctx, types.NamespacedName{Name: name, Namespace: testNamespace}, &sandboxv1alpha1.NetworkTemplate{})
			}, "5s", "100ms").Should(Succeed())

			_, err := doReconcile(name)
			Expect(err).NotTo(HaveOccurred())

			nt = getNetworkTemplate(name)
			readyCond := meta.FindStatusCondition(nt.Status.Conditions, string(sandboxv1alpha1.NetworkTemplateReady))
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCond.Reason).To(Equal(CondReasonNetworkTemplateInvalid))
		})

		It("should be idempotent across multiple reconciles", func() {
			name := "nt-idempotent"
			createNetworkTemplate(name, utils.WithAllowedEgress("1.1.1.0/24"))
			defer deleteNetworkTemplate(name)
			defer deleteNetworkPolicy(name)

			for i := 0; i < 3; i++ {
				_, err := doReconcile(name)
				Expect(err).NotTo(HaveOccurred())
			}

			np := getNetworkPolicy(name)
			Expect(np).NotTo(BeNil())

			nt := getNetworkTemplate(name)
			readyCond := meta.FindStatusCondition(nt.Status.Conditions, string(sandboxv1alpha1.NetworkTemplateReady))
			Expect(readyCond.Status).To(Equal(metav1.ConditionTrue))
		})
	})

	Context("Immutability", func() {
		It("should ignore spec updates - NetworkPolicy remains unchanged", func() {
			name := "nt-immutable"
			createNetworkTemplate(name, utils.WithAllowedEgress("8.8.8.0/24"))
			defer deleteNetworkTemplate(name)
			defer deleteNetworkPolicy(name)

			_, err := doReconcile(name)
			Expect(err).NotTo(HaveOccurred())

			np := getNetworkPolicy(name)
			Expect(np.Spec.Egress[0].To[0].IPBlock.CIDR).To(Equal("8.8.8.0/24"))

			// Update the spec to a different CIDR (also public, so it's valid)
			nt := getNetworkTemplate(name)
			nt.Spec.AllowedEgress = []string{"1.1.1.0/24"}
			Expect(k8sClient.Update(ctx, nt)).To(Succeed())

			// Reconcile again - policy should NOT change (immutable)
			_, err = doReconcile(name)
			Expect(err).NotTo(HaveOccurred())

			np = getNetworkPolicy(name)
			Expect(np.Spec.Egress[0].To[0].IPBlock.CIDR).To(Equal("8.8.8.0/24"))
		})
	})

	Context("Deletion", func() {
		It("should block deletion while sandboxes reference template", func() {
			templateName := "nt-blocked-delete"
			sandboxName := "sandbox-blocking-delete"
			sandboxTemplateName := "st-for-blocked-delete"

			sandboxTemplate := utils.NewTestSandboxTemplate(sandboxTemplateName, testNamespace)
			Expect(k8sClient.Create(ctx, sandboxTemplate)).To(Succeed())
			defer func() {
				_ = k8sClient.Delete(ctx, sandboxTemplate)
			}()

			createNetworkTemplate(templateName)
			defer deleteNetworkPolicy(templateName)

			_, err := doReconcile(templateName)
			Expect(err).NotTo(HaveOccurred())

			sandbox := utils.NewTestSandbox(sandboxName, testNamespace, sandboxTemplateName,
				utils.WithNetworkTemplateRef(templateName))
			Expect(k8sClient.Create(ctx, sandbox)).To(Succeed())
			defer func() {
				_ = k8sClient.Delete(ctx, sandbox)
			}()

			Eventually(func() error {
				return k8sCache.Get(ctx, types.NamespacedName{Name: sandboxName, Namespace: testNamespace}, &sandboxv1alpha1.Sandbox{})
			}, "5s", "100ms").Should(Succeed())

			nt := getNetworkTemplate(templateName)
			Expect(k8sClient.Delete(ctx, nt)).To(Succeed())

			// Wait for cache to see DeletionTimestamp
			Eventually(func() bool {
				cached := &sandboxv1alpha1.NetworkTemplate{}
				if err := k8sCache.Get(ctx, types.NamespacedName{Name: templateName, Namespace: testNamespace}, cached); err != nil {
					return false
				}
				return !cached.DeletionTimestamp.IsZero()
			}, "5s", "100ms").Should(BeTrue())

			// Reconcile should requeue because sandbox still references it
			result, err := doReconcile(templateName)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(5 * time.Second))

			// Finalizer should still be present
			nt = getNetworkTemplate(templateName)
			Expect(nt).NotTo(BeNil())
			Expect(controllerutil.ContainsFinalizer(nt, NetworkTemplateFinalizer)).To(BeTrue())
		})

		It("should remove finalizer and complete deletion when no sandboxes reference template", func() {
			name := "nt-allowed-delete"
			createNetworkTemplate(name)
			defer deleteNetworkPolicy(name)

			_, err := doReconcile(name)
			Expect(err).NotTo(HaveOccurred())

			nt := getNetworkTemplate(name)
			Expect(controllerutil.ContainsFinalizer(nt, NetworkTemplateFinalizer)).To(BeTrue())

			Expect(k8sClient.Delete(ctx, nt)).To(Succeed())

			// Wait for cache to see DeletionTimestamp
			Eventually(func() bool {
				nt := getNetworkTemplate(name)
				return nt != nil && !nt.DeletionTimestamp.IsZero()
			}, "5s", "100ms").Should(BeTrue())

			// Reconcile - should remove finalizer since no sandboxes reference it
			_, err = doReconcile(name)
			Expect(err).NotTo(HaveOccurred())

			// Finalizer should be removed, allowing Kubernetes to complete deletion
			Eventually(func() bool {
				nt := getNetworkTemplate(name)
				return nt == nil || !controllerutil.ContainsFinalizer(nt, NetworkTemplateFinalizer)
			}, "5s", "100ms").Should(BeTrue())
		})
	})

	Context("NetworkPolicy Recreation", func() {
		It("should recreate NetworkPolicy if deleted out-of-band", func() {
			name := "nt-recreate-policy"
			createNetworkTemplate(name, utils.WithAllowedEgress("8.8.8.0/24"))
			defer deleteNetworkTemplate(name)
			defer deleteNetworkPolicy(name)

			_, err := doReconcile(name)
			Expect(err).NotTo(HaveOccurred())
			Expect(getNetworkPolicy(name)).NotTo(BeNil())

			// Delete the NetworkPolicy directly (simulating out-of-band deletion)
			deleteNetworkPolicy(name)
			Eventually(func() bool {
				return getNetworkPolicy(name) == nil
			}, "2s", "100ms").Should(BeTrue())

			// Reconcile should recreate it
			_, err = doReconcile(name)
			Expect(err).NotTo(HaveOccurred())

			np := getNetworkPolicy(name)
			Expect(np).NotTo(BeNil())
			Expect(np.Spec.Egress[0].To[0].IPBlock.CIDR).To(Equal("8.8.8.0/24"))
		})
	})

	Context("Ready Condition", func() {
		It("should set ObservedGeneration in Ready condition", func() {
			name := "nt-observed-gen"
			createNetworkTemplate(name)
			defer deleteNetworkTemplate(name)
			defer deleteNetworkPolicy(name)

			_, err := doReconcile(name)
			Expect(err).NotTo(HaveOccurred())

			nt := getNetworkTemplate(name)
			readyCond := meta.FindStatusCondition(nt.Status.Conditions, string(sandboxv1alpha1.NetworkTemplateReady))
			Expect(readyCond.ObservedGeneration).To(Equal(nt.Generation))
		})
	})

	Context("Edge Cases", func() {
		It("should handle non-existent NetworkTemplate gracefully", func() {
			result, err := doReconcile("non-existent-template")
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(ctrl.Result{}))
		})

		It("should handle concurrent NetworkPolicy creation (AlreadyExists)", func() {
			name := "nt-concurrent-create"
			createNetworkTemplate(name)
			defer deleteNetworkTemplate(name)
			defer deleteNetworkPolicy(name)

			// Pre-create the NetworkPolicy to simulate race condition
			np := &networkingv1.NetworkPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      network.GetNetworkPolicyName(name),
					Namespace: testNamespace,
				},
				Spec: networkingv1.NetworkPolicySpec{
					PodSelector: metav1.LabelSelector{},
				},
			}
			Expect(k8sClient.Create(ctx, np)).To(Succeed())

			// Reconcile should handle AlreadyExists gracefully
			_, err := doReconcile(name)
			Expect(err).NotTo(HaveOccurred())

			nt := getNetworkTemplate(name)
			readyCond := meta.FindStatusCondition(nt.Status.Conditions, string(sandboxv1alpha1.NetworkTemplateReady))
			Expect(readyCond.Status).To(Equal(metav1.ConditionTrue))
		})

		It("should not remove finalizer if template has DeletionTimestamp but sandboxes exist", func() {
			templateName := "nt-deletion-with-sandboxes"
			sandboxName := "sandbox-for-deletion-test"
			sandboxTemplateName := "st-for-deletion-test"

			sandboxTemplate := utils.NewTestSandboxTemplate(sandboxTemplateName, testNamespace)
			Expect(k8sClient.Create(ctx, sandboxTemplate)).To(Succeed())
			defer func() {
				_ = k8sClient.Delete(ctx, sandboxTemplate)
			}()

			createNetworkTemplate(templateName)
			defer deleteNetworkPolicy(templateName)

			_, _ = doReconcile(templateName)

			sandbox := utils.NewTestSandbox(sandboxName, testNamespace, sandboxTemplateName,
				utils.WithNetworkTemplateRef(templateName))
			Expect(k8sClient.Create(ctx, sandbox)).To(Succeed())

			Eventually(func() error {
				return k8sCache.Get(ctx, types.NamespacedName{Name: sandboxName, Namespace: testNamespace}, &sandboxv1alpha1.Sandbox{})
			}, "5s", "100ms").Should(Succeed())

			nt := getNetworkTemplate(templateName)
			Expect(k8sClient.Delete(ctx, nt)).To(Succeed())

			// Wait for cache to see DeletionTimestamp
			Eventually(func() bool {
				cached := &sandboxv1alpha1.NetworkTemplate{}
				if err := k8sCache.Get(ctx, types.NamespacedName{Name: templateName, Namespace: testNamespace}, cached); err != nil {
					return false
				}
				return !cached.DeletionTimestamp.IsZero()
			}, "5s", "100ms").Should(BeTrue())

			// Reconcile should block deletion because sandbox still references it
			result, err := doReconcile(templateName)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(5 * time.Second))

			// Finalizer should still be present
			nt = getNetworkTemplate(templateName)
			Expect(controllerutil.ContainsFinalizer(nt, NetworkTemplateFinalizer)).To(BeTrue())

			// Delete sandbox, then template deletion should proceed
			Expect(k8sClient.Delete(ctx, sandbox)).To(Succeed())
			Eventually(func() error {
				return k8sCache.Get(ctx, types.NamespacedName{Name: sandboxName, Namespace: testNamespace}, &sandboxv1alpha1.Sandbox{})
			}, "5s", "100ms").ShouldNot(Succeed())

			_, err = doReconcile(templateName)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() bool {
				nt := getNetworkTemplate(templateName)
				return nt == nil || !controllerutil.ContainsFinalizer(nt, NetworkTemplateFinalizer)
			}, "5s", "100ms").Should(BeTrue())
		})
	})
})
