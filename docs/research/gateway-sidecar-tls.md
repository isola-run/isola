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

## Approaches That Avoid Per-Sandbox Secrets

**Constraint:** Creating a Secret per sandbox is not viable -- it adds unnecessary API server load at scale. The operator currently creates zero Secrets per sandbox. The approaches below preserve this property.

### A. Shared Certificate (One Secret for All Sidecars)

One TLS cert+key (e.g., CN=`isola-sidecar`) stored in a single Secret, mounted into every sandbox pod.

**How it works:**
- Operator manages one Secret `isola-sidecar-tls` containing a server cert+key signed by the operator's CA.
- Every sandbox pod spec references this Secret as a volume, mounted at `/etc/isola/tls/`.
- Sidecar starts HTTPS using the shared cert. Gateway verifies against the CA.
- Kubernetes mounts Secrets via tmpfs (in-memory). Kubelet only sends Secret data to nodes with pods referencing it.

**Security properties:**
- Confidentiality: Yes (TLS encryption).
- Authentication: Group-only -- proves the sidecar holds the shared cert, but gateway cannot distinguish individual pods by certificate alone (must rely on IP matching `Sandbox.Status.PodIP`).
- Compromise of one pod exposes the private key for all sidecars. Exactly the weakness in KubeVirt's GHSA-ggp9-c99x-54gp advisory.

**Complexity:** Low (~50 lines of cert generation + standard volume mount).

### B. Signing Endpoint with Projected SA Token (Linkerd Model)

Sidecar generates a keypair at startup, authenticates to the operator via a projected SA token, and receives a signed cert. Cert lives only in sidecar memory. Zero per-sandbox Secrets.

**Key insight:** Projected volumes with `serviceAccountToken` source work **independently** of `automountServiceAccountToken: false`. They are orthogonal mechanisms -- `automountServiceAccountToken` only governs the automatic default token injection. An explicit projected volume is always honored.

**How it works:**
1. Operator adds a projected volume to each sandbox pod spec:
   ```yaml
   volumes:
   - name: isola-identity-token
     projected:
       sources:
       - serviceAccountToken:
           audience: "isola-identity"
           expirationSeconds: 3600
           path: token
   ```
   The custom audience ensures this token is **useless for K8s API access** but verifiable by the operator's signing endpoint.

2. Sidecar starts, generates an ECDSA P-256 keypair in memory, reads the projected SA token from `/var/run/secrets/isola/token`, and sends a CSR + token to the operator's signing endpoint.

3. Operator validates the token via `TokenReview` API with `audiences: ["isola-identity"]`. The response includes `authentication.kubernetes.io/pod-name` and `authentication.kubernetes.io/pod-uid` in `status.user.extra`, proving the exact pod identity.

4. Operator signs the CSR with the CA key and returns the leaf cert.

5. Sidecar starts HTTPS with the signed cert. Gateway verifies against the CA cert.

**Token properties (GA since K8s 1.12, feature gates removed in 1.21):**
- Kubelet auto-refreshes at 80% of TTL or 24h, whichever is sooner.
- Token is bound to the specific pod -- automatically invalidated when the pod is deleted.
- JWT contains pod name, UID, namespace, service account name.
- Token file is atomically replaced on rotation.

