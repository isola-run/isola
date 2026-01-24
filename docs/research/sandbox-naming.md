# Sandbox Naming Research

## Current State

The system currently uses **three separate identifiers** for sandboxes:

| Identifier | Example | Where Generated | Storage Location | Used By |
|------------|---------|-----------------|------------------|---------|
| UUID | `550e8400-e29b-41d4-a716-446655440000` | Gateway (`uuid.New()`) | Label `sandbox-id` | Gateway API endpoints |
| K8s resource name | `sandbox-550e8400` | Gateway (derived from UUID) | `metadata.name` | Operator, K8s, Pod naming |
| Display name | `my-sandbox` | User-provided | Annotation `isola.run/sandbox-name` | API responses only |

### Problems with Current Approach

1. **Mapping complexity**: Code must convert between UUID and K8s name in multiple places
2. **Collision risk**: 8-char truncation of UUID for K8s name can cause collisions (~1 in 4 billion, but still non-zero)
3. **Redundant storage**: UUID stored in label, then truncated version used as resource name
4. **Non-unique display names**: Multiple sandboxes can have the same user-provided name
5. **Confusing TODO comment**: `// why this exists? ('sandbox-id')` in operator code shows even maintainers are confused

### Current Flow

```
Client Request: POST /api/v1/sandboxes { "name": "my-sandbox" }
                              ↓
Gateway generates UUID: "550e8400-e29b-41d4-a716-446655440000"
                              ↓
Creates Sandbox CR:
  - metadata.name: "sandbox-550e8400"           ← K8s identifier
  - labels["sandbox-id"]: "550e8400-e29b..."    ← Full UUID (redundant?)
  - annotations["isola.run/sandbox-name"]: "my-sandbox"  ← Display only
                              ↓
API Response: { "id": "550e8400-e29b...", "name": "my-sandbox" }
```

---

## Proposed Solution: Single Unified Name

Use the **Kubernetes resource name** (`metadata.name`) as the **sole identifier** throughout the entire system.

### Design Principles

1. **Single source of truth**: `metadata.name` is the sandbox's identity
2. **DNS-1123 compliant**: Required by K8s (lowercase alphanumeric + `-`, max 63 chars)
3. **URL-safe**: Can be used directly in REST API paths
4. **Human-friendly when possible**: Memorable names improve UX
5. **Guaranteed unique**: K8s enforces uniqueness within namespace

### Recommended Approach: Server-Generated Short IDs with Optional User Names

#### Option A: NanoID-style Short IDs (Recommended)

Generate unique, URL-safe identifiers like modern APIs do.

```
Format: sb-<nanoid12>
Examples: sb-v1stgxr8z5jd, sb-qi6b2loa3mxp, sb-k8np4wht1ycf
```

**Characteristics:**
- 12-char NanoID = 21 bits entropy per char × 12 = ~71 bits entropy
- Collision probability: ~1 in 2^35 after 1 million sandboxes
- Alphabet: `0123456789abcdefghijklmnopqrstuvwxyz` (DNS-safe)
- Total length: 15 chars (`sb-` + 12)

**Implementation:**
```go
import "github.com/jaevor/go-nanoid"

func GenerateSandboxName() string {
    // DNS-safe alphabet (lowercase alphanumeric only)
    gen, _ := nanoid.CustomASCII("0123456789abcdefghijklmnopqrstuvwxyz", 12)
    return "sb-" + gen()
}
```

#### Option B: Human-Friendly Names (Docker/Heroku style)

Generate memorable names from adjective-noun combinations.

```
Format: <adjective>-<noun>-<4hex>
Examples: swift-falcon-a7f2, calm-river-3b9c, bold-summit-e1d4
```

**Characteristics:**
- ~100 adjectives × ~100 nouns × 65536 suffixes = ~655 million combinations
- Much more memorable for humans
- Slightly longer (typically 15-25 chars)

**Implementation:**
```go
var adjectives = []string{"swift", "calm", "bold", "keen", "warm", ...}
var nouns = []string{"falcon", "river", "summit", "harbor", "meadow", ...}

func GenerateSandboxName() string {
    adj := adjectives[rand.Intn(len(adjectives))]
    noun := nouns[rand.Intn(len(nouns))]
    suffix := fmt.Sprintf("%04x", rand.Intn(65536))
    return fmt.Sprintf("%s-%s-%s", adj, noun, suffix)
}
```

#### Option C: User-Provided Names with Fallback

Allow users to specify their preferred name, fall back to auto-generated.

```go
func CreateSandbox(req CreateSandboxRequest) string {
    if req.Name != "" && isValidDNSName(req.Name) {
        // Check uniqueness
        if !sandboxExists(req.Name) {
            return req.Name
        }
        // Append suffix if name taken
        return fmt.Sprintf("%s-%s", req.Name, generateShortID(4))
    }
    return generateSandboxName() // Auto-generate
}
```

