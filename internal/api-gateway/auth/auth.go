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

// Package auth provides the api-gateway's request authentication: a single
// Authenticator seam plus the Huma middleware that enforces it. The static API
// key implementation lives in static.go; future schemes (k8s TokenReview, OIDC)
// can be added behind the same interface without touching the middleware, the
// SDKs, or the OpenAPI contract.
package auth

import (
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

// The header and scheme used to present credentials. These live in the gateway's
// auth package, not internal/constants, because they are an api-gateway HTTP edge
// concern and not part of the sidecar wire contract.
const (
	authHeader   = "Authorization"
	bearerPrefix = "Bearer "

	// securitySchemeName is the OpenAPI security scheme key referenced by the
	// global requirement and by PublicSecurity's override.
	securitySchemeName = "bearerAuth"
)

// ApplySecurityScheme declares the API-wide bearer auth security scheme and makes
// it the default requirement for every operation; public operations opt out with
// PublicSecurity(). Both the running gateway and the OpenAPI generator call this so
// the documented contract and the enforced contract cannot drift. Note that Huma
// only documents this — runtime enforcement is the job of Middleware.
func ApplySecurityScheme(config *huma.Config) {
	config.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		securitySchemeName: {Type: "http", Scheme: "bearer", BearerFormat: "opaque"},
	}
	config.Security = []map[string][]string{{securitySchemeName: {}}}
}

// Authenticator validates the credentials on an incoming request. It is the single
// seam through which future schemes (k8s TokenReview, OIDC) can be added without
// touching the middleware, the SDKs, or the OpenAPI contract. It returns nil when
// the request is authenticated and ErrUnauthenticated for missing or invalid
// credentials.
type Authenticator interface {
	Authenticate(ctx huma.Context) error
}

// ErrUnauthenticated indicates missing or invalid credentials. The middleware maps
// it to a 401 with an identical body regardless of the specific cause, to avoid a
// missing-vs-wrong-credential oracle.
var ErrUnauthenticated = errors.New("missing or invalid credentials")

// Middleware enforces authentication for every operation that is not public. An
// operation is public when it declares an empty OpenAPI security requirement
// (Security: []map[string][]string{{}}); all other operations leave Security nil,
// inherit the API-wide requirement, and are enforced. New operations are therefore
// secured by default — opting one out is an explicit, in-spec act.
func Middleware(api huma.API, a Authenticator) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		if isPublic(ctx.Operation()) {
			next(ctx)
			return
		}
		if err := a.Authenticate(ctx); err != nil {
			ctx.SetHeader("WWW-Authenticate", `Bearer realm="isola"`)
			_ = huma.WriteErr(api, ctx, http.StatusUnauthorized, ErrUnauthenticated.Error())
			return // short-circuit: do not call next
		}
		next(ctx)
	}
}

// PublicSecurity is the OpenAPI security requirement that marks an operation as
// public: a single empty requirement that overrides the API-wide bearer
// requirement, both in the generated spec and at runtime (Middleware recognizes it
// via isPublic and skips authentication). A fresh slice is returned each call so
// callers never share mutable state. Used by the health and version operations.
func PublicSecurity() []map[string][]string {
	return []map[string][]string{{}}
}

// isPublic reports whether an operation opts out of authentication by declaring an
// empty security requirement.
func isPublic(op *huma.Operation) bool {
	for _, requirement := range op.Security {
		if len(requirement) == 0 {
			return true
		}
	}
	return false
}
