# internal/operator/controller/network/cidr/

CIDR validation, blocked range definitions, and exception computation for NetworkPolicy rules.

## Key Exports

- `BlockedV4` — IPv4 prefixes sandboxes must not reach (RFC 1918, link-local, cloud metadata, multicast, etc.)
- `BlockedV6` — IPv6 prefixes sandboxes must not reach (ULA, link-local)
- `ComputeExcept()` — Given a target CIDR, computes the `except` entries needed to block private/reserved ranges

## Design

When a sandbox specifies `allowedEgressCIDRs`, the NetworkPolicy must allow those CIDRs while still blocking private ranges. `ComputeExcept` calculates which blocked ranges overlap with each allowed CIDR and returns them as `except` entries for the K8s NetworkPolicy spec.
