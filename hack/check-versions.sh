#!/usr/bin/env bash
# Verify that VERSION, Chart.yaml (version + appVersion), Python SDK
# _version.py, and TypeScript SDK (version.ts + package.json) all agree.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

VERSION="$(tr -d '[:space:]' < VERSION)"
CHART_FILE="charts/isola/Chart.yaml"
PYSDK_VERSION_FILE="sdks/python/src/isola/_version.py"
TSSDK_VERSION_FILE="sdks/typescript/src/version.ts"
TSSDK_PACKAGE_JSON="sdks/typescript/package.json"

if ! command -v yq >/dev/null; then
    echo "ERROR: yq not found on PATH. Install mikefarah/yq v4 (https://github.com/mikefarah/yq)." >&2
    exit 3
fi

chart_version=$(yq '.version' "$CHART_FILE")
chart_app_version=$(yq '.appVersion' "$CHART_FILE")
pysdk_version=$(awk -F'"' '/^__version__/ {print $2; exit}' "$PYSDK_VERSION_FILE")
tssdk_version=$(awk -F'"' '/^export const VERSION/ {print $2; exit}' "$TSSDK_VERSION_FILE")
tssdk_package_version=$(awk -F'"' '/"version":/ {print $4; exit}' "$TSSDK_PACKAGE_JSON")

fail=0
for pair in \
    "$CHART_FILE version|$chart_version" \
    "$CHART_FILE appVersion|$chart_app_version" \
    "$PYSDK_VERSION_FILE __version__|$pysdk_version" \
    "$TSSDK_VERSION_FILE VERSION|$tssdk_version" \
    "$TSSDK_PACKAGE_JSON version|$tssdk_package_version"; do
    label="${pair%%|*}"
    val="${pair##*|}"
    if [[ "$VERSION" != "$val" ]]; then
        echo "ERROR: VERSION ($VERSION) != $label ($val)" >&2
        fail=1
    fi
done

if [[ $fail -ne 0 ]]; then
    echo "Hint: run ./hack/bump-version.sh <X.Y.Z[-rc.N]> to realign all version files." >&2
    exit 1
fi

echo "OK: VERSION=$VERSION, chart=$chart_version/$chart_app_version, py=$pysdk_version, ts=$tssdk_version/$tssdk_package_version"
