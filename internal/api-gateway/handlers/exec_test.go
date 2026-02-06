package handlers

import (
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	sandboxv1alpha1 "github.com/isola-ai/isola-sb/api/v1alpha1"
)

func minimalPodTemplate() corev1.PodTemplateSpec {
	return corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "main", Image: "busybox"},
			},
		},
	}
}

var _ = Describe("Exec Proxy", func() {
	Describe("GET /v1/sandboxes/{sandboxID}/ws/exec", func() {
		It("returns 404 for non-existent sandbox", func() {
			resp := testAPI.Get("/v1/sandboxes/nonexistent/ws/exec")
			Expect(resp.Code).To(Equal(http.StatusNotFound))
		})

		It("returns 503 for sandbox that is not ready", func() {
			sandbox := &sandboxv1alpha1.Sandbox{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "not-ready-sb",
					Namespace: testNamespace,
				},
				Spec: sandboxv1alpha1.SandboxSpec{
					PodTemplate: minimalPodTemplate(),
				},
			}
			Expect(k8sClient.Create(ctx, sandbox)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, sandbox)
			})

			resp := testAPI.Get("/v1/sandboxes/not-ready-sb/ws/exec")
			Expect(resp.Code).To(Equal(http.StatusServiceUnavailable))
		})
	})
})
