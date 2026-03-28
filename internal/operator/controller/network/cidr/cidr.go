// Copyright The Isola Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cidr

import (
	"fmt"
	"net/netip"
)

// BlockedV4 are IPv4 prefixes excluded from sandbox egress via NetworkPolicy ipBlock rules.
//
// Goal: block nearby infrastructure (best-effort), not the real internet. This covers private and
// shared-address networks, link-local, cloud metadata endpoints, known cluster Service CIDRs,
// platform infrastructure (Azure Wire Server), and IANA-reserved non-globally-routable
// space. Sandboxes keep normal internet and cloud-service access (uploading to S3/GCS,
// calling external APIs, pulling images, etc.).
//
// Do not add cloud API data-plane endpoints here (GCP Private Google Access VIPs,
// DirectPath ranges, etc.). Those carry real application traffic and blocking them
// breaks sandbox access to cloud services.
//
// CNI caveat: whether ipBlock rules match pod-to-pod traffic is not defined by the
// Kubernetes API - it depends on the CNI. For example, on GKE Dataplane V2,
// ipBlock never covers pod traffic, pod isolation relies on the default-deny policy
// alone. On Calico, ipBlock does cover pod traffic, so these entries also block egress
// to pods whose IPs fall in the listed ranges.
//
// These entries must be kept in sync with the static Helm NetworkPolicy template
// (sandbox-allow-internet-egress-networkpolicy.yaml).
var BlockedV4 = []netip.Prefix{
	netip.MustParsePrefix("10.0.0.0/8"),       // RFC 1918: 10.0.0.0 - 10.255.255.255 (Class A private)
	netip.MustParsePrefix("100.64.0.0/10"),    // RFC 6598: 100.64.0.0 - 100.127.255.255 (Carrier-grade NAT / shared address space)
	netip.MustParsePrefix("169.254.0.0/16"),   // RFC 3927: Link-local (includes cloud metadata 169.254.169.254)
	netip.MustParsePrefix("172.16.0.0/12"),    // RFC 1918: 172.16.0.0 - 172.31.255.255 (Class B private)
	netip.MustParsePrefix("192.168.0.0/16"),   // RFC 1918: 192.168.0.0 - 192.168.255.255 (Class C private)
	netip.MustParsePrefix("198.18.0.0/15"),    // RFC 2544: Benchmark testing range (also used by GKE on AWS)
	netip.MustParsePrefix("240.0.0.0/4"),      // RFC 1112: Reserved (Class E)
	netip.MustParsePrefix("34.118.224.0/20"),  // GKE-managed Service ClusterIP range (newer GKE)
	netip.MustParsePrefix("224.0.0.0/4"),      // RFC 1112: Multicast
	netip.MustParsePrefix("192.0.0.0/24"),     // RFC 6890: IETF Protocol Assignments
	netip.MustParsePrefix("192.0.2.0/24"),     // RFC 5737: Documentation (TEST-NET-1)
	netip.MustParsePrefix("192.88.99.0/24"),   // RFC 7526: 6to4 Relay Anycast (deprecated)
	netip.MustParsePrefix("198.51.100.0/24"),  // RFC 5737: Documentation (TEST-NET-2)
	netip.MustParsePrefix("203.0.113.0/24"),   // RFC 5737: Documentation (TEST-NET-3)
	netip.MustParsePrefix("168.63.129.16/32"), // Azure: Wire Server (VM Agent, DHCP, DNS, health probes)
}

