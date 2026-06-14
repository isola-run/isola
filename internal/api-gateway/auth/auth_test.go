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

package auth

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Middleware", func() {
	Describe("public operations", func() {
		It("serves a public operation without any credentials", func() {
			resp := testAPI.Get("/public")
			Expect(resp.Code).To(Equal(200))
		})

		It("serves a public operation even when credentials are present", func() {
			resp := testAPI.Get("/public", "Authorization: Bearer "+validKeys[0])
			Expect(resp.Code).To(Equal(200))
		})
	})

	Describe("secured operations", func() {
		It("accepts a valid key", func() {
			resp := testAPI.Get("/secured", "Authorization: Bearer "+validKeys[0])
			Expect(resp.Code).To(Equal(200))
		})

		It("accepts any configured key (multi-key)", func() {
			resp := testAPI.Get("/secured", "Authorization: Bearer "+validKeys[1])
			Expect(resp.Code).To(Equal(200))
		})

		It("rejects a missing Authorization header with 401", func() {
			resp := testAPI.Get("/secured")
			Expect(resp.Code).To(Equal(401))
			Expect(resp.Header().Get("WWW-Authenticate")).To(Equal(`Bearer realm="isola"`))
		})

		It("rejects a wrong key with 401", func() {
			resp := testAPI.Get("/secured", "Authorization: Bearer not-a-valid-key")
			Expect(resp.Code).To(Equal(401))
			Expect(resp.Header().Get("WWW-Authenticate")).To(Equal(`Bearer realm="isola"`))
		})

		It("rejects a non-Bearer scheme with 401", func() {
			resp := testAPI.Get("/secured", "Authorization: Basic dXNlcjpwYXNz")
			Expect(resp.Code).To(Equal(401))
		})

		It("returns an identical body for missing vs wrong credentials (no oracle)", func() {
			missing := testAPI.Get("/secured")
			wrong := testAPI.Get("/secured", "Authorization: Bearer not-a-valid-key")
			Expect(missing.Code).To(Equal(401))
			Expect(wrong.Code).To(Equal(401))
			Expect(wrong.Body.String()).To(Equal(missing.Body.String()))
		})
	})
})
