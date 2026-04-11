# Research: TLS Between API Gateway and Sandbox Sidecar

**Date:** 2026-04-11
**Status:** Research / Proposal
**Context:** The API gateway communicates with sandbox sidecars over plain HTTP using pod IPs (port 10032). This document surveys how Kubernetes-native OSS projects handle internal component TLS without requiring cert-manager or external PKI.

## Current State

The gateway discovers each sidecar via `Sandbox.Status.PodIP` (set by the operator from `pod.Status.PodIP`) and makes plain HTTP calls:

```go
sidecarURL := fmt.Sprintf("http://%s:%d/v1/commands?%s", sb.Status.PodIP, h.sidecarPort, params.Encode())
```

There is no TLS, no authentication, and no integrity protection on the gateway-to-sidecar path.

---

## Survey of Kubernetes-Native Projects

### 1. KubeVirt -- Operator-Managed Full Internal PKI

**The closest architectural match to Isola.** `virt-operator` manages TLS for communication between `virt-api`, `virt-handler`, and `virt-controller`.

- **CA generation:** Operator generates a self-signed ECDSA CA using Go `crypto/x509` + `crypto/ecdsa`, via an internal `triple` package (`NewCA()`, `NewServerKeyPair()`, `NewClientKeyPair()`).
- **Storage:** CA cert+key in a Secret (`kubevirt-ca`). Per-component leaf certs in separate Secrets (`kubevirt-virt-handler-certs`, etc.), mounted as pod volumes.
- **Rotation:** Continuous reconciliation. CA default validity 7 days, renewed at ~80% lifetime. Leaf certs 24h, renewed at ~80%. Fully automatic.
- **mTLS:** Yes, between virt-api and virt-handler.
- **cert-manager:** Not supported (feature request exists).
- **Lesson learned:** A 2025 advisory (GHSA-ggp9-c99x-54gp) showed that shared TLS bundles allowed component impersonation. Use distinct identities per component.

### 2. Linkerd -- Control-Plane CA with SPIFFE Identity

- **CA generation:** The `identity` control-plane component acts as an intermediate CA. Trust anchor + issuer generated at install time (ECDSA P-256).
- **Storage:** Trust anchor in a ConfigMap (`linkerd-identity-trust-roots`). Issuer cert+key in a Secret (`linkerd-identity-issuer`).
- **Leaf cert flow:** Each proxy generates a keypair at startup, sends a CSR over gRPC to the identity service. Identity service validates the pod's ServiceAccount token via `TokenReview`, then signs. SPIFFE-format identities (`spiffe://trust-domain/ns/<ns>/sa/<sa>`).
- **Rotation:** Leaf certs expire every 24h, auto-re-requested. CA has 1-year default, manual rotation (or cert-manager for automation).
- **cert-manager:** Optional, for CA rotation only.

### 3. Istio (istiod) -- Built-in CA with Self-Signed Root

- **CA generation:** istiod generates a self-signed RSA root CA at first startup (configurable to 4096-bit). Core implementation in `security/pkg/pki/ca/ca.go`.
- **Storage:** CA key+cert in Secret `istio-ca-secret`. Root cert distributed to all namespaces via ConfigMap `istio-ca-root-cert`.
- **Leaf cert flow:** Envoy sidecars generate a key, send CSR over gRPC (port 15012). istiod validates and returns a signed cert.
- **Rotation:** Workload certs default 24h. Root CA has a `SelfSignedCARootCertRotator` for automatic rotation.
- **cert-manager:** Optional. Supports pluggable CAs (user-provided `cacerts` Secret).
- **K8s CSR API:** Optional via "Chiron" -- istiod can act as a Registration Authority creating K8s CertificateSigningRequests.

### 4. Vault Agent Injector -- Leader-Elected Auto-TLS

- **CA generation:** Auto-generates CA + leaf certs at startup ("Auto TLS" mode, default).
- **Leader election:** With multiple replicas, leader is elected via ConfigMap. Leader generates CA, patches webhook `caBundle`, distributes cert+key via Secret.
- **Rotation:** Leader regenerates certs approaching expiration and updates the shared Secret.
- **cert-manager:** Optional alternative.

### 5. Cilium (Hubble) -- Dedicated `certgen` Tool

