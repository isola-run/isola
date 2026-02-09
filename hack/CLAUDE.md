# hack/

Development utility scripts and configuration.

## Files

- `setup.sh` — One-time local dev environment setup: creates Kind cluster, local registry, installs gVisor in Kind nodes, installs Go tools (golangci-lint, govulncheck, setup-envtest, controller-gen, lefthook)
- `kind-config.yaml` — Kind cluster configuration for local development
- `boilerplate.go.txt` — Apache 2.0 license header for generated Go files (used by controller-gen)

## Tool Version Pins

`setup.sh` defines pinned versions for all dev tools. These must be kept in sync with CI workflows. See the root `CLAUDE.md` Tooling Versions table for the full mapping.

## Usage

```bash
./hack/setup.sh   # Run once to set up local dev environment
tilt up            # Then start dev environment
```
