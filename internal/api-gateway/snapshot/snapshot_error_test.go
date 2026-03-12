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

package snapshot

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	sandboxv1alpha1 "github.com/isola-ai/isola/api/v1alpha1"
)

// newErrorTestAPI creates a test API with an interceptor-wrapped fake client.
// A running sandbox is pre-seeded so POST handlers pass GetReadySandbox validation.
func newErrorTestAPI(funcs interceptor.Funcs) humatest.TestAPI {
	runningSandbox := &sandboxv1alpha1.Sandbox{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-sandbox",
			Namespace: testNamespace,
		},
		Spec: sandboxv1alpha1.SandboxSpec{
			PodTemplate: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "sandbox", Image: "alpine:latest"},
					},
				},
			},
		},
		Status: sandboxv1alpha1.SandboxStatus{
			PodIP: "127.0.0.1",
			Conditions: []metav1.Condition{
				{
					Type:               "Ready",
					Status:             metav1.ConditionTrue,
					Reason:             "PodRunning",
					LastTransitionTime: metav1.Now(),
				},
			},
		},
	}

	baseClient := fake.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithObjects(runningSandbox).
		WithStatusSubresource(runningSandbox).
		Build()
	wrappedClient := interceptor.NewClient(baseClient, funcs)
	_, api := humatest.New(GinkgoT(), huma.DefaultConfig("Test API", "0.1.0"))
	h := New(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		testNamespace,
		wrappedClient,
	)
	Register(api, h)
	return api
}

var _ = Describe("Snapshot Error Handling", func() {
	Describe("POST /sandboxes/{id}/rootfssnapshots", func() {
		It("returns 409 when k8s Create returns AlreadyExists", func() {
			api := newErrorTestAPI(interceptor.Funcs{
				Create: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.CreateOption) error {
					return apierrors.NewAlreadyExists(schema.GroupResource{Group: "sandbox.isola.run", Resource: "rootfssnapshots"}, "test")
				},
			})

			resp := api.Post("/sandboxes/test-sandbox/rootfssnapshots",
				strings.NewReader(`{"snapshotName":"test"}`))
			Expect(resp.Code).To(Equal(409))
		})

		It("returns 422 when k8s Create returns Invalid", func() {
			api := newErrorTestAPI(interceptor.Funcs{
				Create: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.CreateOption) error {
					return apierrors.NewInvalid(
						schema.GroupKind{Group: "sandbox.isola.run", Kind: "RootfsSnapshot"},
						"test",
						field.ErrorList{field.Invalid(field.NewPath("spec"), nil, "invalid spec")},
					)
				},
			})

			resp := api.Post("/sandboxes/test-sandbox/rootfssnapshots",
				strings.NewReader(`{"snapshotName":"test"}`))
			Expect(resp.Code).To(Equal(422))
		})

		It("returns 403 when k8s Create returns Forbidden", func() {
			api := newErrorTestAPI(interceptor.Funcs{
				Create: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.CreateOption) error {
					return apierrors.NewForbidden(schema.GroupResource{Group: "sandbox.isola.run", Resource: "rootfssnapshots"}, "test", fmt.Errorf("not allowed"))
				},
			})

			resp := api.Post("/sandboxes/test-sandbox/rootfssnapshots",
				strings.NewReader(`{"snapshotName":"test"}`))
			Expect(resp.Code).To(Equal(403))
		})

		It("returns 429 with Retry-After when k8s Create returns TooManyRequests", func() {
			api := newErrorTestAPI(interceptor.Funcs{
				Create: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.CreateOption) error {
					return apierrors.NewTooManyRequests("rate limit exceeded", 30)
				},
			})

			resp := api.Post("/sandboxes/test-sandbox/rootfssnapshots",
				strings.NewReader(`{"snapshotName":"test"}`))
			Expect(resp.Code).To(Equal(429))
			Expect(resp.Header().Get("Retry-After")).To(Equal("30"))
		})

		It("includes Retry-After when k8s Create returns ServerTimeout", func() {
			api := newErrorTestAPI(interceptor.Funcs{
				Create: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.CreateOption) error {
					return apierrors.NewServerTimeout(
						schema.GroupResource{Group: "sandbox.isola.run", Resource: "rootfssnapshots"},
						"create", 10,
					)
				},
			})

			resp := api.Post("/sandboxes/test-sandbox/rootfssnapshots",
				strings.NewReader(`{"snapshotName":"test"}`))
			Expect(resp.Code).To(Equal(500))
			Expect(resp.Header().Get("Retry-After")).To(Equal("10"))
		})

		It("returns 503 when k8s Create returns ServiceUnavailable", func() {
			api := newErrorTestAPI(interceptor.Funcs{
				Create: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.CreateOption) error {
					return apierrors.NewServiceUnavailable("apiserver overloaded")
				},
			})

			resp := api.Post("/sandboxes/test-sandbox/rootfssnapshots",
				strings.NewReader(`{"snapshotName":"test"}`))
			Expect(resp.Code).To(Equal(503))
		})

		It("returns 500 when k8s Create returns a non-status error", func() {
			api := newErrorTestAPI(interceptor.Funcs{
				Create: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.CreateOption) error {
					return fmt.Errorf("connection refused")
				},
			})

			resp := api.Post("/sandboxes/test-sandbox/rootfssnapshots",
				strings.NewReader(`{"snapshotName":"test"}`))
			Expect(resp.Code).To(Equal(500))
		})
	})

	Describe("GET /sandboxes/{id}/rootfssnapshots/{snapId}", func() {
		It("returns 403 when k8s Get returns Forbidden", func() {
			api := newErrorTestAPI(interceptor.Funcs{
				Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
					return apierrors.NewForbidden(schema.GroupResource{Group: "sandbox.isola.run", Resource: "rootfssnapshots"}, "some-id", fmt.Errorf("not allowed"))
				},
			})

			resp := api.Get("/sandboxes/test-sandbox/rootfssnapshots/some-id")
			Expect(resp.Code).To(Equal(403))
		})

		It("returns 429 with Retry-After when k8s Get returns TooManyRequests", func() {
			api := newErrorTestAPI(interceptor.Funcs{
				Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
					return apierrors.NewTooManyRequests("throttled", 60)
				},
			})

			resp := api.Get("/sandboxes/test-sandbox/rootfssnapshots/some-id")
			Expect(resp.Code).To(Equal(429))
			Expect(resp.Header().Get("Retry-After")).To(Equal("60"))
		})

		It("returns 500 when k8s Get returns a non-status error", func() {
			api := newErrorTestAPI(interceptor.Funcs{
				Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
					return fmt.Errorf("etcd unavailable")
				},
			})

			resp := api.Get("/sandboxes/test-sandbox/rootfssnapshots/some-id")
			Expect(resp.Code).To(Equal(500))
		})
	})
})
