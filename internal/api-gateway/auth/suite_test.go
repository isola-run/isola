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
	"context"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// validKeys are the keys the test authenticator accepts. Two keys exercise the
// multi-key acceptance and constant-time OR-accumulation paths.
var validKeys = []string{"key-one-secret", "key-two-secret"}

var testAPI humatest.TestAPI

type okOutput struct {
	Body struct {
		OK bool `json:"ok"`
	}
}

func okHandler(context.Context, *struct{}) (*okOutput, error) {
	out := &okOutput{}
	out.Body.OK = true
	return out, nil
}

func TestAuth(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "API Gateway Auth Suite")
}

var _ = BeforeSuite(func() {
	// Mirror the gateway's production config: a global bearer requirement plus the
	// auth middleware. Public operations opt out via PublicSecurity().
	config := huma.DefaultConfig("Auth Test API", "0.1.0")
	config.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"bearerAuth": {Type: "http", Scheme: "bearer", BearerFormat: "opaque"},
	}
	config.Security = []map[string][]string{{"bearerAuth": {}}}

	// humatest.TestAPI embeds huma.API, so it is used directly for middleware
	// registration and operation registration.
	_, testAPI = humatest.New(GinkgoT(), config)

	authenticator, err := NewStaticKeyAuthenticator(validKeys)
	Expect(err).NotTo(HaveOccurred())
	testAPI.UseMiddleware(Middleware(testAPI, authenticator))

	// A secured operation (inherits the global requirement) and a public one.
	huma.Register(testAPI, huma.Operation{
		OperationID: "securedGet",
		Method:      http.MethodGet,
		Path:        "/secured",
	}, okHandler)
	huma.Register(testAPI, huma.Operation{
		OperationID: "publicGet",
		Method:      http.MethodGet,
		Path:        "/public",
		Security:    PublicSecurity(),
	}, okHandler)
})
