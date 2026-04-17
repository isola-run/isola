# Root Makefile for isola

SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

# Go toolchain version — auto-downloads the correct Go toolchain regardless of
# what's installed locally (requires Go 1.21+). Must match go.mod `go` directive.
GO_VERSION ?= 1.26.2
GOTOOLCHAIN = go$(GO_VERSION)
export GOTOOLCHAIN

##@ Version metadata

# VERSION sourced from the VERSION file at the repo root (argo-cd pattern).
# Release workflow bumps it; dev builds just read it in place.
VERSION        ?= $(shell cat VERSION)
GIT_COMMIT     ?= $(shell git rev-parse --short=7 HEAD 2>/dev/null || echo unknown)
# BUILD_DATE uses committer ISO 8601 date: same commit -> same timestamp -> reproducible.
# Falls back to wall-clock only when git is unavailable (tarball builds).
BUILD_DATE     ?= $(shell git log -1 --format=%cI 2>/dev/null || date -u +'%Y-%m-%dT%H:%M:%SZ')
# git status --porcelain catches both modified-tracked AND untracked files that would
# land in the Docker build context. Matches argo-cd / k8s / cert-manager / cluster-api / velero.
GIT_TREE_STATE ?= $(shell if [ -z "$$(git status --porcelain 2>/dev/null)" ]; then echo clean; else echo dirty; fi)

LDFLAGS := -s -w \
	-X github.com/isola-run/isola/internal/version.gitVersion=$(VERSION) \
	-X github.com/isola-run/isola/internal/version.gitCommit=$(GIT_COMMIT) \
	-X github.com/isola-run/isola/internal/version.buildDate=$(BUILD_DATE) \
	-X github.com/isola-run/isola/internal/version.gitTreeState=$(GIT_TREE_STATE)

# Shared between `build` and `openapi` so -trimpath + ldflags apply identically in
# both paths — avoids info.version drift between the built binary and the
# `go run`-invoked openapi-gen on the same checkout.
GO_FLAGS := -trimpath -ldflags='$(LDFLAGS)'
GO_BUILD := CGO_ENABLED=0 go build $(GO_FLAGS)

##@ General

.PHONY: help
help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: generate
generate: ## Generate CRD DeepCopy methods
	controller-gen object:headerFile="hack/boilerplate.go.txt" paths="./api/..."

# Raw CRDs are generated to config/crd/bases/ (kubebuilder convention). They're
# the source envtest points at for controller/gateway tests. hack/generate-crd-templates.sh
# then wraps them with Helm conditionals (.Values.crds.enabled / .Values.crds.keep)
# into charts/isola/templates/crd/ so `helm upgrade` keeps CRDs in sync (unlike
# Helm's special crds/ directory, which is install-only).
.PHONY: manifests
manifests: ## Generate CRD and RBAC manifests
	@mkdir -p config/crd/bases charts/isola/generated
	controller-gen rbac:roleName=isola-operator crd webhook \
		paths="./api/..." paths="./internal/operator/controller/..." \
		output:crd:artifacts:config=config/crd/bases \
		output:rbac:artifacts:config=charts/isola/generated
	./hack/generate-crd-templates.sh config/crd/bases charts/isola/templates/crd

.PHONY: openapi
openapi: ## Generate OpenAPI specs for HTTP services
	@mkdir -p api/openapi
	go run $(GO_FLAGS) ./cmd/openapi-gen -service api-gateway > api/openapi/api-gateway.yaml
	go run $(GO_FLAGS) ./cmd/openapi-gen -service sandbox-sidecar > api/openapi/sandbox-sidecar.yaml

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
	@if ! git diff --quiet -- config/crd/bases/ charts/isola/templates/crd/ charts/isola/generated/; then \
		echo "ERROR: Generated manifests are out of sync"; \
		echo "Run 'make manifests' and commit the changes"; \
		git diff --stat -- config/crd/bases/ charts/isola/templates/crd/ charts/isola/generated/; \
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

##@ Build

.PHONY: build
build: ## Build all binaries
	$(GO_BUILD) -o bin/operator ./cmd/operator
	$(GO_BUILD) -o bin/sandbox-sidecar ./cmd/sandbox-sidecar
	$(GO_BUILD) -o bin/snapshot-uploader ./cmd/snapshot-uploader
	$(GO_BUILD) -o bin/api-gateway ./cmd/api-gateway

.PHONY: run-operator
run-operator: ## Run operator from your host
	go run ./cmd/operator/main.go
