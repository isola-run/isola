# Root Makefile for isola

SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

# Go toolchain version — auto-downloads the correct Go toolchain regardless of
# what's installed locally (requires Go 1.21+). Must match go.mod `go` directive.
GO_VERSION ?= 1.26.2
GOTOOLCHAIN = go$(GO_VERSION)
export GOTOOLCHAIN

##@ General

.PHONY: help
help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: generate
generate: ## Generate CRD DeepCopy methods
	controller-gen object:headerFile="hack/boilerplate.go.txt" paths="./api/..."

.PHONY: manifests
manifests: ## Generate CRD and RBAC manifests directly to Helm chart
	@mkdir -p charts/isola/generated
	controller-gen rbac:roleName=isola-operator crd webhook \
		paths="./api/..." paths="./internal/operator/controller/..." \
		output:crd:artifacts:config=charts/isola/crds \
		output:rbac:artifacts:config=charts/isola/generated

.PHONY: openapi
openapi: ## Generate OpenAPI specs for HTTP services
	@mkdir -p api/openapi
	go run ./cmd/openapi-gen -service api-gateway > api/openapi/api-gateway.yaml
	go run ./cmd/openapi-gen -service sandbox-sidecar > api/openapi/sandbox-sidecar.yaml

.PHONY: check-openapi
check-openapi: openapi ## Verify OpenAPI specs are up-to-date
	@if ! git diff --quiet -- api/openapi/; then \
		echo "ERROR: OpenAPI specs are out of sync"; \
		echo "Run 'make openapi' and commit the changes"; \
		git diff --stat -- api/openapi/; \
		exit 1; \
	fi

.PHONY: fmt
fmt: ## Run golangci-lint fmt
	golangci-lint fmt ./...

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: lint
lint: ## Run golangci-lint
	golangci-lint run

.PHONY: lint-fix
lint-fix: ## Run golangci-lint --fix
	golangci-lint run --fix

.PHONY: vulncheck
vulncheck: ## Run govulncheck
	govulncheck ./...

.PHONY: tidy
tidy: ## Run go mod tidy
	go mod tidy

.PHONY: check-manifests
check-manifests: manifests ## Verify generated manifests are up-to-date
	@if ! git diff --quiet -- charts/isola/crds/ charts/isola/generated/; then \
		echo "ERROR: Generated manifests are out of sync"; \
		echo "Run 'make manifests' and commit the changes"; \
		git diff --stat -- charts/isola/crds/ charts/isola/generated/; \
		exit 1; \
	fi

.PHONY: check-all
check-all: vet lint vulncheck check-openapi check-manifests sdk-python-check-all ## Run all checks (read-only, CI-safe)

.PHONY: fix-all
fix-all: fmt lint-fix sdk-python-fix-all ## Fix all auto-fixable issues

##@ Testing

ENVTEST_K8S_VERSION ?= 1.35
FOCUS ?=
SKIP ?=
GO_TEST_FLAGS ?=

.PHONY: test
test: ## Run all tests
	KUBEBUILDER_ASSETS="$$(setup-envtest use $(ENVTEST_K8S_VERSION) -p path)" \
		go test ./... -coverprofile cover.out $(GO_TEST_FLAGS)

.PHONY: test-verbose
test-verbose: ## Run all tests with verbose output
	KUBEBUILDER_ASSETS="$$(setup-envtest use $(ENVTEST_K8S_VERSION) -p path)" \
		go test ./... -v -coverprofile cover.out $(GO_TEST_FLAGS)

.PHONY: test-operator
test-operator: ## Run operator tests (supports FOCUS=pattern)
	KUBEBUILDER_ASSETS="$$(setup-envtest use $(ENVTEST_K8S_VERSION) -p path)" \
		go test $(if $(FOCUS),./internal/operator/controller,./internal/operator/...) -v \
		$(if $(FOCUS),-ginkgo.focus="$(FOCUS)") $(if $(SKIP),-ginkgo.skip="$(SKIP)") $(GO_TEST_FLAGS)

