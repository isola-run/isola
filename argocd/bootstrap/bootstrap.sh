#!/bin/bash

# Complete ArgoCD Bootstrap Script for Isola Platform
# This script sets up ArgoCD and deploys the Isola applications

set -e

REPO_URL=${REPO_URL:-"https://github.com/yourusername/dev-isola.git"}
ENVIRONMENT=${ENVIRONMENT:-"dev"}
ARGOCD_NAMESPACE="argocd"

echo "========================================="
echo "Isola Platform ArgoCD Bootstrap"
echo "========================================="
echo "Repository: ${REPO_URL}"
echo "Environment: ${ENVIRONMENT}"
echo ""

# Function to wait for resource
wait_for_resource() {
    local resource=$1
    local namespace=$2
    local timeout=${3:-300}
    
    echo "Waiting for ${resource} in namespace ${namespace}..."
    kubectl wait --for=condition=available --timeout=${timeout}s ${resource} -n ${namespace}
}

# Step 1: Install ArgoCD if not already installed
if ! kubectl get namespace ${ARGOCD_NAMESPACE} &> /dev/null; then
    echo "Installing ArgoCD..."
    ./install-argocd.sh --${ENVIRONMENT}
else
    echo "ArgoCD namespace already exists, skipping installation..."
fi

# Step 2: Apply ArgoCD Project
echo ""
echo "Creating ArgoCD Project..."
kubectl apply -f ../projects/isola-project.yaml

# Step 3: Create namespaces for Isola
echo ""
echo "Creating Isola namespaces..."
kubectl apply -f - <<EOF
apiVersion: v1
kind: Namespace
metadata:
  name: isola-control-plane
  labels:
    name: isola-control-plane
    environment: ${ENVIRONMENT}
---
apiVersion: v1
kind: Namespace
metadata:
  name: isola-sandboxes
  labels:
    name: isola-sandboxes
    environment: ${ENVIRONMENT}
EOF

# Step 4: Apply App of Apps
echo ""
echo "Deploying App of Apps for ${ENVIRONMENT}..."
kubectl apply -f ../app-of-apps/${ENVIRONMENT}.yaml

# Step 5: Trigger initial sync
echo ""
echo "Triggering initial sync..."
argocd app sync isola-apps-${ENVIRONMENT} --server localhost:8080 --insecure || {
    echo "ArgoCD CLI not found or sync failed. You can sync manually from the UI."
}

# Step 6: Display status
echo ""
echo "========================================="
echo "Bootstrap Complete!"
echo "========================================="
echo ""
echo "ArgoCD Applications Status:"
kubectl get applications -n ${ARGOCD_NAMESPACE}

echo ""
echo "To access ArgoCD:"
echo "1. Port-forward: kubectl port-forward svc/argocd-server -n ${ARGOCD_NAMESPACE} 8080:443"
echo "2. Get admin password: kubectl -n ${ARGOCD_NAMESPACE} get secret argocd-initial-admin-secret -o jsonpath=\"{.data.password}\" | base64 -d"
echo "3. Login at: https://localhost:8080 with username 'admin'"
echo ""
echo "To watch application sync status:"
echo "watch kubectl get applications -n ${ARGOCD_NAMESPACE}"
