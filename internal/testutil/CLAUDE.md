# internal/testutil/

Shared test utilities and fixtures.

## Sub-packages

### `utils/`
- `fixtures.go` — CRD test fixtures using functional options pattern (`WithSandboxActiveDeadline`, `WithNetworkSpec`, `WithInternetAccess`, `WithSandboxShutdownStrategy`, `WithPodTemplate`, etc.)
- `matchers.go` — Custom Gomega matchers for test assertions
- `utils.go` — Command execution helpers, project directory resolution, cert-manager install/uninstall utilities (some from Kubebuilder scaffold — may be partially unused)

### `e2e/`
- Ginkgo test coordination for end-to-end tests
