#!/usr/bin/env bash
# Verify that VERSION, Chart.yaml (version + appVersion), and the Python SDK
# _version.py all agree. Runs in CI on every PR (via
# .github/workflows/check-versions.yml) and as the first step of the release
# workflow's validate job.
#
# The e2e test harness is frozen at 0.0.0 (see tests/e2e/pyproject.toml) and
# is not checked here.
#
# See docs/superpowers/specs/2026-04-17-first-release-design.md §2.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

VERSION="$(tr -d '[:space:]' < VERSION)"
CHART_FILE="charts/isola/Chart.yaml"
PYSDK_VERSION_FILE="sdks/python/src/isola/_version.py"

# Use yq (YAML-aware) to read Chart.yaml — same parser hack/bump-version.sh uses
# to write it. Avoids awk/yq asymmetry that would silently drift if Chart.yaml
# gained quoted values, comments, or nested fields.
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
