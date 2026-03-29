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

// openapi-gen generates OpenAPI specs for the HTTP services without starting servers.
// Usage:
//
//	go run ./cmd/openapi-gen -service api-gateway > api/openapi/api-gateway.yaml
//	go run ./cmd/openapi-gen -service sandbox-sidecar > api/openapi/sandbox-sidecar.yaml
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	"github.com/isola-run/isola/internal/api-gateway/command"
	"github.com/isola-run/isola/internal/api-gateway/filesystem"
	"github.com/isola-run/isola/internal/api-gateway/health"
	"github.com/isola-run/isola/internal/api-gateway/rootfssnapshot"
	"github.com/isola-run/isola/internal/api-gateway/sandbox"
	sidecarCmd "github.com/isola-run/isola/internal/sandbox-sidecar/command"
	sidecarFs "github.com/isola-run/isola/internal/sandbox-sidecar/filesystem"
	sidecarHealth "github.com/isola-run/isola/internal/sandbox-sidecar/health"
)

func main() {
	service := flag.String("service", "", "Service to generate spec for: api-gateway or sandbox-sidecar")
	format := flag.String("format", "yaml", "Output format: yaml or json")
	flag.Parse()

	if *service == "" {
		fmt.Fprintln(os.Stderr, "error: -service is required")
		flag.Usage()
		os.Exit(1)
	}

	var api huma.API

	switch *service {
	case "api-gateway":
		api = setupAPIGateway()
	case "sandbox-sidecar":
		api = setupSandboxSidecar()
	default:
		fmt.Fprintf(os.Stderr, "error: unknown service %q\n", *service)
		os.Exit(1)
	}

	var out []byte
	var err error
	if *format == "json" {
		out, err = api.OpenAPI().MarshalJSON()
	} else {
		out, err = api.OpenAPI().YAML()
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	fmt.Print(string(out))
}

func setupAPIGateway() huma.API {
	r := chi.NewRouter()
	config := huma.DefaultConfig("Isola Sandbox API", "0.1.0")
	config.Info.Description = "API for managing sandboxes"
	api := humachi.New(r, config)

	// nil dependencies - handlers won't be called, only their signatures are inspected
	health.Register(api, health.New(nil, nil))

	v1 := huma.NewGroup(api, "/v1")
	sandbox.Register(v1, sandbox.New(nil, "", nil))
	rootfssnapshot.Register(v1, rootfssnapshot.New(nil, "", nil))
	filesystem.Register(v1, filesystem.New(nil, "", nil, nil))
	command.Register(v1, command.New(nil, "", nil, nil))

	return api
}

func setupSandboxSidecar() huma.API {
	r := chi.NewRouter()
	config := huma.DefaultConfig("Isola Sandbox Sidecar API", "0.1.0")
	config.Info.Description = "Internal API for sandbox filesystem operations"
	api := humachi.New(r, config)

	// nil dependencies - handlers won't be called, only their signatures are inspected
	sidecarHealth.Register(api, sidecarHealth.New())

	v1 := huma.NewGroup(api, "/v1")
	sidecarFs.Register(v1, sidecarFs.New(nil, nil, nil))
	sidecarCmd.Register(v1, sidecarCmd.New(nil, nil, nil, nil))

	return api
}
