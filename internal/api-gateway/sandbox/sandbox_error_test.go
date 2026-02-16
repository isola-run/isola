package sandbox

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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func newErrorTestAPI(funcs interceptor.Funcs) humatest.TestAPI {
	baseClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()
	wrappedClient := interceptor.NewClient(baseClient, funcs)
	_, api := humatest.New(GinkgoT(), huma.DefaultConfig("Test API", "1.0.0"))
	h := New(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		testNamespace,
		wrappedClient,
	)
	Register(api, h)
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

		It("returns 422 when k8s Create returns Invalid (StatusError with code 422)", func() {
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
			Expect(resp.Code).To(Equal(422))
		})

		It("returns 403 when k8s Create returns Forbidden", func() {
			api := newErrorTestAPI(interceptor.Funcs{
				Create: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.CreateOption) error {
					return apierrors.NewForbidden(schema.GroupResource{Group: "sandbox.isola.run", Resource: "sandboxes"}, "test", fmt.Errorf("not allowed"))
				},
			})

			resp := api.Post("/sandboxes", strings.NewReader(`{"podTemplate":{"container":{"image":"python:3.12"}}}`))
			Expect(resp.Code).To(Equal(403))
		})

		It("returns 429 when k8s Create returns TooManyRequests", func() {
			api := newErrorTestAPI(interceptor.Funcs{
				Create: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.CreateOption) error {
					return apierrors.NewTooManyRequests("rate limit exceeded", 30)
				},
			})

			resp := api.Post("/sandboxes", strings.NewReader(`{"podTemplate":{"container":{"image":"python:3.12"}}}`))
			Expect(resp.Code).To(Equal(429))
		})

		It("returns 503 when k8s Create returns ServiceUnavailable", func() {
			api := newErrorTestAPI(interceptor.Funcs{
				Create: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.CreateOption) error {
					return apierrors.NewServiceUnavailable("apiserver overloaded")
				},
			})

			resp := api.Post("/sandboxes", strings.NewReader(`{"podTemplate":{"container":{"image":"python:3.12"}}}`))
			Expect(resp.Code).To(Equal(503))
		})

		It("forwards status code from wrapped K8s errors", func() {
			api := newErrorTestAPI(interceptor.Funcs{
				Create: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.CreateOption) error {
					inner := &apierrors.StatusError{ErrStatus: metav1.Status{
						Code:    403,
						Message: "wrapped forbidden",
						Reason:  metav1.StatusReasonForbidden,
					}}
					return fmt.Errorf("outer context: %w", inner)
				},
			})

			resp := api.Post("/sandboxes", strings.NewReader(`{"podTemplate":{"container":{"image":"python:3.12"}}}`))
			Expect(resp.Code).To(Equal(403))
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
		It("returns 403 when k8s Get returns Forbidden", func() {
			api := newErrorTestAPI(interceptor.Funcs{
				Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
					return apierrors.NewForbidden(schema.GroupResource{Group: "sandbox.isola.run", Resource: "sandboxes"}, "some-id", fmt.Errorf("not allowed"))
				},
			})

			resp := api.Get("/sandboxes/some-id")
			Expect(resp.Code).To(Equal(403))
		})

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
		It("returns 403 when k8s List returns Forbidden", func() {
			api := newErrorTestAPI(interceptor.Funcs{
				List: func(_ context.Context, _ client.WithWatch, _ client.ObjectList, _ ...client.ListOption) error {
					return apierrors.NewForbidden(schema.GroupResource{Group: "sandbox.isola.run", Resource: "sandboxes"}, "", fmt.Errorf("not allowed"))
				},
			})

			resp := api.Get("/sandboxes")
			Expect(resp.Code).To(Equal(403))
		})

		It("returns 500 when k8s List fails", func() {
			api := newErrorTestAPI(interceptor.Funcs{
				List: func(_ context.Context, _ client.WithWatch, _ client.ObjectList, _ ...client.ListOption) error {
					return fmt.Errorf("etcd unavailable")
				},
			})

			resp := api.Get("/sandboxes")
			Expect(resp.Code).To(Equal(500))
		})

		// Happy-path test using newErrorTestAPI for a clean fake client with no data.
		It("returns empty array (not null) when no sandboxes exist", func() {
			api := newErrorTestAPI(interceptor.Funcs{})

			resp := api.Get("/sandboxes")
			Expect(resp.Code).To(Equal(200))

			var list ListSandboxesResponse
			Expect(json.NewDecoder(resp.Body).Decode(&list)).To(Succeed())
			Expect(list.Sandboxes).NotTo(BeNil())
			Expect(list.Sandboxes).To(BeEmpty())
		})
	})

	Describe("DELETE /sandboxes/{id}", func() {
		It("returns 403 when k8s Delete returns Forbidden", func() {
			api := newErrorTestAPI(interceptor.Funcs{
				Delete: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.DeleteOption) error {
					return apierrors.NewForbidden(schema.GroupResource{Group: "sandbox.isola.run", Resource: "sandboxes"}, "some-id", fmt.Errorf("not allowed"))
				},
			})

			resp := api.Delete("/sandboxes/some-id")
			Expect(resp.Code).To(Equal(403))
		})

		It("returns 500 when k8s Delete returns a generic error", func() {
			api := newErrorTestAPI(interceptor.Funcs{
				Delete: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.DeleteOption) error {
					return fmt.Errorf("etcd unavailable")
				},
			})

			resp := api.Delete("/sandboxes/some-id")
			Expect(resp.Code).To(Equal(500))
		})

		It("returns 204 when k8s Delete returns NotFound (idempotent)", func() {
			api := newErrorTestAPI(interceptor.Funcs{
				Delete: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.DeleteOption) error {
					return apierrors.NewNotFound(schema.GroupResource{Group: "sandbox.isola.run", Resource: "sandboxes"}, "some-id")
				},
			})

			resp := api.Delete("/sandboxes/some-id")
			Expect(resp.Code).To(Equal(204))
		})
	})
})
