package handlers

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func newHealthErrorTestAPI(funcs interceptor.Funcs) humatest.TestAPI {
	baseClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()
	wrappedClient := interceptor.NewClient(baseClient, funcs)
	_, api := humatest.New(GinkgoT(), huma.DefaultConfig("Test API", "1.0.0"))
	h := NewHealthHandlers(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		wrappedClient,
	)
	RegisterHealthRoutes(api, h)
	return api
}

var _ = Describe("Health Error Handling", func() {
	Describe("GET /ready", func() {
		It("returns 503 when k8s List fails", func() {
			api := newHealthErrorTestAPI(interceptor.Funcs{
				List: func(_ context.Context, _ client.WithWatch, _ client.ObjectList, _ ...client.ListOption) error {
					return fmt.Errorf("etcd unavailable")
				},
			})

			resp := api.Get("/ready")
			Expect(resp.Code).To(Equal(503))
		})
	})
})
