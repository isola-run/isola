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

VERSION="$(cat VERSION | tr -d '[:space:]')"
CHART_FILE="charts/isola/Chart.yaml"
PYSDK_VERSION_FILE="sdks/python/src/isola/_version.py"

chart_version=$(awk '/^version:/ {print $2; exit}' "$CHART_FILE")
chart_app_version=$(awk '/^appVersion:/ {print $2; exit}' "$CHART_FILE" | tr -d '"')
pysdk_version=$(awk -F'"' '/^__version__/ {print $2; exit}' "$PYSDK_VERSION_FILE")

fail=0
if [[ "$VERSION" != "$chart_version" ]]; then
    echo "ERROR: VERSION ($VERSION) != $CHART_FILE version ($chart_version)" >&2
    fail=1
fi
if [[ "$VERSION" != "$chart_app_version" ]]; then
    echo "ERROR: VERSION ($VERSION) != $CHART_FILE appVersion ($chart_app_version)" >&2
    fail=1
fi
if [[ "$VERSION" != "$pysdk_version" ]]; then
    echo "ERROR: VERSION ($VERSION) != $PYSDK_VERSION_FILE __version__ ($pysdk_version)" >&2
    fail=1
fi

if [[ $fail -ne 0 ]]; then
    echo "Hint: run ./hack/bump-version.sh <X.Y.Z[-rc.N]> to realign all version files." >&2
    exit 1
fi

echo "OK: VERSION=$VERSION, chart version=$chart_version, chart appVersion=$chart_app_version, SDK __version__=$pysdk_version"
