# Root Makefile for isola multi-service repository

SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

SERVICES := isola-operator isola-gw isola-agent
SERVICE_DIRS := $(addprefix services/,$(SERVICES))

# Tool versions (pinned)
GOLANGCI_LINT_VERSION ?= v2.8.0
GOVULNCHECK_VERSION ?= v1.1.4

LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p "$(LOCALBIN)"

GOLANGCI_LINT = $(LOCALBIN)/golangci-lint
GOVULNCHECK = $(LOCALBIN)/govulncheck

##@ General

.PHONY: help
help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development (All Services)

.PHONY: lint-all
lint-all: golangci-lint ## Run golangci-lint on all services
	@for dir in $(SERVICE_DIRS); do \
		echo "=== Linting $$dir ==="; \
		(cd $$dir && "$(GOLANGCI_LINT)" run); \
	done

.PHONY: lint-fix-all
lint-fix-all: golangci-lint ## Run golangci-lint --fix on all services
	@for dir in $(SERVICE_DIRS); do \
		echo "=== Fixing $$dir ==="; \
		(cd $$dir && "$(GOLANGCI_LINT)" run --fix); \
	done

.PHONY: fmt-all
fmt-all: golangci-lint ## Run golangci-lint fmt on all services
	@for dir in $(SERVICE_DIRS); do \
		echo "=== Formatting $$dir ==="; \
		(cd $$dir && "$(GOLANGCI_LINT)" fmt ./...); \
	done

.PHONY: vet-all
vet-all: ## Run go vet on all services
	@for dir in $(SERVICE_DIRS); do \
		echo "=== Vetting $$dir ==="; \
		(cd $$dir && go vet ./...); \
	done

.PHONY: vulncheck-all
vulncheck-all: govulncheck ## Run govulncheck on all services
	@for dir in $(SERVICE_DIRS); do \
		echo "=== Vulnerability check $$dir ==="; \
		(cd $$dir && "$(GOVULNCHECK)" ./...); \
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

##@ Dependencies

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT)
$(GOLANGCI_LINT): $(LOCALBIN)
	$(call go-install-tool,$(GOLANGCI_LINT),github.com/golangci/golangci-lint/v2/cmd/golangci-lint,$(GOLANGCI_LINT_VERSION))

.PHONY: govulncheck
govulncheck: $(GOVULNCHECK)
$(GOVULNCHECK): $(LOCALBIN)
	$(call go-install-tool,$(GOVULNCHECK),golang.org/x/vuln/cmd/govulncheck,$(GOVULNCHECK_VERSION))

.PHONY: install-tools
install-tools: golangci-lint govulncheck ## Install all required tools

# go-install-tool will 'go install' any package with custom target and name of binary, if it doesn't exist
# $1 - target path with name of binary
# $2 - package url which can be installed
# $3 - specific version of package
define go-install-tool
@[ -f "$(1)-$(3)" ] && [ "$$(readlink -- "$(1)" 2>/dev/null)" = "$(1)-$(3)" ] || { \
set -e; \
package=$(2)@$(3) ;\
echo "Downloading $${package}" ;\
rm -f "$(1)" ;\
GOBIN="$(LOCALBIN)" go install $${package} ;\
mv "$(LOCALBIN)/$$(basename "$(1)")" "$(1)-$(3)" ;\
} ;\
ln -sf "$$(realpath "$(1)-$(3)")" "$(1)"
endef
