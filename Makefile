# Root Makefile for isola-sb

SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

##@ General

.PHONY: help
help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: generate
generate: ## Generate code (DeepCopy methods)
	controller-gen object:headerFile="hack/boilerplate.go.txt" paths="./api/..."

.PHONY: manifests
manifests: ## Generate CRD and RBAC manifests
	controller-gen rbac:roleName=isola-operator-manager-role crd webhook \
		paths="./api/..." paths="./internal/operator/controller/..." \
		output:crd:artifacts:config=config/crd/bases \
		output:rbac:artifacts:config=config/rbac

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

.PHONY: check-all
check-all: vet lint vulncheck ## Run all checks (read-only, CI-safe)

.PHONY: fix-all
fix-all: fmt lint-fix ## Fix all auto-fixable issues

##@ Testing

ENVTEST_K8S_VERSION ?= 1.34

.PHONY: test
test: ## Run tests
	KUBEBUILDER_ASSETS="$$(setup-envtest use $(ENVTEST_K8S_VERSION) -p path)" \
		go test $$(go list ./... | grep -v /e2e) -coverprofile cover.out

.PHONY: test-verbose
test-verbose: ## Run tests with verbose output
	KUBEBUILDER_ASSETS="$$(setup-envtest use $(ENVTEST_K8S_VERSION) -p path)" \
		go test ./internal/operator/controller/... -v -ginkgo.v -coverprofile cover.out

.PHONY: test-focus
test-focus: ## Run tests matching FOCUS pattern
	KUBEBUILDER_ASSETS="$$(setup-envtest use $(ENVTEST_K8S_VERSION) -p path)" \
		go test ./internal/operator/controller/... -v -ginkgo.v -ginkgo.focus="$(FOCUS)"

##@ Build

.PHONY: build
build: ## Build all binaries
	go build -o bin/operator ./cmd/operator
	go build -o bin/gateway ./cmd/gateway
	go build -o bin/agent ./cmd/agent
	go build -o bin/uploader ./cmd/uploader

.PHONY: run-operator
run-operator: ## Run operator from your host
	go run ./cmd/operator/main.go

##@ Benchmarking

.PHONY: benchmark
benchmark: ## Run Go microbenchmarks
	KUBEBUILDER_ASSETS="$$(setup-envtest use $(ENVTEST_K8S_VERSION) -p path)" \
		go test -bench=. -benchmem -benchtime=10s \
		./internal/operator/controller/... \
		| tee benchmark-results.txt

.PHONY: benchmark-compare
benchmark-compare: ## Run benchmarks and compare with baseline (requires benchstat)
	@if ! command -v benchstat &> /dev/null; then \
		echo "Installing benchstat..."; \
		go install golang.org/x/perf/cmd/benchstat@latest; \
	fi
	KUBEBUILDER_ASSETS="$$(setup-envtest use $(ENVTEST_K8S_VERSION) -p path)" \
		go test -bench=. -benchmem -benchtime=10s -count=5 \
		./internal/operator/controller/... \
		| tee benchmark-new.txt
	@if [ -f benchmark-baseline.txt ]; then \
		benchstat benchmark-baseline.txt benchmark-new.txt; \
	else \
		echo "No baseline found. Run 'make benchmark-save-baseline' to create one."; \
	fi

.PHONY: benchmark-save-baseline
benchmark-save-baseline: ## Save current benchmark results as baseline
	KUBEBUILDER_ASSETS="$$(setup-envtest use $(ENVTEST_K8S_VERSION) -p path)" \
		go test -bench=. -benchmem -benchtime=10s -count=5 \
		./internal/operator/controller/... \
		| tee benchmark-baseline.txt
	@echo "Baseline saved to benchmark-baseline.txt"

.PHONY: benchmark-k6
benchmark-k6: ## Run k6 load tests (requires k6 and running cluster)
	@if ! command -v k6 &> /dev/null; then \
		echo "k6 not found. Install from https://k6.io/docs/getting-started/installation/"; \
		exit 1; \
	fi
	k6 run tests/benchmark/k6/sandbox_churn.js \
		--out json=k6-results.json \
		-e ISOLA_API_URL=$${ISOLA_API_URL:-http://localhost:30080} \
		-e ISOLA_API_KEY=$${ISOLA_API_KEY:-iso_sk_demo}

.PHONY: benchmark-stress
benchmark-stress: ## Run Python stress tests (requires running cluster)
	cd tests/e2e && uv run pytest -m stress -v --tb=short

.PHONY: benchmark-monitor
benchmark-monitor: ## Monitor cluster resources during benchmark
	./tests/benchmark/scripts/monitor.sh -d 300 -o ./benchmark-results

.PHONY: benchmark-all
benchmark-all: benchmark benchmark-k6 ## Run all benchmarks
