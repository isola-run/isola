#!/bin/bash

# Script exists if any command fails
set -euo pipefail

# Build the Docker images
echo "Building isola-controller Docker image..."
minikube image build -t isola-controller:dev -f services/isola_controller/Dockerfile .

echo "Building isola-agent Docker image..."
minikube image build -t isola-agent:dev -f services/isola_agent/Dockerfile .

# Deploy with Helm
echo "Deploying with Helm..."
helm upgrade --install isola-controller charts/isola-controller \
  -f charts/isola-controller/values-dev.yaml \
  -n isola-control-plane \
  --create-namespace

# Force pod restart to pick up the new image
echo "Restarting deployment to pick up new image..."
kubectl rollout restart deployment/isola-controller -n isola-control-plane

# Wait for deployment
echo "Waiting for deployment to be ready..."
kubectl rollout status deployment/isola-controller -n isola-control-plane --timeout=60s