// BlockedV6 are IPv6 prefixes excluded from sandbox egress. Same policy as BlockedV4.
var BlockedV6 = []netip.Prefix{
	netip.MustParsePrefix("fc00::/7"),           // RFC 4193: Unique Local Address (ULA) - IPv6 equivalent of RFC 1918
	netip.MustParsePrefix("fe80::/10"),          // RFC 4291: Link-local - auto-configured addresses for local network
	netip.MustParsePrefix("2600:2d00:0:4::/64"), // GKE-managed Service IPv6 range (dual-stack clusters)
	netip.MustParsePrefix("ff00::/8"),           // Multicast
	netip.MustParsePrefix("64:ff9b:1::/48"),     // RFC 8215: NAT64 local-use prefix
	netip.MustParsePrefix("100::/64"),           // RFC 6666: Discard-Only prefix
	netip.MustParsePrefix("100:0:0:1::/64"),     // RFC 9780: Dummy IPv6 Prefix
	netip.MustParsePrefix("2001::/32"),          // RFC 4380: Teredo (largely obsolete)
	netip.MustParsePrefix("2001:2::/48"),        // RFC 5180: Benchmarking
	netip.MustParsePrefix("2001:db8::/32"),      // RFC 3849: Documentation
	netip.MustParsePrefix("2002::/16"),          // RFC 3056: 6to4 (relay anycast deprecated by RFC 7526)
	netip.MustParsePrefix("3fff::/20"),          // RFC 9637: Documentation prefix
	netip.MustParsePrefix("5f00::/16"),          // RFC 9602: SRv6 SIDs (not globally routable)
	netip.MustParsePrefix("2600:2d00:0:2::/63"), // GCP: Cloud Router Next Hop
}

// ComputeExcept returns the list of blocked CIDRs to exclude from allowed in a NetworkPolicy.
// Returns error if allowed is inside or equals any blocked CIDR.
//
// Algorithm:
//
//	For each blocked CIDR B:
//	  If B == A or B contains A => validation error
//	  Else if A contains B      => add B to except list
//	  Else                      => ignore (disjoint)
//
// Order guarantee: The returned slice follows the definition order of BlockedV4/BlockedV6,
// ensuring deterministic output across invocations (no reconcile churn).
func ComputeExcept(allowed netip.Prefix) ([]netip.Prefix, error) {
	allowed = allowed.Masked() // Canonicalize

	blocked := BlockedV4
	if allowed.Addr().Is6() {
		blocked = BlockedV6
	}

	var except []netip.Prefix
	for _, b := range blocked {
		if allowed == b {
			return nil, fmt.Errorf("CIDR %s equals blocked range", allowed)
		}
		if prefixContains(b, allowed) {
			return nil, fmt.Errorf("CIDR %s is inside blocked range %s", allowed, b)
		}
		if prefixContains(allowed, b) {
			except = append(except, b)
		}
		// else: disjoint, no action
	}
	return except, nil
}

// prefixContains returns true if outer fully contains inner.
func prefixContains(outer, inner netip.Prefix) bool {
	outer = outer.Masked()
	inner = inner.Masked()

	// Different families can't contain each other
	if outer.Addr().Is4() != inner.Addr().Is4() {
		return false
	}
	// outer must have same or smaller prefix length (larger network)
	if outer.Bits() > inner.Bits() {
		return false
	}
	return outer.Contains(inner.Addr())
}

// ParsePrefix parses a CIDR string and returns the canonicalized prefix.
// Does NOT validate against blocked ranges - caller should use ComputeExcept for egress CIDRs.
func ParsePrefix(cidrStr string) (netip.Prefix, error) {
	p, err := netip.ParsePrefix(cidrStr)
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("invalid CIDR %q: %w", cidrStr, err)
	}

	// Reject IPv4-mapped IPv6 addresses (e.g., ::ffff:10.0.0.0/104).
	// Note: zones are already rejected by netip.ParsePrefix per go.dev/issue/51899.
	if p.Addr().Is4In6() {
		return netip.Prefix{}, fmt.Errorf("invalid CIDR %q: IPv4-mapped IPv6 not allowed", cidrStr)
	}

	return p.Masked(), nil
}

// ParseDNSServerIP parses and validates a DNS server IP address.
// Returns the validated address. Rejects IPv4-mapped IPv6 but allows private IPs
// (users may have internal DNS servers in private ranges).
func ParseDNSServerIP(ipStr string) (netip.Addr, error) {
	addr, err := netip.ParseAddr(ipStr)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("invalid DNS server IP %q: %w", ipStr, err)
	}
	if addr.Is4In6() {
		return netip.Addr{}, fmt.Errorf("invalid DNS server IP %q: IPv4-mapped IPv6 not allowed", ipStr)
	}
	return addr, nil
}
