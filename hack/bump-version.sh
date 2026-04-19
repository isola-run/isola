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

# yq v4 (mikefarah/yq) is YAML-aware; avoids sed fragility on Chart.yaml.
if ! command -v yq >/dev/null; then
    echo "ERROR: yq not found on PATH. Install mikefarah/yq v4 (https://github.com/mikefarah/yq)." >&2
    exit 3
fi

echo "$NEW_VERSION" > VERSION

yq -i ".version = \"$NEW_VERSION\"" charts/isola/Chart.yaml
yq -i ".appVersion = \"$NEW_VERSION\"" charts/isola/Chart.yaml

# Hatchling reads __version__ from this file via [tool.hatch.version] path.
cat > sdks/python/src/isola/_version.py <<EOF
# Managed by hack/bump-version.sh — do not edit.
__version__ = "$NEW_VERSION"
EOF

# make openapi must rerun: info.version in api/openapi/*.yaml is baked from VERSION.
make openapi

./hack/check-versions.sh

echo
echo "Next steps:"
echo "  1. Create CHANGELOG/v${NEW_VERSION}.md with release notes."
echo "  2. git add -A && git commit -m \"chore: release v$NEW_VERSION\""
echo "  3. Open release PR; CI must pass."
echo "  4. After merge: git tag v$NEW_VERSION && git push origin v$NEW_VERSION"
