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
var BlockedV4 = []netip.Prefix{
	netip.MustParsePrefix("10.0.0.0/8"),      // RFC 1918: 10.0.0.0 - 10.255.255.255 (Class A private)
	netip.MustParsePrefix("100.64.0.0/10"),   // RFC 6598: 100.64.0.0 - 100.127.255.255 (Carrier-grade NAT / shared address space)
	netip.MustParsePrefix("169.254.0.0/16"),  // RFC 3927: Link-local (includes cloud metadata 169.254.169.254)
	netip.MustParsePrefix("172.16.0.0/12"),   // RFC 1918: 172.16.0.0 - 172.31.255.255 (Class B private)
	netip.MustParsePrefix("192.168.0.0/16"),  // RFC 1918: 192.168.0.0 - 192.168.255.255 (Class C private)
	netip.MustParsePrefix("198.18.0.0/15"),   // RFC 2544: Benchmark testing range (also used by GKE on AWS)
	netip.MustParsePrefix("240.0.0.0/4"),     // RFC 1112: 240.0.0.0 - 255.255.255.255 (Class E reserved)
	netip.MustParsePrefix("34.118.224.0/20"), // GKE-managed Service ClusterIP range (newer GKE)
	netip.MustParsePrefix("224.0.0.0/4"),     // Multicast
}

// BlockedV6 are IPv6 prefixes that sandboxes should never reach via CIDR-based rules.
var BlockedV6 = []netip.Prefix{
	netip.MustParsePrefix("fc00::/7"),           // RFC 4193: Unique Local Address (ULA) - IPv6 equivalent of RFC 1918
	netip.MustParsePrefix("fe80::/10"),          // RFC 4291: Link-local - auto-configured addresses for local network
	netip.MustParsePrefix("2600:2d00:0:4::/64"), // GKE-managed Service IPv6 range (dual-stack clusters)
	netip.MustParsePrefix("ff00::/8"),           // Multicast
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
