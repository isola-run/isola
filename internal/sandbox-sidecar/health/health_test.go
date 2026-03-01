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
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Health", func() {
	It("returns 200 with status ok", func() {
		resp := testAPI.Get("/health")

		Expect(resp.Code).To(Equal(http.StatusOK))
		Expect(resp.Header().Get("Content-Type")).To(ContainSubstring("application/json"))

		var body map[string]string
		err := json.NewDecoder(resp.Body).Decode(&body)
		Expect(err).NotTo(HaveOccurred())
		Expect(body).To(HaveKeyWithValue("status", "ok"))
	})
})