.PHONY: test-gateway
test-gateway: ## Run api-gateway tests (supports FOCUS=pattern)
	KUBEBUILDER_ASSETS="$$(setup-envtest use $(ENVTEST_K8S_VERSION) -p path)" \
		go test ./internal/api-gateway/... -v $(if $(FOCUS),-ginkgo.focus="$(FOCUS)") $(if $(SKIP),-ginkgo.skip="$(SKIP)") $(GO_TEST_FLAGS)

.PHONY: test-sidecar
test-sidecar: ## Run sandbox-sidecar tests (supports FOCUS=pattern)
	go test ./internal/sandbox-sidecar/... -v $(if $(FOCUS),-ginkgo.focus="$(FOCUS)") $(if $(SKIP),-ginkgo.skip="$(SKIP)") $(GO_TEST_FLAGS)

##@ Python SDK

.PHONY: sdk-python-sync
sdk-python-sync: ## Sync Python SDK dependencies from lockfile
	cd sdks/python && uv sync --frozen --extra dev

.PHONY: sdk-python-fmt
sdk-python-fmt: ## Format Python SDK
	cd sdks/python && uv run --frozen --extra dev ruff format .

.PHONY: sdk-python-lint
sdk-python-lint: ## Lint Python SDK
	cd sdks/python && uv run --frozen --extra dev ruff check .

.PHONY: sdk-python-lint-fix
sdk-python-lint-fix: ## Lint Python SDK with auto-fix
	cd sdks/python && uv run --frozen --extra dev ruff check --fix .

.PHONY: sdk-python-typecheck
sdk-python-typecheck: ## Type-check Python SDK
	cd sdks/python && uv run --frozen --extra dev mypy src

.PHONY: sdk-python-check-all
sdk-python-check-all: ## Run all Python SDK checks (no tests)
	$(MAKE) sdk-python-lint
	$(MAKE) sdk-python-typecheck

.PHONY: sdk-python-fix-all
sdk-python-fix-all: ## Fix all auto-fixable Python SDK issues
	$(MAKE) sdk-python-fmt
	$(MAKE) sdk-python-lint-fix

.PHONY: test-sdk-python
test-sdk-python: ## Run Python SDK tests
	cd sdks/python && uv run --frozen --extra dev pytest -q

.PHONY: test-sdk-python-verbose
test-sdk-python-verbose: ## Run Python SDK tests with verbose output
	cd sdks/python && uv run --frozen --extra dev pytest -v

##@ E2E Testing

E2E_WORKERS ?= 20

.PHONY: test-e2e
test-e2e: ## Run E2E tests in parallel (requires running cluster: tilt up)
	cd tests/e2e && uv run --frozen pytest -n $(E2E_WORKERS) --dist load -q $(if $(FOCUS),-k "$(FOCUS)")

.PHONY: test-e2e-slow
test-e2e-slow: ## Run E2E tests including slow tests
	cd tests/e2e && uv run --frozen pytest -n $(E2E_WORKERS) --dist load --slow -q $(if $(FOCUS),-k "$(FOCUS)")

.PHONY: test-e2e-verbose
test-e2e-verbose: ## Run E2E tests in parallel with verbose output
	cd tests/e2e && uv run --frozen pytest -n $(E2E_WORKERS) --dist load -v $(if $(FOCUS),-k "$(FOCUS)")

##@ Release

CRD_BUNDLE ?= isola-crds.yaml

.PHONY: crd-bundle
crd-bundle: ## Generate concatenated CRD bundle for standalone installation
	@: > $(CRD_BUNDLE)
	@for f in $$(ls charts/isola/crds/*.yaml | sort); do \
		cat "$$f" >> $(CRD_BUNDLE); \
	done
	@echo "Generated $(CRD_BUNDLE)"

##@ Build

.PHONY: build
build: ## Build all binaries
	go build -o bin/operator ./cmd/operator
	go build -o bin/sandbox-sidecar ./cmd/sandbox-sidecar
	go build -o bin/snapshot-uploader ./cmd/snapshot-uploader
	go build -o bin/api-gateway ./cmd/api-gateway

.PHONY: run-operator
run-operator: ## Run operator from your host
	go run ./cmd/operator/main.go