- **CA generation:** `cilium-certgen` CLI tool generates CA + leaf certs, stored as Secrets. Uses CloudFlare CFSSL (not raw `crypto/x509`).
- **Three deployment modes:**
  1. Helm-time generation (no auto-renewal).
  2. CronJob running `certgen` periodically.
  3. cert-manager (optional).
- **Rotation:** CronJob-based or manual.

### 6. Argo CD -- Per-Component Self-Signed (No Shared CA)

- **CA generation:** None. Each component (`argocd-repo-server`, `argocd-server`) auto-generates its own self-signed cert at startup.
- **Validation:** **Skipped by default** (`--repo-server-strict-tls` opt-in). Essentially "encryption without authentication."
- **Rotation:** Not automatic. Pod restart picks up new certs from Secrets.
- **cert-manager:** Optional.
- **Weakness:** No shared CA means no mutual authentication by default. Better than plaintext, but does not prevent MITM.

### 7. Prometheus Operator -- `kube-webhook-certgen` Job

- **CA generation:** One-shot Job (`kube-webhook-certgen`) generates CA + leaf cert (100-year expiry), stores in Secret, patches webhook `caBundle`.
- **Rotation:** None (relies on long expiry).
- **cert-manager:** Optional alternative.

### 8. Knative -- Requires cert-manager

- **No built-in cert generation.** Internal TLS (`system-internal-tls`) requires cert-manager. Uses a self-signed ClusterIssuer but still needs cert-manager to process it.
- **Status:** Experimental.
- **Lesson:** This is the dependency we want to avoid.

### 9. NATS -- User-Provided Certs Only

- Expects TLS certs to be pre-provisioned as Secrets. No built-in generation.
- Recommends cert-manager. Not a useful model for zero-dependency goal.

### 10. Tekton -- Minimal

- Operator creates a TLS Secret for TektonResult, but no rotation. Internal components mostly use plain HTTP and rely on network-level isolation.

---

## Summary Table

| Project | Built-in CA | Crypto Library | CA Storage | Auto-Rotation | cert-manager Optional | K8s CSR API |
|---------|-------------|----------------|------------|---------------|----------------------|-------------|
| **KubeVirt** | Yes (operator) | crypto/x509, ECDSA | Per-component Secrets | Yes (reconcile loop) | No | No |
| **Linkerd** | Yes (identity svc) | crypto/x509, ECDSA | Secret + ConfigMap | Leaf: yes, CA: manual | Yes (CA only) | No |
| **Istio** | Yes (istiod) | crypto/x509, RSA | Secret + ConfigMap | Yes (root rotator) | Yes | Yes (optional) |
| **Vault Injector** | Yes (auto-TLS) | crypto/x509 | Secret (leader-distributed) | Yes (leader renews) | Yes | No |
| **Cilium** | Yes (certgen) | CFSSL | Secrets | CronJob-based | Yes | No |
| **Argo CD** | Per-component self-signed | crypto/x509 | In-memory or Secret | No | Yes | No |
| **Prometheus Op** | Via webhook-certgen Job | crypto/x509 | Secret | No (100y expiry) | Yes | No |
| **Knative** | No | N/A | N/A | Via cert-manager | **Required** | No |
| **NATS** | No | N/A | User-provided | Manual | Recommended | No |
| **Tekton** | Partial | Minimal | Secret | No | Optional | No |

---

## Technical Approaches Evaluated

### A. Operator-Managed Self-Signed CA (Recommended)

The operator generates a CA at startup/reconciliation using Go `crypto/x509`, stores it in a Secret, and issues leaf certs for each component.

**Used by:** KubeVirt, Linkerd, Istio, Vault Injector, Cilium

**How it works for Isola:**
1. Operator generates ECDSA P-256 CA cert+key, stores in Secret `isola-ca` in the operator namespace.
2. Operator generates a gateway leaf cert (DNS SAN: `isola-api-gateway.<ns>.svc`), stores in Secret `isola-api-gateway-tls`.
3. For each sandbox pod, operator generates a sidecar leaf cert (IP SAN: pod IP, or DNS SAN if using headless service). The cert+key are stored in a per-sandbox Secret mounted into the pod.
4. Gateway loads CA cert to verify sidecar connections. Sidecar loads CA cert to verify gateway connections (mTLS).
5. Reconciliation loop checks cert validity. Regenerates at ~80% of lifetime.