**How Linkerd does this:**
- Linkerd's mutating webhook injects a projected volume with audience `identity.l5d.io` and 86400s expiration.
- Proxy generates ECDSA P-256 keypair, sends CSR over gRPC to `linkerd-identity` control-plane component.
- Identity service validates via `TokenReview`, checks SA/namespace match the CSR's SPIFFE identity.
- Signs and returns the leaf cert. Proxy leaf certs expire every 24h, auto-re-requested.
- Linkerd had issues when `automountServiceAccountToken: false` was set (issues #3183, #4651, #6862), resolved by injecting their OWN projected volume -- confirming the two mechanisms are independent.

**Security properties:**
- Confidentiality: Yes (TLS encryption).
- Authentication: Strong per-pod identity (K8s-signed JWT bound to specific pod, verified via TokenReview).
- No shared private keys -- each sidecar has its own keypair. Compromise of one pod does not affect others.

**Complexity:** High (~500-800 lines: signing endpoint, CSR handling, TokenReview validation, cert issuance, CA management). But this is a one-time implementation in the operator.

### C. Shared Cert + Projected SA Token Authentication (Hybrid)

Combine approach A (shared cert for encryption) with approach B's token (for per-pod authentication). Simpler than a full signing endpoint.

**How it works:**
1. All sidecars use a shared TLS cert for encryption (one Secret, mounted everywhere).
2. Sidecar reads a projected SA token (audience `isola-gateway`) and presents it on every request (e.g., as a header or in a TLS-protected auth exchange).
3. Gateway validates the token via `TokenReview` to confirm the specific pod identity.

**Security properties:**
- Encryption via shared cert (compromise exposes the key, but an attacker still can't authenticate as a different pod).
- Per-pod authentication via projected SA token.
- Weaker than approach B (shared cert key is still a single point of compromise for confidentiality), but the authentication layer prevents impersonation.

**Complexity:** Medium. Simpler than a full signing endpoint -- no CSR flow needed.

### D. Trust-on-First-Use (TOFU)

Each sidecar generates a self-signed cert at startup (in memory). Gateway pins the cert fingerprint on first connection.

**How it works:**
1. Sidecar generates ECDSA P-256 keypair + self-signed cert at startup. Listens on HTTPS.
2. Gateway first connects with a custom `tls.Config.VerifyPeerCertificate` that accepts any cert and records its SHA-256 fingerprint in `Sandbox.Status.CertFingerprint`.
3. Subsequent connections verify the presented cert matches the pinned fingerprint.

**MITM vulnerability window:** The first connection is unauthenticated. If an attacker intercepts it, they pin their own cert. Mitigations: gateway connects immediately after pod Ready, NetworkPolicy restricts pod access to gateway only.

**Security properties:**
- Confidentiality: Yes (TLS).
- Authentication: Moderate -- strong after first connection, vulnerable during TOFU window.
- No shared keys. Per-pod identity after pinning.

**Complexity:** Medium (~200 lines). Needs a new CRD status field.

### E. CNI-Level Encryption (WireGuard / IPsec)

Push encryption to the network layer. Transparent to application code.

**How it works:** Cilium or Calico with WireGuard encrypts all inter-node pod traffic automatically. Each node creates a WireGuard keypair and establishes tunnels.

**Critical limitation:** Intra-node traffic is NOT encrypted by default. Node-level identity only -- any pod on the cluster can reach any other pod; encryption doesn't provide pod-level authentication. Also requires a specific CNI (Cilium/Calico), which is a hard external dependency we can't mandate.

**Verdict:** Not viable as sole solution. Could be a complementary defense-in-depth layer for users who happen to run Cilium/Calico.

### Comparison Table

| Approach | Per-Pod Secrets | Confidentiality | Per-Pod Auth | Complexity | External Deps |
|----------|:-:|:-:|:-:|:-:|:-:|
| **A. Shared Cert** | 0 (1 total) | Yes | No (group) | Low | None |
| **B. Signing Endpoint** | 0 | Yes | Yes (strong) | High | None |
| **C. Shared Cert + Token** | 0 (1 total) | Yes | Yes (strong) | Medium | None |
| **D. TOFU** | 0 | Yes | Yes (after pin) | Medium | None |
| **E. CNI Encryption** | 0 | Inter-node only | No | Low (app) | Cilium/Calico |

---

## Reusable Go Libraries

| Library | Crypto | Key Type | Approach | Pros | Cons |
|---------|--------|----------|----------|------|------|
| **[open-policy-agent/cert-controller](https://github.com/open-policy-agent/cert-controller)** | `crypto/x509` | RSA 2048 | controller-runtime `Runnable`, reconciler + timer | Production-proven (261+ dependents), handles caBundle injection, `IsReady` channel | Designed for webhook certs, not per-pod signing |
| **[knative.dev/pkg/webhook/certificates](https://pkg.go.dev/knative.dev/pkg/webhook/certificates)** | `crypto/x509` | ECDSA P-256 | Reconciler-based | Elegant, modern crypto | Heavy dependency closure from knative/pkg |
| **[k8s.io/client-go/util/cert](https://github.com/kubernetes/client-go/blob/master/util/cert/cert.go)** | `crypto/x509` | RSA 2048 | Utility functions | Already in our dependency tree, `GenerateSelfSignedCertKey()` | No rotation, no Secret management |
| **Raw `crypto/x509`** | `crypto/x509` | Your choice | DIY | Full control, zero deps | Must implement rotation, Secret writes yourself |

### Notable: OPA Gatekeeper's `cert-controller`

Best fit for CA lifecycle + gateway cert management (not per-pod signing). `CertRotator` implements `manager.Runnable`:

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

Defaults: CA validity 10 years, server cert 1 year, renewal lookahead 90 days, rotation check every 12 hours.

### Notable: `controller-runtime/pkg/certwatcher`

**Important caveat:** `certwatcher` is a file watcher only -- it does NOT generate certificates. It watches cert/key files on disk (fsnotify + periodic re-read every 10s) and hot-reloads them into `tls.Config`. Useful on the *consuming* side (gateway or sidecar loading certs from mounted Secrets). The controller-runtime maintainers explicitly declined to add cert generation ([issue #3038](https://github.com/kubernetes-sigs/controller-runtime/issues/3038)).

---

## Recommendation for Isola

**Approach B (Signing Endpoint with Projected SA Token)** is the strongest option. It provides per-pod identity with zero per-sandbox Secrets, using only GA Kubernetes APIs. This is the Linkerd model adapted for Isola's architecture.

If the implementation cost of a signing endpoint is too high for the initial version, **Approach C (Shared Cert + Projected SA Token)** is a pragmatic stepping stone: shared cert for encryption, projected token for per-pod authentication. It can be upgraded to Approach B later.

### Design Sketch (Approach B)

```
Operator
  |
  |-- generates CA cert+key ---------> Secret "isola-ca" (1 Secret)
  |-- generates gateway client cert --> Secret "isola-gateway-tls" (1 Secret)
  |-- runs signing endpoint on :9443
  |
  |-- per sandbox pod spec:
  |     injects projected volume:
  |       serviceAccountToken(audience="isola-identity", exp=3600)
  |     injects CA cert via ConfigMap volume (for sidecar to verify gateway)
  |
Sidecar startup:                    Gateway:
  1. Generate ECDSA P-256 keypair     1. Load client cert from Secret
  2. Read projected SA token          2. Load CA cert from Secret
  3. Send CSR + token to operator     3. Connect to sidecar with TLS
  4. Operator: TokenReview(token)        - Verify server cert against CA
     -> extracts pod name/UID            - Present client cert (mTLS)
     -> signs CSR, returns leaf cert
  5. Start HTTPS server with cert
```

**Resource overhead per sandbox:** Zero Secrets, zero additional API calls (projected token is kubelet-managed). The only new API call is the CSR signing request from sidecar to operator (one per pod startup + rotation).

**Total new cluster-wide resources:** 2 Secrets (CA + gateway cert), 1 ConfigMap (CA cert public), signing endpoint in operator.

### Signing Endpoint Options

The operator's signing endpoint can be exposed as:

1. **HTTPS endpoint on operator pod** (e.g., `:9443/v1/sign`). Sidecar calls it directly via operator's service DNS. Simple, but requires the operator's service to be reachable from the sandbox namespace (needs a NetworkPolicy allowance).

2. **Kubernetes subresource** on the Sandbox CRD (e.g., `/apis/isola.run/v1alpha1/sandboxes/{name}/sign`). More Kubernetes-native, but implementing CRD subresources with custom handlers is complex.

3. **gRPC endpoint** (like Linkerd). More efficient for cert issuance but adds a protocol.

Option 1 is the simplest to implement.

### Open Questions

1. **Signing endpoint availability:** The sidecar must reach the operator's signing endpoint at startup. If the operator is temporarily unavailable, the sidecar should retry with backoff. The sidecar can serve plain HTTP as a fallback until it obtains a cert (gateway would need to handle both).

2. **mTLS or one-way TLS:** With approach B, one-way TLS (gateway verifies sidecar cert) already provides strong authentication because only the operator's CA can issue valid certs. mTLS (sidecar also verifies gateway client cert) adds defense-in-depth but requires the gateway to present a cert. Recommended for the final design.

3. **Cert lifetime and rotation:** Sidecar leaf certs should be short-lived (e.g., 1-24h). The sidecar re-requests before expiry using the same projected SA token (which the kubelet auto-rotates). No restart needed.

4. **cert-manager as optional alternative:** Support user-provided CA Secret (`isola-ca` with annotation `isola.run/managed-by: external`). If present, skip CA generation and use the provided CA for signing.

5. **CA rotation strategy:** During CA rotation, both old and new CA certs must be trusted simultaneously. Use a CA bundle (concatenated PEM) in the trust ConfigMap. KubeVirt handles this with short CA validity (7 days) and regeneration at 80% lifetime.

6. **Fallback for initial startup:** On very first pod startup, before the sidecar has a cert, the gateway could use the projected SA token directly for authentication over plain HTTP (approach C as fallback), then upgrade to TLS once the sidecar obtains its cert.

---

## References

### Project implementations
- [KubeVirt cert bootstrap source](https://github.com/kubevirt/kubevirt/blob/main/pkg/certificates/bootstrap/cert-manager.go)
- [KubeVirt Security Fundamentals](https://kubevirt.io/2020/KubeVirt-Security-Fundamentals.html)
- [KubeVirt GHSA-ggp9-c99x-54gp (shared TLS bundle vulnerability)](https://github.com/kubevirt/kubevirt/security/advisories/GHSA-ggp9-c99x-54gp)
- [Linkerd automatic mTLS](https://linkerd.io/2-edge/features/automatic-mtls/)
- [Linkerd identity pipeline deep dive](https://dev.to/gtrekter/from-trust-anchors-to-spiffe-ids-understanding-linkerds-automated-identity-pipeline-37k9)
- [Linkerd bound SA token adoption](https://linkerd.io/2021/12/28/using-kubernetess-new-bound-service-account-tokens-for-secure-workload-identity/)
- [Linkerd automountServiceAccountToken issues (#3183, #4651, #6862)](https://github.com/linkerd/linkerd2/issues/6862)
- [Istio CA source (ca.go)](https://github.com/istio/istio/blob/master/security/pkg/pki/ca/ca.go)
- [Istio pluggable CA certs](https://istio.io/latest/docs/tasks/security/cert-management/plugin-ca-cert/)
- [Argo CD TLS configuration](https://argo-cd.readthedocs.io/en/stable/operator-manual/tls/)
- [Cilium certgen](https://github.com/cilium/certgen)
- [Cilium WireGuard transparent encryption](https://docs.cilium.io/en/stable/security/network/encryption-wireguard/)

### Go libraries
- [OPA cert-controller](https://github.com/open-policy-agent/cert-controller)
- [controller-runtime certwatcher](https://github.com/kubernetes-sigs/controller-runtime/blob/main/pkg/certwatcher/certwatcher.go)
- [controller-runtime issue #3038 (no built-in cert generation)](https://github.com/kubernetes-sigs/controller-runtime/issues/3038)
- [Knative webhook certificates source](https://github.com/knative/pkg/blob/main/webhook/certificates/resources/certs.go)
- [kube-webhook-certgen](https://github.com/jkroepke/kube-webhook-certgen)
- [k8s.io/client-go/util/cert](https://github.com/kubernetes/client-go/blob/master/util/cert/cert.go)

### Kubernetes APIs
- [Projected volumes](https://kubernetes.io/docs/concepts/storage/projected-volumes/)
- [ServiceAccountToken projected source](https://kubernetes.io/docs/concepts/security/service-accounts/)
- [TokenRequest API](https://kubernetes.io/docs/reference/kubernetes-api/authentication-resources/token-request-v1/)
- [TokenReview API](https://kubernetes.io/docs/reference/access-authn-authz/certificate-signing-requests/)
- [KEP-1205: Bound Service Account Tokens](https://github.com/kubernetes/enhancements/tree/master/keps/sig-auth/1205-bound-service-account-tokens)

### Other
- [Vault Agent Injector TLS](https://developer.hashicorp.com/vault/docs/deploy/kubernetes/injector/installation)
- [Calico WireGuard encryption](https://docs.tigera.io/calico/latest/network-policy/encrypt-cluster-pod-traffic)
