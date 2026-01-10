# NetworkTemplate

A **NetworkTemplate** defines network isolation policies for sandboxes, controlling which external addresses sandboxes can communicate with.

---

## Overview

NetworkTemplates provide:

- **Egress control** - Restrict outbound traffic to specific CIDR ranges
- **Ingress control** - Allow inbound traffic from specific sources
- **DNS configuration** - Specify custom DNS servers
- **Security defaults** - Automatically block risky addresses (cloud metadata, link-local)

---

## Resource Definition

```yaml
apiVersion: sandbox.isola.run/v1alpha1
kind: NetworkTemplate
metadata:
  name: my-network-template
  namespace: isola-sandboxes
spec:
  # Outbound traffic allowed to these CIDR ranges
  allowedEgress:
    - "10.0.0.0/8"        # Private network
    - "8.8.8.8/32"        # Google DNS
    - "8.8.4.4/32"        # Google DNS backup

  # Inbound traffic allowed from these CIDR ranges (optional)
  allowedIngress:
    - "10.0.0.0/8"        # Allow from private network

  # Custom DNS servers (max 3)
  dnsServers:
    - "8.8.8.8"
    - "8.8.4.4"
```

---

## Spec Fields

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `allowedEgress` | []string | No | `[]` (deny all) | CIDR ranges for outbound traffic |
| `allowedIngress` | []string | No | `[]` (deny all) | CIDR ranges for inbound traffic |
| `dnsServers` | []string | No | `[]` | Custom DNS servers (max 3) |

---

## Built-in Templates

Isola includes these default NetworkTemplates:

### isola-isolated (Default)

Denies all network traffic:

```yaml
apiVersion: sandbox.isola.run/v1alpha1
kind: NetworkTemplate
metadata:
  name: isola-isolated
  namespace: isola-sandboxes
spec:
  # No allowedEgress - deny all outbound
  # No allowedIngress - deny all inbound
  # No dnsServers - use cluster DNS
```

### isola-egress-only

Allows all outbound traffic:

```yaml
apiVersion: sandbox.isola.run/v1alpha1
kind: NetworkTemplate
metadata:
  name: isola-egress-only
  namespace: isola-sandboxes
spec:
  allowedEgress:
    - "0.0.0.0/0"  # Allow all IPv4
  dnsServers:
    - "8.8.8.8"
    - "8.8.4.4"
```

---

## Security Defaults

The operator automatically blocks these CIDR ranges, even if included in `allowedEgress`:

| CIDR | Reason |
|------|--------|
| `169.254.0.0/16` | AWS/Cloud metadata service (IMDS) |
| `fe80::/10` | IPv6 link-local (network discovery) |

This prevents sandboxes from:
- Accessing cloud provider metadata/credentials
- Performing network discovery attacks

---

## How It Works

When a sandbox references a NetworkTemplate, the operator:

1. Resolves the template (by reference or embedded spec)
2. Creates a Kubernetes `NetworkPolicy` targeting the sandbox pod
3. Configures egress/ingress rules based on the template
4. Always allows ingress on port 8080 from the operator (for agent communication)

```yaml
# Generated NetworkPolicy (simplified)
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: sandbox-my-sandbox
  namespace: isola-sandboxes
spec:
  podSelector:
    matchLabels:
      sandbox.isola.run/name: my-sandbox
  policyTypes:
    - Egress
    - Ingress
  egress:
    - to:
        - ipBlock:
            cidr: 10.0.0.0/8
  ingress:
    - from:
        - namespaceSelector:
            matchLabels:
              kubernetes.io/metadata.name: isola-system
      ports:
        - port: 8080
```

---

## Examples

### Web Access Only

Allow HTTP/HTTPS to the internet:

```yaml
apiVersion: sandbox.isola.run/v1alpha1
kind: NetworkTemplate
metadata:
  name: web-access
  namespace: isola-sandboxes
spec:
  allowedEgress:
    - "0.0.0.0/0"  # All IPv4
  dnsServers:
    - "8.8.8.8"
```

### Internal Services Only

Restrict to internal networks:

```yaml
apiVersion: sandbox.isola.run/v1alpha1
kind: NetworkTemplate
metadata:
  name: internal-only
  namespace: isola-sandboxes
spec:
  allowedEgress:
    - "10.0.0.0/8"      # Private Class A
    - "172.16.0.0/12"   # Private Class B
    - "192.168.0.0/16"  # Private Class C
  dnsServers:
    - "10.0.0.53"  # Internal DNS server
```

### Specific External API

Allow only specific external services:

