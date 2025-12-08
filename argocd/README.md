# ArgoCD Setup for Isola Platform

This directory contains the ArgoCD configuration for deploying the Isola platform using GitOps practices.

## Directory Structure

```
argocd/
├── applications/       # Individual ArgoCD Application manifests
├── bootstrap/         # Bootstrap scripts for setting up ArgoCD
├── projects/         # ArgoCD AppProject definitions
└── README.md        # This file
```

## Quick Start


### Bootstrap ArgoCD and Deploy Applications

```bash
cd argocd/bootstrap
./bootstrap.sh
```

This will:
1. Install ArgoCD if not already present
2. Create the Isola project
3. Create necessary namespaces
4. Deploy all application manifests directly
5. Display the deployment status

## Managing Applications


### Adding New Applications

1. Create a new application manifest in `applications/`:

2. Apply the manifest:
```bash
kubectl apply -f applications/your-app-dev.yaml
```

### Removing Applications

```bash
kubectl delete application <app-name> -n argocd
```

## Accessing ArgoCD UI

1. Port-forward the ArgoCD server:
```bash
kubectl port-forward svc/argocd-server -n argocd 8080:443
```

2. Get the admin password:
```bash
kubectl -n argocd get secret argocd-initial-admin-secret -o jsonpath="{.data.password}" | base64 -d
```

3. Access the UI at https://localhost:8080 with:
   - Username: `admin`
   - Password: (from step 2)

## Environment-Specific Deployments

Applications are named with environment suffixes (e.g., `-dev`, `-staging`, `-prod`). The bootstrap script uses the `ENVIRONMENT` variable to deploy the appropriate applications:

```bash
ENVIRONMENT=staging ./bootstrap.sh
```