# Core Concepts

Understanding Isola's architecture and core concepts will help you build robust, secure sandbox environments.

---

## Overview

Isola is built on three Kubernetes Custom Resource Definitions (CRDs):

| Resource | Purpose |
|----------|---------|
| **[Sandbox](./sandbox.md)** | A running isolated environment instance |
| **[SandboxTemplate](./sandbox-template.md)** | Reusable configuration for sandbox pods |
| **[NetworkTemplate](./network-template.md)** | Network isolation policy definition |

---

## Concept Map

```
┌─────────────────────────────────────────────────────────────────────┐
│                        SandboxTemplate                              │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  - Container image                                           │   │
│  │  - Resource limits (CPU, memory)                             │   │
│  │  - Timeout duration                                          │   │
│  │  - Shutdown policy (Delete / SnapshotFilesystem)             │   │
│  └─────────────────────────────────────────────────────────────┘   │
└─────────────────────────────────┬───────────────────────────────────┘
                                  │ references
                                  ▼
┌─────────────────────────────────────────────────────────────────────┐
│                           Sandbox                                    │
│  ┌──────────────────┐    ┌──────────────────────────────────────┐   │
│  │ spec:            │    │ status:                               │   │
│  │  - templateRef   │────│  - conditions (Ready, PodReady, etc) │   │
│  │  - network       │    │  - timeoutAt                         │   │
│  │  - labels        │    │  - podName                           │   │
│  └──────────────────┘    └──────────────────────────────────────┘   │
└─────────────────────────────────┬───────────────────────────────────┘
                                  │ references or embeds
                                  ▼
┌─────────────────────────────────────────────────────────────────────┐
│                       NetworkTemplate                                │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  - allowedEgress  (outbound CIDR ranges)                    │   │
│  │  - allowedIngress (inbound CIDR ranges)                     │   │
│  │  - dnsServers     (custom DNS servers)                      │   │
│  └─────────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────────┘
```

---

## Resource Relationships

### Template Reference Pattern

Sandboxes reference templates by name, enabling template reuse:

```yaml
apiVersion: sandbox.isola.run/v1alpha1
kind: Sandbox
metadata:
  name: my-sandbox
spec:
  templateRef:
    name: python-sandbox  # References a SandboxTemplate
  network:
    templateRef:
      name: egress-only   # References a NetworkTemplate
```

### Embedded Network Spec Pattern

For one-off configurations, embed the network spec directly:

```yaml
apiVersion: sandbox.isola.run/v1alpha1
kind: Sandbox
metadata:
  name: isolated-sandbox
spec:
  templateRef:
    name: python-sandbox
  network:
    spec:
      # Creates an owned NetworkTemplate (garbage-collected with sandbox)
      allowedEgress:
        - "10.0.0.0/8"
      dnsServers:
        - "8.8.8.8"
```

---

## Lifecycle States

Sandboxes progress through these states:

```
┌─────────┐     ┌──────────┐     ┌─────────┐     ┌─────────────┐     ┌─────────┐
│ pending │────▶│ starting │────▶│ running │────▶│ terminating │────▶│ stopped │
└─────────┘     └──────────┘     └─────────┘     └─────────────┘     └─────────┘
                                      │                                    │
                                      │                                    │
                                      ▼                                    ▼
                                 [Operations]                          [error]
                                 - Execute                         (if failed)
                                 - Upload
                                 - Download
```

| State | Description | Operations Allowed |
|-------|-------------|-------------------|
| `pending` | Sandbox created, awaiting pod | None |
| `starting` | Pod created, containers starting | None |
| `running` | Pod ready, accepting commands | Execute, Upload, Download |
| `terminating` | Graceful shutdown in progress | None |
| `stopped` | Sandbox terminated normally | None |
| `error` | Sandbox failed | None |

---

## Conditions vs Phases

Isola uses the Kubernetes **conditions pattern** instead of the deprecated phase pattern. Multiple conditions can be true simultaneously:

```yaml
status:
  conditions:
    - type: Ready
      status: "True"
      reason: "AllConditionsMet"
      message: "Sandbox is ready"
      lastTransitionTime: "2025-01-10T10:00:00Z"
    - type: PodReady
      status: "True"
      reason: "PodRunning"
    - type: NetworkConfigured
      status: "True"
      reason: "NetworkPolicyApplied"
```

| Condition | Meaning |
|-----------|---------|
| `Ready` | Aggregate: Pod ready + Network configured + Not timed out |
| `PodReady` | Sandbox pod is running and healthy |
| `NetworkConfigured` | NetworkPolicy has been applied |
| `TimedOut` | Sandbox exceeded its timeout |
| `SnapshottingFilesystem` | Filesystem snapshot in progress (shutdown policy) |

---

## Agent Sidecar

Every sandbox pod includes an **isola-agent** sidecar container that handles:

- **File uploads** - Writing files to the sandbox filesystem
- **File downloads** - Reading files from the sandbox filesystem
- **Command execution** - Running commands in the main container
- **Process discovery** - Finding the main container process via `/proc`

```
┌─────────────────────────────────────────────────┐
│               Sandbox Pod                        │
│                                                 │
│  ┌─────────────────┐   ┌─────────────────────┐  │
│  │ Main Container  │   │   isola-agent       │  │
│  │                 │   │                     │  │
│  │  - User code    │◀──│  - File operations  │  │
│  │  - Your image   │   │  - Command proxy    │  │
│  │                 │   │  - Port 8080        │  │
│  └─────────────────┘   └─────────────────────┘  │
│                               ▲                  │
│                               │                  │
└───────────────────────────────│──────────────────┘
                                │
                        Gateway requests
```

The agent runs as root to access `/proc/<pid>/root` of the main container for file operations.

---

## Timeouts and Shutdown Policies

### Timeouts

Sandboxes auto-terminate after their configured timeout:

```yaml
# In SandboxTemplate
spec:
  timeoutSeconds: 300  # Terminate after 5 minutes
```

The operator converts this to an absolute timestamp:

```yaml
# In Sandbox status
status:
  timeoutAt: "2025-01-10T10:05:00Z"
```

### Shutdown Policies

Control what happens when a sandbox terminates:

| Policy | Behavior |
|--------|----------|
| `Delete` | Delete the pod immediately (default) |
| `SnapshotFilesystem` | Upload filesystem to S3 before deletion |

```yaml
spec:
  shutdownPolicy:
    policy: SnapshotFilesystem
    # Filesystem will be uploaded to configured S3 bucket
```

---

## Multi-tenancy

Isola supports multi-tenant deployments via API key-based tenant isolation:

- Each API key identifies a tenant
- Sandboxes are labeled with tenant ID
- Gateway filters list operations by tenant
- S3 file paths include tenant ID: `uploads/{tenant}/{sandbox}/{file}`

---

## Next Steps

Deep dive into each resource:

- **[Sandbox](./sandbox.md)** - Running sandbox instances
- **[SandboxTemplate](./sandbox-template.md)** - Reusable configurations
- **[NetworkTemplate](./network-template.md)** - Network isolation policies
