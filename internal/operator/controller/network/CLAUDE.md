# internal/operator/controller/network/

Custom per-sandbox NetworkPolicy builder.

## When Custom Policies Are Created

Most sandboxes use static Helm-installed NetworkPolicies based on pod labels. This package creates custom NetworkPolicies only when `allowAllInternet` is not true AND:
- Custom egress CIDRs are specified (`allowedEgressCIDRs`)
- Custom nameservers are specified

## Architecture

Static policies (deployed via Helm):
- `sandbox-default-deny` — Denies all traffic for `app.kubernetes.io/name=isola-sandbox` pods
- `sandbox-allow-internet` — Allows internet egress for `isola.run/allow-internet=true` pods
- `sandbox-allow-cluster-dns` — Allows cluster DNS for `isola.run/allow-cluster-dns=true` pods

Custom policies (built by this package):
- Created per-sandbox when CIDR or DNS rules are specified
- Automatically compute exception blocks for blocked ranges (private IPs, cloud metadata, etc.)

## Sub-package

- `cidr/` — Blocked CIDR range definitions (`BlockedV4`, `BlockedV6`) and `ComputeExcept()` for calculating NetworkPolicy `except` entries
