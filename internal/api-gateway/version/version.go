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

// Handlers serves a snapshot of the gateway's own version and the Kubernetes
// apiserver version observed at startup. The Kubernetes version is captured
// once when the process starts; a control-plane upgrade is not reflected until
// the gateway restarts.
type Handlers struct {
	response VersionResponse
}

func New(gatewayVersion string, k8sVersion *k8sversion.Info) *Handlers {
	return &Handlers{
		response: VersionResponse{
			Gateway: GatewayInfo{Version: gatewayVersion},
			Kubernetes: KubernetesInfo{
				GitVersion: k8sVersion.GitVersion,
				Major:      k8sVersion.Major,
				Minor:      k8sVersion.Minor,
				Platform:   k8sVersion.Platform,
			},
		},
	}
}

func (h *Handlers) GetVersion(ctx context.Context, input *struct{}) (*VersionOutput, error) {
	return &VersionOutput{Body: h.response}, nil
}

func Register(api huma.API, h *Handlers) {
	huma.Register(api, huma.Operation{
		OperationID: "getVersion",
		Method:      http.MethodGet,
		Path:        "/version",
		Summary:     "Version info",
		Description: "Returns the API gateway version and the Kubernetes apiserver version observed when the gateway started.",
		Tags:        []string{"version"},
	}, h.GetVersion)
}
