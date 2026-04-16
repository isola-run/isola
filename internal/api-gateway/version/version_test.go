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

	"github.com/isola-run/isola/internal/api-gateway/version"
)

func TestGetVersion(t *testing.T) {
	_, api := humatest.New(t, huma.DefaultConfig("Test API", "0.1.0"))
	version.Register(api, version.New("1.2.3"))

	resp := api.Get("/version")
	if resp.Code != 200 {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}

	var got version.VersionResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Version != "1.2.3" {
		t.Errorf("version: got %q, want %q", got.Version, "1.2.3")
	}
}
