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

---

## Appendix: Decision-by-Decision Grounding for Option B

This section maps every structural decision in the proposed `ShutdownPolicy` to a concrete, verified upstream precedent.

### Proposed Design (for reference)

```go
// +kubebuilder:validation:XValidation:rule="!(has(self.snapshotRootfs) && self.strategy != 'SnapshotRootfs')",message="snapshotRootfs must be nil if strategy is not SnapshotRootfs"
// +kubebuilder:validation:XValidation:rule="!(!has(self.snapshotRootfs) && self.strategy == 'SnapshotRootfs')",message="snapshotRootfs must be specified for SnapshotRootfs strategy"
type ShutdownPolicy struct {
    // +unionDiscriminator
    // +kubebuilder:default=Delete
    // +kubebuilder:validation:Enum=Delete;SnapshotRootfs
    Strategy SandboxShutdownStrategy `json:"strategy,omitempty"`

    // +optional
    SnapshotRootfs *SnapshotRootfsConfig `json:"snapshotRootfs,omitempty"`
}

type SnapshotRootfsConfig struct {
    // +optional
    SnapshotName   *string `json:"snapshotName,omitempty"`
    // +optional
    // +kubebuilder:default=300
    // +kubebuilder:validation:Minimum=1
    TimeoutSeconds *int64  `json:"timeoutSeconds,omitempty"`
}
```

---

### Decision 1: Use an explicit `Strategy` enum discriminator (not implicit "which pointer is non-nil")

**Why not implicit?** The older K8s pattern (VolumeSource, ProbeHandler) has no discriminator — you infer the type from which pointer is set. KEP-1027 explicitly deprecates this for new APIs:

> "In some cases there are more discriminator values than there are member fields defined in the struct when that specific member requires no configuration."

The `Delete` strategy has no configuration. With an implicit union, you'd need an empty `DeleteConfig{}` struct — awkward and misleading. An explicit discriminator naturally handles "variants with no params."

**Precedent: `DeploymentStrategy`** (k8s.io/api/apps/v1/types.go, verified from source):

```go
type DeploymentStrategy struct {
    // +optional
    Type DeploymentStrategyType `json:"type,omitempty"`
    // Rolling update config params. Present only if DeploymentStrategyType = RollingUpdate.
    // +optional
    RollingUpdate *RollingUpdateDeployment `json:"rollingUpdate,omitempty"`
}
```

`Recreate` has no params struct — only `RollingUpdate` does. The `Type` discriminator makes this natural. Our `Delete` (no params) and `SnapshotRootfs` (has params) maps 1:1 to this pattern.

The upstream source even contains this TODO acknowledging the direction:
```
// TODO: Update this to follow our convention for oneOf, whatever we decide it to be.
```

**Precedent: Gateway API `HTTPRouteFilter`** (kubernetes-sigs/gateway-api, verified from source):

```go
type HTTPRouteFilter struct {
    // +unionDiscriminator
    // +kubebuilder:validation:Enum=RequestHeaderModifier;ResponseHeaderModifier;RequestMirror;RequestRedirect;URLRewrite;ExtensionRef;CORS
    // +required
    Type HTTPRouteFilterType `json:"type"`
    // ...variant pointer fields...
}
```

Uses an explicit `Type` enum with `+unionDiscriminator` marker. This is the most modern, SIG-reviewed K8s API (Gateway API is a Kubernetes SIG-Network project with extensive review).

**Precedent: KEP-1027** (kubernetes/enhancements, verified from source):

```go
type Union struct {
    // +unionDiscriminator
    // +required
    UnionType UnionType
    // +unionMember
    // +optional
    FieldA int
}
```

The KEP's formal recommendation: discriminator is a `+required` string enum, members are `+optional` pointers.

---

### Decision 2: Discriminator value "Delete" exists WITHOUT a corresponding member struct

Our `Delete` strategy needs no configuration — no timeout, no snapshot name. So there's no `*DeleteConfig` field. The discriminator value `Delete` is valid but has no corresponding pointer.

**Precedent: `DeploymentStrategy`** (verified from source):

```go
const (
    RecreateDeploymentStrategyType DeploymentStrategyType = "Recreate"
    RollingUpdateDeploymentStrategyType DeploymentStrategyType = "RollingUpdate"
)
```

