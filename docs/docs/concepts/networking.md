---
sidebar_position: 2
title: Networking
---

# Networking

Isola provides fine-grained network isolation for sandboxes using Kubernetes NetworkPolicies.

## Default Behavior

By default, sandboxes have **deny-all egress** with a sink DNS configuration. DNS queries resolve to `127.0.0.1`, causing them to fail fast rather than hang.

No inbound traffic from outside the sandbox pod is allowed by default.

## Network Configuration

Network isolation is configured through the `network` field on a sandbox. This configuration is **immutable after creation**.

```json
{
  "podTemplate": {
    "container": { "image": "python:3.12" }
  },
  "network": {
    "allowInternetEgress": true,
    "allowClusterDNS": false,
    "allowedEgressCIDRs": ["10.0.0.0/8"],
    "nameservers": ["8.8.8.8", "1.1.1.1"]
  }
}
```

### Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `allowInternetEgress` | bool | `false` | Allow egress to `0.0.0.0/0` and `::/0`. Private ranges and cloud metadata IPs are automatically blocked. |
| `allowClusterDNS` | bool | `false` | Allow DNS queries to cluster DNS (kube-dns/CoreDNS). Sets `DNSPolicy=ClusterFirst` when true. |
| `allowedEgressCIDRs` | string[] | `[]` | Specific CIDRs the sandbox can reach. Blocked ranges (private IPs, cloud metadata) are rejected. |
| `nameservers` | string[] | `[]` | Custom DNS servers (max 3). Combined with cluster DNS if `allowClusterDNS=true`. |

## How It Works

Isola uses a combination of static and dynamic NetworkPolicies:

### Static Policies (Deployed by Helm)

These are installed once and use label selectors to match sandbox pods:

1. **Default deny** — Blocks all egress from sandbox pods
2. **Allow ingress** — Permits ingress from the API gateway/sidecar
3. **Allow internet egress** — Matches pods with label `isola.run/allow-internet-egress=true`
4. **Allow cluster DNS** — Matches pods with label `isola.run/allow-cluster-dns=true`

### Dynamic Policies (Created by Operator)

The operator creates a custom per-sandbox NetworkPolicy only when:
- `allowedEgressCIDRs` is specified, **and**
- `allowInternetEgress` is not `true` (since internet access already covers all CIDRs)

Or when custom `nameservers` are specified and `allowInternetEgress` is not `true`.

### Pod Labels

The operator applies labels to sandbox pods based on the network configuration:

| Label | Applied When |
|-------|-------------|
| `isola.run/allow-internet-egress=true` | `allowInternetEgress` is `true` |
| `isola.run/allow-cluster-dns=true` | `allowClusterDNS` is `true` |

### DNS Behavior

| Configuration | DNS Policy | Nameservers |
|--------------|-----------|-------------|
| No network config | `None` | `127.0.0.1` (sink) |
| `allowClusterDNS: true` | `ClusterFirst` | Cluster DNS |
| Custom `nameservers` only | `None` | Specified nameservers |
| `allowClusterDNS: true` + custom `nameservers` | `ClusterFirst` | Combined |
| `allowInternetEgress: true` (no custom nameservers) | `None` | `127.0.0.1` (sink) |

:::tip
If you enable `allowInternetEgress` and need DNS resolution, either set `allowClusterDNS: true` or provide custom `nameservers` (e.g., `["8.8.8.8", "1.1.1.1"]`). Internet egress alone does not configure DNS servers.
:::

## Examples

### Fully Isolated (Default)

```json
{
  "podTemplate": { "container": { "image": "python:3.12" } }
}
```

No network access. DNS queries fail immediately.

### Internet Access with DNS

```json
{
  "podTemplate": { "container": { "image": "python:3.12" } },
  "network": {
    "allowInternetEgress": true,
    "nameservers": ["8.8.8.8", "1.1.1.1"]
  }
}
```

Can reach public internet with Google/Cloudflare DNS. Private ranges and cloud metadata are automatically blocked. Without custom `nameservers` or `allowClusterDNS`, DNS queries would fail even with internet egress enabled.

### Specific CIDRs Only

```json
{
  "podTemplate": { "container": { "image": "python:3.12" } },
  "network": {
    "allowedEgressCIDRs": ["10.100.0.0/16"],
    "nameservers": ["10.100.0.2"]
  }
}
```

Can only reach `10.100.0.0/16`. Uses custom DNS server at `10.100.0.2`.
