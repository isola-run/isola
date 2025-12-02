#!/bin/bash

# Deploy script for Isola Controller
# This script builds and deploys the controller to Kubernetes (Minikube or other)

set -e  # Exit on error

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
CONTROLLER_IMAGE="isola-controller:dev"
AGENT_IMAGE="isola-agent:dev"
NAMESPACE="isola-system"
DEPLOYMENT_NAME="isola-controller"

# Parse command line arguments
ENVIRONMENT="minikube"  # Default to minikube
SKIP_BUILD=false
DEPLOY_AGENT=false

while [[ $# -gt 0 ]]; do
  case $1 in
    --env)
      ENVIRONMENT="$2"
      shift 2
      ;;
    --skip-build)
      SKIP_BUILD=true
      shift
      ;;
    --with-agent)
      DEPLOY_AGENT=true
      shift
      ;;
    --help)
      echo "Usage: ./deploy-controller.sh [OPTIONS]"
      echo "Options:"
      echo "  --env <env>       Environment (minikube|docker|k8s) [default: minikube]"
      echo "  --skip-build      Skip building images"
      echo "  --with-agent      Also deploy the agent"
      echo "  --help           Show this help message"
      exit 0
      ;;
    *)
      echo "Unknown option: $1"
      exit 1
      ;;
  esac
done

echo -e "${GREEN}🚀 Deploying Isola Controller${NC}"
echo -e "Environment: ${YELLOW}$ENVIRONMENT${NC}"
echo ""

# Function to check if command exists
command_exists() {
    command -v "$1" >/dev/null 2>&1
}

# Build Docker images
if [ "$SKIP_BUILD" = false ]; then
    echo -e "${GREEN}🔨 Building Docker images...${NC}"
    
    if [ "$ENVIRONMENT" = "minikube" ]; then
        # Check if minikube is running
        if ! command_exists minikube; then
            echo -e "${RED}Error: minikube not found. Please install minikube first.${NC}"
            exit 1
        fi
        
        if ! minikube status | grep -q "Running"; then
            echo -e "${RED}Error: Minikube is not running. Start it with: minikube start${NC}"
            exit 1
        fi
        
        # Use minikube's docker daemon
        echo "Using Minikube's Docker daemon..."
        eval $(minikube docker-env)
        
        # Disable buildx for minikube to avoid buildkit issues
        export DOCKER_BUILDKIT=0
        
        # Build controller image using legacy builder
        echo "Building controller image (using legacy builder for Minikube compatibility)..."
        docker build --no-cache -t $CONTROLLER_IMAGE -f services/isola_controller/Dockerfile .
        
        if [ "$DEPLOY_AGENT" = true ]; then
            echo "Building agent image (using legacy builder for Minikube compatibility)..."
            docker build --no-cache -t $AGENT_IMAGE -f services/isola_agent/Dockerfile .
        fi
        
    elif [ "$ENVIRONMENT" = "docker" ]; then
        # Local Docker build
        echo "Building for local Docker..."
        docker build -t $CONTROLLER_IMAGE -f services/isola_controller/Dockerfile .
        
        if [ "$DEPLOY_AGENT" = true ]; then
            docker build -t $AGENT_IMAGE -f services/isola_agent/Dockerfile .
        fi
        
    elif [ "$ENVIRONMENT" = "k8s" ]; then
        # For real Kubernetes, you might want to push to a registry
        echo -e "${YELLOW}Note: For production Kubernetes, you should push images to a registry${NC}"
        docker build -t $CONTROLLER_IMAGE -f services/isola_controller/Dockerfile .
        # docker push $CONTROLLER_IMAGE  # Uncomment and configure registry
    fi
    
    echo -e "${GREEN}✅ Images built successfully${NC}"
else
    echo -e "${YELLOW}Skipping image build...${NC}"
fi

