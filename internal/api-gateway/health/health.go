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
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	sandboxv1alpha1 "github.com/isola-ai/isola/api/v1alpha1"
)

type HealthResponse struct {
	Status string `json:"status" example:"ok" doc:"Health status"`
}

type HealthOutput struct {
	Body HealthResponse
}

type Handlers struct {
	logger    *slog.Logger
	k8sClient client.Client
}

func New(logger *slog.Logger, k8sClient client.Client) *Handlers {
	return &Handlers{
		logger:    logger,
		k8sClient: k8sClient,
	}
}

func (h *Handlers) GetHealth(ctx context.Context, input *struct{}) (*HealthOutput, error) {
	return &HealthOutput{Body: HealthResponse{Status: "ok"}}, nil
}

func (h *Handlers) GetReady(ctx context.Context, input *struct{}) (*HealthOutput, error) {
	sandboxList := &sandboxv1alpha1.SandboxList{}
	if err := h.k8sClient.List(ctx, sandboxList, client.Limit(1)); err != nil {
		h.logger.Error("readiness check failed", "error", err)
		return nil, huma.Error503ServiceUnavailable("not ready")
	}

	return &HealthOutput{Body: HealthResponse{Status: "ok"}}, nil
}

func Register(api huma.API, h *Handlers) {
	huma.Register(api, huma.Operation{
		OperationID: "getHealth",
		Method:      http.MethodGet,
		Path:        "/health",
		Summary:     "Health check",
		Description: "Returns the health status of the API",
		Tags:        []string{"health"},
	}, h.GetHealth)

	huma.Register(api, huma.Operation{
		OperationID: "getHealthz",
		Method:      http.MethodGet,
		Path:        "/healthz",
		Summary:     "Health check (alias)",
		Description: "Returns the health status of the API",
		Tags:        []string{"health"},
	}, h.GetHealth)

	huma.Register(api, huma.Operation{
		OperationID:   "getReady",
		Method:        http.MethodGet,
		Path:          "/ready",
		Summary:       "Readiness check",
		Description:   "Returns the readiness status of the API",
		Tags:          []string{"health"},
		DefaultStatus: http.StatusOK,
		Errors:        []int{http.StatusServiceUnavailable},
	}, h.GetReady)

	huma.Register(api, huma.Operation{
		OperationID:   "getReadyz",
		Method:        http.MethodGet,
		Path:          "/readyz",
		Summary:       "Readiness check (alias)",
		Description:   "Returns the readiness status of the API",
		Tags:          []string{"health"},
		DefaultStatus: http.StatusOK,
		Errors:        []int{http.StatusServiceUnavailable},
	}, h.GetReady)
}
