#!/bin/bash

# ArgoCD Installation Script
# This script installs ArgoCD in your Kubernetes cluster

set -e

ARGOCD_VERSION=${ARGOCD_VERSION:-stable}
NAMESPACE="argocd"

echo "Installing ArgoCD version: ${ARGOCD_VERSION}"

# Create namespace
echo "Creating ArgoCD namespace..."
kubectl apply -f argocd-namespace.yaml

# Install ArgoCD
echo "Installing ArgoCD..."
kubectl apply -n ${NAMESPACE} -f https://raw.githubusercontent.com/argoproj/argo-cd/${ARGOCD_VERSION}/manifests/install.yaml

# Wait for ArgoCD to be ready
echo "Waiting for ArgoCD to be ready..."
kubectl wait --for=condition=available --timeout=600s \
  deployment/argocd-server \
  deployment/argocd-repo-server \
  deployment/argocd-redis \
  deployment/argocd-dex-server \
  deployment/argocd-applicationset-controller \
  deployment/argocd-notifications-controller \
  -n ${NAMESPACE}

echo "ArgoCD installed successfully!"