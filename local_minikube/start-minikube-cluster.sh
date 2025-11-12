#!/bin/bash

set -euo pipefail

if ! command -v minikube >/dev/null 2>&1; then
  echo "error: minikube not found, please install it with install-minikube.sh" >&2
  exit 1
fi

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" &> /dev/null && pwd)

echo "starting minikube single-node cluster..."
minikube start --container-runtime=containerd --docker-opt containerd=/var/run/containerd/containerd.sock

# of course, in production we shouldn't put a .key file in the image...
echo "copying current minikube ssl data so it's available for the images built..."
cp ~/.minikube/ca.crt ${SCRIPT_DIR}/ssl/
cp ~/.minikube/profiles/minikube/client.crt ${SCRIPT_DIR}/ssl/
cp ~/.minikube/profiles/minikube/client.key ${SCRIPT_DIR}/ssl/

echo "build images..."
cd ${SCRIPT_DIR}/..
minikube image build -t isola-controller:dev -f services/isola_controller/Dockerfile .
minikube image build -t isola-agent:dev -f services/isola_agent/Dockerfile .

echo "applying local_minikube manifests..."
cd ${SCRIPT_DIR}/
kubectl apply -f ${SCRIPT_DIR}/manifests/

echo "pods:"
kubectl get pods
