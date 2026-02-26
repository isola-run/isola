# Security Review: Isola

**Date:** 2026-02-25
**Scope:** Full codebase — operator, api-gateway, sandbox-sidecar, uploader, Python SDK, Helm charts, CRDs

---

## Executive Summary

Isola is a Kubernetes-native sandboxing platform with a well-designed multi-layer security architecture: NetworkPolicies for network isolation, namespace separation, gVisor runtime sandboxing, and a REST API that narrows the attack surface compared to raw CRD access. The codebase demonstrates strong security awareness in several areas — the write-only environment variable pattern, comprehensive private IP range blocking, distroless container images, and proper command execution without shell injection.

However, the review identified **4 critical**, **6 high**, **10 medium**, and several low-severity findings. The most impactful issues are: (1) symlink-based sandbox escape in filesystem operations, (2) no validation of user-supplied PodTemplate fields allowing container breakout via direct CRD access, (3) no authentication on the API gateway, and (4) no TLS between gateway and sidecar.

---

## Findings Summary

| ID | Severity | Category | Finding |
|----|----------|----------|---------|
| F-01 | CRITICAL | Filesystem | Symlink-based sandbox escape on file write |
| F-02 | CRITICAL | Filesystem | Symlink-based sandbox escape on file read |
| F-03 | CRITICAL | Operator | No PodTemplate validation — sandbox escape via arbitrary PodSpec |
| F-04 | CRITICAL | API Gateway | No authentication or authorization on any endpoint |
| F-05 | HIGH | Filesystem | TOCTOU race between stat and open on file read |
| F-06 | HIGH | Filesystem | TOCTOU race between create and chown on file write |
| F-07 | HIGH | Comms | No TLS between api-gateway and sidecar |
| F-08 | HIGH | Comms | No request authentication between gateway and sidecar |
| F-09 | HIGH | Container | Sidecar container has minimal security hardening |
| F-10 | HIGH | Container | No `automountServiceAccountToken: false` on sandbox pods |
| F-11 | MEDIUM | API Gateway | No rate limiting or resource exhaustion protections |
| F-12 | MEDIUM | API Gateway | No HTTP server timeouts (slowloris vulnerability) |
| F-13 | MEDIUM | API Gateway | No request/response body size limits |
| F-14 | MEDIUM | API Gateway | No validation pattern on sandbox ID path parameter |
| F-15 | MEDIUM | Network | DNS nameserver as unrestricted egress channel |
| F-16 | MEDIUM | Operator | Network label injection bypass via PodTemplate labels |
| F-17 | MEDIUM | Operator | Operator RBAC is cluster-scoped (not namespace-scoped) |
| F-18 | MEDIUM | Comms | Sidecar accessible from sandbox container via localhost |
| F-19 | MEDIUM | Comms | No HTTP client timeouts on gateway-to-sidecar requests |
| F-20 | MEDIUM | SDK | No TLS enforcement; plain HTTP accepted without warning |
| F-21 | LOW | API Gateway | Sidecar 4xx error details forwarded verbatim to client |
| F-22 | LOW | API Gateway | Kubernetes error messages forwarded to client |
| F-23 | LOW | Filesystem | No file size limits on upload or download |
| F-24 | LOW | Filesystem | File created with 0666 permissions |
| F-25 | LOW | Command | Command entries never cleaned from in-memory map |
| F-26 | LOW | Network | No `maxItems` constraint on `allowedEgressCIDRs` |

---

## Critical Findings

### F-01: Symlink-Based Sandbox Escape on File Write

**File:** `internal/sandbox-sidecar/filesystem/filesystem.go:138-169`

The `PostFilesystem` handler constructs a host path via `filepath.Join(h.procFS.GetRoot(pid), resolvedPath)` and then calls `os.Create(targetPath)`. There is no check that `targetPath`, after kernel symlink resolution, remains within `/proc/<pid>/root`. A malicious sandbox process can:

1. Create a symlink at `/tmp/evil` pointing to an arbitrary path (e.g., `/proc/1/root/etc/shadow`)
2. Request the API to write to `/tmp/evil`
3. The sidecar follows the symlink and writes to the host filesystem

Since the sidecar runs as **root** (UID 0), it can follow arbitrary symlinks and write to any accessible path. The same issue applies to `mkdirAllChown` which uses `os.Stat` (follows symlinks) and `os.Chown` on paths.

**Impact:** Full sandbox escape. An attacker controlling code in a sandbox can write arbitrary files on the host node.

