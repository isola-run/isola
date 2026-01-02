#!/bin/bash

# Script exits if any command fails
set -euo pipefail

ROOT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" &> /dev/null && pwd)
MINIKUBE_BIN=${MINIKUBE_BIN:-minikube}
KUBECTL_BIN=${KUBECTL_BIN:-kubectl}
HELM_BIN=${HELM_BIN:-helm}

require_cmd() {
  local bin="$1"
  local hint="$2"

  if ! command -v "${bin}" >/dev/null 2>&1; then
    echo "error: ${bin} not found. ${hint}" >&2
    exit 1
  fi
}

build_image() {
  local image_name=$1
  local dockerfile_path=$2
  local context_dir=$3

  echo "  → building ${image_name}..."
  "${MINIKUBE_BIN}" image rm "${image_name}" >/dev/null 2>&1 || true
  (cd "${context_dir}" && "${MINIKUBE_BIN}" image build -t "${image_name}" -f "${dockerfile_path}" .)

  if ! "${MINIKUBE_BIN}" image ls | grep -q "${image_name}"; then
    echo "ERROR: build failed for ${image_name}" >&2
    exit 1
  fi
}

ensure_minikube() {
  echo "Ensuring minikube is running..."
  if ! "${MINIKUBE_BIN}" status >/dev/null 2>&1; then
    "${MINIKUBE_BIN}" start --container-runtime=containerd --docker-opt containerd=/var/run/containerd/containerd.sock
  fi

  if ! "${MINIKUBE_BIN}" addons list | grep -q "registry.*enabled"; then
    echo "  → enabling registry addon..."
    "${MINIKUBE_BIN}" addons enable registry
  fi

  if ! "${MINIKUBE_BIN}" addons list | grep -q "gvisor.*enabled"; then
    echo "  → enabling gvisor addon..."
    "${MINIKUBE_BIN}" addons enable gvisor
  fi
}

ensure_runtime_class() {
  echo "Ensuring gVisor RuntimeClass exists..."
  "${KUBECTL_BIN}" apply -f "${ROOT_DIR}/local_minikube/manifests/gvisor-runtime-class.yaml"
}

require_cmd "${MINIKUBE_BIN}" "Install it via local_minikube/install-minikube.sh or your package manager."
require_cmd "${KUBECTL_BIN}" "kubectl must be on PATH to apply manifests."
require_cmd "${HELM_BIN}" "helm is required for deploying charts."

ensure_minikube
ensure_runtime_class

echo "Building images..."
build_image "isola-controller:dev" "services/isola_controller/Dockerfile" "${ROOT_DIR}"
build_image "isola-agent:dev" "Dockerfile" "${ROOT_DIR}/services/isola-agent"
build_image "isola-operator:dev" "Dockerfile" "${ROOT_DIR}/services/isola-operator"

# Deploy LocalStack for S3 storage
echo "Deploying LocalStack..."
"${KUBECTL_BIN}" create namespace localstack --dry-run=client -o yaml | "${KUBECTL_BIN}" apply -f -
"${HELM_BIN}" repo add localstack https://localstack.github.io/helm-charts --force-update
"${HELM_BIN}" upgrade --install localstack localstack/localstack -n localstack --create-namespace --wait
"${KUBECTL_BIN}" rollout status deployment/localstack -n localstack --timeout=180s

echo "Creating S3 bucket (if missing)..."
# Use the LocalStack pod to create the bucket (ignore error if it already exists)
"${KUBECTL_BIN}" -n localstack exec deploy/localstack -- \
  awslocal s3api create-bucket --bucket isola-uploads >/dev/null 2>&1 || true

# Deploy with Helm
# Deploy isola-operator first (installs CRDs + operator)
echo "Deploying isola-operator with Helm..."
# todo: we should find a proper solution to that:
echo "Helm doesn't override existing CRDs. For dev, manually delete isola CRDs..."
"${KUBECTL_BIN}" delete crd sandboxes.sandbox.isola.run sandboxtemplates.sandbox.isola.run >/dev/null 2>&1 || true
"${HELM_BIN}" upgrade --install isola-operator charts/isola-operator \
  -f charts/isola-operator/values-dev.yaml \
  -n isola-control-plane \
  --create-namespace \
  --wait
"${KUBECTL_BIN}" wait --for=condition=Established --timeout=90s crd/sandboxes.sandbox.isola.run crd/sandboxtemplates.sandbox.isola.run

echo "Deploying isola-controller with Helm..."
"${HELM_BIN}" upgrade --install isola-controller charts/isola-controller \
  -f charts/isola-controller/values-dev.yaml \
  -n isola-control-plane \
  --create-namespace \
  --wait

# Force pod restart to pick up the new images
echo "Restarting deployments to pick up new images..."
"${KUBECTL_BIN}" rollout restart deployment/isola-operator -n isola-control-plane
"${KUBECTL_BIN}" rollout restart deployment/isola-controller -n isola-control-plane

# Wait for deployments
echo "Waiting for deployments to be ready..."
"${KUBECTL_BIN}" rollout status deployment/isola-operator -n isola-control-plane --timeout=180s
"${KUBECTL_BIN}" rollout status deployment/isola-controller -n isola-control-plane --timeout=180s
