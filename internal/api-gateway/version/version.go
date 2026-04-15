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
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	k8sversion "k8s.io/apimachinery/pkg/version"
)

type GatewayInfo struct {
	Version string `json:"version" example:"0.1.0" doc:"API gateway version"`
}

type KubernetesInfo struct {
	GitVersion string `json:"gitVersion" example:"v1.34.0" doc:"Kubernetes git version reported by the apiserver"`
	Major      string `json:"major" example:"1" doc:"Major Kubernetes version"`
	Minor      string `json:"minor" example:"34" doc:"Minor Kubernetes version"`
	Platform   string `json:"platform" example:"linux/amd64" doc:"Kubernetes apiserver platform"`
}

type VersionResponse struct {
	Gateway    GatewayInfo    `json:"gateway"`
	Kubernetes KubernetesInfo `json:"kubernetes"`
}

type VersionOutput struct {
	Body VersionResponse
}

// ServerVersionGetter is the subset of k8s.io/client-go/discovery.DiscoveryInterface
// that this handler needs. It is satisfied by *discovery.DiscoveryClient.
type ServerVersionGetter interface {
	ServerVersion() (*k8sversion.Info, error)
}

// Handlers serves the gateway's own version and the Kubernetes apiserver
// version. The apiserver version is fetched from the discovery API on every
// request, so a control-plane upgrade is reflected immediately; the trade-off
// is that /version returns 503 if the apiserver is unreachable.
type Handlers struct {
	logger         *slog.Logger
	gatewayVersion string
	discovery      ServerVersionGetter
}

func New(logger *slog.Logger, gatewayVersion string, discovery ServerVersionGetter) *Handlers {
	return &Handlers{
		logger:         logger,
		gatewayVersion: gatewayVersion,
		discovery:      discovery,
	}
}

func (h *Handlers) GetVersion(ctx context.Context, input *struct{}) (*VersionOutput, error) {
	info, err := h.discovery.ServerVersion()
	if err != nil {
		h.logger.Error("failed to discover kubernetes server version", "error", err)
		return nil, huma.Error503ServiceUnavailable("kubernetes apiserver unavailable")
	}

	return &VersionOutput{
		Body: VersionResponse{
			Gateway: GatewayInfo{Version: h.gatewayVersion},
			Kubernetes: KubernetesInfo{
				GitVersion: info.GitVersion,
				Major:      info.Major,
				Minor:      info.Minor,
				Platform:   info.Platform,
			},
		},
	}, nil
}

func Register(api huma.API, h *Handlers) {
	huma.Register(api, huma.Operation{
		OperationID:   "getVersion",
		Method:        http.MethodGet,
		Path:          "/version",
		Summary:       "Version info",
		Description:   "Returns the API gateway version and the Kubernetes apiserver version (queried on each request).",
		Tags:          []string{"version"},
		DefaultStatus: http.StatusOK,
		Errors:        []int{http.StatusServiceUnavailable},
	}, h.GetVersion)
}
