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

package httputil

import (
	"context"
	"errors"
	"fmt"
	"io"
	"syscall"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("IsClientDisconnect", func() {
	DescribeTable("classifies stream errors",
		func(err error, expected bool) {
			Expect(IsClientDisconnect(err)).To(Equal(expected))
		},
		Entry("EPIPE", syscall.EPIPE, true),
		Entry("ECONNRESET", syscall.ECONNRESET, true),
		Entry("context canceled", context.Canceled, true),
		Entry("wrapped EPIPE", fmt.Errorf("write tcp: %w", syscall.EPIPE), true),
		Entry("unexpected EOF", io.ErrUnexpectedEOF, false),
		Entry("deadline exceeded", context.DeadlineExceeded, false),
		Entry("arbitrary error", errors.New("boom"), false),
		Entry("nil", nil, false),
	)
})
