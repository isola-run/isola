#!/usr/bin/env bash
# Bump all version-holding files to the given semver.
#
# Writes:
#   VERSION                            (plaintext)
#   charts/isola/Chart.yaml            (version + appVersion)
#   sdks/python/src/isola/_version.py  (__version__ constant; hatchling reads it)
# Then runs:
#   make openapi                       (regenerates api/openapi/*.yaml)
#
# Does NOT touch:
#   sdks/python/pyproject.toml       (no version string; hatchling reads src/isola/_version.py)
#   tests/e2e/pyproject.toml         (frozen at 0.0.0, see §3.4 of the design doc)
#
# Usage:
#   ./hack/bump-version.sh 0.5.0
#   ./hack/bump-version.sh 0.5.0-rc.0
#
# See docs/superpowers/specs/2026-04-17-first-release-design.md §2.
set -euo pipefail

if [[ $# -ne 1 ]]; then
    echo "Usage: $0 <X.Y.Z[-rc.N]>" >&2
    exit 2
fi

NEW_VERSION="$1"

# Validate input. Accept GA (X.Y.Z) or RC (X.Y.Z-rc.N) only — other
# pre-release identifiers (-alpha.N, -beta.N) would need a semver-regex
# widening here and in the release workflow's validate job.
if ! [[ "$NEW_VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-rc\.[0-9]+)?$ ]]; then
    echo "ERROR: '$NEW_VERSION' does not match ^[0-9]+\\.[0-9]+\\.[0-9]+(-rc\\.[0-9]+)?$" >&2
    exit 2
fi

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

# 1. VERSION file (bare semver, no v prefix).
echo "$NEW_VERSION" > VERSION
echo "wrote VERSION=$NEW_VERSION"

# 2. Chart.yaml version + appVersion via yq (YAML-aware; avoids sed fragility).
#    Requires yq v4 (mikefarah/yq). Pre-installed on ubuntu-latest; developers
#    install via `go install github.com/mikefarah/yq/v4@latest` or their package manager.
if ! command -v yq >/dev/null; then
    echo "ERROR: yq not found on PATH. Install mikefarah/yq v4 (https://github.com/mikefarah/yq)." >&2
    exit 3
fi

yq -i ".version = \"$NEW_VERSION\"" charts/isola/Chart.yaml
yq -i ".appVersion = \"$NEW_VERSION\"" charts/isola/Chart.yaml
echo "wrote charts/isola/Chart.yaml version=$NEW_VERSION, appVersion=$NEW_VERSION"

# 3. Python SDK _version.py (committed; hatchling reads __version__ from it at build time).
PYSDK_VERSION_FILE="sdks/python/src/isola/_version.py"
cat > "$PYSDK_VERSION_FILE" <<EOF
# Single source of truth for the Python SDK version at runtime.
# Kept in sync with /VERSION and charts/isola/Chart.yaml by hack/bump-version.sh
# (and verified by hack/check-versions.sh). Hatchling reads __version__ from
# this file at build time via [tool.hatch.version] path = "src/isola/_version.py".
__version__ = "$NEW_VERSION"
EOF
echo "wrote $PYSDK_VERSION_FILE __version__=$NEW_VERSION"

# 4. Regenerate OpenAPI specs so info.version matches.
make openapi
echo "regenerated api/openapi/*.yaml"

# 5. Self-check.
./hack/check-versions.sh

echo
echo "Next steps:"
echo "  1. Create CHANGELOG/v${NEW_VERSION}.md with release notes."
echo "  2. git add -A && git commit -m \"chore: release v$NEW_VERSION\""
echo "  3. Open release PR; CI must pass."
echo "  4. After merge: git tag v$NEW_VERSION && git push origin v$NEW_VERSION"
