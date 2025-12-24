#!/bin/bash

set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" &> /dev/null && pwd)
MINIKUBE_BIN=${MINIKUBE_BIN:-minikube}

require_cmd() {
  local bin="$1"
  local hint="$2"

  if ! command -v "${bin}" >/dev/null 2>&1; then
    echo "error: ${bin} not found. ${hint}" >&2
    exit 1
  fi
}

require_cmd "${MINIKUBE_BIN}" "Install it via ${SCRIPT_DIR}/install-minikube.sh or set MINIKUBE_BIN to your minikube binary."
require_cmd kubectl "kubectl must be on PATH to apply manifests."
require_cmd make "make is required for the isola-operator deploy step."

echo "starting minikube single-node cluster..."
"${MINIKUBE_BIN}" start --container-runtime=containerd --docker-opt containerd=/var/run/containerd/containerd.sock
if ! "${MINIKUBE_BIN}" addons list | grep -q "gvisor.*enabled"; then
  "${MINIKUBE_BIN}" addons enable gvisor
fi
if ! "${MINIKUBE_BIN}" addons list | grep -q "registry.*enabled"; then
  echo "  → enabling registry addon..."
  "${MINIKUBE_BIN}" addons enable registry
fi

build_and_verify_image() {
  local IMAGE_NAME=$1
  local DOCKERFILE_PATH=$2
  local CONTEXT=${3:-.}
  
  echo "  → removing old ${IMAGE_NAME}..."
  "${MINIKUBE_BIN}" image rm "${IMAGE_NAME}" 2>/dev/null || true
  
  echo "  → building ${IMAGE_NAME}..."

  local DOCKERFILE_IN_CONTEXT=$(basename "${DOCKERFILE_PATH}")
  if [[ "${CONTEXT}" == "." ]]; then
    DOCKERFILE_IN_CONTEXT="${DOCKERFILE_PATH}"
  else
    # Verify Dockerfile exists in context
    if [[ ! -f "${CONTEXT}/${DOCKERFILE_IN_CONTEXT}" ]]; then
       echo "Warning: ${DOCKERFILE_IN_CONTEXT} not found in ${CONTEXT}, using full path"
       DOCKERFILE_IN_CONTEXT="${DOCKERFILE_PATH}"
    fi
  fi

  "${MINIKUBE_BIN}" image build  -t "${IMAGE_NAME}" -f "${DOCKERFILE_IN_CONTEXT}" "${CONTEXT}"
  
  # https://github.com/kubernetes/minikube/issues/16576
  # Verify image exists because build failures don't return non-zero exit codes
  if ! "${MINIKUBE_BIN}" image ls | grep -q "${IMAGE_NAME}"; then
    echo "ERROR: Build failed for ${IMAGE_NAME}" >&2
    return 1
  fi
  
  echo "✓ ${IMAGE_NAME} built successfully"
}

echo "building images..."
cd "${SCRIPT_DIR}/.."
build_and_verify_image "isola-controller:dev" "services/isola_controller/Dockerfile" "."
build_and_verify_image "isola-agent:dev" "services/isola_agent/Dockerfile" "."
build_and_verify_image "isola-operator:dev" "services/isola-operator/Dockerfile" "services/isola-operator"

echo "applying manifests in correct order..."
cd "${SCRIPT_DIR}/"

echo "  → creating namespaces..."
kubectl apply -f "${SCRIPT_DIR}/manifests/isola-control-plane-namespace.yaml"
kubectl apply -f "${SCRIPT_DIR}/manifests/isola-sandboxes-namespace.yaml"

echo "  → creating runtime class..."
kubectl apply -f "${SCRIPT_DIR}/manifests/gvisor-runtime-class.yaml"

echo "  → creating service accounts..."
kubectl apply -f "${SCRIPT_DIR}/manifests/isola-controller-service-account.yaml"

echo "  → creating RBAC resources..."
kubectl apply -f "${SCRIPT_DIR}/manifests/isola-controller-role.yaml"
kubectl apply -f "${SCRIPT_DIR}/manifests/isola-controller-role-binding.yaml"

echo "  → creating services..."
kubectl apply -f "${SCRIPT_DIR}/manifests/isola-controller-service.yaml"
kubectl apply -f "${SCRIPT_DIR}/manifests/isola-controller-nodeport.yaml"

echo "  → installing isola-operator CRDs..."
kubectl apply -f "${SCRIPT_DIR}/../services/isola-operator/config/crd/bases/"

echo "  → deploying isola-operator..."
cd "${SCRIPT_DIR}/../services/isola-operator"
make deploy IMG=isola-operator:dev
cd "${SCRIPT_DIR}"

echo "  → creating/updating deployments..."
kubectl apply -f "${SCRIPT_DIR}/manifests/isola-controller-deployment.yaml"
kubectl apply -f "${SCRIPT_DIR}/manifests/isola-agent-deployment.yaml"

echo "forcing pod restart to pick up new images..."
kubectl rollout restart deployment/isola-controller -n isola-control-plane
kubectl rollout restart deployment/isola-agent -n isola-control-plane

echo "waiting for deployments to be ready..."
kubectl rollout status deployment/isola-controller -n isola-control-plane --timeout=60s
kubectl rollout status deployment/isola-agent -n isola-control-plane --timeout=60s

echo -e "\n=== Pods in isola-control-plane ==="
kubectl get pods -n isola-control-plane -o wide

echo -e "\n=== Services in isola-control-plane ==="
kubectl get svc -n isola-control-plane

echo -e "\nDeployment complete! ✓"
