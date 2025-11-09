### test
uv run -m pytest

### lint / format
uv run ruff check --fix .

### type check
uv run mypy .

### install
uv sync # installs main project.dependencies deps in .venv
uv sync --dev # installs development dependency-groups.dev deps in .venv

### dependencies
uv add <dependency> # adds a new project dependency (e.g. uv add websockets)
uv add --dev <dependency> # adds a new development dependency (e.g. uv add --dev pytest)

### sandbox backends
- `SANDBOX_BACKEND=agent` (default) keeps the legacy websocket-driven sandbox provisioning via the `AgentManager`.
- `SANDBOX_BACKEND=kubernetes` provisions sandboxes directly with the in-repo `KubernetesManager`. This requires a working kubeconfig or in-cluster credentials.
- Set `KUBERNETES_NAMESPACE` (defaults to `isola-sandboxes`) if you need to isolate controller pods per environment.
- The Kubernetes backend currently powers the `/sandboxes` CRUD endpoints (including stop/restart/delete). The agent backend continues to handle only create requests via connected agents.
- See `local_minikube/` for helper scripts and manifests when you want a local cluster to exercise the Kubernetes backend.
