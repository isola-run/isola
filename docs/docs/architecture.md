---
sidebar_position: 3
slug: /architecture
title: Architecture
---

# Architecture

Isola is a multi-service system built on Kubernetes. It uses Custom Resource Definitions (CRDs) for declarative sandbox management and a REST API for programmatic control.

## Components

### Operator

The operator is a Kubernetes controller that watches `Sandbox` and `RootfsSnapshot` custom resources. It runs in the `isola-system` namespace.

**Responsibilities:**
- Creates and manages sandbox pods from `Sandbox` CRs
- Injects the sandbox sidecar container into each pod
- Applies network policies based on the sandbox's `NetworkSpec`
- Manages sandbox timeouts via `activeDeadlineSeconds`
- Handles the `sandbox.isola.run/cleanup` finalizer for safe deletion
- Orchestrates filesystem snapshot jobs from `RootfsSnapshot` CRs

### API Gateway

A REST API server that provides external access to sandbox operations. It uses the [Huma](https://huma.rocks/) framework on a [chi](https://github.com/go-chi/chi) router.

**Responsibilities:**
- CRUD operations for sandboxes (translates REST types to/from CRD types)
- Proxies command and filesystem operations to the sandbox sidecar
- Validates request structure (domain defaults are applied by the operator)

The gateway uses a cached Kubernetes client for reading Sandbox CRs and proxies sidecar requests via HTTP to the sandbox pod's IP.

### Sandbox Sidecar

A lightweight HTTP service injected into every sandbox pod by the operator. It listens on port `10032`.

**Responsibilities:**
- Command execution via `nsenter` into the sandbox container's namespaces
- File read/write operations through `/proc/<pid>/root`
- Container discovery using the `ISOLA_CONTAINER_NAME` environment variable

The sidecar shares the process namespace with the sandbox container (`shareProcessNamespace: true`), allowing it to discover and interact with container processes.

### Uploader

A job container used for filesystem snapshot operations. It is created by the operator when a `RootfsSnapshot` CR is processed.

**Responsibilities:**
- Reads the sandbox container's rootfs via gVisor's overlay filesystem
- Creates a tarball of the filesystem
- Uploads to cloud storage (S3, GCS, or Azure Blob Storage via [gocloud.dev](https://gocloud.dev/))
- Reports results through the Kubernetes termination log

## Data Flow

```
┌────────────┐     REST      ┌─────────────┐    HTTP     ┌─────────────────┐
│            │   ──────────▶  │             │  ────────▶  │ Sandbox Pod     │
│  Client    │                │ API Gateway │             │  ┌───────────┐  │
│ (SDK/curl) │  ◀──────────  │             │  ◀────────  │  │  Sidecar  │  │
│            │                │             │             │  └───────────┘  │
└────────────┘                └──────┬──────┘             │  ┌───────────┐  │
                                     │                    │  │ Container │  │
                              K8s API│                    │  └───────────┘  │
                                     │                    └─────────────────┘
                              ┌──────▼──────┐
                              │  Operator   │
                              │ (controller)│
                              └─────────────┘
```

1. **Create:** Client sends a `POST /sandboxes` request to the API gateway, which creates a `Sandbox` CR. The operator picks it up and creates the pod.
2. **Execute:** Client sends a `POST /sandboxes/{id}/commands` request. The gateway proxies this to the sidecar, which runs the command via `nsenter`.
3. **Stream:** Client reads `GET /sandboxes/{id}/commands/{cmdId}/stdout`. The gateway proxies the byte stream from the sidecar.
4. **Delete:** Client sends `DELETE /sandboxes/{id}`. The gateway deletes the `Sandbox` CR. The operator's finalizer cleans up associated resources.

## Namespaces

| Namespace | Contents |
|-----------|----------|
| `isola-system` | Operator, API gateway |
| `isola-sandboxes` | Sandbox pods, NetworkPolicies, snapshot jobs |

## CRDs

Isola defines two Custom Resource Definitions in the `sandbox.isola.run` API group:

- **Sandbox** (`v1alpha1`) — Represents a running sandbox instance
- **RootfsSnapshot** (`v1alpha1`) — Triggers a filesystem snapshot of a sandbox

Both are namespaced resources. See [Sandboxes](./concepts/sandboxes.md) and [Snapshots](./concepts/snapshots.md) for details.

## Security Model

- **gVisor runtime:** Sandbox containers run under gVisor (`runsc`) by default, providing kernel-level isolation
- **Network isolation:** Deny-all egress by default with opt-in internet access and CIDR allowlists
- **Process namespace sharing:** Enables the sidecar to discover and interact with sandbox processes without privileged access
- **Write-only environment variables:** The API returns container info without env vars to prevent secret leakage
- **Finalizers:** Ensure cleanup of NetworkPolicies and other resources before sandbox deletion
