# Root Makefile for isola-sb

SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

##@ General

.PHONY: help
help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: swagger
swagger: ## Generate api-gateway Swagger docs
	go tool swag init -g cmd/api-gateway/main.go -o api/openapi --parseDependency --parseInternal

.PHONY: check-swagger
check-swagger: swagger ## Verify api-gateway Swagger docs are up-to-date
	@if ! git diff --quiet -- api/openapi/; then \
		echo "ERROR: Swagger docs are out of sync"; \
		echo "Run 'make swagger' and commit the changes"; \
		git diff --stat -- api/openapi/; \
		exit 1; \
	fi

.PHONY: generate
generate: ## Generate CRD DeepCopy methods
	controller-gen object:headerFile="hack/boilerplate.go.txt" paths="./api/..."

.PHONY: manifests
manifests: ## Generate CRD and RBAC manifests directly to Helm chart
	@mkdir -p charts/isola-operator/generated
	controller-gen rbac:roleName=isola-operator crd webhook \
		paths="./api/..." paths="./internal/operator/controller/..." \
		output:crd:artifacts:config=charts/isola-operator/crds \
		output:rbac:artifacts:config=charts/isola-operator/generated

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
	@if ! git diff --quiet -- charts/isola-operator/crds/ charts/isola-operator/generated/; then \
		echo "ERROR: Generated manifests are out of sync"; \
		echo "Run 'make manifests' and commit the changes"; \
		git diff --stat -- charts/isola-operator/crds/ charts/isola-operator/generated/; \
		exit 1; \
	fi

.PHONY: check-all
check-all: vet lint vulncheck check-swagger check-manifests ## Run all checks (read-only, CI-safe)

.PHONY: fix-all
fix-all: fmt lint-fix ## Fix all auto-fixable issues

##@ Testing

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

.PHONY: test-gateway
test-gateway: ## Run api-gateway tests (supports FOCUS=pattern)
	KUBEBUILDER_ASSETS="$$(setup-envtest use $(ENVTEST_K8S_VERSION) -p path)" \
		go test ./internal/api-gateway/... -v $(if $(FOCUS),-ginkgo.focus="$(FOCUS)") $(if $(SKIP),-ginkgo.skip="$(SKIP)") $(GO_TEST_FLAGS)

.PHONY: test-sidecar
test-sidecar: ## Run sandbox-sidecar tests (supports FOCUS=pattern)
	go test ./internal/sandbox-sidecar/... -v $(if $(FOCUS),-ginkgo.focus="$(FOCUS)") $(if $(SKIP),-ginkgo.skip="$(SKIP)") $(GO_TEST_FLAGS)

##@ Build

.PHONY: build
build: ## Build all binaries
	go build -o bin/operator ./cmd/operator
	go build -o bin/sandbox-sidecar ./cmd/sandbox-sidecar
	go build -o bin/uploader ./cmd/uploader
	go build -o bin/api-gateway ./cmd/api-gateway

.PHONY: run-operator
run-operator: ## Run operator from your host
	go run ./cmd/operator/main.go
