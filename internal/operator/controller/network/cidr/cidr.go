/*
Copyright 2025 isola.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cidr

import (
	"fmt"
	"net/netip"
)

// BlockedV4 are IPv4 prefixes that sandboxes should never reach via CIDR-based rules.
//
// IMPORTANT: Keep in sync with charts/isola/templates/operator/sandbox-allow-internet-egress-networkpolicy.yaml
var BlockedV4 = []netip.Prefix{
	// RFC 1918: Private address space
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),

	// RFC 6598: Carrier-grade NAT
	netip.MustParsePrefix("100.64.0.0/10"),

	// Reserved / special-purpose
	netip.MustParsePrefix("0.0.0.0/8"),       // RFC 791 §3.2: "This network"
	netip.MustParsePrefix("127.0.0.0/8"),     // RFC 1122 §3.2.1.3: Loopback
	netip.MustParsePrefix("169.254.0.0/16"),  // RFC 3927: Link-local (covers cloud metadata 169.254.169.254)
	netip.MustParsePrefix("192.0.0.0/24"),    // RFC 6890 §2.1: IETF Protocol Assignments
	netip.MustParsePrefix("192.0.2.0/24"),    // RFC 5737: Documentation (TEST-NET-1)
	netip.MustParsePrefix("192.88.99.0/24"),  // RFC 7526: 6to4 Relay Anycast (deprecated)
	netip.MustParsePrefix("198.18.0.0/15"),   // RFC 2544: Benchmarking
	netip.MustParsePrefix("198.51.100.0/24"), // RFC 5737: Documentation (TEST-NET-2)
	netip.MustParsePrefix("203.0.113.0/24"),  // RFC 5737: Documentation (TEST-NET-3)
	netip.MustParsePrefix("224.0.0.0/4"),     // RFC 1112: Multicast (covers 233.252.0.0/24 MCAST-TEST-NET)
	netip.MustParsePrefix("240.0.0.0/4"),     // RFC 1112 §4: Reserved / Class E (covers 255.255.255.255/32)

	// Cloud provider internal
	netip.MustParsePrefix("34.118.224.0/20"),  // GKE-managed Service ClusterIP range (Autopilot 1.27+, Standard 1.29+)
	netip.MustParsePrefix("199.36.153.4/30"),  // GCP Private Google Access: restricted.googleapis.com
	netip.MustParsePrefix("199.36.153.8/30"),  // GCP Private Google Access: private.googleapis.com
	netip.MustParsePrefix("34.126.0.0/18"),    // GCP DirectPath: gRPC direct connectivity to Google APIs (functional only from GCE VMs)
	netip.MustParsePrefix("168.63.129.16/32"), // Azure Wire Server: VM Agent, DHCP, DNS, health probes
}

// BlockedV6 are IPv6 prefixes that sandboxes should never reach via CIDR-based rules.
//
// IMPORTANT: Keep in sync with charts/isola/templates/operator/sandbox-allow-internet-egress-networkpolicy.yaml
var BlockedV6 = []netip.Prefix{
	// Reserved / special-purpose
	netip.MustParsePrefix("::/128"),         // RFC 4291: Unspecified address
	netip.MustParsePrefix("::1/128"),        // RFC 4291: Loopback
	netip.MustParsePrefix("::ffff:0:0/96"),  // RFC 4291: IPv4-mapped address (reserved by protocol, never on the wire)
	netip.MustParsePrefix("64:ff9b:1::/48"), // RFC 8215: NAT64 well-known prefix (local-use, non-globally-routable)
	netip.MustParsePrefix("100::/64"),       // RFC 6666: Discard-Only prefix
	netip.MustParsePrefix("100:0:0:1::/64"), // RFC 9780: Dummy IPv6 Prefix (not routable)
	netip.MustParsePrefix("2001:2::/48"),    // RFC 5180: Benchmarking
	netip.MustParsePrefix("2001:db8::/32"),  // RFC 3849: Documentation
	netip.MustParsePrefix("3fff::/20"),      // RFC 9637: Documentation (IPv6 TEST-NET equivalent)
	netip.MustParsePrefix("5f00::/16"),      // RFC 9602: Segment Routing (SRv6) SIDs (non-globally-routable)
	netip.MustParsePrefix("fc00::/7"),       // RFC 4193: Unique Local Address (ULA)
	netip.MustParsePrefix("fe80::/10"),      // RFC 4291: Link-local
	netip.MustParsePrefix("ff00::/8"),       // RFC 4291: Multicast

	// Deprecated / transition mechanisms
	netip.MustParsePrefix("2001::/32"),    // RFC 4380: Teredo (deprecated tunnel mechanism)
	netip.MustParsePrefix("2001:10::/28"), // RFC 4843: ORCHID (terminated 2014, returned to IANA)
	netip.MustParsePrefix("2002::/16"),    // RFC 3056: 6to4 (deprecated)

	// Cloud provider internal
	netip.MustParsePrefix("2001:4860:8040::/42"),   // GCP DirectPath / direct-connectivity API entry point
	netip.MustParsePrefix("2600:2d00:0:2::/64"),    // GCP Router Next Hop
	netip.MustParsePrefix("2600:2d00:0:3::/64"),    // GCP Router Next Hop
	netip.MustParsePrefix("2600:2d00:0:4::/64"),    // GKE IPv6 Service Range
	netip.MustParsePrefix("2600:2d00:2:1000::/56"), // GCP Private Google Access: restricted.googleapis.com (IPv6)
	netip.MustParsePrefix("2600:2d00:2:2000::/56"), // GCP Private Google Access: private.googleapis.com (IPv6)
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
