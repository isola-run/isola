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

var _ = Describe("ParseKeys", func() {
	DescribeTable("splitting raw configuration values",
		func(raw string, expected []string) {
			Expect(ParseKeys(raw)).To(Equal(expected))
		},
		Entry("empty string yields nil", "", []string(nil)),
		Entry("whitespace-only yields nil", "   \n  ", []string(nil)),
		Entry("single key", "key1", []string{"key1"}),
		Entry("comma-separated", "key1,key2", []string{"key1", "key2"}),
		Entry("newline-separated", "key1\nkey2", []string{"key1", "key2"}),
		Entry("CRLF-separated", "key1\r\nkey2", []string{"key1", "key2"}),
		Entry("trims surrounding whitespace", " key1 , key2 \n key3 ", []string{"key1", "key2", "key3"}),
		Entry("drops empty fields", "key1,,key2,", []string{"key1", "key2"}),
	)
})

var _ = Describe("NewStaticKeyAuthenticator", func() {
	It("errors when no keys are provided", func() {
		_, err := NewStaticKeyAuthenticator(nil)
		Expect(err).To(HaveOccurred())

		_, err = NewStaticKeyAuthenticator([]string{})
		Expect(err).To(HaveOccurred())
	})

	It("builds an authenticator from one or more keys", func() {
		authenticator, err := NewStaticKeyAuthenticator([]string{"a", "b"})
		Expect(err).NotTo(HaveOccurred())
		Expect(authenticator).NotTo(BeNil())
	})
})