Two enum values, but only one member field (`RollingUpdate *RollingUpdateDeployment`). `Recreate` has no config struct — it's a valid discriminator value with no associated params. This is the exact same shape as our `Delete` (no params) vs `SnapshotRootfs` (has params).

**Precedent: KEP-1027** (verified from source):

> "In some cases there are more discriminator values than there are member fields defined in the struct when that specific member requires no configuration."

KEP-1027 explicitly blesses this pattern. A discriminator value without a corresponding member is not a gap — it's the designed way to handle parameterless variants.

---

### Decision 3: Strategy-specific params live in a nested struct (`*SnapshotRootfsConfig`), not flat on `ShutdownPolicy`

Currently `TimeoutSeconds` sits flat on `ShutdownPolicy` with a comment "Only used when Strategy is SnapshotRootfs." Adding `SnapshotName` the same way worsens this — two fields relevant to only one variant, with more likely to come. Moving them into `SnapshotRootfsConfig` scopes them to where they belong.

**Precedent: `DeploymentStrategy`** (verified from source):

```go
type DeploymentStrategy struct {
    Type          DeploymentStrategyType    `json:"type,omitempty"`
    RollingUpdate *RollingUpdateDeployment  `json:"rollingUpdate,omitempty"`
}

type RollingUpdateDeployment struct {
    MaxUnavailable *intstr.IntOrString `json:"maxUnavailable,omitempty"`
    MaxSurge       *intstr.IntOrString `json:"maxSurge,omitempty"`
}
```

`MaxUnavailable` and `MaxSurge` are not flat on `DeploymentStrategy` — they're nested in `RollingUpdateDeployment`. This is the exact analogy: `TimeoutSeconds` and `SnapshotName` belong in `SnapshotRootfsConfig`, not flat on `ShutdownPolicy`.

**Precedent: Gateway API `HTTPRouteFilter`** (verified from source):

Each filter type's params live in a separate struct:
- `RequestHeaderModifier *HTTPHeaderFilter`
- `RequestRedirect *HTTPRequestRedirectFilter`
- `URLRewrite *HTTPURLRewriteFilter`
- `RequestMirror *HTTPRequestMirrorFilter`

None of these fields' sub-parameters leak up to `HTTPRouteFilter` level. Each variant is fully encapsulated in its own type.

**Precedent: Gateway API `GRPCRouteFilter`** (verified from source — same pattern applied independently to gRPC):

```go
type GRPCRouteFilter struct {
    // +unionDiscriminator
    Type                   GRPCRouteFilterType       `json:"type"`
    RequestHeaderModifier  *HTTPHeaderFilter          `json:"requestHeaderModifier,omitempty"`
    ResponseHeaderModifier *HTTPHeaderFilter          `json:"responseHeaderModifier,omitempty"`
    RequestMirror          *HTTPRequestMirrorFilter   `json:"requestMirror,omitempty"`
    ExtensionRef           *LocalObjectReference      `json:"extensionRef,omitempty"`
}
```

The pattern is applied consistently across both HTTP and gRPC route types — it's not a one-off.

---

### Decision 4: Use paired CEL XValidation rules (Gateway API double-negation pattern)

The validation enforces: "if strategy is X, then X's config must be present; if strategy is not X, then X's config must be absent." We use the double-negation form: `!(has(self.field) && self.type != 'X')` and `!(!has(self.field) && self.type == 'X')`.

**Why not a single ternary rule?** The ternary form (`self.strategy == 'SnapshotRootfs' ? has(self.snapshotRootfs) : !has(self.snapshotRootfs)`) works, but the double-negation form is the established convention across the entire Gateway API. Using a non-standard form would be an unforced divergence.

**Precedent: Gateway API `HTTPRouteFilter`** (verified from source — 14 XValidation rules, one pair per variant):

```go
// +kubebuilder:validation:XValidation:message="filter.requestHeaderModifier must be nil if the filter.type is not RequestHeaderModifier",rule="!(has(self.requestHeaderModifier) && self.type != 'RequestHeaderModifier')"
// +kubebuilder:validation:XValidation:message="filter.requestHeaderModifier must be specified for RequestHeaderModifier filter.type",rule="!(!has(self.requestHeaderModifier) && self.type == 'RequestHeaderModifier')"
```

