#!/bin/bash

# Quick deployment script for Isola Controller
# This is a simplified version for quick deployments to Minikube

set -e

echo "🚀 Quick Deploy - Isola Controller to Minikube"

# Check if minikube is running
if ! minikube status | grep -q "Running" 2>/dev/null; then
    echo "⚠️  Minikube is not running. Starting minikube..."
    minikube start
fi

# Use minikube's docker daemon
eval $(minikube docker-env)

# Build controller image
echo "🔨 Building controller image..."
docker build -t isola-controller:dev -f services/isola_controller/Dockerfile .

# Build agent image
echo "🔨 Building agent image..."
docker build -t isola-agent:dev -f services/isola_agent/Dockerfile .

# Create namespace
kubectl create namespace isola-system --dry-run=client -o yaml | kubectl apply -f -

# Apply manifests
echo "📦 Deploying to Kubernetes..."
kubectl apply -n isola-system -f local_minikube/manifests/

# Restart deployments to pick up new images
kubectl rollout restart deployment/isola-controller -n isola-system
kubectl rollout restart deployment/isola-agent -n isola-system

# Wait for rollout
echo "⏳ Waiting for deployment..."
kubectl rollout status deployment/isola-controller -n isola-system --timeout=120s

# Show access info
MINIKUBE_IP=$(minikube ip)
echo ""
echo "✅ Deployment complete!"
echo ""
echo "📡 Access the controller at: http://$MINIKUBE_IP:30080"
echo ""
echo "Or use port-forward for localhost access:"
echo "kubectl port-forward -n isola-system svc/isola-controller-service 3000:30080"
echo ""
echo "View logs with:"
echo "kubectl logs -n isola-system -l app=isola-controller -f"