---

## Comparison Table

| Aspect | Option A (NanoID) | Option B (Human-Friendly) | Option C (User-Provided) |
|--------|-------------------|---------------------------|--------------------------|
| Uniqueness | Excellent (crypto-random) | Good (random + suffix) | Requires validation |
| Memorability | Poor | Excellent | Depends on user |
| Length | 15 chars | 15-25 chars | Variable |
| Collision handling | Not needed | Not needed | Suffix appended |
| Implementation complexity | Simple | Medium | Medium |
| API consistency | Always same format | Always same format | Variable format |

---

## Recommended Implementation

### Phase 1: Simplify to Single Name

**Changes required:**

1. **Gateway** (`internal/gateway/`):
   - Generate name at creation time (not UUID)
   - Use name directly as `metadata.name`
   - Remove `sandbox-id` label
   - Remove `isola.run/sandbox-name` annotation
   - API: `id` field becomes the K8s name (or rename to `name`)

2. **API Model** (`internal/gateway/models/sandbox.go`):
   ```go
   type Sandbox struct {
       Name         string            `json:"name"`  // The sole identifier
       State        SandboxState      `json:"state"`
       // ... rest unchanged
   }
   ```

3. **OpenAPI Spec** (`cmd/gateway/openapi.yaml`):
   - Change `/{id}` to `/{name}` in all paths
   - Or keep as `/{id}` but document it's the K8s name
   - Update format from `uuid` to `string` with pattern

4. **Operator** (`internal/operator/controller/`):
   - Remove TODO comment about `sandbox-id`
   - Use `sandbox.Name` consistently everywhere (already mostly does)

5. **K8s Manager** (`internal/gateway/kubernetes/manager.go`):
   ```go
   func (m *Manager) CreateSandboxCR(ctx context.Context, name string, req CreateSandboxRequest) error {
       sandboxBody := &unstructured.Unstructured{
           Object: map[string]interface{}{
               "metadata": map[string]interface{}{
                   "name":      name,  // Direct use, no derivation
                   "namespace": m.namespace,
                   "labels": map[string]interface{}{
                       "managed-by": "isola-gw",
                   },
                   // No sandbox-id label needed
                   // No sandbox-name annotation needed
               },
               // ...
           },
       }
   }
   ```

6. **Lookups simplified**:
   ```go
   // Before: UUID → derived name → K8s Get
   sandboxName := fmt.Sprintf("sandbox-%s", sandboxID[:8])
   sandbox, err := client.Get(ctx, sandboxName, ...)

   // After: Direct lookup by name
   sandbox, err := client.Get(ctx, name, ...)
   ```

### Phase 2: Consider User-Provided Names

If user experience benefits from memorable names:

1. Accept optional `name` in create request
2. Validate DNS-1123 compliance
3. Check uniqueness in namespace
4. Auto-generate if not provided or collision detected

---

## Name Generation Location

**Recommended: Gateway generates the name**

Rationale:
- Gateway is the entry point for sandbox creation
- Can validate/check uniqueness before K8s call
- Keeps operator simple (just reconciles what exists)
- Matches current pattern (just removing UUID indirection)

Alternative (operator generates):
- Would require admission webhook or mutating webhook
- More complex, not necessary given current architecture

---

## Migration Considerations

Since "backward compatibility is not required at this stage" (per CLAUDE.md):

1. Update CRD if needed (likely no changes needed since `metadata.name` is standard)
2. Update all code to use single name pattern
3. Update tests to use new naming
4. Update documentation/OpenAPI spec

No migration of existing sandboxes needed since breaking changes are allowed.

---

## Files to Modify

| File | Changes |
|------|---------|
| `internal/gateway/models/sandbox.go` | Replace `ID` with `Name` as sole identifier |
| `internal/gateway/models/requests.go` | Update `CreateSandboxRequest.Name` semantics |
| `internal/gateway/handlers/sandboxes.go` | Generate name, use directly |
| `internal/gateway/kubernetes/manager.go` | Remove UUID derivation, use name directly |
| `cmd/gateway/openapi.yaml` | Update path params and schemas |
| `internal/operator/controller/sandbox_controller.go` | Remove `sandbox-id` label logic |
| Tests | Update to use new naming pattern |

---

## Summary

| Current | Proposed |
|---------|----------|
| 3 identifiers (UUID, K8s name, display name) | 1 identifier (K8s name) |
| UUID stored in label | No separate storage needed |
| Derived K8s name (`sandbox-<8chars>`) | Generated unique name directly |
| Display name in annotation | K8s name IS the display name |
| Mapping code in multiple places | Direct use everywhere |

**Recommendation**: Implement **Option A (NanoID)** for simplicity and reliability, with future option to add user-provided names if needed.
