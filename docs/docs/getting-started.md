---
sidebar_position: 2
slug: /getting-started
title: Getting Started
---

# Getting Started

This guide walks you through setting up Isola and creating your first sandbox.

## Prerequisites

- A Kubernetes cluster (v1.25+)
- [Helm](https://helm.sh/docs/intro/install/) v3
- [gVisor](https://gvisor.dev/docs/user_guide/install/) installed on cluster nodes (for the default `gvisor` runtime)

For local development, you'll also need:

- [Docker](https://docs.docker.com/get-docker/)
- [Kind](https://kind.sigs.k8s.io/docs/user/quick-start/#installation)
- [Tilt](https://docs.tilt.dev/install.html)

## Install with Helm

```bash
helm install isola ./charts/isola \
  --namespace isola-system \
  --create-namespace
```

This deploys the operator and API gateway into the `isola-system` namespace. Sandbox pods are created in the `isola-sandboxes` namespace by default.

## Create a Sandbox

### Using the Python SDK

Install the SDK:

```bash
pip install isola
```

Create and interact with a sandbox:

```python
from isola import Isola

with Isola(base_url="http://localhost:8080") as client:
    # Create a sandbox with a Python image
    sandbox = client.sandboxes.create(image="python:3.12")

    # Run a command
    cmd = sandbox.commands.run(cmd="python", args=["-c", "print('Hello from Isola!')"])

    # Stream stdout
    with cmd.stdout() as stream:
        for chunk in stream:
            print(chunk, end="")

    # Clean up
    sandbox.delete()
```

### Using the REST API

```bash
# Create a sandbox
curl -X POST http://localhost:8080/sandboxes \
  -H "Content-Type: application/json" \
  -d '{
    "podTemplate": {
      "container": {
        "image": "ubuntu:22.04"
      }
    }
  }'

# Run a command (replace SANDBOX_ID with the returned id)
curl -X POST http://localhost:8080/sandboxes/SANDBOX_ID/commands \
  -H "Content-Type: application/json" \
  -d '{
    "cmd": "echo",
    "args": ["Hello from Isola!"]
  }'
```

### Using kubectl

```yaml
apiVersion: sandbox.isola.run/v1alpha1
kind: Sandbox
metadata:
  name: my-sandbox
  namespace: isola-sandboxes
spec:
  podTemplate:
    spec:
      containers:
        - name: main
          image: python:3.12
  activeDeadlineSeconds: 3600
```

```bash
kubectl apply -f sandbox.yaml
```

## Next Steps

- Learn about the [Architecture](./architecture.md)
- Explore [Sandbox Concepts](./concepts/sandboxes.md)
- Read the [Python SDK Guide](./sdk/overview.md)
- Browse the [REST API Reference](./api/overview.md)
