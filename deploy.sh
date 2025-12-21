#!/bin/bash

# Script exists if any command fails
set -euo pipefail

# Build the Docker image
echo "Building Docker image..."
minikube image build -t isola-controller:dev -f services/isola_controller/Dockerfile .

# Deploy with Helm
echo "Deploying with Helm..."
helm upgrade --install isola-controller charts/isola-controller \
  -f charts/isola-controller/values-dev.yaml \
  -n isola-control-plane \
  --create-namespace

# Wait for deployment
echo "Waiting for deployment to be ready..."
kubectl rollout status deployment/isola-controller -n isola-control-plane --timeout=60s