**Pros:**
- Zero external dependencies.
- Natural fit with controller-runtime reconciliation.
- Full control over cert lifecycle.
- Battle-tested in production (KubeVirt has the same operator-to-pod-component pattern).
- ~200 lines of Go for core cert generation.

**Cons:**
- Must handle CA rotation carefully (dual-trust-root during transitions).
- Must handle Secret update race conditions.
- Per-sandbox Secrets add API server load (one Secret per sandbox).

### B. Kubernetes CSR API (`certificates.k8s.io`)

Operator creates `CertificateSigningRequest` resources; a signer approves and signs them.

**Used by:** Istio (optional, via Chiron)

**Pros:**
- Native K8s API.

**Cons:**
- Built-in signers are for K8s API auth, not arbitrary service TLS.
- Custom signers require implementing a signing controller (equivalent complexity to self-signed CA).
- Operator creating and approving its own CSRs is an anti-pattern (requires broad RBAC, defeats security model).
- Certs are irrevocable.
- Not adopted by any surveyed project as primary mechanism.

**Verdict:** Not recommended. More complexity than self-signed CA with no clear benefit.

### C. SPIFFE/SPIRE

Node-level agents attest workloads and issue short-lived X.509 SVIDs.

**Pros:** Industry-standard, very short-lived certs, cross-cluster identity.

**Cons:** Heavy dependency (SPIRE server + DaemonSet). Overkill for a single operator's internal TLS.

**Verdict:** Not appropriate. Platform-level identity system, not something an individual operator should require.

### D. Per-Component Self-Signed (Argo CD Pattern)

Each component generates its own self-signed cert. Peers skip verification.

**Pros:** Simplest to implement. Encryption in transit.

**Cons:** No authentication. Does not prevent MITM within the cluster. Essentially security theater if the threat model includes compromised pods.

**Verdict:** Only appropriate if the goal is purely encryption-in-transit with no authentication requirement.

### E. Sidecar Init Container Generates Its Own Cert

An init container in the sandbox pod generates a key+cert signed by the operator's CA (CA key provided as a mounted Secret or via a signing endpoint).

**Pros:** No per-sandbox Secret creation by the operator.

**Cons:** Distributing the CA private key to init containers is a security risk. A signing endpoint adds complexity.

**Verdict:** Not recommended in the simple form. The signing endpoint variant is essentially re-implementing Linkerd's identity service.

---

## Recommendation for Isola

**Approach: Operator-managed self-signed CA (KubeVirt model)**

This is the dominant pattern among K8s-native projects that need internal TLS without external dependencies. It maps cleanly to Isola's architecture:

| Isola Component | KubeVirt Equivalent | Cert Type |
|----------------|--------------------|-----------| 
| Operator | virt-operator | CA owner, cert issuer |
| API Gateway | virt-api | Server+client cert |
| Sandbox Sidecar | virt-handler | Server cert (per-pod) |

### Design Sketch

```
Operator (reconcile loop)
  |
  |-- generates CA cert+key --> Secret "isola-ca"
  |
  |-- generates gateway cert --> Secret "isola-api-gateway-tls"
  |       (DNS SAN: isola-api-gateway.<ns>.svc)
  |       mounted into gateway deployment
  |
  |-- per sandbox:
  |     generates sidecar cert --> Secret "isola-sandbox-<id>-tls"
  |       (IP SAN: pod IP)
  |       mounted into sandbox pod
  |
Gateway                          Sidecar
  |                                |
  |-- TLS dial (verify via CA) --> |
  |   (presents client cert)       | (presents server cert)
  |                                | (verifies client cert via CA)
```

---

## Reusable Go Libraries

The second research dimension: what existing Go libraries can we use rather than writing cert generation from scratch?

