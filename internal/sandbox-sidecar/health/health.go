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
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

type HealthResponse struct {
	Status string `json:"status" example:"ok" doc:"Health status"`
}

type HealthOutput struct {
	Body HealthResponse
}

type Handlers struct{}

func New() *Handlers {
	return &Handlers{}
}

func (h *Handlers) GetHealth(ctx context.Context, input *struct{}) (*HealthOutput, error) {
	return &HealthOutput{Body: HealthResponse{Status: "ok"}}, nil
}

func Register(api huma.API, h *Handlers) {
	huma.Register(api, huma.Operation{
		OperationID: "getHealth",
		Method:      http.MethodGet,
		Path:        "/health",
		Summary:     "Health check",
		Description: "Returns the health status of the sidecar",
		Tags:        []string{"health"},
	}, h.GetHealth)

	huma.Register(api, huma.Operation{
		OperationID: "getHealthz",
		Method:      http.MethodGet,
		Path:        "/healthz",
		Summary:     "Health check (alias)",
		Description: "Returns the health status of the sidecar",
		Tags:        []string{"health"},
	}, h.GetHealth)
}