```yaml
apiVersion: sandbox.isola.run/v1alpha1
kind: NetworkTemplate
metadata:
  name: api-access
  namespace: isola-sandboxes
spec:
  allowedEgress:
    # GitHub API
    - "140.82.112.0/20"
    - "192.30.252.0/22"
    # PyPI
    - "151.101.0.0/16"
    # npm registry
    - "104.16.0.0/12"
  dnsServers:
    - "8.8.8.8"
```

### Bidirectional Communication

Allow both inbound and outbound:

```yaml
apiVersion: sandbox.isola.run/v1alpha1
kind: NetworkTemplate
metadata:
  name: bidirectional
  namespace: isola-sandboxes
spec:
  allowedEgress:
    - "10.0.0.0/8"
  allowedIngress:
    - "10.0.0.0/8"
  dnsServers:
    - "10.0.0.53"
```

---

## Reference vs Embedded

### Reference Pattern

Share a template across multiple sandboxes:

```yaml
# 1. Create the template once
apiVersion: sandbox.isola.run/v1alpha1
kind: NetworkTemplate
metadata:
  name: shared-network
  namespace: isola-sandboxes
spec:
  allowedEgress:
    - "0.0.0.0/0"

---
# 2. Reference in sandboxes
apiVersion: sandbox.isola.run/v1alpha1
kind: Sandbox
metadata:
  name: sandbox-a
spec:
  templateRef:
    name: python-sandbox
  network:
    templateRef:
      name: shared-network  # Shared template
```

**Benefits:**
- Single point of configuration
- Easy to update policy for all sandboxes
- Reduced YAML duplication

### Embedded Pattern

Create a unique template per sandbox:

```yaml
apiVersion: sandbox.isola.run/v1alpha1
kind: Sandbox
metadata:
  name: unique-sandbox
spec:
  templateRef:
    name: python-sandbox
  network:
    spec:
      allowedEgress:
        - "10.1.2.3/32"  # Very specific access
      dnsServers:
        - "8.8.8.8"
```

**Benefits:**
- Template is owned by sandbox (garbage collected together)
- Unique configuration per sandbox
- No need to manage separate template resources

---

## Network Policy Behavior

### Default Deny

If no network template is specified, the default `isola-isolated` template applies:

```yaml
# This sandbox has no network access
apiVersion: sandbox.isola.run/v1alpha1
kind: Sandbox
metadata:
  name: isolated-sandbox
spec:
  templateRef:
    name: python-sandbox
  # network: not specified = isola-isolated
```

### Immutability

Network configuration is **immutable** after sandbox creation. To change network policy:

1. Delete the sandbox
2. Create a new sandbox with updated network configuration

This prevents runtime network policy changes that could bypass security controls.

---

## Debugging Network Issues

### Check NetworkPolicy

```bash
# View the generated NetworkPolicy
kubectl get networkpolicy -n isola-sandboxes

# Describe for details
kubectl describe networkpolicy sandbox-my-sandbox -n isola-sandboxes
```

### Test Connectivity from Sandbox

```bash
# Via API - test DNS
curl -X POST "http://localhost:8080/api/v1/sandboxes/$ID/execute" \
  -H "X-API-Key: $API_KEY" \
  -d '{"command": "nslookup google.com"}'

# Test HTTP connectivity
curl -X POST "http://localhost:8080/api/v1/sandboxes/$ID/execute" \
  -H "X-API-Key: $API_KEY" \
  -d '{"command": "curl -I https://google.com"}'

# Check what network config is applied
curl -X POST "http://localhost:8080/api/v1/sandboxes/$ID/execute" \
  -H "X-API-Key: $API_KEY" \
  -d '{"command": "cat /etc/resolv.conf"}'
```

### Common Issues

| Symptom | Possible Cause | Solution |
|---------|----------------|----------|
| DNS resolution fails | No DNS servers configured | Add `dnsServers` to template |
| Connection refused | CIDR not in allowedEgress | Add target CIDR to `allowedEgress` |
| Connection timeout | NetworkPolicy blocking | Verify NetworkPolicy rules |
| Can reach metadata service | Security gap | Ensure blocked CIDRs are applied |

---

## Best Practices

1. **Principle of least privilege** - Only allow necessary egress
2. **Use internal DNS** - Point to internal DNS servers when possible
3. **Audit regularly** - Review network templates periodically
4. **Test policies** - Verify connectivity after changes
5. **Document purpose** - Add annotations explaining why access is needed

```yaml
metadata:
  name: github-access
  annotations:
    isola.run/purpose: "Access GitHub API for code fetching"
    isola.run/approved-by: "security-team"
```
