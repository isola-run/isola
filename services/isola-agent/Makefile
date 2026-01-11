# Makefile for isola-agent

SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

MAKEFILE_DIR := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))
ROOT_DIR := $(abspath $(MAKEFILE_DIR)/../..)
LOCALBIN := $(ROOT_DIR)/bin

GOLANGCI_LINT := $(LOCALBIN)/golangci-lint
GOVULNCHECK := $(LOCALBIN)/govulncheck

.PHONY: all
all: build

##@ General

.PHONY: help
help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: fmt
fmt: ## Run golangci-lint fmt
	"$(GOLANGCI_LINT)" fmt ./...

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: lint
lint: ## Run golangci-lint
	"$(GOLANGCI_LINT)" run

.PHONY: lint-fix
lint-fix: ## Run golangci-lint with fixes
	"$(GOLANGCI_LINT)" run --fix

.PHONY: vulncheck
vulncheck: ## Run govulncheck
	"$(GOVULNCHECK)" ./...

.PHONY: test
test: ## Run tests
	go test ./... -coverprofile cover.out

.PHONY: check
check: vet lint vulncheck ## Run all checks (read-only, CI-safe)

.PHONY: fix
fix: fmt lint-fix ## Fix all auto-fixable issues

##@ Build

.PHONY: build
build: vet ## Build the binary
	go build -o bin/isola-agent cmd/main.go
