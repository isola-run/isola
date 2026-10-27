#!/bin/bash
# Copyright The Isola Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"
KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-isola-dev}"
REGISTRY_NAME="${REGISTRY_NAME:-kind-registry}"
REGISTRY_PORT="${REGISTRY_PORT:-5001}"

# Tool versions (keep in sync with CI workflows)
GOLANGCI_LINT_VERSION="v2.15.0"
GOVULNCHECK_VERSION="v1.9.0"
SETUP_ENVTEST_VERSION="v0.25.2"
CONTROLLER_GEN_VERSION="v0.23.0"
LEFTHOOK_VERSION="v2.2.2"

TOOLS_ONLY=0
case "$*" in
    "") ;;
    --tools-only) TOOLS_ONLY=1 ;;
    -h|--help)
        echo "Usage: $0 [--tools-only]"
        echo "  --tools-only  Install pinned Go tools without setting up the cluster"
        exit 0
        ;;
    *)
        echo "Usage: $0 [--tools-only]" >&2
        exit 1
        ;;
esac

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

install_go_tool() {
    local tool="$1"
    local package="$2"
    local version="$3"

    echo "  Installing $tool $version with Go $GO_VERSION..."
    go install "${package}@${version}"
}

echo ""
echo "Checking prerequisites..."
check_tool "go" "Install Go: https://go.dev/dl/"
if [ "$TOOLS_ONLY" = "0" ]; then
    check_tool "docker" "Install Docker: https://docs.docker.com/get-docker/"
    check_tool "kind" "Install Kind: https://kind.sigs.k8s.io/docs/user/quick-start/#installation"
    check_tool "kubectl" "Install kubectl: https://kubernetes.io/docs/tasks/tools/"
    check_tool "helm" "Install Helm: https://helm.sh/docs/intro/install/"
    check_tool "tilt" "Install Tilt: https://docs.tilt.dev/install.html"
fi

GO_VERSION=$(awk '$1 == "go" {print $2; exit}' "${ROOT_DIR}/go.mod")
export GOTOOLCHAIN="go${GO_VERSION}"
TOOL_BIN=$(go env GOBIN)
if [ -z "$TOOL_BIN" ]; then
    TOOL_BIN=$(go env GOPATH)
    TOOL_BIN="${TOOL_BIN%%:*}/bin"
fi
export PATH="${TOOL_BIN}:${PATH}"

echo ""
echo "Installing development tools..."
install_go_tool "golangci-lint" "github.com/golangci/golangci-lint/v2/cmd/golangci-lint" "$GOLANGCI_LINT_VERSION"
install_go_tool "govulncheck" "golang.org/x/vuln/cmd/govulncheck" "$GOVULNCHECK_VERSION"
install_go_tool "lefthook" "github.com/evilmartians/lefthook/v2" "$LEFTHOOK_VERSION"
install_go_tool "setup-envtest" "sigs.k8s.io/controller-runtime/tools/setup-envtest" "$SETUP_ENVTEST_VERSION"
install_go_tool "controller-gen" "sigs.k8s.io/controller-tools/cmd/controller-gen" "$CONTROLLER_GEN_VERSION"

if [ "$TOOLS_ONLY" = "1" ]; then
    echo ""
    echo "Tools installed in ${TOOL_BIN}. Keep this directory first on PATH."
    exit 0
fi

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
    docker run -d --restart=always -p "127.0.0.1:${REGISTRY_PORT}:5000" --network bridge --name "$REGISTRY_NAME" registry:2
fi

echo ""
echo "Setting up Kind cluster..."
if kind get clusters 2>/dev/null | grep -q "^${KIND_CLUSTER_NAME}$"; then
    echo "  Cluster '${KIND_CLUSTER_NAME}' already exists"
else
    echo "  Creating cluster '${KIND_CLUSTER_NAME}'..."
    kind create cluster --name "${KIND_CLUSTER_NAME}" --config "${SCRIPT_DIR}/kind-config.yaml"
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

# gVisor and its RuntimeClass are installed by the chart's gvisor-installer
# DaemonSet (enabled in values-dev.yaml) once tilt deploys isola.

echo ""
echo "Verifying cluster..."
kubectl cluster-info --context "kind-${KIND_CLUSTER_NAME}"
kubectl get nodes

echo ""
echo "Setting kubectl context..."
kubectl config use-context "kind-${KIND_CLUSTER_NAME}"

echo ""
echo "Setting up pre-commit hooks..."
cd "${ROOT_DIR}" && lefthook install
echo "  [OK] Lefthook hooks installed"

echo ""
echo "=== Setup Complete ==="
echo ""
echo "Next steps:"
echo "  1. Start development:  tilt up"
echo "  2. Run Go tests:       make test"
echo "  3. Run E2E tests:      make test-e2e"
echo "  4. Teardown:           kind delete cluster --name ${KIND_CLUSTER_NAME}"
echo ""