# Deploy to Kubernetes
if [ "$ENVIRONMENT" = "minikube" ] || [ "$ENVIRONMENT" = "k8s" ]; then
    echo -e "${GREEN}📦 Deploying to Kubernetes...${NC}"
    
    # Check if kubectl is available
    if ! command_exists kubectl; then
        echo -e "${RED}Error: kubectl not found. Please install kubectl.${NC}"
        exit 1
    fi
    
    # Create namespace if it doesn't exist
    echo "Creating namespace..."
    kubectl create namespace $NAMESPACE --dry-run=client -o yaml | kubectl apply -f -
    
    # Apply manifests
    echo "Applying controller manifests..."
    kubectl apply -n $NAMESPACE -f local_minikube/manifests/isola-controller-deployment.yaml
    kubectl apply -n $NAMESPACE -f local_minikube/manifests/isola-controller-service.yaml
    
    if [ "$ENVIRONMENT" = "minikube" ]; then
        # Apply NodePort service for minikube
        kubectl apply -n $NAMESPACE -f local_minikube/manifests/isola-controller-nodeport.yaml
    fi
    
    if [ "$DEPLOY_AGENT" = true ]; then
        echo "Applying agent manifests..."
        kubectl apply -n $NAMESPACE -f local_minikube/manifests/isola-agent-deployment.yaml
    fi
    
    # Restart deployment to pick up new image
    echo "Restarting deployment..."
    kubectl rollout restart deployment/$DEPLOYMENT_NAME -n $NAMESPACE
    
    # Wait for rollout to complete
    echo "Waiting for deployment to be ready..."
    if kubectl rollout status deployment/$DEPLOYMENT_NAME -n $NAMESPACE --timeout=120s; then
        echo -e "${GREEN}✅ Deployment successful!${NC}"
    else
        echo -e "${RED}⚠️ Deployment rollout timed out. Check pod status:${NC}"
        kubectl get pods -n $NAMESPACE -l app=isola-controller
    fi
    
    # Show access information
    echo ""
    echo -e "${GREEN}📡 Access Information:${NC}"
    
    if [ "$ENVIRONMENT" = "minikube" ]; then
        MINIKUBE_IP=$(minikube ip)
        echo -e "Controller API: ${YELLOW}http://$MINIKUBE_IP:30080${NC}"
        echo ""
        echo "To access from localhost, use port-forward:"
        echo -e "${YELLOW}kubectl port-forward -n $NAMESPACE svc/isola-controller-service 3000:30080${NC}"
    else
        echo "Service created. Configure your ingress or use port-forward:"
        echo -e "${YELLOW}kubectl port-forward -n $NAMESPACE svc/isola-controller-service 3000:30080${NC}"
    fi
    
elif [ "$ENVIRONMENT" = "docker" ]; then
    echo -e "${GREEN}🐳 Running with Docker Compose...${NC}"
    
    if ! command_exists docker-compose; then
        echo -e "${RED}Error: docker-compose not found. Please install Docker Compose.${NC}"
        exit 1
    fi
    
    # Use docker-compose
    if [ "$DEPLOY_AGENT" = true ]; then
        docker-compose up -d
    else
        docker-compose up -d isola-controller
    fi
    
    echo -e "${GREEN}✅ Controller started with Docker Compose${NC}"
    echo ""
    echo -e "${GREEN}📡 Access Information:${NC}"
    echo -e "Controller API: ${YELLOW}http://localhost:3000${NC}"
fi

# Show logs command
echo ""
echo -e "${GREEN}📋 Useful Commands:${NC}"
if [ "$ENVIRONMENT" = "minikube" ] || [ "$ENVIRONMENT" = "k8s" ]; then
    echo "View logs:"
    echo -e "  ${YELLOW}kubectl logs -n $NAMESPACE -l app=isola-controller -f${NC}"
    echo "Check pods:"
    echo -e "  ${YELLOW}kubectl get pods -n $NAMESPACE${NC}"
    echo "Delete deployment:"
    echo -e "  ${YELLOW}kubectl delete -n $NAMESPACE -f local_minikube/manifests/${NC}"
elif [ "$ENVIRONMENT" = "docker" ]; then
    echo "View logs:"
    echo -e "  ${YELLOW}docker-compose logs -f isola-controller${NC}"
    echo "Stop services:"
    echo -e "  ${YELLOW}docker-compose down${NC}"
fi

echo ""
echo -e "${GREEN}✨ Deployment complete!${NC}"
