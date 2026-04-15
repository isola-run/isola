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

package version_test

import (
	"encoding/json"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	k8sversion "k8s.io/apimachinery/pkg/version"

	"github.com/isola-run/isola/internal/api-gateway/version"
)

func TestGetVersion(t *testing.T) {
	_, api := humatest.New(t, huma.DefaultConfig("Test API", "0.1.0"))
	version.Register(api, version.New("1.2.3", &k8sversion.Info{
		GitVersion: "v1.34.0",
		Major:      "1",
		Minor:      "34",
		Platform:   "linux/amd64",
	}))

	resp := api.Get("/version")
	if resp.Code != 200 {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}

	var got version.VersionResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if got.Gateway.Version != "1.2.3" {
		t.Errorf("gateway version: got %q, want %q", got.Gateway.Version, "1.2.3")
	}
	if got.Kubernetes.GitVersion != "v1.34.0" {
		t.Errorf("k8s gitVersion: got %q, want %q", got.Kubernetes.GitVersion, "v1.34.0")
	}
	if got.Kubernetes.Major != "1" || got.Kubernetes.Minor != "34" {
		t.Errorf("k8s major/minor: got %q/%q, want 1/34", got.Kubernetes.Major, got.Kubernetes.Minor)
	}
	if got.Kubernetes.Platform != "linux/amd64" {
		t.Errorf("k8s platform: got %q, want %q", got.Kubernetes.Platform, "linux/amd64")
	}
}
