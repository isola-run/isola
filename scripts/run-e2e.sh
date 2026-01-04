#!/bin/bash
set -euo pipefail

# Configuration
NAMESPACE="isola-control-plane"
SERVICE_NAME="isola-controller-nodeport"
TEST_DIR="services/isola_controller"
READY_TIMEOUT_SECONDS=180
PF_PID=""

# Function to cleanup background processes
cleanup() {
    if [ -n "$PF_PID" ]; then
        echo "Stopping port-forward (PID: $PF_PID)..."
        kill $PF_PID 2>/dev/null || true
    fi
}
trap cleanup EXIT

echo "⏳ Waiting for control plane to be ready..."
if ! kubectl get crd sandboxes.sandbox.isola.run >/dev/null 2>&1; then
    echo "❌ Sandbox CRDs are missing. Run ./deploy.sh first." >&2
    exit 1
fi

kubectl wait --for=condition=Established --timeout=60s crd/sandboxes.sandbox.isola.run crd/sandboxtemplates.sandbox.isola.run
kubectl rollout status deployment/isola-operator -n $NAMESPACE --timeout=${READY_TIMEOUT_SECONDS}s
kubectl rollout status deployment/isola-controller -n $NAMESPACE --timeout=${READY_TIMEOUT_SECONDS}s

if ! kubectl get svc -n $NAMESPACE $SERVICE_NAME >/dev/null 2>&1; then
    echo "❌ Service $SERVICE_NAME not found in namespace $NAMESPACE" >&2
    exit 1
fi

echo "🔍 Detecting Gateway URL..."

GATEWAY_URL=""

# Method 1: Try Minikube (if available and running)
if command -v minikube &> /dev/null; then
    if minikube status &> /dev/null; then
        echo "   Minikube detected."
        # Get the URL directly
        URL=$(minikube service $SERVICE_NAME -n $NAMESPACE --url 2>/dev/null | head -n 1 || true)
        if [ -n "$URL" ]; then
            GATEWAY_URL="$URL"
            echo "   ✅ Found Gateway via Minikube: $GATEWAY_URL"
        fi
    fi
fi

# Method 2: Fallback to Port Forwarding
if [ -z "$GATEWAY_URL" ]; then
    echo "   Minikube service URL not found. Falling back to port-forward..."
    
    # Find a random free port
    LOCAL_PORT=30080
    
    echo "   Forwarding port $LOCAL_PORT -> service/$SERVICE_NAME..."
    kubectl port-forward -n $NAMESPACE service/$SERVICE_NAME $LOCAL_PORT:$LOCAL_PORT > /dev/null 2>&1 &
    PF_PID=$!
    
    # Wait for port forward to be ready
    sleep 3
    
    GATEWAY_URL="http://127.0.0.1:$LOCAL_PORT"
    echo "   ✅ Port-forward established at: $GATEWAY_URL"
fi

if [ -z "$GATEWAY_URL" ]; then
    echo "❌ Failed to determine Gateway URL."
    exit 1
fi

echo "🚀 Running E2E Tests against $GATEWAY_URL..."

# Install dependencies if needed (assuming user has python/pip)
# Use --break-system-packages for this dev environment, or ideally use a venv
echo "   Installing dependencies..."
python3 -m pip install pytest requests --quiet --break-system-packages

# Run the test
export GATEWAY_URL="$GATEWAY_URL"
cd "$TEST_DIR"
python3 -m pytest test_sandbox_e2e.py -v -s
