# Isola Documentation

**Isola** is a Kubernetes-native sandbox platform for creating isolated, ephemeral execution environments. Perfect for running untrusted code, AI agent workflows, automated testing, and secure multi-tenant workloads.

---

## Quick Navigation

| Section | Description |
|---------|-------------|
| [Getting Started](./getting-started.md) | Create your first sandbox in 5 minutes |
| [Core Concepts](./concepts/README.md) | Understand sandboxes, templates, and networking |
| [API Reference](./api/README.md) | Complete REST API documentation |
| [Tutorials](./tutorials/README.md) | Step-by-step guides for common use cases |
| [Configuration](./configuration.md) | Template and network policy configuration |

---

## What is Isola?

Isola provides **secure, isolated sandbox environments** that:

- **Spin up in seconds** - Lightweight Kubernetes pods with automatic lifecycle management
- **Isolate by default** - Network policies prevent sandboxes from accessing your infrastructure
- **Scale effortlessly** - Leverage Kubernetes for horizontal scaling
- **Clean up automatically** - Configurable timeouts and shutdown policies

### Architecture Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                         Your Application                        │
│                    (REST API / Python SDK)                      │
└────────────────────────────┬────────────────────────────────────┘
                             │
                             ▼
┌─────────────────────────────────────────────────────────────────┐
│                         isola-gw                                │
│              REST API Gateway + File Storage                    │
└────────────────────────────┬────────────────────────────────────┘
                             │
              ┌──────────────┴──────────────┐
              ▼                             ▼
┌─────────────────────────┐   ┌─────────────────────────────────┐
│    isola-operator       │   │     isola-sandboxes namespace   │
│  Kubernetes Controller  │   │  ┌───────────────────────────┐  │
│  - Sandbox lifecycle    │   │  │      Sandbox Pod          │  │
│  - Network policies     │   │  │  ┌─────────┐ ┌─────────┐  │  │
│  - Template management  │   │  │  │  Main   │ │  Agent  │  │  │
│                         │   │  │  │Container│ │ Sidecar │  │  │
└─────────────────────────┘   │  │  └─────────┘ └─────────┘  │  │
                              │  └───────────────────────────┘  │
                              └─────────────────────────────────┘
```

---

## Key Features

### Sandbox Management
Create, monitor, and terminate sandboxes via REST API or Kubernetes CRDs.

```bash
# Create a sandbox via API
curl -X POST http://localhost:8080/api/v1/sandboxes \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"name": "my-sandbox", "autoStart": true}'
```

### Command Execution
Execute commands inside running sandboxes and capture output.

```bash
# Run a command
curl -X POST http://localhost:8080/api/v1/sandboxes/$ID/execute \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"command": "python -c \"print(42)\""}'
```

### File Operations
Upload and download files to/from sandboxes with S3 integration for large files.

```bash
# Upload a file
curl -X POST http://localhost:8080/api/v1/sandboxes/$ID/files \
  -H "X-API-Key: $API_KEY" \
  -F "file=@script.py" \
  -F "path=/home/user/script.py"
```

### Network Isolation
Configure network policies to control egress/ingress traffic.

```yaml
# Allow only outbound traffic to specific IPs
apiVersion: sandbox.isola.run/v1alpha1
kind: NetworkTemplate
metadata:
  name: limited-egress
spec:
  allowedEgress:
    - "10.0.0.0/8"
  dnsServers:
    - "8.8.8.8"
```

---

## Use Cases

| Use Case | Description |
|----------|-------------|
| **AI Agent Execution** | Run LLM-generated code safely in isolated environments |
| **Automated Testing** | Spin up ephemeral environments for integration tests |
| **Code Playgrounds** | Provide users with safe code execution environments |
| **CI/CD Pipelines** | Execute build steps in isolated, reproducible containers |
| **Multi-tenant SaaS** | Isolate customer workloads with network policies |

---

## Next Steps

1. **[Getting Started](./getting-started.md)** - Set up Isola and create your first sandbox
2. **[Core Concepts](./concepts/README.md)** - Learn about sandboxes, templates, and networking
3. **[API Reference](./api/README.md)** - Explore the complete REST API
