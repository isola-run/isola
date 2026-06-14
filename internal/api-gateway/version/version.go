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

package version

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/isola-run/isola/internal/api-gateway/auth"
	internalversion "github.com/isola-run/isola/internal/version"
)

type VersionOutput struct {
	Body internalversion.Info
}

type Handlers struct{}

func New() *Handlers { return &Handlers{} }

func (h *Handlers) GetVersion(ctx context.Context, input *struct{}) (*VersionOutput, error) {
	return &VersionOutput{Body: internalversion.Get()}, nil
}

func Register(api huma.API, h *Handlers) {
	huma.Register(api, huma.Operation{
		OperationID: "getVersion",
		Method:      http.MethodGet,
		Path:        "/version",
		Summary:     "Version info",
		Description: "Returns build-time version and VCS metadata.",
		Tags:        []string{"version"},
		Security:    auth.PublicSecurity(),
	}, h.GetVersion)
}
