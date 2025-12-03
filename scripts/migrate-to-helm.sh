#!/bin/bash

# Migration script from raw Kubernetes manifests to Helm charts

set -e

echo "========================================="
echo "Migrating to Helm and ArgoCD"
echo "========================================="

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Check prerequisites
check_prerequisites() {
    echo "Checking prerequisites..."
    
    if ! command -v helm &> /dev/null; then
        echo -e "${RED}❌ Helm not found. Please install Helm 3.x${NC}"
        exit 1
    fi
    
    if ! command -v kubectl &> /dev/null; then
        echo -e "${RED}❌ kubectl not found. Please install kubectl${NC}"
        exit 1
    fi
    
    echo -e "${GREEN}✓ All prerequisites met${NC}"
}

# Backup existing manifests
backup_manifests() {
    echo ""
    echo "Backing up existing manifests..."
    
    if [ -d "local_minikube/manifests" ]; then
        cp -r local_minikube/manifests local_minikube/manifests.backup.$(date +%Y%m%d_%H%M%S)
        echo -e "${GREEN}✓ Manifests backed up${NC}"
    else
        echo -e "${YELLOW}⚠ No existing manifests found${NC}"
    fi
}

# Test Helm charts
test_helm_charts() {
    echo ""
    echo "Testing Helm charts..."
    
    # Test controller chart
    if [ -d "charts/isola-controller" ]; then
        helm lint charts/isola-controller
        echo -e "${GREEN}✓ isola-controller chart valid${NC}"
    fi
    
    # Test agent chart if it exists
    if [ -d "charts/isola-agent" ]; then
        helm lint charts/isola-agent
        echo -e "${GREEN}✓ isola-agent chart valid${NC}"
    fi
    
    # Test platform chart
    if [ -d "charts/isola-platform" ]; then
        helm dependency update charts/isola-platform
        helm lint charts/isola-platform
        echo -e "${GREEN}✓ isola-platform chart valid${NC}"
    fi
}

# Dry run deployment
dry_run() {
    echo ""
    echo "Performing dry run..."
    
    helm install isola-platform charts/isola-platform \
        --dry-run \
        --debug \
        -f charts/isola-platform/values-dev.yaml \
        --namespace isola-control-plane \
        --create-namespace > /tmp/helm-dry-run.yaml
    
    echo -e "${GREEN}✓ Dry run successful (output saved to /tmp/helm-dry-run.yaml)${NC}"
}

# Main migration
main() {
    check_prerequisites
    backup_manifests
    test_helm_charts
    dry_run
    
    echo ""
    echo "========================================="
    echo -e "${GREEN}Migration preparation complete!${NC}"
    echo "========================================="
    echo ""
    echo "Next steps:"
    echo "1. Review the Helm charts in the 'charts/' directory"
    echo "2. Update image repositories and tags in values files"
    echo "3. Install ArgoCD: cd argocd/bootstrap && ./install-argocd.sh"
    echo "4. Deploy using GitOps: ./bootstrap.sh"
    echo ""
    echo "To deploy manually with Helm:"
    echo "  helm install isola-platform charts/isola-platform -f charts/isola-platform/values-dev.yaml -n isola-control-plane --create-namespace"
    echo ""
    echo "To clean up old manifests:"
    echo "  kubectl delete -f local_minikube/manifests/"
}

# Run main function
main
