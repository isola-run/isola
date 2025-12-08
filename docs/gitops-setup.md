# GitOps Setup Guide for Isola Platform

## Overview

This guide explains how to set up and use the GitOps workflow for the Isola platform using ArgoCD and Helm.

## Prerequisites

- Kubernetes cluster (Minikube for local development)
- kubectl configured
- Helm 3.x installed
- Git repository for your code

## Directory Structure

```
dev-isola/
├── charts/                 # Helm charts for all services
├── argocd/                 # ArgoCD configurations
├── environments/           # Environment-specific configs
└── docs/                   # Documentation
```

## Setup Instructions

### 1. Install ArgoCD

```bash
# Navigate to bootstrap directory
cd argocd/bootstrap

# Install ArgoCD
./install-argocd.sh --dev

# Wait for ArgoCD to be ready
kubectl wait --for=condition=available --timeout=600s \
  deployment -l app.kubernetes.io/part-of=argocd -n argocd
```

### 2. Access ArgoCD UI

```bash
# Port-forward ArgoCD server
kubectl port-forward svc/argocd-server -n argocd 8080:443

# Get admin password
kubectl -n argocd get secret argocd-initial-admin-secret \
  -o jsonpath="{.data.password}" | base64 -d

# Access UI at https://localhost:8080
# Username: admin
# Password: <output from above command>
```

### 3. Bootstrap the Platform

```bash
# Run bootstrap script
cd argocd/bootstrap
./bootstrap.sh

# Or manually apply configurations
kubectl apply -f ../projects/isola-project.yaml
kubectl apply -f ../app-of-apps/dev.yaml
```

## Deployment Workflow

### Local Development

1. **Build and push images:**
```bash
# Build controller image
docker build -t isola-controller:dev services/isola_controller/

# Build agent image
docker build -t isola-agent:dev services/isola_agent/

# For Minikube, load images directly
minikube image load isola-controller:dev
minikube image load isola-agent:dev
```

2. **Test Helm charts locally:**
```bash
# Lint charts
helm lint charts/isola-controller
helm lint charts/isola-platform

# Dry run
helm install isola-platform charts/isola-platform \
  --dry-run --debug -f charts/isola-platform/values-dev.yaml

# Install manually
helm install isola-platform charts/isola-platform \
  -f charts/isola-platform/values-dev.yaml \
  --namespace isola-control-plane --create-namespace
```

### GitOps Deployment

1. **Make changes to your code or configurations**

2. **Commit and push to Git:**
```bash
git add .
git commit -m "feat: update controller configuration"
git push origin main
```

3. **ArgoCD automatically syncs** (if auto-sync is enabled)

4. **Or manually sync:**
```bash
# Using ArgoCD CLI
argocd app sync isola-platform-dev

# Or from UI
# Navigate to application and click "Sync"
```

## Managing Environments

### Development Environment
- Auto-sync enabled
- Resource limits reduced
- Debug logging enabled
- NodePort services for local access

### Staging Environment
- Manual sync preferred
- Production-like resource limits
- Info logging
- LoadBalancer or Ingress access

### Production Environment
- Manual sync only
- Full resource allocation
- Error logging only
- Ingress with TLS

### Adding a New Environment

1. **Create values file:**
```bash
cp charts/isola-platform/values.yaml \
   charts/isola-platform/values-staging.yaml
# Edit values as needed
```

2. **Create ArgoCD application:**
```bash
cp argocd/applications/isola-platform-dev.yaml \
   argocd/applications/isola-platform-staging.yaml
# Update namespace and values file reference
```

3. **Create App-of-Apps:**
```bash
cp argocd/app-of-apps/dev.yaml \
   argocd/app-of-apps/staging.yaml
# Update to include staging apps
```

## Common Operations

### Update Application

```bash
# Update chart version
vim charts/isola-controller/Chart.yaml

# Update values
vim charts/isola-controller/values-dev.yaml

# Commit and push
git add . && git commit -m "Update controller" && git push

# Sync in ArgoCD
argocd app sync isola-controller-dev
```

### Rollback

```bash
# View history
argocd app history isola-controller-dev

# Rollback to previous version
argocd app rollback isola-controller-dev 1

# Or use Git revert
git revert HEAD
git push
```

### Debug Sync Issues

```bash
# Check application status
kubectl get applications -n argocd

# Detailed application info
argocd app get isola-controller-dev

# Check events
kubectl get events -n isola-control-plane

# View logs
kubectl logs -n argocd deployment/argocd-application-controller
```

## Security Considerations

### Secrets Management

**Option 1: Sealed Secrets**
```bash
# Install sealed-secrets controller
kubectl apply -f https://github.com/bitnami-labs/sealed-secrets/releases/download/v0.18.0/controller.yaml

# Create and seal secret
echo -n mypassword | kubectl create secret generic mysecret \
  --dry-run=client --from-file=password=/dev/stdin -o yaml | \
  kubeseal -o yaml > sealed-secret.yaml
```

**Option 2: External Secrets Operator**
```bash
# Install ESO
helm repo add external-secrets https://charts.external-secrets.io
helm install external-secrets external-secrets/external-secrets \
  -n external-secrets-system --create-namespace
```

### RBAC

Ensure proper RBAC is configured:
```yaml
# In values.yaml
rbac:
  create: true
  rules:
    - apiGroups: [""]
      resources: ["pods"]
      verbs: ["get", "list", "watch"]
```

## Monitoring

### ArgoCD Metrics

```bash
# Enable metrics
kubectl patch svc argocd-metrics -n argocd \
  -p '{"spec": {"type": "NodePort"}}'

# Access metrics
curl http://localhost:8082/metrics
```

### Application Health

```bash
# Check application health
argocd app health isola-platform-dev

# View resource tree
argocd app resources isola-platform-dev
```

## Troubleshooting

### Common Issues

1. **Sync Out of Sync**
   - Check for manual changes in cluster
   - Verify Git repository is accessible
   - Check for resource conflicts

2. **Image Pull Errors**
   - Verify image exists
   - Check image pull secrets
   - For Minikube: ensure image is loaded

3. **Permission Denied**
   - Verify RBAC configuration
   - Check service account bindings
   - Review ArgoCD project permissions

### Useful Commands

```bash
# Force sync
argocd app sync isola-platform-dev --force

# Prune resources
argocd app sync isola-platform-dev --prune

# Hard refresh
argocd app get isola-platform-dev --hard-refresh

# Delete and recreate
argocd app delete isola-platform-dev
kubectl apply -f argocd/applications/isola-platform-dev.yaml
```

## Best Practices

1. **Version Control Everything**
   - All configurations in Git
   - Use semantic versioning for charts
   - Tag releases appropriately

2. **Environment Separation**
   - Separate namespaces per environment
   - Different clusters for prod/non-prod
   - Restricted access to production

3. **Progressive Rollout**
   - Deploy to dev first
   - Promote to staging after validation
   - Manual approval for production

4. **Monitoring and Alerting**
   - Set up notifications for sync failures
   - Monitor application health
   - Track deployment metrics

5. **Documentation**
   - Document all changes
   - Maintain runbooks
   - Keep architecture diagrams updated

## Next Steps

1. Set up CI/CD pipeline for automated testing
2. Implement secrets management solution
3. Configure monitoring and alerting
4. Add network policies for security
5. Implement backup and disaster recovery

## References

- [ArgoCD Documentation](https://argo-cd.readthedocs.io/)
- [Helm Documentation](https://helm.sh/docs/)
- [Kubernetes Documentation](https://kubernetes.io/docs/)
- [GitOps Principles](https://www.gitops.tech/)
