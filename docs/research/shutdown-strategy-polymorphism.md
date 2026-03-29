# Shutdown Strategy Polymorphism: Research & Options

## Problem Statement

The current `ShutdownPolicy` CRD type is:

```go
type ShutdownPolicy struct {
    Strategy       SandboxShutdownStrategy `json:"strategy,omitempty"`    // Delete | SnapshotRootfs
    TimeoutSeconds *int64                  `json:"timeoutSeconds,omitempty"` // only used for SnapshotRootfs
}
```

We want to add `snapshotName` (user-chosen name for the resulting rootfs snapshot), but it only applies to `SnapshotRootfs`, not `Delete`. `TimeoutSeconds` already has this same problem. Adding more strategy-specific fields to a flat struct makes the type progressively "overfit" to one variant while accumulating fields irrelevant to others.

The goal is to find an idiomatic pattern that:
1. Keeps the CRD clean and extensible for future shutdown actions
2. Maps well to a REST API (gateway layer)
3. Produces good SDK ergonomics (Python discriminated unions)
4. Uses CEL/kubebuilder validation to enforce "field X is only valid when strategy is Y"

---

## Current State in the Codebase

- **CRD**: `ShutdownPolicy` lives in `api/v1alpha1/sandbox_types.go` with a flat `Strategy` enum + `TimeoutSeconds`
- **Controller**: `executeShutdownPolicy()` in `sandbox_controller.go:880` switches on `Strategy`; `createShutdownSnapshot()` at line 1118 hardcodes `SnapshotName: sandbox.Name`
- **Gateway**: Shutdown policy is **not yet exposed** in the REST API — no gateway handler references it
- **SDK**: No shutdown policy types in the Python SDK yet
- **Snapshot naming**: Currently `GetShutdownSnapshotName()` returns `"{sandboxName}-shutdown"` — no user control

---

## Industry Patterns (Grounded Research)

### Pattern 1: Flat Struct with Optional Fields (Current Approach)

**Source**: Kubernetes `DeploymentStrategy`

```go
// k8s.io/api/apps/v1/types.go
type DeploymentStrategy struct {
    Type          DeploymentStrategyType           `json:"type,omitempty"`
    RollingUpdate *RollingUpdateDeployment         `json:"rollingUpdate,omitempty"`
}
```

Only `RollingUpdate` has the extra `rollingUpdate` config; `Recreate` ignores it. The API convention is: the field is simply ignored (or rejected) when the discriminator doesn't match.

**How it works**: The discriminator field (`Type`) tells you which optional sub-fields are relevant. Irrelevant fields are either silently ignored or validated away.

**Pros**:
- Simple, familiar to every K8s user
- Minimal CRD schema complexity
- Easy to extend: just add another optional field for a new strategy
- controller-gen handles it natively

**Cons**:
- "Loose" typing — nothing in the Go struct prevents setting `rollingUpdate` when `type: Recreate`
- Requires CEL or webhook validation to enforce field relevance
- As strategies multiply, the flat struct accumulates many optional fields

**K8s upstream examples using this pattern**:
- `DeploymentStrategy` (Type + RollingUpdate params)
- `PersistentVolumeSource` (dozens of optional provider-specific structs, though without an explicit discriminator)
- `ServiceSpec` (Type field + ClusterIP/NodePort/LoadBalancer-specific fields)

---

### Pattern 2: Discriminated Union with Embedded Structs (K8s Canonical)

**Source**: Kubernetes `Probe`, `LifecycleHandler`, `VolumeSource`

```go
// k8s.io/api/core/v1/types.go
type ProbeHandler struct {
    Exec      *ExecAction      `json:"exec,omitempty"`
    HTTPGet   *HTTPGetAction   `json:"httpGet,omitempty"`
    TCPSocket *TCPSocketAction `json:"tcpSocket,omitempty"`
    GRPC      *GRPCAction      `json:"grpc,omitempty"`
}
```

No explicit discriminator field — the "type" is implied by which pointer is non-nil. Exactly one must be set.

