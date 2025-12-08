#!/bin/bash

# Complete ArgoCD Bootstrap Script for Isola Platform
# This script sets up ArgoCD and deploys the Isola applications

set -e

REPO_URL=${REPO_URL:-"https://github.com/omereli/dev-isola.git"}
ENVIRONMENT=${ENVIRONMENT:-"dev"}
ARGOCD_NAMESPACE="argocd"
SSH_PRIVATE_KEY_PATH=${SSH_PRIVATE_KEY_PATH:-"~/.ssh/id_ed25519"}

echo "Isola Platform ArgoCD Bootstrap"

# Step 1: Install ArgoCD if not already installed
if ! kubectl get namespace ${ARGOCD_NAMESPACE} &> /dev/null; then
    echo "Installing ArgoCD..."
    ./install-argocd.sh --${ENVIRONMENT}
else
    echo "ArgoCD namespace already exists, skipping installation..."
fi

# Step 2: Configure Git repository access (if SSH key is provided)
if [ -n "${SSH_PRIVATE_KEY_PATH}" ] && [ -f "${SSH_PRIVATE_KEY_PATH/#\~/$HOME}" ] && command -v argocd &> /dev/null; then
    echo ""
    echo "Configuring Git repository access..."
    # Convert HTTPS URL to SSH format
    SSH_REPO_URL=$(echo ${REPO_URL} | sed 's|https://github.com/|git@github.com:|')
    # Expand ~ to home directory
    EXPANDED_KEY_PATH="${SSH_PRIVATE_KEY_PATH/#\~/$HOME}"
    
    # Port-forward ArgoCD server
    kubectl port-forward svc/argocd-server -n ${ARGOCD_NAMESPACE} 8080:443 > /dev/null 2>&1 &
    PORT_FORWARD_PID=$!
    sleep 3
    
    # Get admin password and login
    ARGOCD_PASSWORD=$(kubectl -n ${ARGOCD_NAMESPACE} get secret argocd-initial-admin-secret -o jsonpath="{.data.password}" | base64 -d 2>/dev/null || echo "")
    if [ -n "${ARGOCD_PASSWORD}" ]; then
        argocd login localhost:8080 --insecure --username admin --password "${ARGOCD_PASSWORD}" > /dev/null 2>&1 && \
        argocd repo add ${SSH_REPO_URL} --ssh-private-key-path "${EXPANDED_KEY_PATH}" --insecure-skip-server-verification > /dev/null 2>&1 && \
        echo "Repository configured successfully" || echo "Repository configuration skipped (may already exist)"
    fi
    
    kill $PORT_FORWARD_PID 2>/dev/null || true
fi

# Step 3: Apply ArgoCD Project
echo ""
echo "Creating ArgoCD Project..."
kubectl apply -f ../projects/isola-project.yaml

# Step 4: Create namespaces for Isola
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

# Step 5: Apply individual ArgoCD Applications
echo ""
echo "Deploying ArgoCD Applications for ${ENVIRONMENT}..."

# Apply all application manifests for the environment
for app_file in ../applications/*-${ENVIRONMENT}.yaml; do
    if [ -f "$app_file" ]; then
        echo "Applying $(basename $app_file)..."
        kubectl apply -f "$app_file"
    fi
done

# Step 6: Wait for applications to be created
echo ""
echo "Waiting for applications to be registered..."
sleep 5

# List deployed applications
echo ""
echo "Deployed Applications:"
kubectl get applications -n ${ARGOCD_NAMESPACE} -o custom-columns=NAME:.metadata.name,SYNC:.status.sync.status,HEALTH:.status.health.status,REVISION:.status.sync.revision


# Step 7: Display status

echo "Bootstrap Complete!"
echo "ArgoCD Applications Status:"
kubectl get applications -n ${ARGOCD_NAMESPACE}