**Recommendation:** Use `openat2` with `RESOLVE_BENEATH` flag to confine all filesystem operations within `/proc/<pid>/root`. Alternatively, resolve symlinks with `filepath.EvalSymlinks()` and verify the result stays within the container root before every operation.

---

### F-02: Symlink-Based Sandbox Escape on File Read

**File:** `internal/sandbox-sidecar/filesystem/filesystem.go:199-221`

The `GetFilesystem` handler uses `os.Stat(targetPath)` and `os.Open(targetPath)`, both of which follow symlinks. A malicious sandbox can create a symlink pointing outside the container root to read arbitrary host files.

**Impact:** Arbitrary file read from the host filesystem — can leak secrets, private keys, and credentials.

**Recommendation:** Same as F-01. Additionally, use `os.Lstat` instead of `os.Stat` if symlink following is not needed, or validate the resolved path.

---

### F-03: No PodTemplate Validation — Sandbox Escape via Arbitrary PodSpec

**Files:** `api/v1alpha1/sandbox_types.go:108-111`, `internal/operator/controller/sandbox_controller.go:191-192`

The `PodTemplate` field uses `+kubebuilder:pruning:PreserveUnknownFields` and `+kubebuilder:validation:Schemaless`, accepting the full `corev1.PodTemplateSpec` without schema validation. The operator directly assigns the user's PodSpec to the sandbox Pod:

```go
Spec: sandbox.Spec.PodTemplate.Spec,  // line 192
```

The operator overrides only `RestartPolicy`, `ShareProcessNamespace`, `RuntimeClassName`, `PriorityClassName`, and DNS settings. It does **not** strip dangerous fields. A user with direct CRD access can set:

- `hostNetwork: true` — bypass all NetworkPolicies
- `hostPID: true` — see all host processes
- `privileged: true` on containers — full host access
- `serviceAccountName` — mount any ServiceAccount token
- `hostPath` volumes — mount arbitrary host directories
- `capabilities.add: [SYS_ADMIN, ...]` — dangerous capabilities

The REST API provides defense-in-depth by only exposing `Image`, `Command`, `Env`, and `Resources`, but this is bypassable by anyone with `kubectl` access.

**Impact:** Complete sandbox escape and potential cluster compromise.

**Recommendation:** Implement a validating admission webhook, or add explicit validation in `CreateSandboxPod()` to strip/reject `hostNetwork`, `hostPID`, `hostIPC`, `privileged`, `hostPath` volumes, arbitrary `serviceAccountName`, `automountServiceAccountToken`, and dangerous capabilities.

---

### F-04: No Authentication or Authorization on API Gateway

**File:** `cmd/api-gateway/main.go:138-159`

The API gateway has zero authentication or authorization. The chi router only has `httplog.RequestLogger` middleware. Any caller who can reach the service can:

- Create/list/delete ANY sandbox
- Execute arbitrary commands in ANY sandbox
- Read/write arbitrary files in ANY sandbox

There is no concept of sandbox ownership or multi-tenancy.

**Impact:** Full access to all sandboxes for any network-reachable caller. The default Helm values use `ClusterIP`, but dev values expose via `NodePort:30080`.

**Recommendation:** Implement authentication middleware (API key, JWT, or mTLS) and an ownership model for sandbox access control before any production deployment.

---

## High Findings

### F-05: TOCTOU Race Between Stat and Open on File Read

**File:** `internal/sandbox-sidecar/filesystem/filesystem.go:204-217`

There is a time-of-check-to-time-of-use race between `os.Stat(targetPath)` (checks regular file) and `os.Open(targetPath)` (opens the file). A malicious process can atomically replace a regular file with a symlink between the two operations.

**Recommendation:** Open the file first with `O_NOFOLLOW`, then `fstat` the file descriptor. Do not stat-then-open separately.

### F-06: TOCTOU Race Between Create and Chown on File Write

**File:** `internal/sandbox-sidecar/filesystem/filesystem.go:146-169`

After `os.Create(targetPath)`, the file is written, then `os.Chmod` and `os.Chown` are called on the **path** (not the file descriptor). A malicious process could replace the file with a symlink between create and chown, causing ownership change on a host file.

**Recommendation:** Use `dst.Chmod()` and `dst.Chown()` (operating on the file descriptor) instead of path-based operations.

### F-07: No TLS Between API Gateway and Sidecar

**Files:** `internal/api-gateway/command/command.go:123`, `cmd/sandbox-sidecar/main.go:87`

All communication uses plain HTTP. Command payloads (including environment variables with potential secrets), file contents, and stdin/stdout streams traverse the pod network unencrypted.

**Recommendation:** Implement mTLS between gateway and sidecar, or deploy a service mesh with transparent mTLS.

