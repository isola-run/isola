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

package apigateway

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	sandboxv1alpha1 "github.com/isola-ai/isola/api/v1alpha1"
)

// statusError matches the huma.StatusError interface for type-assertion in tests.
type statusError interface {
	GetStatus() int
}

// headerCarrier matches the interface returned by huma.ErrorWithHeaders.
type headerCarrier interface {
	GetHeaders() http.Header
}

var discardLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

func readySandbox(name, namespace, podIP string) *sandboxv1alpha1.Sandbox {
	return &sandboxv1alpha1.Sandbox{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
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
			PodIP: podIP,
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
}

func newFakeClient(objs ...client.Object) client.Client {
	s := scheme.Scheme
	Expect(sandboxv1alpha1.AddToScheme(s)).To(Succeed())
	builder := fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(&sandboxv1alpha1.Sandbox{})
	if len(objs) > 0 {
		builder = builder.WithObjects(objs...)
	}
	return builder.Build()
}

// expectStatus asserts that err (or a wrapped inner error) implements GetStatus() and returns the expected code.
func expectStatus(err error, code int) {
	GinkgoHelper()
	var se statusError
	Expect(errors.As(err, &se)).To(BeTrue(), "expected error chain to include GetStatus()")
	Expect(se.GetStatus()).To(Equal(code))
}

var _ = Describe("GetReadySandbox", func() {
	const (
		namespace = "test-ns"
		id        = "sb-1"
	)

	It("returns the sandbox when it is ready with a PodIP", func() {
		sb := readySandbox(id, namespace, "10.0.0.1")
		k8s := newFakeClient(sb)

		// Status subresource must be updated separately with the fake client
		Expect(k8s.Status().Update(context.Background(), sb)).To(Succeed())

		result, err := GetReadySandbox(context.Background(), k8s, namespace, id, discardLogger)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).NotTo(BeNil())
		Expect(result.Name).To(Equal(id))
		Expect(result.Status.PodIP).To(Equal("10.0.0.1"))
	})

	It("returns 404 when the sandbox does not exist", func() {
		k8s := newFakeClient()

		result, err := GetReadySandbox(context.Background(), k8s, namespace, "nonexistent", discardLogger)
		Expect(result).To(BeNil())
		Expect(err).To(HaveOccurred())
		expectStatus(err, 404)
	})

	It("returns K8s error when Get fails with a non-NotFound error", func() {
		s := scheme.Scheme
		Expect(sandboxv1alpha1.AddToScheme(s)).To(Succeed())
		k8s := interceptor.NewClient(
			fake.NewClientBuilder().WithScheme(s).Build(),
			interceptor.Funcs{
				Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
					return apierrors.NewForbidden(
						schema.GroupResource{Group: "sandbox.isola.run", Resource: "sandboxes"},
						id, fmt.Errorf("not allowed"),
					)
				},
			},
		)

		result, err := GetReadySandbox(context.Background(), k8s, namespace, id, discardLogger)
		Expect(result).To(BeNil())
		Expect(err).To(HaveOccurred())
		expectStatus(err, 403)
	})

	It("returns 409 when the sandbox is not in running status", func() {
		sb := readySandbox(id, namespace, "10.0.0.1")
		sb.Status.Conditions = []metav1.Condition{
			{
				Type:               "Ready",
				Status:             metav1.ConditionFalse,
				Reason:             "PodPending",
				LastTransitionTime: metav1.Now(),
			},
		}
		k8s := newFakeClient(sb)
		Expect(k8s.Status().Update(context.Background(), sb)).To(Succeed())

		result, err := GetReadySandbox(context.Background(), k8s, namespace, id, discardLogger)
		Expect(result).To(BeNil())
		Expect(err).To(HaveOccurred())
		expectStatus(err, 409)
	})

	It("returns 409 when the sandbox is running but has no PodIP", func() {
		sb := readySandbox(id, namespace, "")
		k8s := newFakeClient(sb)
		Expect(k8s.Status().Update(context.Background(), sb)).To(Succeed())

		result, err := GetReadySandbox(context.Background(), k8s, namespace, id, discardLogger)
		Expect(result).To(BeNil())
		Expect(err).To(HaveOccurred())
		expectStatus(err, 409)
	})

	It("returns 500 when Get fails with a non-status generic error", func() {
		s := scheme.Scheme
		Expect(sandboxv1alpha1.AddToScheme(s)).To(Succeed())
		k8s := interceptor.NewClient(
			fake.NewClientBuilder().WithScheme(s).Build(),
			interceptor.Funcs{
				Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
					return fmt.Errorf("etcd unavailable")
				},
			},
		)

		result, err := GetReadySandbox(context.Background(), k8s, namespace, id, discardLogger)
		Expect(result).To(BeNil())
		Expect(err).To(HaveOccurred())
		expectStatus(err, 500)
	})
})

