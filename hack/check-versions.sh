#!/usr/bin/env bash
# Verify that VERSION, Chart.yaml (version + appVersion), and the Python SDK
# _version.py all agree.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

VERSION="$(tr -d '[:space:]' < VERSION)"
CHART_FILE="charts/isola/Chart.yaml"
PYSDK_VERSION_FILE="sdks/python/src/isola/_version.py"

if ! command -v yq >/dev/null; then
    echo "ERROR: yq not found on PATH. Install mikefarah/yq v4 (https://github.com/mikefarah/yq)." >&2
    exit 3
fi

chart_version=$(yq '.version' "$CHART_FILE")
chart_app_version=$(yq '.appVersion' "$CHART_FILE")
pysdk_version=$(awk -F'"' '/^__version__/ {print $2; exit}' "$PYSDK_VERSION_FILE")

fail=0
for pair in \
    "$CHART_FILE version|$chart_version" \
    "$CHART_FILE appVersion|$chart_app_version" \
    "$PYSDK_VERSION_FILE __version__|$pysdk_version"; do
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

echo "OK: VERSION=$VERSION, chart version=$chart_version, chart appVersion=$chart_app_version, SDK __version__=$pysdk_version"
