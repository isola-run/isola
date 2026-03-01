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

package health

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