**Precedent: Gateway API `GRPCRouteFilter`** (verified from source — 8 XValidation rules, same pattern):

```go
// +kubebuilder:validation:XValidation:message="filter.requestHeaderModifier must be nil if the filter.type is not RequestHeaderModifier",rule="!(has(self.requestHeaderModifier) && self.type != 'RequestHeaderModifier')"
// +kubebuilder:validation:XValidation:message="filter.requestHeaderModifier must be specified for RequestHeaderModifier filter.type",rule="!(!has(self.requestHeaderModifier) && self.type == 'RequestHeaderModifier')"
```

The pattern is formulaic: for each member field, produce two rules. Applied identically in both HTTP and gRPC route types. Our `ShutdownPolicy` has one member field (`snapshotRootfs`), so we need exactly 2 rules.

**Why CEL and not a webhook?** The isola codebase already uses CEL XValidation in `sandbox_types.go` (lines 78, 105-108, 150-151) for immutability and IP validation. Adding discriminated union rules is consistent with the existing validation approach — no new infrastructure needed.

---

### Decision 5: The discriminator field name is `strategy` (not `type`)

Gateway API uses `type`. DeploymentStrategy uses `type`. We use `strategy`. Why?

**The existing field is already named `Strategy`.** This is not a new field — it already exists in the current `ShutdownPolicy` struct. Renaming it to `type` would be a gratuitous breaking change that contradicts the domain language. The CRD already reads `strategy: SnapshotRootfs`, and that's more descriptive than `type: SnapshotRootfs` in the sandbox context.

**KEP-1027 does not mandate a specific name.** The KEP says the discriminator field name "defaults to the go (i.e `CamelCase`) representation of the field name" and should match the JSON serialization. There is no requirement that it be called `type` — it just needs to be a string enum marked `+unionDiscriminator`.

**Precedent: `DeploymentStrategy` uses `Type`** but it's a deployment-generic concept. In isola, the parent struct is already `ShutdownPolicy` — calling the discriminator `strategy` inside a "policy" struct reads naturally as "the strategy within this policy."

---

### Decision 6: The member field name matches the discriminator value (`snapshotRootfs` for `SnapshotRootfs`)

**Precedent: `DeploymentStrategy`** (verified from source):

Discriminator value `RollingUpdate` → field name `rollingUpdate`. The field name is the camelCase form of the enum value.

**Precedent: Gateway API** (verified from source):

Discriminator value `RequestHeaderModifier` → field name `requestHeaderModifier`. Discriminator value `URLRewrite` → field name `urlRewrite`. Consistent: enum value PascalCase → field name camelCase.

Our design: discriminator value `SnapshotRootfs` → field name `snapshotRootfs`. Same convention.

**Precedent: KEP-1027** (verified from source):

> "The `<memberName>` should match the serialized JSON name of the field case-insensitively."

Our naming is exactly in line with this.

---

### Decision 7: `SnapshotName` is optional (not required) in `SnapshotRootfsConfig`

When the user doesn't specify a snapshot name, the controller generates one (currently `{sandboxName}-shutdown`). This is the common K8s pattern: provide a sensible default for names when the user doesn't care, but let them override when they do.

**Precedent: Kubernetes `generateName`** — resources can omit `name` and provide `generateName` for auto-generated names. The pattern of "optional user-chosen name, system-generated default" is core K8s.

**Precedent: `RollingUpdateDeployment`** (verified from source):

```go
type RollingUpdateDeployment struct {
    // +optional
    MaxUnavailable *intstr.IntOrString `json:"maxUnavailable,omitempty"`
    // +optional
    MaxSurge       *intstr.IntOrString `json:"maxSurge,omitempty"`
}
```

Both fields are optional with defaults (25%). Our `SnapshotName` (defaults to sandbox name) and `TimeoutSeconds` (defaults to 300) follow the same principle: optional with documented defaults applied by the controller.

---

### Decision 8: `+kubebuilder:default=Delete` on the discriminator (defaulting to the parameterless variant)