### F-08: No Request Authentication Between Gateway and Sidecar

The sidecar accepts all HTTP requests on port 10032 with no authentication. The NetworkPolicy restricts access to only the api-gateway, but this is a single layer of defense.

**Recommendation:** Add a shared secret or per-pod token authentication mechanism for defense-in-depth.

### F-09: Sidecar Container Has Minimal Security Hardening

**File:** `internal/operator/controller/sandbox_controller.go:119-131`

The sidecar only sets `RunAsUser: 0`. Missing:
- `AllowPrivilegeEscalation: false`
- `Capabilities.Drop: ["ALL"]`
- `ReadOnlyRootFilesystem: true`
- Resource limits

**Recommendation:** Add these security settings. The sidecar needs root UID for `/proc` access but not full capabilities.

### F-10: No `automountServiceAccountToken: false` on Sandbox Pods

The operator does not disable service account token mounting. Sandbox containers have access to the Kubernetes API by default.

**Recommendation:** Set `automountServiceAccountToken: false` in `CreateSandboxPod()`.

---

## Medium Findings

### F-11: No Rate Limiting

No rate limiting middleware on any endpoint. An attacker can rapidly create sandboxes, exhausting cluster resources.

### F-12: No HTTP Server Timeouts

**File:** `cmd/api-gateway/main.go:161-164`

The HTTP server has no `ReadHeaderTimeout`, `IdleTimeout`, or `WriteTimeout`, making it vulnerable to slowloris attacks.

**Recommendation:** Set `ReadHeaderTimeout: 10s`, `IdleTimeout: 120s`.

### F-13: No Request/Response Body Size Limits

File uploads and downloads have no size limits. The code has explicit TODO comments acknowledging this. An attacker can upload arbitrarily large files or trigger unbounded downloads.

### F-14: No Validation Pattern on Sandbox ID

The `{id}` path parameter has no validation pattern (unlike `{cmdId}` which has a UUID regex). This allows unnecessary K8s API calls and potential log injection.

**Recommendation:** Add `pattern:"^[a-z][a-z0-9]{1,62}$"` to sandbox ID parameters.

### F-15: DNS Nameserver as Egress Channel

**File:** `internal/operator/controller/network/builder.go:173-200`

Custom nameservers get a NetworkPolicy egress rule on port 53. This enables DNS tunneling for data exfiltration. Additionally, `169.254.169.254` could be specified as a nameserver (port 53 only).

**Recommendation:** Validate nameserver IPs against blocked CIDR ranges, especially rejecting `169.254.0.0/16`.

### F-16: Network Label Injection via PodTemplate Labels

**File:** `internal/operator/controller/sandbox_controller.go:167-179`

User-supplied PodTemplate labels are applied first. If a user sets `isola.run/allow-internet-egress: "true"` but does NOT set `network.allowInternetEgress: true`, `buildNetworkLabels` returns an empty map for that key, so the user's label persists — matching the Helm-installed internet egress NetworkPolicy.

**Recommendation:** Explicitly delete `isola.run/allow-internet-egress` and `isola.run/allow-cluster-dns` labels before applying computed network labels:
```go
delete(labels, LabelAllowInternet)
delete(labels, LabelAllowClusterDNS)
maps.Copy(labels, buildNetworkLabels(sandbox.Spec.Network))
```

### F-17: Operator RBAC is Cluster-Scoped

The operator ClusterRole grants pod/job/networkpolicy CRUD cluster-wide. If the operator's ServiceAccount is compromised, it can create pods in any namespace.

### F-18: Sidecar Accessible from Sandbox via Localhost

**File:** `cmd/sandbox-sidecar/main.go:80`

The sidecar binds to `0.0.0.0:10032`. Since the pod uses shared PID namespace, the sandbox container can call `localhost:10032` and execute commands, read files, etc. This is limited impact since the sandbox already has code execution, but it could enable cross-container access in multi-container pods.

### F-19: No HTTP Client Timeouts on Gateway-to-Sidecar Requests

**File:** `cmd/api-gateway/main.go:104-112`

The sidecar HTTP client has no timeout. A malicious/hung sidecar could hold connections indefinitely.

### F-20: Python SDK Accepts Plain HTTP Without Warning

**File:** `sdks/python/src/isola/_client.py:31-33`

The SDK accepts any `base_url` including `http://`. No TLS enforcement or warning for non-localhost HTTP.

---

## Positive Security Practices

The review identified many well-implemented security measures:

