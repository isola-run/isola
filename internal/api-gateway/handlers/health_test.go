package handlers

import (
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/isola-ai/isola-sb/internal/api-gateway/models"
)

var _ = Describe("Health Endpoints", func() {
	DescribeTable("health check endpoints",
		func(path string) {
			resp := doGet(path)
			DeferCleanup(resp.Body.Close)

			Expect(resp).To(HaveHTTPStatus(200))
			Expect(resp).To(HaveHTTPHeaderWithValue("Content-Type", "application/json; charset=utf-8"))

			var health models.HealthResponse
			Expect(json.NewDecoder(resp.Body).Decode(&health)).To(Succeed())
			Expect(health.Status).To(Equal("ok"))
		},
		Entry("liveness probe (/health)", "/api/v1/health"),
		Entry("readiness probe (/ready)", "/api/v1/ready"),
	)

})

var _ = Describe("Unknown Endpoints", func() {
	It("returns 404 for unregistered paths", func() {
		resp := doGet("/api/v1/nonexistent")
		DeferCleanup(resp.Body.Close)

		Expect(resp).To(HaveHTTPStatus(404))
	})
})
