---
sidebar_position: 1
title: Sandboxes
---

# Sandboxes

A Sandbox is the core resource in Isola. It represents an isolated container environment running on Kubernetes.

## Sandbox Lifecycle

Sandboxes go through the following statuses:

| Status | Description |
|--------|-------------|
| `creating` | Pod is being scheduled and containers are starting |
| `running` | Pod is ready and accepting commands |
| `shuttingDown` | Sandbox is being deleted or snapshotted |
| `failed` | Pod encountered an error |
| `stopped` | Pod has exited successfully |
| `unknown` | Status cannot be determined |

## Creating a Sandbox

At minimum, you need to provide a container image:

```json
{
  "podTemplate": {
    "container": {
      "image": "python:3.12"
    }
  }
}
```

Containers with no `command` specified get `["sleep", "infinity"]` injected by the operator, keeping the sandbox alive for interactive use.

### Container Configuration

```json
{
  "podTemplate": {
    "container": {
      "image": "python:3.12",
      "command": ["/bin/bash"],
      "env": {
        "MY_VAR": "my-value"
      },
      "resources": {
        "requests": {
          "cpu": "100m",
          "memory": "256Mi"
        },
        "limits": {
          "cpu": "500m",
          "memory": "512Mi",
          "ephemeralStorage": "1Gi"
        }
      }
    }
  }
}
```

### Timeouts

Set `activeDeadlineSeconds` to automatically shut down a sandbox after a duration:

```json
{
  "podTemplate": {
    "container": { "image": "python:3.12" }
  },
  "activeDeadlineSeconds": 3600
}
```

The operator calculates an absolute timeout (`status.timeoutAt`) from the pod's start time.

## Sandbox CRD

The `Sandbox` CRD is in the `sandbox.isola.run/v1alpha1` API group.

### Spec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `podTemplate` | PodTemplateSpec | Yes | Inlined Kubernetes pod template |
| `activeDeadlineSeconds` | int64 | No | Max lifetime in seconds |
| `shutdownPolicy` | ShutdownPolicy | No | Action on shutdown (default: `Delete`) |
| `network` | NetworkSpec | No | Network isolation config (immutable after creation) |

### Shutdown Policy

| Strategy | Description |
|----------|-------------|
| `Delete` | Default. Sandbox pod is deleted immediately. |
| `SnapshotRootfs` | Creates a filesystem snapshot before deletion. Requires gVisor runtime. |

### Status

Sandbox status uses Kubernetes [conditions](https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/#pod-conditions), not the deprecated phase pattern:

| Condition | Description |
|-----------|-------------|
| `Ready` | Aggregate readiness condition |
| `PodReady` | Pod is up and running |
| `NetworkConfigured` | Network policies are applied |
| `TimedOut` | Sandbox exceeded its deadline |
| `SnapshottingFilesystem` | Filesystem snapshot is in progress |

### Finalizer

Every sandbox has a `sandbox.isola.run/cleanup` finalizer that ensures associated resources (NetworkPolicies, etc.) are cleaned up before the sandbox is deleted.

## Environment Variables

Environment variables are **write-only**: you can set them when creating a sandbox, but the API response omits them to prevent leaking secrets. The `ContainerSpec` (request) has an `env` field; the `ContainerInfo` (response) does not.

## Single-Container Model

The REST API currently operates on a single container per sandbox. The `podTemplate.container` field in the API maps to the first container in the pod spec.
