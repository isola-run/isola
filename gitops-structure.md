# GitOps Folder Structure with ArgoCD and Helm

## Recommended Directory Structure

```
dev-isola/
├── charts/                               # Helm charts
│   ├── isola-controller/                 # Controller service chart
│   │   ├── Chart.yaml
│   │   ├── values.yaml
│   │   ├── values-dev.yaml               # Dev overrides
│   │   ├── values-staging.yaml           # Staging overrides
│   │   ├── values-prod.yaml              # Production overrides
│   │   └── templates/
│   │       ├── deployment.yaml
│   │       ├── service.yaml
│   │       ├── serviceaccount.yaml
│   │       ├── role.yaml
│   │       ├── rolebinding.yaml
│   │       ├── configmap.yaml
│   │       ├── _helpers.tpl
│   │       └── NOTES.txt
│   │
│   ├── isola-agent/                      # Agent service chart
│   │   ├── Chart.yaml
│   │   ├── values.yaml
│   │   ├── values-dev.yaml
│   │   ├── values-staging.yaml
│   │   ├── values-prod.yaml
│   │   └── templates/
│   │       ├── deployment.yaml
│   │       ├── service.yaml
│   │       ├── configmap.yaml
│   │       ├── _helpers.tpl
│   │       └── NOTES.txt
│   │
│   └── isola-platform/                   # Umbrella chart
│       ├── Chart.yaml
│       ├── Chart.lock
│       ├── values.yaml
│       ├── values-dev.yaml
│       ├── values-staging.yaml
│       ├── values-prod.yaml
│       └── templates/
│           ├── namespaces.yaml
│           └── NOTES.txt
│
├── argocd/                                # ArgoCD configurations
│   ├── bootstrap/                        # ArgoCD installation & setup
│   │   ├── argocd-namespace.yaml
│   │   ├── argocd-install.yaml          # Or reference to Helm chart
│   │   └── argocd-ingress.yaml
│   │
│   ├── app-of-apps/                      # Parent applications
│   │   ├── dev.yaml                      # Dev environment apps
│   │   ├── staging.yaml                  # Staging environment apps
│   │   └── production.yaml               # Production environment apps
│   │
│   ├── applications/                      # Individual ArgoCD applications
│   │   ├── isola-platform-dev.yaml
│   │   ├── isola-platform-staging.yaml
│   │   ├── isola-platform-prod.yaml
│   │   ├── isola-controller-dev.yaml     # If deploying separately
│   │   └── isola-agent-dev.yaml          # If deploying separately
│   │
│   └── projects/                          # ArgoCD projects
│       └── isola-project.yaml
│
├── environments/                          # Environment-specific configs
│   ├── dev/
│   │   ├── kustomization.yaml            # Optional Kustomize overlays
│   │   └── config/
│   │       ├── secrets.yaml              # Encrypted with Sealed Secrets/SOPS
│   │       └── configmaps.yaml
│   ├── staging/
│   │   ├── kustomization.yaml
│   │   └── config/
│   │       ├── secrets.yaml
│   │       └── configmaps.yaml
│   └── production/
│       ├── kustomization.yaml
│       └── config/
│           ├── secrets.yaml
│           └── configmaps.yaml
│
├── services/                              # [Existing] Application source code
├── common/                                # [Existing] Shared library
├── local_minikube/                        # Local development
│   ├── legacy-manifests/                 # Move current manifests here
│   └── setup/
│       ├── install-argocd.sh
│       └── configure-local-cluster.sh
└── docs/
    ├── gitops/
    │   ├── argocd-setup.md
    │   ├── deployment-workflow.md
    │   └── secrets-management.md
    └── api/
```

## Migration Strategy

### Phase 1: Helm Charts Creation
1. Create base Helm charts for each service
2. Use templating for environment-specific values
3. Test locally with `helm template` and `helm install`

### Phase 2: ArgoCD Setup
1. Install ArgoCD in the cluster
2. Create ArgoCD Project for isolation
3. Deploy App-of-Apps pattern for environment management

### Phase 3: GitOps Workflow
1. All changes via Git commits
2. ArgoCD auto-syncs from Git
3. Progressive rollout (dev → staging → prod)

## Key Benefits

1. **Separation of Concerns**: Charts, ArgoCD configs, and environments are clearly separated
2. **Reusability**: Single chart with multiple values files for different environments
3. **GitOps Ready**: Everything is declarative and version controlled
4. **Scalable**: Easy to add new services or environments
5. **Local Development**: Preserved local Minikube setup while enabling GitOps

## Next Steps

1. Convert existing manifests to Helm templates
2. Create ArgoCD application manifests
3. Setup secrets management (Sealed Secrets or SOPS)
4. Configure CI/CD pipeline for image updates
5. Document deployment procedures
