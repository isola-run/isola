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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	sandboxv1alpha1 "github.com/isola-ai/isola-sb/api/v1alpha1"
)

var _ = Describe("SandboxIngress Controller", func() {
	var (
		reconciler *SandboxIngressReconciler
	)

	BeforeEach(func() {
		reconciler = &SandboxIngressReconciler{
			Client:           k8sClient,
			Scheme:           scheme.Scheme,
			IngressDomain:    "sandboxes.example.com",
			GatewayName:      "sandbox-gateway",
			GatewayNamespace: "isola-system",
		}
	})

	Context("when ingress domain is not configured", func() {
		It("should set GatewayNotEnabled condition", func() {
			ingressName := "ingress-no-domain"
			sandboxName := "sandbox-no-domain"

			ingress := &sandboxv1alpha1.SandboxIngress{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ingressName,
					Namespace: testNamespace,
				},
				Spec: sandboxv1alpha1.SandboxIngressSpec{
					SandboxRef:    sandboxName,
					ContainerPort: 8080,
				},
			}
			Expect(k8sClient.Create(ctx, ingress)).To(Succeed())
			defer deleteIngress(ctx, ingressName)

			// Use a reconciler without ingress domain
			noIngressReconciler := &SandboxIngressReconciler{
				Client:           k8sClient,
				Scheme:           scheme.Scheme,
				IngressDomain:    "",
				GatewayName:      "sandbox-gateway",
				GatewayNamespace: "isola-system",
			}

			_, err := noIngressReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: ingressName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Fetch updated ingress
			updatedIngress := &sandboxv1alpha1.SandboxIngress{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: ingressName, Namespace: testNamespace}, updatedIngress)).To(Succeed())

			// Check Ready condition
			readyCond := meta.FindStatusCondition(updatedIngress.Status.Conditions, SandboxIngressReadyCondition)
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCond.Reason).To(Equal(CondReasonGatewayNotEnabled))
		})
	})

	Context("when referenced sandbox does not exist", func() {
		It("should set SandboxNotFound condition", func() {
			ingressName := "ingress-no-sandbox"

			ingress := &sandboxv1alpha1.SandboxIngress{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ingressName,
					Namespace: testNamespace,
				},
				Spec: sandboxv1alpha1.SandboxIngressSpec{
					SandboxRef:    "nonexistent-sandbox",
					ContainerPort: 8080,
				},
			}
			Expect(k8sClient.Create(ctx, ingress)).To(Succeed())
			defer deleteIngress(ctx, ingressName)

			// Reconcile to add finalizer
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: ingressName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Reconcile again to check sandbox
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: ingressName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Fetch updated ingress
			updatedIngress := &sandboxv1alpha1.SandboxIngress{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: ingressName, Namespace: testNamespace}, updatedIngress)).To(Succeed())

			// Check SandboxReady condition
			sandboxCond := meta.FindStatusCondition(updatedIngress.Status.Conditions, SandboxIngressSandboxReadyCondition)
			Expect(sandboxCond).NotTo(BeNil())
			Expect(sandboxCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(sandboxCond.Reason).To(Equal(CondReasonSandboxNotFound))
		})
	})

	Context("when sandbox is not ready", func() {
		It("should set SandboxNotReady condition", func() {
			ingressName := "ingress-sandbox-not-ready"
			sandboxName := "sandbox-not-ready"

			// Create sandbox without Ready condition
			sandbox := &sandboxv1alpha1.Sandbox{
				ObjectMeta: metav1.ObjectMeta{
					Name:      sandboxName,
					Namespace: testNamespace,
					Labels: map[string]string{
						"sandbox-id": sandboxName,
					},
				},
				Spec: sandboxv1alpha1.SandboxSpec{
					Image: "busybox:latest",
				},
			}
			Expect(k8sClient.Create(ctx, sandbox)).To(Succeed())
			defer deleteSandbox(ctx, sandboxName)

			ingress := &sandboxv1alpha1.SandboxIngress{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ingressName,
					Namespace: testNamespace,
				},
				Spec: sandboxv1alpha1.SandboxIngressSpec{
					SandboxRef:    sandboxName,
					ContainerPort: 8080,
				},
			}
			Expect(k8sClient.Create(ctx, ingress)).To(Succeed())
			defer deleteIngress(ctx, ingressName)

			// Reconcile to add finalizer
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: ingressName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Reconcile again to check sandbox readiness
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: ingressName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Fetch updated ingress
			updatedIngress := &sandboxv1alpha1.SandboxIngress{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: ingressName, Namespace: testNamespace}, updatedIngress)).To(Succeed())

			// Check SandboxReady condition
			sandboxCond := meta.FindStatusCondition(updatedIngress.Status.Conditions, SandboxIngressSandboxReadyCondition)
			Expect(sandboxCond).NotTo(BeNil())
			Expect(sandboxCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(sandboxCond.Reason).To(Equal(CondReasonSandboxNotReady))
		})
	})

	Context("when sandbox is ready", func() {
		It("should create Service for the ingress", func() {
			ingressName := "ingress-ready-sandbox"
			sandboxName := "sandbox-ready-for-ingress"

			// Create sandbox with Ready condition
			sandbox := &sandboxv1alpha1.Sandbox{
				ObjectMeta: metav1.ObjectMeta{
					Name:      sandboxName,
					Namespace: testNamespace,
					Labels: map[string]string{
						"sandbox-id": sandboxName,
					},
				},
				Spec: sandboxv1alpha1.SandboxSpec{
					Image: "busybox:latest",
				},
			}
			Expect(k8sClient.Create(ctx, sandbox)).To(Succeed())
			defer deleteSandbox(ctx, sandboxName)

			// Set sandbox Ready condition
			meta.SetStatusCondition(&sandbox.Status.Conditions, metav1.Condition{
				Type:   SandboxReadyCondition,
				Status: metav1.ConditionTrue,
				Reason: "Ready",
			})
			Expect(k8sClient.Status().Update(ctx, sandbox)).To(Succeed())

			ingress := &sandboxv1alpha1.SandboxIngress{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ingressName,
					Namespace: testNamespace,
				},
				Spec: sandboxv1alpha1.SandboxIngressSpec{
					SandboxRef:    sandboxName,
					ContainerPort: 8080,
				},
			}
			Expect(k8sClient.Create(ctx, ingress)).To(Succeed())
			defer deleteIngress(ctx, ingressName)
			defer deleteService(ctx, ingressName+"-ingress")

			// Reconcile to add finalizer
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: ingressName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Reconcile again to create resources
			// Note: HTTPRoute creation will fail since Gateway API CRDs aren't installed
			// but Service should be created
			_, _ = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: ingressName, Namespace: testNamespace},
			})

			// Verify Service was created
			svc := &corev1.Service{}
			serviceName := ingressName + "-ingress"
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: testNamespace}, svc)).To(Succeed())

			// Verify Service spec
			Expect(svc.Spec.Selector["sandbox-id"]).To(Equal(sandboxName))
			Expect(svc.Spec.Ports).To(HaveLen(1))
			Expect(svc.Spec.Ports[0].Port).To(Equal(int32(80)))
			Expect(svc.Spec.Ports[0].TargetPort.IntVal).To(Equal(int32(8080)))
		})

		It("should not recreate Service if it already exists", func() {
			ingressName := "ingress-existing-service"
			sandboxName := "sandbox-existing-service"

			// Create sandbox with Ready condition
			sandbox := &sandboxv1alpha1.Sandbox{
				ObjectMeta: metav1.ObjectMeta{
					Name:      sandboxName,
					Namespace: testNamespace,
					Labels: map[string]string{
						"sandbox-id": sandboxName,
					},
				},
				Spec: sandboxv1alpha1.SandboxSpec{
					Image: "busybox:latest",
				},
			}
			Expect(k8sClient.Create(ctx, sandbox)).To(Succeed())
			defer deleteSandbox(ctx, sandboxName)

			meta.SetStatusCondition(&sandbox.Status.Conditions, metav1.Condition{
				Type:   SandboxReadyCondition,
				Status: metav1.ConditionTrue,
				Reason: "Ready",
			})
			Expect(k8sClient.Status().Update(ctx, sandbox)).To(Succeed())

			ingress := &sandboxv1alpha1.SandboxIngress{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ingressName,
					Namespace: testNamespace,
				},
				Spec: sandboxv1alpha1.SandboxIngressSpec{
					SandboxRef:    sandboxName,
					ContainerPort: 9000,
				},
			}
			Expect(k8sClient.Create(ctx, ingress)).To(Succeed())
			defer deleteIngress(ctx, ingressName)
			defer deleteService(ctx, ingressName+"-ingress")

			// Reconcile to add finalizer
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: ingressName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Reconcile to create Service
			_, _ = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: ingressName, Namespace: testNamespace},
			})

			// Get Service and record its UID
			svc := &corev1.Service{}
			serviceName := ingressName + "-ingress"
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: testNamespace}, svc)).To(Succeed())
			originalUID := svc.UID

			// Reconcile again
			_, _ = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: ingressName, Namespace: testNamespace},
			})

			// Verify Service UID is unchanged (not recreated)
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: testNamespace}, svc)).To(Succeed())
			Expect(svc.UID).To(Equal(originalUID))
		})
	})

	Context("finalizer handling", func() {
		It("should add finalizer on first reconcile", func() {
			ingressName := "ingress-finalizer-add"

			ingress := &sandboxv1alpha1.SandboxIngress{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ingressName,
					Namespace: testNamespace,
				},
				Spec: sandboxv1alpha1.SandboxIngressSpec{
					SandboxRef:    "some-sandbox",
					ContainerPort: 8080,
				},
			}
			Expect(k8sClient.Create(ctx, ingress)).To(Succeed())
			defer deleteIngress(ctx, ingressName)

			// First reconcile should add finalizer
			result, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: ingressName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Requeue).To(BeTrue())

			// Verify finalizer was added
			updatedIngress := &sandboxv1alpha1.SandboxIngress{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: ingressName, Namespace: testNamespace}, updatedIngress)).To(Succeed())
			Expect(updatedIngress.Finalizers).To(ContainElement(SandboxIngressFinalizer))
		})

		It("should remove finalizer on deletion", func() {
			ingressName := "ingress-finalizer-remove"

			ingress := &sandboxv1alpha1.SandboxIngress{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ingressName,
					Namespace: testNamespace,
				},
				Spec: sandboxv1alpha1.SandboxIngressSpec{
					SandboxRef:    "some-sandbox",
					ContainerPort: 8080,
				},
			}
			Expect(k8sClient.Create(ctx, ingress)).To(Succeed())

			// Add finalizer via reconcile
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: ingressName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Delete the ingress
			Expect(k8sClient.Delete(ctx, ingress)).To(Succeed())

			// Reconcile to remove finalizer
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: ingressName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify ingress is deleted
			deletedIngress := &sandboxv1alpha1.SandboxIngress{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: ingressName, Namespace: testNamespace}, deletedIngress)
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})
	})

	Context("GetServiceName and GetHTTPRouteName", func() {
		It("should return correct names", func() {
			ingress := &sandboxv1alpha1.SandboxIngress{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-ingress",
					Namespace: testNamespace,
				},
			}
			Expect(ingress.GetServiceName()).To(Equal("my-ingress-ingress"))
			Expect(ingress.GetHTTPRouteName()).To(Equal("my-ingress-route"))
		})
	})
})

func deleteIngress(ctx context.Context, name string) {
	ingress := &sandboxv1alpha1.SandboxIngress{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: testNamespace}, ingress); err == nil {
		// Remove finalizer to allow deletion
		ingress.Finalizers = nil
		_ = k8sClient.Update(ctx, ingress)
		_ = k8sClient.Delete(ctx, ingress)
	}
}

func deleteService(ctx context.Context, name string) {
	svc := &corev1.Service{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: testNamespace}, svc); err == nil {
		_ = k8sClient.Delete(ctx, svc)
	}
}
