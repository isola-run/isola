#!/bin/bash
set -euo pipefail

KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-isola-dev}"
REGISTRY_NAME="${REGISTRY_NAME:-kind-registry}"

echo "=== Tearing Down Isola Development Environment ==="

# Stop Tilt if running
if pgrep -f "tilt up" > /dev/null 2>&1; then
    echo "Stopping Tilt..."
    pkill -f "tilt up" || true
    sleep 2
fi

# Delete Kind cluster
echo ""
echo "Deleting Kind cluster..."
if kind get clusters 2>/dev/null | grep -q "^${KIND_CLUSTER_NAME}$"; then
    kind delete cluster --name "${KIND_CLUSTER_NAME}"
    echo "  Cluster '${KIND_CLUSTER_NAME}' deleted"
else
    echo "  Cluster '${KIND_CLUSTER_NAME}' not found"
fi

# Optionally remove registry
echo ""
read -p "Remove local registry container? [y/N] " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    if docker ps -a --format '{{.Names}}' | grep -q "^${REGISTRY_NAME}$"; then
        docker rm -f "$REGISTRY_NAME" 2>/dev/null || true
        echo "  Registry '${REGISTRY_NAME}' removed"
    else
        echo "  Registry '${REGISTRY_NAME}' not found"
    fi
else
    echo "  Keeping registry (reusable for future clusters)"
fi

echo ""
echo "=== Teardown Complete ==="
echo ""
echo "To start fresh, run: ./scripts/setup.sh"
