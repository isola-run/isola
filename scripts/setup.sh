#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"
KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-isola-dev}"
REGISTRY_NAME="${REGISTRY_NAME:-kind-registry}"
REGISTRY_PORT="${REGISTRY_PORT:-5001}"

echo "=== Isola Development Environment Setup ==="

# Check for required tools
check_tool() {
    local tool="$1"
    local hint="$2"

    if ! command -v "$tool" &> /dev/null; then
        echo "ERROR: $tool is not installed. $hint"
        exit 1
    fi
    echo "  [OK] $tool found"
}

echo ""
echo "Checking prerequisites..."
check_tool "docker" "Install Docker: https://docs.docker.com/get-docker/"
check_tool "kind" "Install Kind: https://kind.sigs.k8s.io/docs/user/quick-start/#installation"
check_tool "kubectl" "Install kubectl: https://kubernetes.io/docs/tasks/tools/"
check_tool "helm" "Install Helm: https://helm.sh/docs/intro/install/"
check_tool "tilt" "Install Tilt: https://docs.tilt.dev/install.html"

# Create local registry if it doesn't exist
echo ""
echo "Setting up local registry..."
if docker inspect "$REGISTRY_NAME" &> /dev/null; then
    echo "  Local registry already exists"
    if ! docker ps -q -f name="$REGISTRY_NAME" | grep -q .; then
        echo "  Starting existing registry..."
        docker start "$REGISTRY_NAME"
    fi
else
    echo "  Creating local registry on localhost:${REGISTRY_PORT}..."
    docker run -d --restart=always -p "${REGISTRY_PORT}:5000" --network bridge --name "$REGISTRY_NAME" registry:2
fi

# Create Kind cluster if it doesn't exist
echo ""
echo "Setting up Kind cluster..."
if kind get clusters 2>/dev/null | grep -q "^${KIND_CLUSTER_NAME}$"; then
    echo "  Cluster '${KIND_CLUSTER_NAME}' already exists"
else
    echo "  Creating cluster '${KIND_CLUSTER_NAME}'..."
    kind create cluster --name "${KIND_CLUSTER_NAME}" --config "${ROOT_DIR}/kind-config.yaml"
fi

# Connect registry to kind network if not already connected
if ! docker network inspect kind 2>/dev/null | grep -q "\"$REGISTRY_NAME\""; then
    echo "  Connecting registry to kind network..."
    docker network connect kind "$REGISTRY_NAME" 2>/dev/null || true
fi

# Configure nodes to use local registry
echo "  Configuring nodes to use local registry..."
for node in $(kind get nodes --name "${KIND_CLUSTER_NAME}" 2>/dev/null); do
    docker exec "$node" mkdir -p /etc/containerd/certs.d/localhost:${REGISTRY_PORT}
    cat <<EOF | docker exec -i "$node" sh -c "cat > /etc/containerd/certs.d/localhost:${REGISTRY_PORT}/hosts.toml"
[host."http://${REGISTRY_NAME}:5000"]
EOF
done

# Create registry configmap for Tilt/other tools to discover
kubectl apply -f - <<EOF
apiVersion: v1
kind: ConfigMap
metadata:
  name: local-registry-hosting
  namespace: kube-public
data:
  localRegistryHosting.v1: |
    host: "localhost:${REGISTRY_PORT}"
    help: "https://kind.sigs.k8s.io/docs/user/local-registry/"
EOF

# Verify cluster is ready
echo ""
echo "Verifying cluster..."
kubectl cluster-info --context "kind-${KIND_CLUSTER_NAME}"
kubectl get nodes

# Setup Python environment for tests
echo ""
echo "Setting up Python test environment..."

# Check for uv (preferred)
if ! command -v uv &> /dev/null; then
    echo "  WARNING: uv not found. Install it: curl -LsSf https://astral.sh/uv/install.sh | sh"
    echo "  Skipping Python environment setup"
else
    echo "  [OK] uv found"

    if [ -d "${ROOT_DIR}/tests/.venv" ]; then
        echo "  Virtual environment already exists"
    else
        echo "  Creating virtual environment with uv..."
        uv venv "${ROOT_DIR}/tests/.venv"
    fi

    echo "  Installing test dependencies..."
    uv pip install --quiet -r "${ROOT_DIR}/tests/requirements.txt" -p "${ROOT_DIR}/tests/.venv"
    echo "  Installed test dependencies"
fi

echo ""
echo "=== Setup Complete ==="
echo ""
echo "Next steps:"
echo "  1. Start development: ./scripts/dev.sh"
echo "  2. Run tests: ./scripts/test-e2e.sh"
echo "  3. Teardown: ./scripts/teardown.sh"
echo ""
