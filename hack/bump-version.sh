#!/usr/bin/env bash
# Bump all version-holding files to the given semver.
#
# Writes:
#   VERSION                                 (plaintext)
#   charts/isola/Chart.yaml                 (version + appVersion)
#   sdks/python/src/isola/_version.py       (__version__ constant; hatchling reads it)
#   sdks/typescript/src/version.ts          (VERSION constant; barrel-exported)
#   sdks/typescript/package.json            (version field; via pnpm version)
#   sdks/typescript/pnpm-lock.yaml          (refreshed via pnpm install --lockfile-only)
# Then runs:
#   make openapi                            (regenerates api/openapi/*.yaml)
#
# Usage:
#   ./hack/bump-version.sh 0.5.0
#   ./hack/bump-version.sh 0.5.0-rc.0
set -euo pipefail

if [[ $# -ne 1 ]]; then
    echo "Usage: $0 <X.Y.Z[-rc.N]>" >&2
    exit 2
fi

NEW_VERSION="$1"

# Validate input. Accept GA (X.Y.Z) or RC (X.Y.Z-rc.N) only.
if ! [[ "$NEW_VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-rc\.[0-9]+)?$ ]]; then
    echo "ERROR: '$NEW_VERSION' does not match ^[0-9]+\\.[0-9]+\\.[0-9]+(-rc\\.[0-9]+)?$" >&2
    exit 2
fi

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

if ! command -v yq >/dev/null; then
    echo "ERROR: yq not found on PATH. Install mikefarah/yq v4 (https://github.com/mikefarah/yq)." >&2
    exit 3
fi

echo "$NEW_VERSION" > VERSION

yq -i ".version = \"$NEW_VERSION\"" charts/isola/Chart.yaml
yq -i ".appVersion = \"$NEW_VERSION\"" charts/isola/Chart.yaml

# Hatchling reads __version__ from this file via [tool.hatch.version] path.
cat > sdks/python/src/isola/_version.py <<EOF
# Managed by hack/bump-version.sh, do not edit.
__version__ = "$NEW_VERSION"
EOF

# TypeScript SDK keeps the version constant in src/version.ts (re-exported via
# the index barrel) and the npm version in package.json. Both must stay in sync.
cat > sdks/typescript/src/version.ts <<EOF
// Managed by hack/bump-version.sh; do not edit by hand.
export const VERSION = "$NEW_VERSION";
EOF
(
    cd sdks/typescript
    # pnpm version preserves package.json formatting better than yq and keeps
    # the bump script within the pnpm toolchain we use everywhere else.
    pnpm version --no-git-tag-version --allow-same-version "$NEW_VERSION"
    # Refresh pnpm-lock.yaml without touching node_modules.
    pnpm install --lockfile-only
)

# make openapi must rerun: info.version in api/openapi/*.yaml is baked from VERSION.
make openapi

./hack/check-versions.sh

echo
echo "Next steps:"
echo "  1. Create CHANGELOG/v${NEW_VERSION}.md with release notes."
echo "  2. git add -A && git commit -m \"chore: release v$NEW_VERSION\""
echo "  3. Open release PR; CI must pass."
echo "  4. After merge: git tag v$NEW_VERSION && git push origin v$NEW_VERSION"
