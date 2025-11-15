#!/bin/bash

set -euo pipefail

if ! command -v minikube >/dev/null 2>&1; then
  echo "error: minikube not found, please install it with install-minikube.sh" >&2
  exit 1
fi

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" &> /dev/null && pwd)

echo "starting minikube single-node cluster..."
minikube start --container-runtime=containerd --docker-opt containerd=/var/run/containerd/containerd.sock
if ! minikube addons list | grep -q "gvisor.*enabled"; then
  minikube addons enable gvisor
fi

echo "build images..."
cd ${SCRIPT_DIR}/..
minikube image build -t isola-controller:dev -f services/isola_controller/Dockerfile .
minikube image build -t isola-agent:dev -f services/isola_agent/Dockerfile .

echo "applying manifests in correct order..."
cd ${SCRIPT_DIR}/

echo "  → creating namespaces..."
kubectl apply -f ${SCRIPT_DIR}/manifests/isola-control-plane-namespace.yaml
kubectl apply -f ${SCRIPT_DIR}/manifests/isola-sandboxes-namespace.yaml

echo "  → creating service accounts..."
kubectl apply -f ${SCRIPT_DIR}/manifests/isola-controller-service-account.yaml

echo "  → creating RBAC resources..."
kubectl apply -f ${SCRIPT_DIR}/manifests/isola-controller-role.yaml
kubectl apply -f ${SCRIPT_DIR}/manifests/isola-controller-role-binding.yaml

echo "  → creating services..."
kubectl apply -f ${SCRIPT_DIR}/manifests/isola-controller-service.yaml
kubectl apply -f ${SCRIPT_DIR}/manifests/isola-controller-nodeport.yaml

echo "  → creating/updating deployments..."
kubectl apply -f ${SCRIPT_DIR}/manifests/isola-controller-deployment.yaml
kubectl apply -f ${SCRIPT_DIR}/manifests/isola-agent-deployment.yaml

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

echo -e "\n=== RBAC Configuration ==="
echo "ServiceAccount: isola-controller (isola-control-plane)"
echo "Role: sandboxes-manager (isola-sandboxes)"
echo "RoleBinding: isola-controller-sandboxes-binding (isola-sandboxes)"

echo -e "\nDeployment complete! ✓"
