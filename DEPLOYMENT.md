# Isola Controller Deployment Guide

## Quick Start (Minikube)

For a quick deployment to Minikube, use:

```bash
./quick-deploy.sh
```

This will:
- Build both controller and agent images
- Deploy to Minikube
- Set up services and networking
- Provide access instructions

## Deployment Options

### 1. Minikube Deployment (Recommended for Development)

```bash
# Full deployment with agent
./deploy-controller.sh --env minikube --with-agent

# Controller only
./deploy-controller.sh --env minikube

# Skip building images (use existing)
./deploy-controller.sh --env minikube --skip-build
```

### 2. Docker Compose (Local Development)

```bash
# Deploy using Docker Compose
./deploy-controller.sh --env docker

# Or use docker-compose directly
docker-compose up -d
```

### 3. Production Kubernetes

```bash
# Deploy to real Kubernetes cluster
./deploy-controller.sh --env k8s

# Note: You'll need to configure image registry for production
```

## Access the Controller

### Minikube
```bash
# Option 1: Direct access via NodePort
MINIKUBE_IP=$(minikube ip)
curl http://$MINIKUBE_IP:30080/health

# Option 2: Port forwarding (recommended)
kubectl port-forward -n isola-system svc/isola-controller-service 3000:30080
# Then access at http://localhost:3000
```

### Docker Compose
```bash
# Direct access
curl http://localhost:3000/health
```

## Useful Commands

### View Logs
```bash
# Kubernetes/Minikube
kubectl logs -n isola-system -l app=isola-controller -f

# Docker Compose
docker-compose logs -f isola-controller
```

### Check Status
```bash
# Kubernetes/Minikube
kubectl get pods -n isola-system
kubectl get svc -n isola-system

# Docker Compose
docker-compose ps
```

### Restart Services
```bash
# Kubernetes/Minikube
kubectl rollout restart deployment/isola-controller -n isola-system

# Docker Compose
docker-compose restart isola-controller
```

### Clean Up
```bash
# Kubernetes/Minikube
kubectl delete namespace isola-system

# Docker Compose
docker-compose down
```

## Testing the Deployment

Once deployed, test the controller:

```bash
# Health check
curl -H "X-API-Key: iso_sk_demo" http://localhost:3000/health

# Create a sandbox
curl -X POST http://localhost:3000/sandboxes \
  -H "X-API-Key: iso_sk_demo" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "test-sandbox",
    "image": "python:3.11",
    "class": "small"
  }'

# Execute a command (after sandbox is started)
curl -X POST http://localhost:3000/sandboxes/{sandbox_id}/execute \
  -H "X-API-Key: iso_sk_demo" \
  -H "Content-Type: application/json" \
  -d '{"command": "echo Hello from Isola"}'

# Upload a file
curl -X POST http://localhost:3000/sandboxes/{sandbox_id}/fs/upload \
  -H "X-API-Key: iso_sk_demo" \
  -H "Content-Type: application/json" \
  -d '{
    "path": "/tmp/test.txt",
    "content": "Hello, World!"
  }'
```

## Troubleshooting

### Pod Not Starting
```bash
# Check pod status
kubectl describe pod -n isola-system -l app=isola-controller

# Check events
kubectl get events -n isola-system --sort-by='.lastTimestamp'
```

### Connection Issues
```bash
# Verify services are running
kubectl get svc -n isola-system

# Check endpoints
kubectl get endpoints -n isola-system
```

### Image Build Failures
```bash
# For Minikube, ensure you're using Minikube's Docker daemon
eval $(minikube docker-env)

# Verify images are available
docker images | grep isola
```

## Configuration

### Environment Variables

The controller supports the following environment variables:

- `SANDBOX_BACKEND`: Backend type (`kubernetes` or `agent`)
- `LOG_LEVEL`: Log level (`debug`, `info`, `warning`, `error`)
- `KUBERNETES_NAMESPACE`: Namespace for sandboxes (default: `isola-sandboxes`)

### Updating Configuration

1. Edit the deployment manifest:
```bash
kubectl edit deployment/isola-controller -n isola-system
```

2. Or modify the manifest file and reapply:
```bash
vim local_minikube/manifests/isola-controller-deployment.yaml
kubectl apply -f local_minikube/manifests/isola-controller-deployment.yaml -n isola-system
```
