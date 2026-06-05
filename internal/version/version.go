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

// Package version exposes build-time version metadata, populated via
// -ldflags -X at build time. Shape follows the Kubernetes / cluster-api
// canonical Info struct.
package version

import (
	"fmt"
	"runtime"
)

// Populated via -ldflags "-X github.com/isola-run/isola/internal/version.gitVersion=..."
var (
	gitVersion   = "dev"
	gitCommit    = ""
	buildDate    = ""
	gitTreeState = "unknown"
)

type Info struct {
	GitVersion   string `json:"gitVersion"             example:"0.1.0"`
	GitCommit    string `json:"gitCommit,omitempty"    example:"abc1234"`
	GitTreeState string `json:"gitTreeState,omitempty" example:"clean"`
	BuildDate    string `json:"buildDate,omitempty"    example:"2026-04-17T12:00:00Z"`
	GoVersion    string `json:"goVersion"              example:"go1.26.4"`
	Platform     string `json:"platform"               example:"linux/amd64"`
}

func Get() Info {
	return Info{
		GitVersion:   gitVersion,
		GitCommit:    gitCommit,
		GitTreeState: gitTreeState,
		BuildDate:    buildDate,
		GoVersion:    runtime.Version(),
		Platform:     fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
	}
}