**The K8s API Conventions document** ([api-conventions.md#unions](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#unions)) calls these "discriminated unions" and states:

> "Unions should have a discriminator field that indicates which union member is set... The discriminator must be a required string field named after the union... Each union member must be an optional field."

The upstream doc recommends a `+unionDiscriminator` tag for documentation, though it is **not enforced by apiserver** — it's purely a convention marker. The actual enforcement is done via CEL XValidation or admission webhooks.

**Applied to our case**:

```go
type ShutdownPolicy struct {
    // +unionDiscriminator
    Strategy             SandboxShutdownStrategy     `json:"strategy"`
    SnapshotRootfs       *SnapshotRootfsConfig       `json:"snapshotRootfs,omitempty"`
}

type SnapshotRootfsConfig struct {
    SnapshotName   *string `json:"snapshotName,omitempty"`
    TimeoutSeconds *int64  `json:"timeoutSeconds,omitempty"`
}
```

**Pros**:
- Clean separation: each strategy's params are in their own struct
- Self-documenting: the struct name tells you what it's for
- Follows the K8s API conventions recommendation
- CEL validation is straightforward (see below)
- Excellent SDK mapping (Python/TS discriminated unions)

**Cons**:
- Slightly more verbose YAML for users
- Breaking change from current flat `TimeoutSeconds`
- One more level of nesting

---

### Pattern 3: Implicit Union (No Discriminator)

**Source**: Kubernetes `VolumeSource`, `PersistentVolumeSource`

```go
type VolumeSource struct {
    HostPath              *HostPathVolumeSource              `json:"hostPath,omitempty"`
    EmptyDir              *EmptyDirVolumeSource              `json:"emptyDir,omitempty"`
    GCEPersistentDisk     *GCEPersistentDiskVolumeSource     `json:"gcePersistentDisk,omitempty"`
    AWSElasticBlockStore  *AWSElasticBlockStoreVolumeSource  `json:"awsElasticBlockStore,omitempty"`
    // ... 20+ more
}
```

No `type` field at all. Exactly one pointer must be non-nil. The "type" is inferred from which field is set.

**Pros**:
- No redundant discriminator field
- Each variant is fully self-contained

**Cons**:
- Harder to validate (must enforce exactly-one-of)
- Worse for OpenAPI/SDK generation — no discriminator property for `oneOf`
- K8s API conventions now **recommend against** this for new APIs in favor of explicit discriminators
- Harder to switch on in controller code

**Verdict**: This is the legacy K8s pattern. New APIs should not use it.

---

### Pattern 4: Argo Workflows / Tekton Style (Embedded Variant Structs)

**Source**: Argo Workflows `Template`

```go
// argoproj/argo-workflows/workflow/v1alpha1/types.go
type Template struct {
    Name       string            `json:"name,omitempty"`
    Container  *corev1.Container `json:"container,omitempty"`
    Script     *ScriptTemplate   `json:"script,omitempty"`
    Resource   *ResourceTemplate `json:"resource,omitempty"`
    DAG        *DAGTemplate      `json:"dag,omitempty"`
    Steps      []ParallelSteps   `json:"steps,omitempty"`
    Suspend    *SuspendTemplate  `json:"suspend,omitempty"`
    // ... common fields ...
    Timeout    string            `json:"timeout,omitempty"`
}
```

This is Pattern 3 (implicit union) but with shared fields at the same level. The "overfit" problem is accepted: `Timeout` applies to some template types but not all.

**Pros**: Familiar to Argo users, flat structure
**Cons**: Exactly the problem you're describing — fields that only apply to some variants

---

### Pattern 5: Velero Backup/Restore Actions (Plugin-based, External References)

**Source**: Velero `BackupSpec`

```go
type BackupSpec struct {
    IncludedNamespaces []string               `json:"includedNamespaces,omitempty"`
    Hooks              BackupHooks            `json:"hooks,omitempty"`
    StorageLocation    string                 `json:"storageLocation,omitempty"`
    // ...
}
```

Velero's action-specific config lives in separate plugin CRDs, not inline. The backup spec references a storage location by name, and the storage location CRD defines provider-specific config.

**Applied to our case**: The shutdown policy would just say `strategy: SnapshotRootfs` and reference a separate `ShutdownSnapshotConfig` CR or ConfigMap.

**Pros**: Maximum decoupling, extensible via plugins
**Cons**: Massive overkill for 2-3 strategies. Introduces operational complexity (extra CRs to manage). Bad SDK ergonomics.

**Verdict**: Wrong level of indirection for this problem.

---

### Pattern 6: Gateway API HTTPRoute Filters (Discriminated Union with CEL)

**Source**: Kubernetes Gateway API `HTTPRouteFilter`

```go
// sigs.k8s.io/gateway-api/apis/v1/types.go
type HTTPRouteFilter struct {
    // +unionDiscriminator
    Type                   HTTPRouteFilterType            `json:"type"`
    RequestHeaderModifier  *HTTPHeaderFilter              `json:"requestHeaderModifier,omitempty"`
    ResponseHeaderModifier *HTTPHeaderFilter              `json:"responseHeaderModifier,omitempty"`
    RequestMirror          *HTTPRequestMirrorFilter       `json:"requestMirror,omitempty"`
    RequestRedirect        *HTTPRequestRedirectFilter     `json:"requestRedirect,omitempty"`
    URLRewrite             *HTTPURLRewriteFilter          `json:"urlRewrite,omitempty"`
    ExtensionRef           *LocalObjectReference          `json:"extensionRef,omitempty"`
}
```

With CEL validation:

```go
// +kubebuilder:validation:XValidation:rule="self.type == 'RequestHeaderModifier' ? has(self.requestHeaderModifier) : !has(self.requestHeaderModifier)"
// +kubebuilder:validation:XValidation:rule="self.type == 'RequestRedirect' ? has(self.requestRedirect) : !has(self.requestRedirect)"
// ... one rule per variant
```

This is the **gold standard** for modern K8s discriminated unions. It combines Pattern 2 (explicit discriminator + embedded structs) with CEL validation to enforce correctness at the API level.

**Pros**:
- Widely adopted in the K8s ecosystem (Gateway API is a SIG-maintained project)
- CEL rules are declarative, no webhook needed
- Excellent OpenAPI mapping (`oneOf` + `discriminator`)
- Clean SDK generation

**Cons**:
- Verbose CEL rules (one per variant per field), though they're formulaic
- Requires controller-gen support for XValidation (available since controller-tools v0.11+)

---

## CEL Validation for Discriminated Unions

### Syntax for "field required when strategy matches, forbidden otherwise"

```go
// +kubebuilder:validation:XValidation:rule="self.strategy == 'SnapshotRootfs' ? has(self.snapshotRootfs) : !has(self.snapshotRootfs)",message="snapshotRootfs must be set when strategy is SnapshotRootfs and must not be set otherwise"
```

### Existing CEL usage in isola

The codebase already uses XValidation in `sandbox_types.go`:
- Immutability checks: `!has(oldSelf.network) || has(self.network)`
- IP validation: `self.all(s, isIP(s))`
- List uniqueness: `self.all(i, self.all(j, i == j || i.containerName != j.containerName))`

Adding discriminated union CEL rules is consistent with existing patterns.

### "Exactly one of" validation

For implicit unions (Pattern 3), you'd need:

```go
// +kubebuilder:validation:XValidation:rule="[has(self.delete), has(self.snapshotRootfs)].exists_one(x, x)",message="exactly one shutdown action must be specified"
```

For discriminated unions (Pattern 2/6), you validate each variant against the discriminator (as shown above), which is clearer.

---

## REST API / OpenAPI Mapping

### OpenAPI 3.x `oneOf` + `discriminator`

```yaml
ShutdownPolicy:
  type: object
  required: [strategy]
  discriminator:
    propertyName: strategy
    mapping:
      delete: '#/components/schemas/DeleteShutdownPolicy'
      snapshotRootfs: '#/components/schemas/SnapshotRootfsShutdownPolicy'
  oneOf:
    - $ref: '#/components/schemas/DeleteShutdownPolicy'
    - $ref: '#/components/schemas/SnapshotRootfsShutdownPolicy'

DeleteShutdownPolicy:
  type: object
  properties:
    strategy:
      type: string
      enum: [delete]

SnapshotRootfsShutdownPolicy:
  type: object
  properties:
    strategy:
      type: string
      enum: [snapshotRootfs]
    snapshotName:
      type: string
    timeoutSeconds:
      type: integer
```

### Huma (Go HTTP framework used in gateway)

Huma supports `oneOf` via Go interfaces or struct embedding. The simplest approach for the gateway is a flat REST struct that mirrors the CRD discriminated union:

```go
type ShutdownPolicy struct {
    Strategy       string                  `json:"strategy" enum:"delete,snapshotRootfs"`
    SnapshotRootfs *SnapshotRootfsConfig   `json:"snapshotRootfs,omitempty"`
}
```

The gateway conversion layer maps REST vocabulary to CRD vocabulary as it already does for other types.

### Python SDK (Pydantic)

Pydantic v2 natively supports discriminated unions:

```python
from typing import Literal, Union
from pydantic import BaseModel, Field

class DeleteShutdownPolicy(BaseModel):
    strategy: Literal["delete"] = "delete"

class SnapshotRootfsShutdownPolicy(BaseModel):
    strategy: Literal["snapshot_rootfs"] = "snapshot_rootfs"
    snapshot_name: str | None = None
    timeout_seconds: int | None = None

ShutdownPolicy = Annotated[
    Union[DeleteShutdownPolicy, SnapshotRootfsShutdownPolicy],
    Field(discriminator="strategy"),
]
```

This gives excellent IDE support, type narrowing, and validation.

---

## Options for Isola

### Option A: Keep Flat, Add Fields (Minimal Change)

```go
type ShutdownPolicy struct {
    Strategy       SandboxShutdownStrategy `json:"strategy,omitempty"`
    TimeoutSeconds *int64                  `json:"timeoutSeconds,omitempty"`
    SnapshotName   *string                 `json:"snapshotName,omitempty"`   // NEW
}
// +kubebuilder:validation:XValidation:rule="self.strategy == 'Delete' ? !has(self.snapshotName) : true",message="snapshotName is only valid for SnapshotRootfs strategy"
// +kubebuilder:validation:XValidation:rule="self.strategy == 'Delete' ? !has(self.timeoutSeconds) : true",message="timeoutSeconds is only valid for SnapshotRootfs strategy"
```

- **Effort**: Low
- **Breaking**: No
- **Extensibility**: Degrades as strategies multiply (each new strategy adds more irrelevant optional fields)
- **Precedent**: `DeploymentStrategy`
- **SDK mapping**: Flat Pydantic model with optional fields, no type narrowing
- **Risk**: If you add a 3rd strategy with its own params, the struct becomes a "god object"

### Option B: Discriminated Union with Nested Config (Recommended)

```go
// +kubebuilder:validation:XValidation:rule="self.strategy == 'SnapshotRootfs' ? has(self.snapshotRootfs) : !has(self.snapshotRootfs)",message="snapshotRootfs config must be set when strategy is SnapshotRootfs"
type ShutdownPolicy struct {
    // +unionDiscriminator
    Strategy       SandboxShutdownStrategy `json:"strategy,omitempty"`
    SnapshotRootfs *SnapshotRootfsConfig   `json:"snapshotRootfs,omitempty"`
}

type SnapshotRootfsConfig struct {
    // +optional
    SnapshotName   *string `json:"snapshotName,omitempty"`
    // +optional
    // +kubebuilder:default=300
    TimeoutSeconds *int64  `json:"timeoutSeconds,omitempty"`
}
```

- **Effort**: Medium (move `TimeoutSeconds` into nested struct, update controller + tests)
- **Breaking**: Yes (but CLAUDE.md says that's fine)
- **Extensibility**: Excellent — each new strategy gets its own config struct
- **Precedent**: Gateway API `HTTPRouteFilter`, K8s API conventions recommendation
- **SDK mapping**: Clean Pydantic discriminated union with type narrowing
- **YAML UX**:
  ```yaml
  shutdownPolicy:
    strategy: SnapshotRootfs
    snapshotRootfs:
      snapshotName: my-checkpoint
      timeoutSeconds: 600
  ```

### Option C: Implicit Union (No Discriminator)

```go
// +kubebuilder:validation:XValidation:rule="[has(self.delete), has(self.snapshotRootfs)].exists_one(x, x)",message="exactly one shutdown action must be specified"
type ShutdownPolicy struct {
    Delete         *DeleteConfig           `json:"delete,omitempty"`
    SnapshotRootfs *SnapshotRootfsConfig   `json:"snapshotRootfs,omitempty"`
}
```

- **Effort**: Medium
- **Breaking**: Yes
- **Extensibility**: Good
- **Precedent**: `VolumeSource` (but this pattern is now discouraged by K8s conventions)
- **SDK mapping**: Requires manual discrimination logic; Pydantic can't auto-discriminate without a shared field
- **Downside**: `DeleteConfig` would be an empty struct `{}`, which is awkward

### Option D: Strategy as Separate Config Object (Over-Engineered)

Reference a separate CR for shutdown config. Like Velero's storage locations.

- **Effort**: High
- **Breaking**: Yes
- **Extensibility**: Maximum (plugin-style)
- **Precedent**: Velero, Crossplane
- **Downside**: Massive overkill. Two CRs to manage for a simple shutdown choice.

---

## Recommendation

**Option B** is the clear winner for isola's situation:

1. It follows the K8s API conventions recommendation for discriminated unions
2. It matches the pattern used by Gateway API (the most modern, well-reviewed K8s API)
3. It maps cleanly to OpenAPI `oneOf` + `discriminator` for the REST layer
4. It produces excellent Pydantic discriminated unions in the Python SDK
5. The breaking change is acceptable per project policy
6. The codebase already uses CEL XValidation, so the enforcement pattern is familiar
7. It naturally solves the `snapshotName` problem — the field lives in `SnapshotRootfsConfig` where it belongs
8. Future strategies (e.g., `Hibernate`, `Checkpoint`) each get their own config struct without polluting others

The redundancy of `strategy: SnapshotRootfs` + `snapshotRootfs: {...}` is a deliberate K8s convention trade-off: the discriminator field enables efficient switching in controllers and clean OpenAPI generation, while the nested struct provides type-safe, variant-specific configuration.
