# tests/e2e/

End-to-end tests using Python.

## Setup

- `pyproject.toml` — Python project config and dependencies
- `uv.lock` — Locked dependencies (managed by uv)

## Running

E2E tests require a running Kind cluster with Isola deployed. They are run via CI (`e2e.yml` workflow) or locally after `tilt up`.

The Go-side coordination lives in `internal/testutil/e2e/`.
