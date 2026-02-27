---
sidebar_position: 2
title: Testing
---

# Testing

Isola uses different testing frameworks for Go and Python components.

## Go Tests

Go tests use [Ginkgo](https://onsi.github.io/ginkgo/) and [Gomega](https://onsi.github.io/gomega/) with [envtest](https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/envtest) for Kubernetes API simulation.

### Running Tests

```bash
# All unit tests with coverage
make test

# Verbose output
make test-verbose

# With race detector
make test GO_TEST_FLAGS="-race"
```

### Component-Specific Tests

```bash
# Operator tests
make test-operator

# Focused operator test
make test-operator FOCUS="TestName"

# API gateway tests
make test-gateway

# Sandbox sidecar tests
make test-sidecar
```

### Test Variables

| Variable | Description |
|----------|-------------|
| `FOCUS` | Ginkgo focus pattern — only run matching tests |
| `SKIP` | Ginkgo skip pattern — skip matching tests |
| `GO_TEST_FLAGS` | Additional flags passed to `go test` |

### Test Patterns

**Operator tests** (`internal/operator/controller/`) use a `FakeClock` via an internal `Clock` interface for deterministic timeout and snapshot testing. Test fixtures in `internal/testutil/utils/fixtures.go` use functional options like `WithSandboxActiveDeadline`, `WithNetworkSpec`, and `WithInternetAccess`.

**API gateway tests** (`internal/api-gateway/`) use `humatest.TestAPI` for HTTP request/response testing against a real envtest Kubernetes backend. Tests use `Eventually()` for cache eventual consistency. Error injection uses controller-runtime's `interceptor.Funcs` for simulating Kubernetes API errors.

**Two-client pattern:** Operator tests use both `k8sClient` (direct, no cache delay) and `k8sCache` (cached, for field index queries). Use the direct client for test assertions to avoid flakiness.

## Python SDK Tests

Python tests use [pytest](https://docs.pytest.org/) with [pytest-asyncio](https://pytest-asyncio.readthedocs.io/) (auto mode) and [respx](https://lundberg.github.io/respx/) for HTTP mocking.

### Running Tests

```bash
# Quick run
make test-sdk-python

# Verbose
make test-sdk-python-verbose
```

### Test Structure

- **Client/sandbox/filesystem tests** use respx for HTTP mocking
- **Streaming tests** use hand-rolled fake API/response objects (not respx) to simulate reconnects, network errors, and chunked delivery
- Async tests run automatically via `asyncio_mode = "auto"` in pytest config

### Type Checking

```bash
make sdk-python-typecheck    # Strict mypy
```

## E2E Tests

End-to-end tests run against a real cluster:

```bash
cd tests/e2e && uv run pytest
```

These require a running Kind cluster with Isola deployed (see [Local Setup](./local-setup.md)).
