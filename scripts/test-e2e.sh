#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"

# Default values
BASE_URL="${ISOLA_BASE_URL:-http://localhost:30080}"
API_KEY="${ISOLA_API_KEY:-iso_sk_demo}"
MARKERS=""
PARALLEL=""
VERBOSE="-v"
EXTRA_ARGS=""
WAIT_FOR_GATEWAY=true

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --smoke)
            MARKERS="-m smoke"
            shift
            ;;
        --regression)
            MARKERS="-m regression"
            shift
            ;;
        --parallel|-p)
            PARALLEL="-n auto"
            shift
            ;;
        --verbose|-vv)
            VERBOSE="-vv"
            shift
            ;;
        --quiet|-q)
            VERBOSE=""
            shift
            ;;
        --base-url)
            BASE_URL="$2"
            shift 2
            ;;
        --api-key)
            API_KEY="$2"
            shift 2
            ;;
        --skip-cleanup)
            EXTRA_ARGS="$EXTRA_ARGS --skip-cleanup"
            shift
            ;;
        --html)
            EXTRA_ARGS="$EXTRA_ARGS --html=report.html --self-contained-html"
            shift
            ;;
        --no-wait)
            WAIT_FOR_GATEWAY=false
            shift
            ;;
        -k)
            EXTRA_ARGS="$EXTRA_ARGS -k $2"
            shift 2
            ;;
        *)
            EXTRA_ARGS="$EXTRA_ARGS $1"
            shift
            ;;
    esac
done

echo "=== Isola E2E Tests ==="
echo "  Base URL: ${BASE_URL}"
echo "  Markers: ${MARKERS:-all}"
echo ""

# Wait for gateway to be ready
if [ "$WAIT_FOR_GATEWAY" = true ]; then
    echo "Checking gateway readiness..."
    for i in {1..30}; do
        if curl -sf "${BASE_URL}/health" > /dev/null 2>&1; then
            echo "  Gateway is ready"
            break
        fi
        if [ "$i" -eq 30 ]; then
            echo "ERROR: Gateway not ready at ${BASE_URL}"
            echo "  Make sure to run ./scripts/dev.sh first"
            exit 1
        fi
        echo "  Waiting for gateway... ($i/30)"
        sleep 2
    done
fi

# Run tests using uv
cd "${ROOT_DIR}/tests"

if ! command -v uv &> /dev/null; then
    echo "ERROR: uv not found. Install it: curl -LsSf https://astral.sh/uv/install.sh | sh"
    exit 1
fi

# Build pytest command
# shellcheck disable=SC2086
uv run --quiet pytest \
    ${VERBOSE} \
    ${MARKERS} \
    ${PARALLEL} \
    --base-url="${BASE_URL}" \
    --api-key="${API_KEY}" \
    ${EXTRA_ARGS}
