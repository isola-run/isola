# .github/

GitHub configuration and CI/CD workflows.

## Workflows (`.github/workflows/`)

| Workflow | Purpose |
|----------|---------|
| `test.yml` | Unit tests with envtest |
| `lint.yml` | golangci-lint checks |
| `codegen.yml` | Verifies generated code is in sync (`make generate manifests`) |
| `e2e.yml` | End-to-end tests on Kind cluster with gVisor |
| `release.yml` | Release/tag pipeline |
| `security.yml` | Security scanning |
| `claude.yml` | Claude AI code review |
| `claude-code-review.yml` | Extended Claude code review rules |

## Tool Version Sync

Workflow files pin tool versions that must match `hack/setup.sh`. See the root `CLAUDE.md` Tooling Versions table.