| Library | Crypto | Key Type | Approach | Pros | Cons |
|---------|--------|----------|----------|------|------|
| **[open-policy-agent/cert-controller](https://github.com/open-policy-agent/cert-controller)** | `crypto/x509` | RSA 2048 | controller-runtime `Runnable`, reconciler + timer | Production-proven (261+ dependents), handles caBundle injection, `IsReady` channel | Brings controller-runtime dep (already in our tree) |
| **[knative.dev/pkg/webhook/certificates](https://pkg.go.dev/knative.dev/pkg/webhook/certificates)** | `crypto/x509` | ECDSA P-256 | Reconciler-based | Elegant, modern crypto | Heavy dependency closure from knative/pkg |
| **[k8s.io/client-go/util/cert](https://github.com/kubernetes/client-go/blob/master/util/cert/cert.go)** | `crypto/x509` | RSA 2048 | Utility functions | Already in our dependency tree, `GenerateSelfSignedCertKey()` | No rotation, no Secret management |
| **Raw `crypto/x509`** | `crypto/x509` | Your choice | DIY | Full control, zero deps | Must implement rotation, Secret writes, caBundle patching |
| **[cloudflare/cfssl](https://github.com/cloudflare/cfssl)** | Various | Configurable | Library | Feature-rich (used by Cilium) | Large dependency, overkill |

### Notable: OPA Gatekeeper's `cert-controller`

The most production-hardened reusable library. `CertRotator` implements `manager.Runnable` and integrates with controller-runtime:

```go
rotator.AddRotator(mgr, &rotator.CertRotator{
    SecretKey:      types.NamespacedName{Namespace: ns, Name: secretName},
    CertDir:        certDir,
    CAName:         caName,
    CAOrganization: caOrganization,
    DNSName:        dnsName,
    IsReady:        setupFinished,
    Webhooks:       []rotator.WebhookInfo{{Name: whName, Type: rotator.Validating}},
})
```

Defaults: CA validity 10 years, server cert 1 year, renewal lookahead 90 days, rotation check every 12 hours. Handles the full lifecycle: CA generation, server cert, Secret storage, caBundle injection, periodic rotation, readiness signaling.

### Notable: `controller-runtime/pkg/certwatcher`

**Important caveat:** `certwatcher` is a file watcher only -- it does NOT generate certificates. It watches cert/key files on disk (fsnotify + periodic re-read every 10s) and hot-reloads them into `tls.Config`. The controller-runtime maintainers explicitly declined to add cert generation ([issue #3038](https://github.com/kubernetes-sigs/controller-runtime/issues/3038)), stating it is outside scope. However, `certwatcher` is useful on the *consuming* side (gateway or sidecar loading certs from mounted Secrets).

---

## Webhook Cert Patterns (Relevant Prior Art)

Most K8s operators already solve a closely related problem: generating self-signed certs for admission webhooks. The patterns translate directly to internal component TLS.

| Pattern | Generation | Rotation | Example |
|---------|-----------|----------|---------|
| **Operator reconcile loop** | Operator generates CA + leaf certs using `crypto/x509`, stores in Secret | Timer-based expiry check in reconcile loop | OPA Gatekeeper, Kyverno, KubeVirt |
| **Helm hook Job** | One-shot Job (`kube-webhook-certgen`) at install time | None (100-year expiry) or re-run Job | Prometheus Operator, ingress-nginx |
| **Leader-elected startup** | Leader generates CA, distributes via Secret | Leader renews approaching expiry | Vault Agent Injector |
| **CronJob** | Periodic Job regenerates certs | Schedule-based (e.g., quarterly) | Cilium certgen |

The **operator reconcile loop** pattern is the natural fit for Isola since the operator already reconciles sandbox state.

---

## Recommendation for Isola

**Approach: Operator-managed self-signed CA (KubeVirt model)**

This is the dominant pattern among K8s-native projects that need internal TLS without external dependencies. It maps cleanly to Isola's architecture:

| Isola Component | KubeVirt Equivalent | Cert Type |
|----------------|--------------------|-----------| 
| Operator | virt-operator | CA owner, cert issuer |
| API Gateway | virt-api | Server+client cert |
| Sandbox Sidecar | virt-handler | Server cert (per-pod) |

### Design Sketch

```
Operator (reconcile loop)
  |
  |-- generates CA cert+key --> Secret "isola-ca"
  |
  |-- generates gateway cert --> Secret "isola-api-gateway-tls"
  |       (DNS SAN: isola-api-gateway.<ns>.svc)
  |       mounted into gateway deployment
  |
  |-- per sandbox:
  |     generates sidecar cert --> Secret "isola-sandbox-<id>-tls"
  |       (IP SAN: pod IP)
  |       mounted into sandbox pod
  |
Gateway                          Sidecar
  |                                |
  |-- TLS dial (verify via CA) --> |
  |   (presents client cert)       | (presents server cert)
  |                                | (verifies client cert via CA)
```

### Implementation Options

**Option A: Use `open-policy-agent/cert-controller`** for CA lifecycle + rotation (already integrates with controller-runtime), extend with custom logic for per-sandbox leaf certs. ~100 lines of glue code.

**Option B: Roll our own** with `crypto/x509` + `crypto/ecdsa` (ECDSA P-256). ~200-300 lines for core cert generation. More control, fewer deps, but must handle rotation edge cases ourselves. KubeVirt's `triple` package is a good reference implementation.

**Option C: Use `k8s.io/client-go/util/cert`** (`GenerateSelfSignedCertKey`) for quick bootstrapping, add rotation logic on top. Already in our dependency tree.

### Open Questions

1. **IP SAN vs DNS SAN for sidecars:** Using pod IP as SAN is simple but the cert must be generated after the pod gets an IP. Alternative: use a headless Service for stable DNS names, or generate certs with a wildcard/pattern the operator controls.

2. **Per-sandbox Secret overhead:** Each sandbox gets a TLS Secret. For high sandbox churn this could be significant API server load. Alternatives:
   - Shared wildcard cert (weaker isolation).
   - In-memory cert delivery via the operator (no Secret, but requires a signing endpoint).
   - Short-lived certs with no persistence (sidecar requests cert from operator at startup via ServiceAccount token auth, like Linkerd).

3. **One-way TLS vs mTLS:** One-way TLS (gateway verifies sidecar) is simpler. mTLS (sidecar also verifies gateway) provides stronger guarantees but requires the gateway to present a client cert.

4. **cert-manager as optional alternative:** Support user-provided CA Secret as an opt-in path. If `isola-ca` Secret already exists and has an annotation like `isola.run/managed-by: external`, skip CA generation.

5. **CA rotation strategy:** During CA rotation, both old and new CA certs must be trusted simultaneously. KubeVirt handles this by keeping validity periods short (7 days CA) and regenerating at 80% lifetime, giving a window where both CAs overlap.

---

## References

- [KubeVirt cert bootstrap source](https://github.com/kubevirt/kubevirt/blob/main/pkg/certificates/bootstrap/cert-manager.go)
- [KubeVirt Security Fundamentals](https://kubevirt.io/2020/KubeVirt-Security-Fundamentals.html)
- [KubeVirt GHSA-ggp9-c99x-54gp (shared TLS bundle vulnerability)](https://github.com/kubevirt/kubevirt/security/advisories/GHSA-ggp9-c99x-54gp)
- [Linkerd automatic mTLS](https://linkerd.io/2-edge/features/automatic-mtls/)
- [Istio CA source (ca.go)](https://github.com/istio/istio/blob/master/security/pkg/pki/ca/ca.go)
- [Istio pluggable CA certs](https://istio.io/latest/docs/tasks/security/cert-management/plugin-ca-cert/)
- [OPA cert-controller](https://github.com/open-policy-agent/cert-controller)
- [Cilium certgen](https://github.com/cilium/certgen)
- [Argo CD TLS configuration](https://argo-cd.readthedocs.io/en/stable/operator-manual/tls/)
- [controller-runtime certwatcher](https://github.com/kubernetes-sigs/controller-runtime/blob/main/pkg/certwatcher/certwatcher.go)
- [controller-runtime issue #3038 (no built-in cert generation)](https://github.com/kubernetes-sigs/controller-runtime/issues/3038)
- [Knative webhook certificates source](https://github.com/knative/pkg/blob/main/webhook/certificates/resources/certs.go)
- [kube-webhook-certgen](https://github.com/jkroepke/kube-webhook-certgen)
- [Vault Agent Injector TLS](https://developer.hashicorp.com/vault/docs/deploy/kubernetes/injector/installation)
- [K8s CSR API docs](https://kubernetes.io/docs/reference/access-authn-authz/certificate-signing-requests/)
