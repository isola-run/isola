#!/bin/bash

set -euo pipefail

if ! command -v minikube >/dev/null 2>&1; then
  echo "error: minikube not found, please install it with install-minikube.sh" >&2
  exit 1
fi

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" &> /dev/null && pwd)

echo "starting minikube single-node cluster..."
minikube start

echo "build images..."
cd ${SCRIPT_DIR}/..
minikube image build -t isola-controller:dev -f services/isola_controller/Dockerfile .
minikube image build -t isola-agent:dev -f services/isola_agent/Dockerfile .

echo "applying local_minikube manifests..."
cd ${SCRIPT_DIR}/
kubectl apply -f ${SCRIPT_DIR}/manifests/

echo "pods:"
kubectl get pods
