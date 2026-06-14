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
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"strings"

	"github.com/danielgtaylor/huma/v2"
)

// staticKeyAuthenticator accepts a fixed set of bearer keys supplied out of band
// (a k8s Secret). It compares fixed-width SHA-256 digests in constant time, OR-ing
// the result across the whole set with no early return, so neither the presented
// key's length, which key matched, nor the set size leaks via timing. Only the
// digests are retained in memory, shrinking the lifetime of plaintext key material.
type staticKeyAuthenticator struct {
	digests [][sha256.Size]byte
}

// NewStaticKeyAuthenticator builds an Authenticator from one or more raw keys. It
// errors if no keys are given; callers that want auth disabled simply do not
// install the middleware.
func NewStaticKeyAuthenticator(keys []string) (Authenticator, error) {
	if len(keys) == 0 {
		return nil, errors.New("at least one API key is required")
	}
	digests := make([][sha256.Size]byte, len(keys))
	for i, key := range keys {
		digests[i] = sha256.Sum256([]byte(key))
	}
	return &staticKeyAuthenticator{digests: digests}, nil
}

func (s *staticKeyAuthenticator) Authenticate(ctx huma.Context) error {
	header := ctx.Header(authHeader)
	if !strings.HasPrefix(header, bearerPrefix) {
		// Exiting before the constant-time loop leaks nothing: a malformed/missing
		// header and a wrong key both yield ErrUnauthenticated, which the middleware
		// renders as an identical 401, so timing reveals no missing-vs-wrong signal.
		return ErrUnauthenticated
	}
	presented := sha256.Sum256([]byte(strings.TrimPrefix(header, bearerPrefix)))

	// OR the comparisons across every key with no early return: the work is
	// independent of which key matches or how many keys are configured.
	var matched int
	for i := range s.digests {
		matched |= subtle.ConstantTimeCompare(presented[:], s.digests[i][:])
	}
	if matched != 1 {
		return ErrUnauthenticated
	}
	return nil
}

// ParseKeys splits a raw configuration value (comma- and/or newline-separated)
// into a clean key list, trimming whitespace and dropping empties. It returns nil
// when no keys are present, which callers treat as "authentication disabled".
func ParseKeys(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r'
	})
	keys := make([]string, 0, len(fields))
	for _, field := range fields {
		if key := strings.TrimSpace(field); key != "" {
			keys = append(keys, key)
		}
	}
	if len(keys) == 0 {
		return nil
	}
	return keys
}
