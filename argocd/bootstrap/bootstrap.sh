#!/bin/bash

# Complete ArgoCD Bootstrap Script for Isola Platform
# This script sets up ArgoCD and deploys the Isola applications

set -e

REPO_URL=${REPO_URL:-"https://github.com/omereli/dev-isola.git"}
ENVIRONMENT=${ENVIRONMENT:-"dev"}
ARGOCD_NAMESPACE="argocd"

echo "Isola Platform ArgoCD Bootstrap"

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

# Step 4: Apply individual ArgoCD Applications
echo ""
echo "Deploying ArgoCD Applications for ${ENVIRONMENT}..."

# Apply all application manifests for the environment
for app_file in ../applications/*-${ENVIRONMENT}.yaml; do
    if [ -f "$app_file" ]; then
        echo "Applying $(basename $app_file)..."
        kubectl apply -f "$app_file"
    fi
done

# Step 5: Wait for applications to be created
echo ""
echo "Waiting for applications to be registered..."
sleep 5

# List deployed applications
echo ""
echo "Deployed Applications:"
kubectl get applications -n ${ARGOCD_NAMESPACE} -o custom-columns=NAME:.metadata.name,SYNC:.status.sync.status,HEALTH:.status.health.status,REVISION:.status.sync.revision


# Step 6: Display status

echo "Bootstrap Complete!"
echo "ArgoCD Applications Status:"
kubectl get applications -n ${ARGOCD_NAMESPACE}