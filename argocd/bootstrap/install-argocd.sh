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

# Patch ArgoCD server for insecure mode (development only)
if [ "$1" == "--dev" ]; then
  echo "Patching ArgoCD server for development mode..."
  kubectl patch svc argocd-server -n ${NAMESPACE} -p '{"spec": {"type": "NodePort"}}'
  kubectl patch deployment argocd-server -n ${NAMESPACE} --type='json' -p='[{"op": "add", "path": "/spec/template/spec/containers/0/command/-", "value": "--insecure"}]'
fi

# Apply ingress if it exists
if [ -f "argocd-ingress.yaml" ]; then
  echo "Applying ArgoCD ingress..."
  kubectl apply -f argocd-ingress.yaml
fi

# Get initial admin password
echo ""
echo "ArgoCD has been installed successfully!"
echo ""
echo "To get the initial admin password, run:"
echo "kubectl -n ${NAMESPACE} get secret argocd-initial-admin-secret -o jsonpath=\"{.data.password}\" | base64 -d"
echo ""
echo "To access ArgoCD UI:"
if [ "$1" == "--dev" ]; then
  NODE_PORT=$(kubectl get svc argocd-server -n ${NAMESPACE} -o jsonpath='{.spec.ports[?(@.name=="https")].nodePort}')
  echo "Access ArgoCD at: https://localhost:${NODE_PORT}"
  echo "Or port-forward: kubectl port-forward svc/argocd-server -n ${NAMESPACE} 8080:443"
else
  echo "Port-forward: kubectl port-forward svc/argocd-server -n ${NAMESPACE} 8080:443"
  echo "Then access: https://localhost:8080"
fi
echo ""
echo "Login with username: admin"