1. **No shell injection in command execution** — Uses `exec.CommandContext` with argument arrays, not shell interpretation. The `--` separator prevents nsenter flag injection.
2. **Write-only environment variables** — Response types deliberately omit env vars, preventing secret leakage.
3. **Comprehensive private IP range blocking** — Blocks RFC 1918, carrier-grade NAT, link-local (169.254.0.0/16 including cloud metadata), GKE service ranges, multicast, and reserved ranges.
4. **IPv4-mapped IPv6 bypass prevention** — `::ffff:10.0.0.0/104` style CIDRs are explicitly rejected.
5. **Network config immutability** — CEL validation rules prevent modifying network settings after sandbox creation.
6. **Default deny-all NetworkPolicies** — Sandboxes start with zero network access.
7. **Distroless container images** — Operator, api-gateway, and uploader use `gcr.io/distroless/static:nonroot`.
8. **Infrastructure container hardening** — Operator and api-gateway deployments have `runAsNonRoot`, `readOnlyRootFilesystem`, `capabilities.drop: [ALL]`, `allowPrivilegeEscalation: false`, and seccomp RuntimeDefault.
9. **Redirect following disabled** on sidecar HTTP client — Prevents SSRF via redirect.
10. **Bounded sidecar error reads** — `io.LimitReader(resp.Body, 4096)` prevents unbounded memory from untrusted sidecar responses.
11. **Opaque 502 for sidecar 5xx errors** — Internal failures don't leak details.
12. **URL-encoded path parameters in Python SDK** — `quote(sandbox_id, safe='')` prevents path traversal.
13. **Pydantic `extra="ignore"`** — Server-injected extra fields are silently discarded.
14. **No sensitive data in logs** — No env vars, credentials, or request bodies logged.
15. **nsenter `WaitDelay` backstop** — 5-second WaitDelay prevents goroutine leaks from stuck processes.
16. **Snapshot job hardening** — Both snapshotter and uploader containers drop all capabilities, use read-only rootfs, and disable privilege escalation.

---

## Prioritized Recommendations

### Immediate (Pre-Production Blockers)

1. **Fix symlink escape in filesystem operations (F-01, F-02)** — Use `openat2(RESOLVE_BENEATH)` or validate resolved paths stay within `/proc/<pid>/root`. This is the most critical finding.
2. **Fix TOCTOU races in filesystem operations (F-05, F-06)** — Use fd-based operations (open then fstat/fchmod/fchown) instead of path-based stat-then-open.
3. **Add PodTemplate validation (F-03)** — Strip/reject dangerous PodSpec fields in the operator, and ideally add an admission webhook.
4. **Add API authentication (F-04)** — At minimum API key auth; long-term JWT or mTLS.
5. **Fix network label injection (F-16)** — Explicitly delete network-related labels before applying computed values.
6. **Disable service account token mounting (F-10)** — Set `automountServiceAccountToken: false` on sandbox pods.

### Short-Term

7. **Harden sidecar SecurityContext (F-09)** — Add capability drops, read-only rootfs, disable privilege escalation, set resource limits.
8. **Add gateway-to-sidecar TLS (F-07)** — mTLS or service mesh.
9. **Add sidecar authentication (F-08)** — Shared secret or per-pod token.
10. **Add HTTP server timeouts (F-12)** — `ReadHeaderTimeout`, `IdleTimeout`.
11. **Add body size limits (F-13)** — `io.LimitReader` on uploads and downloads.
12. **Add sandbox ID validation pattern (F-14)** — Regex pattern on path parameters.

### Medium-Term

13. **Add rate limiting (F-11)** — Per-client throttling and sandbox creation limits.
14. **Validate DNS nameservers against blocked ranges (F-15)**.
15. **Add `maxItems` to `allowedEgressCIDRs` (F-26)**.
16. **Scope operator RBAC to sandbox namespace (F-17)**.
17. **Add sidecar HTTP client timeouts (F-19)**.
18. **Add TLS enforcement to Python SDK (F-20)**.
19. **Command entry cleanup in sidecar memory (F-25)**.

---

## Methodology

This review was conducted through static analysis of all source code across:
- 4 Go services (`operator`, `api-gateway`, `sandbox-sidecar`, `uploader`)
- CRD type definitions (`api/v1alpha1/`)
- Helm chart templates and values (`charts/isola/`)
- Python SDK (`sdks/python/`)
- OpenAPI specifications (`api/openapi/`)
- Dockerfiles for all services
- Test files for behavioral verification

The review focused on: command injection, path traversal, symlink attacks, TOCTOU races, privilege escalation, network isolation bypass, authentication/authorization, secret handling, resource exhaustion, and transport security.
