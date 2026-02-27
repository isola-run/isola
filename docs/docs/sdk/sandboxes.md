---
sidebar_position: 3
title: Sandboxes
slug: /sdk/sandboxes
---

# SDK Sandboxes

## Creating Sandboxes

```python
sandbox = client.sandboxes.create(
    image="python:3.12",
    command=["/bin/bash"],             # Optional entrypoint override
    env={"MY_VAR": "value"},           # Optional environment variables
    cpu="500m",                        # Optional CPU limit
    memory="512Mi",                    # Optional memory limit
    ephemeral_storage="1Gi",           # Optional ephemeral storage limit
    active_deadline_seconds=3600,      # Optional timeout
    network=NetworkSpec(               # Optional network config
        allow_internet_egress=True,
    ),
)
```

### Parameters

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `image` | str | Yes | Container image |
| `command` | list[str] | No | Entrypoint override |
| `env` | dict[str, str] | No | Environment variables |
| `cpu` | str | No | CPU request/limit (K8s quantity) |
| `memory` | str | No | Memory request/limit (K8s quantity) |
| `ephemeral_storage` | str | No | Ephemeral storage request/limit |
| `active_deadline_seconds` | int | No | Max lifetime in seconds |
| `network` | NetworkSpec | No | Network isolation config |

When `cpu`, `memory`, or `ephemeral_storage` are provided, the SDK sets both `requests` and `limits` to the same value.

## Listing Sandboxes

```python
summaries = client.sandboxes.list()

for s in summaries:
    print(f"{s.id}: {s.status} (created {s.creation_timestamp})")
```

Returns a list of `SandboxSummary` objects with `id`, `status`, and `creation_timestamp`.

## Getting a Sandbox

```python
sandbox = client.sandboxes.get("my-sandbox-id")

print(sandbox.id)
print(sandbox.status)
print(sandbox.creation_timestamp)
print(sandbox.network)
print(sandbox.active_deadline_seconds)
```

## Sandbox Properties

| Property | Type | Description |
|----------|------|-------------|
| `id` | str | Sandbox identifier |
| `status` | SandboxStatus | Current status enum |
| `creation_timestamp` | datetime | When the sandbox was created |
| `network` | NetworkSpec \| None | Network configuration |
| `active_deadline_seconds` | int \| None | Timeout setting |

## Deleting Sandboxes

```python
sandbox.delete()
```

## SandboxStatus Enum

```python
from isola import SandboxStatus

SandboxStatus.CREATING        # "creating"
SandboxStatus.RUNNING         # "running"
SandboxStatus.SHUTTING_DOWN   # "shuttingDown"
SandboxStatus.FAILED          # "failed"
SandboxStatus.STOPPED         # "stopped"
SandboxStatus.UNKNOWN         # "unknown"
```

## NetworkSpec

```python
from isola import NetworkSpec

network = NetworkSpec(
    allow_internet_egress=True,
    allow_cluster_dns=False,
    allowed_egress_cidrs=["10.0.0.0/8"],
    nameservers=["8.8.8.8"],
)
```

| Field | Type | Description |
|-------|------|-------------|
| `allow_internet_egress` | bool \| None | Allow public internet egress |
| `allow_cluster_dns` | bool \| None | Allow cluster DNS queries |
| `allowed_egress_cidrs` | list[str] \| None | CIDR allowlist |
| `nameservers` | list[str] \| None | Custom DNS servers (max 3) |