When `shutdownPolicy` is set but `strategy` is omitted, it defaults to `Delete` — the simplest, safest behavior. This means `shutdownPolicy: {}` is valid and equivalent to explicit deletion.

**Precedent: `DeploymentStrategy`** — While the Go type doesn't use a kubebuilder default marker (it predates them), the API documentation states "Default is RollingUpdate." The convention of defaulting the discriminator to the most common variant is established.

**Why default to `Delete` and not `SnapshotRootfs`?** `Delete` is the zero-config variant (no params needed). Defaulting to `SnapshotRootfs` would fail CEL validation if `snapshotRootfs` config is absent. Defaulting to the parameterless variant avoids this bootstrapping problem.

---

### Decision 9: The YAML has "redundant" nesting (`strategy: SnapshotRootfs` + `snapshotRootfs: ...`)

The user writes:
```yaml
shutdownPolicy:
  strategy: SnapshotRootfs
  snapshotRootfs:
    snapshotName: my-checkpoint
```

The `SnapshotRootfs` appears twice: once as the discriminator value, once as the field key. This feels redundant. Why not eliminate the discriminator and just use "which field is set" (implicit union)?

**Three reasons from upstream sources:**

1. **Controller code clarity.** Switching on `sandbox.Spec.ShutdownPolicy.Strategy` is a string comparison. Without a discriminator, the controller must check each pointer for nil — which is error-prone as variants grow. The `DeploymentStrategy` controller switches on `Type`, not on `RollingUpdate != nil`.

2. **OpenAPI generation.** The `discriminator.propertyName` in OpenAPI directly maps to this field. Without it, SDK generators must infer the variant from which optional field is set — most generators do this poorly (documented issues in openapi-typescript-codegen #751, hey-api/openapi-ts #3270, redux-toolkit #3369). An explicit discriminator is the only reliable path to good SDK codegen.

3. **K8s convention direction.** KEP-1027 states that all new unions MUST have a discriminator. The implicit pattern (VolumeSource, ProbeHandler) is legacy. Gateway API — the most recent SIG-approved API — uses explicit discriminators exclusively.

**Precedent: Stripe Payment Methods** — The industry's most respected API uses the same "redundant" pattern:
```json
{
  "type": "card",
  "card": { "brand": "visa", "last4": "4242" }
}
```
`type: "card"` + `"card": {...}` — the type name appears twice. Stripe chose this deliberately for SDK ergonomics and parsing reliability across dozens of language SDKs.

---

### Summary: Decision Provenance Table

| # | Decision | Primary Precedent | Secondary Precedent | Anti-Precedent (why not) |
|---|----------|-------------------|---------------------|--------------------------|
| 1 | Explicit discriminator enum | DeploymentStrategy, Gateway API HTTPRouteFilter, KEP-1027 | GRPCRouteFilter | VolumeSource/ProbeHandler (implicit, now deprecated for new APIs) |
| 2 | Parameterless variant has no member struct | DeploymentStrategy (`Recreate`), KEP-1027 text | — | cert-manager IssuerConfig (forces empty struct for simple variants) |
| 3 | Params nested in variant-specific struct | DeploymentStrategy (`RollingUpdateDeployment`), Gateway API | GRPCRouteFilter | Argo Template (flat, suffers from the exact problem we're solving) |
| 4 | Paired CEL double-negation rules | Gateway API HTTPRouteFilter (14 rules), GRPCRouteFilter (8 rules) | — | Single ternary rule (works but non-standard) |
| 5 | Discriminator named `strategy` not `type` | KEP-1027 (no name mandate), existing field | — | — |
| 6 | Member field name matches enum value (camelCase) | DeploymentStrategy, Gateway API, KEP-1027 text | Stripe API | — |
| 7 | Optional SnapshotName with controller default | RollingUpdateDeployment (optional with defaults), K8s generateName | — | — |
| 8 | Default to parameterless variant (`Delete`) | DeploymentStrategy (defaults to RollingUpdate, the most common) | — | — |
| 9 | "Redundant" discriminator + member key | Gateway API, DeploymentStrategy, KEP-1027, Stripe Payment Methods | — | VolumeSource (implicit, poor codegen) |
