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
GOLANGCI_LINT_VERSION="v2.9.0"
GOVULNCHECK_VERSION="v1.1.4"
SETUP_ENVTEST_VERSION="release-0.24"
CONTROLLER_GEN_VERSION="v0.20.0"
LEFTHOOK_VERSION="v2.0.15"

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

check_optional_tool() {
    local tool="$1"
    local hint="$2"

    if ! command -v "$tool" &> /dev/null; then
        echo "  [SKIP] $tool not found (optional). $hint"
        return 1
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
if check_optional_tool "golangci-lint" "Install: go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${GOLANGCI_LINT_VERSION}"; then
    installed_version=$(golangci-lint version --short 2>/dev/null || echo "unknown")
    if [ "$installed_version" != "${GOLANGCI_LINT_VERSION#v}" ]; then
        echo "    [WARN] Version mismatch: installed $installed_version, expected ${GOLANGCI_LINT_VERSION#v}"
    fi
fi
if check_optional_tool "govulncheck" "Install: go install golang.org/x/vuln/cmd/govulncheck@${GOVULNCHECK_VERSION}"; then
    installed_version=$(govulncheck -version 2>&1 | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | head -1 || echo "unknown")
    if [ "$installed_version" != "$GOVULNCHECK_VERSION" ]; then
        echo "    [WARN] Version mismatch: installed $installed_version, expected $GOVULNCHECK_VERSION"
    fi
fi
check_optional_tool "lefthook" "Install: go install github.com/evilmartians/lefthook/v2@${LEFTHOOK_VERSION}" && HAS_LEFTHOOK=1

check_optional_tool "setup-envtest" "Install: go install sigs.k8s.io/controller-runtime/tools/setup-envtest@${SETUP_ENVTEST_VERSION}"
check_optional_tool "controller-gen" "Install: go install sigs.k8s.io/controller-tools/cmd/controller-gen@${CONTROLLER_GEN_VERSION}"

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

if [ "${HAS_LEFTHOOK:-}" = "1" ]; then
    echo ""
    echo "Setting up pre-commit hooks..."
    cd "${ROOT_DIR}" && lefthook install
    echo "  [OK] Lefthook hooks installed"
fi

echo ""
echo "=== Setup Complete ==="
echo ""
echo "Next steps:"
echo "  1. Start development:  tilt up"
echo "  2. Run Go tests:       make test"
echo "  3. Run E2E tests:      make test-e2e"
echo "  4. Teardown:           kind delete cluster --name ${KIND_CLUSTER_NAME}"
echo ""
