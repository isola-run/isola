package handlers

import (
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Health Endpoints", func() {
	DescribeTable("health check endpoints",
		func(path string) {
			resp := testAPI.Get(path)

			Expect(resp.Code).To(Equal(200))
			Expect(resp.Header().Get("Content-Type")).To(ContainSubstring("application/json"))

			var health HealthResponse
			Expect(json.NewDecoder(resp.Body).Decode(&health)).To(Succeed())
			Expect(health.Status).To(Equal("ok"))
		},
		Entry("liveness probe (/health)", "/health"),
		Entry("readiness probe (/ready)", "/ready"),
	)
})

var _ = Describe("Unknown Endpoints", func() {
	It("returns 404 for unregistered paths", func() {
		resp := testAPI.Get("/nonexistent")

		Expect(resp.Code).To(Equal(404))
	})
})
