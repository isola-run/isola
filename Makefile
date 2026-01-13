# Root Makefile for isola multi-service repository

SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

SERVICES := isola-operator isola-gw isola-agent
SERVICE_DIRS := $(addprefix services/,$(SERVICES))

##@ General

.PHONY: help
help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development (All Services)

.PHONY: lint-all
lint-all: ## Run golangci-lint on all services
	@for dir in $(SERVICE_DIRS); do \
		echo "=== Linting $$dir ==="; \
		(cd $$dir && golangci-lint run); \
	done

.PHONY: lint-fix-all
lint-fix-all: ## Run golangci-lint --fix on all services
	@for dir in $(SERVICE_DIRS); do \
		echo "=== Fixing $$dir ==="; \
		(cd $$dir && golangci-lint run --fix); \
	done

.PHONY: fmt-all
fmt-all: ## Run golangci-lint fmt on all services
	@for dir in $(SERVICE_DIRS); do \
		echo "=== Formatting $$dir ==="; \
		(cd $$dir && golangci-lint fmt ./...); \
	done

.PHONY: vet-all
vet-all: ## Run go vet on all services
	@for dir in $(SERVICE_DIRS); do \
		echo "=== Vetting $$dir ==="; \
		(cd $$dir && go vet ./...); \
	done

.PHONY: vulncheck-all
vulncheck-all: ## Run govulncheck on all services
	@for dir in $(SERVICE_DIRS); do \
		echo "=== Vulnerability check $$dir ==="; \
		(cd $$dir && govulncheck ./...); \
	done

.PHONY: tidy-all
tidy-all: ## Run go mod tidy on all services
	@for dir in $(SERVICE_DIRS); do \
		echo "=== Tidying $$dir ==="; \
		(cd $$dir && go mod tidy); \
	done

.PHONY: check-all
check-all: vet-all lint-all vulncheck-all ## Run all checks (read-only, CI-safe)

.PHONY: fix-all
fix-all: fmt-all lint-fix-all ## Fix all auto-fixable issues
