#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"
KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-isola-dev}"
REGISTRY_NAME="${REGISTRY_NAME:-kind-registry}"
REGISTRY_PORT="${REGISTRY_PORT:-5001}"
GVISOR_URL="https://storage.googleapis.com/gvisor/releases/release/latest"

echo "=== Isola Development Environment Setup ==="

check_tool() {
    local tool="$1"
    local hint="$2"

    if ! command -v "$tool" &> /dev/null; then
        echo "ERROR: $tool is not installed. $hint"
        exit 1
    fi
    echo "  [OK] $tool found"
}

# Install gVisor (runsc) in a Kind node and configure containerd
# https://gvisor.dev/docs/user_guide/install/
# https://gvisor.dev/docs/user_guide/containerd/quick_start/
install_gvisor_in_node() {
    local node="$1"

    echo "  Installing gVisor in node: $node"

    local arch
    arch=$(docker exec "$node" uname -m)
    case "$arch" in
        x86_64) arch="x86_64" ;;
        aarch64) arch="aarch64" ;;
        *) echo "ERROR: Unsupported architecture: $arch"; exit 1 ;;
    esac

    local gvisor_url="${GVISOR_URL}/${arch}"
    docker exec "$node" sh -c "
        set -e
        cd /tmp
        curl -fsSL '${gvisor_url}/runsc' -o runsc
        curl -fsSL '${gvisor_url}/runsc.sha512' -o runsc.sha512
        curl -fsSL '${gvisor_url}/containerd-shim-runsc-v1' -o containerd-shim-runsc-v1
        curl -fsSL '${gvisor_url}/containerd-shim-runsc-v1.sha512' -o containerd-shim-runsc-v1.sha512
        sha512sum -c runsc.sha512
        sha512sum -c containerd-shim-runsc-v1.sha512
        rm -f *.sha512
        chmod a+rx runsc containerd-shim-runsc-v1
        mv runsc containerd-shim-runsc-v1 /usr/local/bin/
    "

    if ! docker exec "$node" grep -q 'plugins.*containerd.*runtimes.*runsc' /etc/containerd/config.toml 2>/dev/null; then
        docker exec "$node" sh -c 'cat >> /etc/containerd/config.toml << "TOML"

# gVisor (runsc) runtime configuration
# References:
#   - https://gvisor.dev/docs/user_guide/containerd/configuration/
#   - https://github.com/google/gvisor/issues/3494 (per-sandbox flag overrides via annotations)
#   - https://github.com/google/gvisor/commit/a53b22ad5283b00b766178eff847c3193c1293b7 (overlay2 self medium)
[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.runsc]
  runtime_type = "io.containerd.runsc.v1"
  # Allow gVisor annotations to pass through to runsc.
  # Required for dev.gvisor.flag.* annotations (e.g., overlay2) to work.
  # By default containerd filters out all annotations; this allowlist enables gVisor-specific ones.
  pod_annotations = ["dev.gvisor.*"]
TOML'
    fi

    # Restart containerd to pick up the new runtime
    docker exec "$node" systemctl restart containerd

    if docker exec "$node" runsc --version &>/dev/null; then
        local version
        version=$(docker exec "$node" runsc --version 2>&1 | head -1)
        echo "    [OK] gVisor installed: $version"
    else
        echo "    [WARN] gVisor installation may have issues"
    fi
}

echo ""
echo "Checking prerequisites..."
check_tool "docker" "Install Docker: https://docs.docker.com/get-docker/"
check_tool "kind" "Install Kind: https://kind.sigs.k8s.io/docs/user/quick-start/#installation"
check_tool "kubectl" "Install kubectl: https://kubernetes.io/docs/tasks/tools/"
check_tool "helm" "Install Helm: https://helm.sh/docs/intro/install/"
check_tool "tilt" "Install Tilt: https://docs.tilt.dev/install.html"

# https://kind.sigs.k8s.io/docs/user/local-registry/
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

echo ""
echo "Setting up Kind cluster..."
if kind get clusters 2>/dev/null | grep -q "^${KIND_CLUSTER_NAME}$"; then
    echo "  Cluster '${KIND_CLUSTER_NAME}' already exists"
else
    echo "  Creating cluster '${KIND_CLUSTER_NAME}'..."
    kind create cluster --name "${KIND_CLUSTER_NAME}" --config "${ROOT_DIR}/kind-config.yaml"
    echo "  Cluster '${KIND_CLUSTER_NAME}' created."
    if [ "${KIND_CLUSTER_NAME}" != "isola-dev" ]; then
        echo "  If you customized the name, update allow_k8s_contexts in Tiltfile to match 'kind-${KIND_CLUSTER_NAME}'."
    fi
fi

if ! docker network inspect kind 2>/dev/null | grep -q "\"$REGISTRY_NAME\""; then
    echo "  Connecting registry to kind network..."
    docker network connect kind "$REGISTRY_NAME" 2>/dev/null || true
fi

echo "  Configuring nodes to use local registry..."
for node in $(kind get nodes --name "${KIND_CLUSTER_NAME}" 2>/dev/null); do
    docker exec "$node" mkdir -p /etc/containerd/certs.d/localhost:${REGISTRY_PORT}
    cat <<EOF | docker exec -i "$node" sh -c "cat > /etc/containerd/certs.d/localhost:${REGISTRY_PORT}/hosts.toml"
[host."http://${REGISTRY_NAME}:5000"]
EOF
done

echo ""
echo "Installing gVisor runtime in cluster nodes..."
for node in $(kind get nodes --name "${KIND_CLUSTER_NAME}" 2>/dev/null); do
    install_gvisor_in_node "$node"
done

# Create registry configmap for Tilt: https://github.com/kubernetes/enhancements/tree/master/keps/sig-cluster-lifecycle/generic/1755-communicating-a-local-registry
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

echo ""
echo "Creating gVisor RuntimeClass..."
kubectl apply -f - <<EOF
apiVersion: node.k8s.io/v1
kind: RuntimeClass
metadata:
  name: gvisor
handler: runsc
EOF

echo ""
echo "Verifying cluster..."
kubectl cluster-info --context "kind-${KIND_CLUSTER_NAME}"
kubectl get nodes

echo ""
echo "Setting kubectl context..."
kubectl config use-context "kind-${KIND_CLUSTER_NAME}"

echo ""
echo "Setting up pre-commit hooks..."
LEFTHOOK_BIN="${GOPATH:-$(go env GOPATH)}/bin/lefthook"
if command -v lefthook &> /dev/null; then
    LEFTHOOK_BIN="lefthook"
fi
if [ -x "$LEFTHOOK_BIN" ]; then
    if [ -f "${ROOT_DIR}/.lefthook.yml" ]; then
        cd "${ROOT_DIR}"
        "$LEFTHOOK_BIN" install
        echo "  [OK] Lefthook hooks installed"
    fi
else
    echo "  [WARN] lefthook not found - pre-commit hooks will not be available"
    echo "  To install: go install github.com/evilmartians/lefthook/v2@v2.0.13"
    echo "  Then run: \$(go env GOPATH)/bin/lefthook install"
fi

echo ""
echo "=== Setup Complete ==="
echo ""
echo "Next steps:"
echo "  1. Start development:  tilt up"
echo "  2. Run tests:    cd tests && uv run pytest"
echo "  3. Teardown:           kind delete cluster --name isola-dev"
echo ""
