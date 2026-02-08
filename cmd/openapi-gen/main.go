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

	apigateway "github.com/isola-ai/isola-sb/internal/api-gateway/handlers"
	sidecar "github.com/isola-ai/isola-sb/internal/sandbox-sidecar/handlers"
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
	config := huma.DefaultConfig("Isola Sandbox API", "1.0.0")
	config.Info.Description = "API for managing sandboxes"
	api := humachi.New(r, config)

	// nil dependencies - handlers won't be called, only their signatures are inspected
	healthHandlers := apigateway.NewHealthHandlers(nil, nil)
	execHandlers := apigateway.NewExecHandlers(nil, nil, "")

	apigateway.RegisterHealthRoutes(api, healthHandlers)
	apigateway.RegisterExecRoutes(api, execHandlers)

	sandboxHandlers := apigateway.NewSandboxHandlers(nil, "", nil)
	apigateway.RegisterSandboxRoutes(api, sandboxHandlers)

	return api
}

func setupSandboxSidecar() huma.API {
	r := chi.NewRouter()
	config := huma.DefaultConfig("Isola Sandbox Sidecar API", "1.0.0")
	config.Info.Description = "Internal API for sandbox filesystem operations"
	api := humachi.New(r, config)

	// nil dependencies - handlers won't be called, only their signatures are inspected
	healthHandlers := sidecar.NewHealthHandlers()
	filesystemHandlers := sidecar.NewFilesystemHandlers(nil, nil, nil)
	execHandlers := sidecar.NewExecHandlers(nil, nil, nil)

	sidecar.RegisterHealthRoutes(api, healthHandlers)
	sidecar.RegisterFilesystemRoutes(api, filesystemHandlers)
	sidecar.RegisterExecRoutes(api, execHandlers)

	return api
}
