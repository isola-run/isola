#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"
KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-isola-dev}"

echo "=== Starting Isola Development Environment ==="

# Verify cluster exists
if ! kind get clusters 2>/dev/null | grep -q "^${KIND_CLUSTER_NAME}$"; then
    echo "ERROR: Kind cluster '${KIND_CLUSTER_NAME}' not found."
    echo "Run ./scripts/setup.sh first."
    exit 1
fi

# Set kubectl context
kubectl config use-context "kind-${KIND_CLUSTER_NAME}"

# Start Tilt
cd "$ROOT_DIR"
echo ""
echo "Starting Tilt..."
echo "  Web UI: http://localhost:10350"
echo "  API endpoint: http://localhost:30080"
echo "  Press Ctrl+C to stop"
echo ""

exec tilt up "$@"