var _ = Describe("K8sErrorToHuma", func() {
	It("returns 404 for NotFound K8s error", func() {
		k8sErr := apierrors.NewNotFound(
			schema.GroupResource{Group: "sandbox.isola.run", Resource: "sandboxes"}, "sb-1",
		)
		err := K8sErrorToHuma(k8sErr, "fallback")
		Expect(err).To(HaveOccurred())
		expectStatus(err, 404)
	})

	It("returns 409 for Conflict K8s error", func() {
		k8sErr := apierrors.NewConflict(
			schema.GroupResource{Group: "sandbox.isola.run", Resource: "sandboxes"},
			"sb-1", fmt.Errorf("conflict"),
		)
		err := K8sErrorToHuma(k8sErr, "fallback")
		Expect(err).To(HaveOccurred())
		expectStatus(err, 409)
	})

	It("returns 500 for InternalError K8s error", func() {
		k8sErr := apierrors.NewInternalError(fmt.Errorf("etcd down"))
		err := K8sErrorToHuma(k8sErr, "fallback")
		Expect(err).To(HaveOccurred())
		expectStatus(err, 500)
	})

	It("includes Retry-After header for TooManyRequests", func() {
		k8sErr := apierrors.NewTooManyRequests("rate limited", 42)
		err := K8sErrorToHuma(k8sErr, "fallback")
		Expect(err).To(HaveOccurred())
		expectStatus(err, 429)

		var headerErr headerCarrier
		Expect(errors.As(err, &headerErr)).To(BeTrue(), "expected error chain to carry headers")
		Expect(headerErr.GetHeaders().Get("Retry-After")).To(Equal("42"))
	})

	It("includes Retry-After header for ServerTimeout", func() {
		k8sErr := apierrors.NewServerTimeout(
			schema.GroupResource{Group: "sandbox.isola.run", Resource: "sandboxes"},
			"get", 15,
		)
		err := K8sErrorToHuma(k8sErr, "fallback")
		Expect(err).To(HaveOccurred())
		expectStatus(err, 500)

		var headerErr headerCarrier
		Expect(errors.As(err, &headerErr)).To(BeTrue(), "expected error chain to carry headers")
		Expect(headerErr.GetHeaders().Get("Retry-After")).To(Equal("15"))
	})

	It("returns 500 with fallback message for non-StatusError", func() {
		err := K8sErrorToHuma(fmt.Errorf("connection refused"), "something went wrong")
		Expect(err).To(HaveOccurred())
		expectStatus(err, 500)
	})

	It("unwraps wrapped StatusError via errors.As", func() {
		inner := apierrors.NewForbidden(
			schema.GroupResource{Group: "sandbox.isola.run", Resource: "sandboxes"},
			"sb-1", fmt.Errorf("not allowed"),
		)
		wrapped := fmt.Errorf("outer: %w", inner)
		err := K8sErrorToHuma(wrapped, "fallback")
		Expect(err).To(HaveOccurred())
		expectStatus(err, 403)
	})
})

var _ = Describe("BodyStream.Resolve", func() {
	It("populates Stream from the request body", func() {
		_, api := humatest.New(GinkgoT(), huma.DefaultConfig("Test API", "0.1.0"))

		var captured BodyStream
		huma.Register(api, huma.Operation{
			OperationID: "test-body-stream",
			Method:      "POST",
			Path:        "/test-body",
		}, func(_ context.Context, input *struct {
			BodyStream
		}) (*struct{}, error) {
			captured = input.BodyStream
			return nil, nil
		})

		api.Post("/test-body", strings.NewReader("hello world"))

		Expect(captured.Stream).NotTo(BeNil())
		data, readErr := io.ReadAll(captured.Stream)
		Expect(readErr).NotTo(HaveOccurred())
		Expect(string(data)).To(Equal("hello world"))
	})
})
