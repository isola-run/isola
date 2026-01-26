package handlers

import (
	"encoding/json"
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Health", func() {
	It("returns 200 with status ok", func() {
		resp := doGet("/health")
		defer func() { _ = resp.Body.Close() }()

		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		Expect(resp.Header.Get("Content-Type")).To(Equal("application/json"))

		var body map[string]string
		err := json.NewDecoder(resp.Body).Decode(&body)
		Expect(err).NotTo(HaveOccurred())
		Expect(body).To(HaveKeyWithValue("status", "ok"))
	})
})
