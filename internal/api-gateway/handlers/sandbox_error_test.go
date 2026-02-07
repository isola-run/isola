package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	sandboxv1alpha1 "github.com/isola-ai/isola-sb/api/v1alpha1"
)

func newErrorTestAPI(funcs interceptor.Funcs) humatest.TestAPI {
	baseClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()
	wrappedClient := interceptor.NewClient(baseClient, funcs)
	_, api := humatest.New(GinkgoT(), huma.DefaultConfig("Test API", "1.0.0"))
	h := NewSandboxHandlers(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		testNamespace,
		wrappedClient,
	)
	RegisterSandboxRoutes(api, h)
	return api
}

var _ = Describe("Sandbox Error Handling", func() {
	Describe("POST /sandboxes", func() {
		It("returns 409 when k8s Create returns AlreadyExists", func() {
			api := newErrorTestAPI(interceptor.Funcs{
				Create: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.CreateOption) error {
					return apierrors.NewAlreadyExists(schema.GroupResource{Group: "sandbox.isola.run", Resource: "sandboxes"}, "test")
				},
			})

			resp := api.Post("/sandboxes", strings.NewReader(`{"podTemplate":{"container":{"image":"python:3.12"}}}`))
			Expect(resp.Code).To(Equal(409))
		})

		It("returns 400 when k8s Create returns a StatusError", func() {
			api := newErrorTestAPI(interceptor.Funcs{
				Create: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.CreateOption) error {
					return apierrors.NewInvalid(
						schema.GroupKind{Group: "sandbox.isola.run", Kind: "Sandbox"},
						"test",
						field.ErrorList{field.Invalid(field.NewPath("spec"), nil, "invalid spec")},
					)
				},
			})

			resp := api.Post("/sandboxes", strings.NewReader(`{"podTemplate":{"container":{"image":"python:3.12"}}}`))
			Expect(resp.Code).To(Equal(400))
		})

		It("returns 500 when k8s Create returns a non-status error", func() {
			api := newErrorTestAPI(interceptor.Funcs{
				Create: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.CreateOption) error {
					return fmt.Errorf("connection refused")
				},
			})

			resp := api.Post("/sandboxes", strings.NewReader(`{"podTemplate":{"container":{"image":"python:3.12"}}}`))
			Expect(resp.Code).To(Equal(500))
		})
	})

	Describe("GET /sandboxes/{id}", func() {
		It("returns 500 when k8s Get returns a non-NotFound error", func() {
			api := newErrorTestAPI(interceptor.Funcs{
				Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
					return fmt.Errorf("etcd unavailable")
				},
			})

			resp := api.Get("/sandboxes/some-id")
			Expect(resp.Code).To(Equal(500))
		})
	})

	Describe("GET /sandboxes", func() {
		It("returns 500 when k8s List fails", func() {
			api := newErrorTestAPI(interceptor.Funcs{
				List: func(_ context.Context, _ client.WithWatch, _ client.ObjectList, _ ...client.ListOption) error {
					return fmt.Errorf("etcd unavailable")
				},
			})

			resp := api.Get("/sandboxes")
			Expect(resp.Code).To(Equal(500))
		})
	})

	Describe("DELETE /sandboxes/{id}", func() {
		It("returns 500 when pre-delete Get returns a non-NotFound error", func() {
			api := newErrorTestAPI(interceptor.Funcs{
				Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
					return fmt.Errorf("etcd unavailable")
				},
			})

			resp := api.Delete("/sandboxes/some-id")
			Expect(resp.Code).To(Equal(500))
		})

		It("returns 404 when k8s Delete returns NotFound (race condition)", func() {
			// Create a real sandbox so the pre-delete Get succeeds
			createResp := testAPI.Post("/sandboxes", strings.NewReader(`{"podTemplate":{"container":{"image":"alpine:latest"}}}`))
			Expect(createResp.Code).To(Equal(201))

			var created SandboxResponse
			Expect(json.NewDecoder(createResp.Body).Decode(&created)).To(Succeed())

			// Wait for it to appear in the cache
			Eventually(func() error {
				return k8sClient.Get(ctx, keyFor(created.ID), &sandboxv1alpha1.Sandbox{})
			}).Should(Succeed())

			// Interceptor delegates Get to shared envtest client, but Delete returns NotFound
			api := newErrorTestAPI(interceptor.Funcs{
				Get: func(c context.Context, _ client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					return k8sClient.Get(c, key, obj, opts...)
				},
				Delete: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.DeleteOption) error {
					return apierrors.NewNotFound(schema.GroupResource{Group: "sandbox.isola.run", Resource: "sandboxes"}, created.ID)
				},
			})

			resp := api.Delete(fmt.Sprintf("/sandboxes/%s", created.ID))
			Expect(resp.Code).To(Equal(404))
		})

		It("returns 500 when k8s Delete returns a generic error", func() {
			// Create a real sandbox so the pre-delete Get succeeds
			createResp := testAPI.Post("/sandboxes", strings.NewReader(`{"podTemplate":{"container":{"image":"alpine:latest"}}}`))
			Expect(createResp.Code).To(Equal(201))

			var created SandboxResponse
			Expect(json.NewDecoder(createResp.Body).Decode(&created)).To(Succeed())

			// Wait for it to appear in the cache
			Eventually(func() error {
				return k8sClient.Get(ctx, keyFor(created.ID), &sandboxv1alpha1.Sandbox{})
			}).Should(Succeed())

			// Interceptor delegates Get to shared envtest client, but Delete returns generic error
			api := newErrorTestAPI(interceptor.Funcs{
				Get: func(c context.Context, _ client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					return k8sClient.Get(c, key, obj, opts...)
				},
				Delete: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.DeleteOption) error {
					return fmt.Errorf("storage backend unavailable")
				},
			})

			resp := api.Delete(fmt.Sprintf("/sandboxes/%s", created.ID))
			Expect(resp.Code).To(Equal(500))
		})
	})
})
