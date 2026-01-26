# Root Makefile for isola-sb

SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

##@ General

.PHONY: help
help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: generate-api
generate-api: ## Generate isola-api OpenAPI code
	go generate ./cmd/isola-api/...

.PHONY: check-api-codegen
check-api-codegen: generate-api ## Verify isola-api OpenAPI code is up-to-date
	@if ! git diff --quiet -- api/openapi.yaml internal/api/generated/; then \
		echo "ERROR: API generated code is out of sync with spec"; \
		echo "Run 'make generate-api' and commit the changes"; \
		git diff --stat -- api/openapi.yaml internal/api/generated/; \
		exit 1; \
	fi

.PHONY: generate
generate: generate-api ## Generate all code (CRD DeepCopy + OpenAPI)
	controller-gen object:headerFile="hack/boilerplate.go.txt" paths="./api/..."

.PHONY: manifests
manifests: ## Generate CRD and RBAC manifests
	controller-gen rbac:roleName=isola-operator-manager-role crd webhook \
		paths="./api/..." paths="./internal/operator/controller/..." \
		output:crd:artifacts:config=config/crd/bases \
		output:rbac:artifacts:config=config/rbac

# Custom golangci-lint binary with nilaway plugin
CUSTOM_GCL ?= ./custom-gcl

.PHONY: custom-gcl
custom-gcl: ## Build custom golangci-lint with nilaway plugin
	@if [ ! -f "$(CUSTOM_GCL)" ] || [ ".custom-gcl.yml" -nt "$(CUSTOM_GCL)" ]; then \
		echo "Building custom golangci-lint with nilaway..."; \
		golangci-lint custom; \
	fi

.PHONY: fmt
fmt: ## Run golangci-lint fmt
	golangci-lint fmt ./...

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: lint
lint: custom-gcl ## Run golangci-lint (with nilaway)
	$(CUSTOM_GCL) run

.PHONY: lint-fix
lint-fix: custom-gcl ## Run golangci-lint --fix (with nilaway)
	$(CUSTOM_GCL) run --fix

.PHONY: vulncheck
vulncheck: ## Run govulncheck
	govulncheck ./...

.PHONY: tidy
tidy: ## Run go mod tidy
	go mod tidy

.PHONY: check-all
check-all: vet lint vulncheck check-api-codegen ## Run all checks (read-only, CI-safe)

.PHONY: fix-all
fix-all: fmt lint-fix ## Fix all auto-fixable issues

##@ Testing
#
# Variables for test filtering (following Cluster API / Kubernetes patterns):
#   FOCUS        - Ginkgo focus pattern (e.g., FOCUS="Reconcile")
#   SKIP         - Ginkgo skip pattern
#   GO_TEST_FLAGS - Additional go test flags (e.g., GO_TEST_FLAGS="-race")

ENVTEST_K8S_VERSION ?= 1.34
FOCUS ?=
SKIP ?=
GO_TEST_FLAGS ?=

.PHONY: test
test: ## Run all tests
	KUBEBUILDER_ASSETS="$$(setup-envtest use $(ENVTEST_K8S_VERSION) -p path)" \
		go test $$(go list ./... | grep -v /e2e) -coverprofile cover.out $(GO_TEST_FLAGS)

.PHONY: test-verbose
test-verbose: ## Run all tests with verbose output
	KUBEBUILDER_ASSETS="$$(setup-envtest use $(ENVTEST_K8S_VERSION) -p path)" \
		go test $$(go list ./... | grep -v /e2e) -v -coverprofile cover.out $(GO_TEST_FLAGS)

.PHONY: test-operator
test-operator: ## Run operator tests (supports FOCUS=pattern)
	KUBEBUILDER_ASSETS="$$(setup-envtest use $(ENVTEST_K8S_VERSION) -p path)" \
		go test $(if $(FOCUS),./internal/operator/controller,./internal/operator/...) -v \
		$(if $(FOCUS),-ginkgo.focus="$(FOCUS)") $(if $(SKIP),-ginkgo.skip="$(SKIP)") $(GO_TEST_FLAGS)

.PHONY: test-api
test-api: ## Run isola-api tests (supports FOCUS=pattern)
	go test ./internal/api/... -v $(if $(FOCUS),-ginkgo.focus="$(FOCUS)") $(if $(SKIP),-ginkgo.skip="$(SKIP)") $(GO_TEST_FLAGS)

##@ Build

.PHONY: build
build: ## Build all binaries
	go build -o bin/operator ./cmd/operator
	go build -o bin/agent ./cmd/agent
	go build -o bin/uploader ./cmd/uploader
	go build -o bin/isola-api ./cmd/isola-api

.PHONY: run-operator
run-operator: ## Run operator from your host
	go run ./cmd/operator/main.go
