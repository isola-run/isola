#!/bin/bash

# Script exits if any command fails
set -euo pipefail

# Build the Docker images
echo "Building isola-controller Docker image..."
minikube image build -t isola-controller:dev -f services/isola_controller/Dockerfile .

echo "Building isola-agent Docker image..."
minikube image build -t isola-agent:dev -f services/isola_agent/Dockerfile .

echo "Building isola-operator Docker image..."
(cd services/isola-operator && minikube image build -t isola-operator:dev .)

# Deploy LocalStack for S3 storage
echo "Deploying LocalStack..."
kubectl create namespace localstack --dry-run=client -o yaml | kubectl apply -f -
helm repo add localstack https://localstack.github.io/helm-charts --force-update
helm upgrade --install localstack localstack/localstack -n localstack --wait

# Create S3 bucket
echo "Creating S3 bucket..."
kubectl run aws-cli --rm -i --restart=Never -n localstack \
  --image=amazon/aws-cli \
  --env="AWS_ACCESS_KEY_ID=test" \
  --env="AWS_SECRET_ACCESS_KEY=test" \
  -- s3api create-bucket --bucket isola-uploads --endpoint-url http://localstack:4566 --region us-east-1 || true

# Deploy with Helm
# Deploy isola-operator first (installs CRDs + operator)
echo "Deploying isola-operator with Helm..."
helm upgrade --install isola-operator charts/isola-operator \
  -f charts/isola-operator/values-dev.yaml \
  -n isola-control-plane \
  --create-namespace

echo "Deploying isola-controller with Helm..."
helm upgrade --install isola-controller charts/isola-controller \
  -f charts/isola-controller/values-dev.yaml \
  -n isola-control-plane \
  --create-namespace

# Force pod restart to pick up the new images
echo "Restarting deployments to pick up new images..."
kubectl rollout restart deployment/isola-operator -n isola-control-plane
kubectl rollout restart deployment/isola-controller -n isola-control-plane

# Wait for deployments
echo "Waiting for deployments to be ready..."
kubectl rollout status deployment/isola-operator -n isola-control-plane --timeout=60s
kubectl rollout status deployment/isola-controller -n isola-control-plane --timeout=60s
