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

// Package cidr provides CIDR validation and blocking logic for NetworkPolicy generation.
package cidr

import (
	"fmt"
	"net/netip"
)

// BlockedV4 are IPv4 prefixes that sandboxes should never reach via CIDR-based rules.
// To reach internal services, use AllowedEgressPeers with explicit pod/namespace selectors.
var BlockedV4 = []netip.Prefix{
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("169.254.0.0/16"), // Link-local (includes cloud metadata 169.254.169.254)
}

// BlockedV6 are IPv6 prefixes that sandboxes should never reach via CIDR-based rules.
var BlockedV6 = []netip.Prefix{
	netip.MustParsePrefix("fc00::/7"),  // Unique Local Address (ULA)
	netip.MustParsePrefix("fe80::/10"), // Link-local
}

// ComputeExcept returns the list of blocked CIDRs to exclude from allowed.
// Returns error if allowed is inside or equals any blocked CIDR.
//
// Algorithm:
//
//	For each blocked CIDR B (same address family):
//	  If B == A or B contains A → validation error
//	  Else if A contains B      → add B to except list
//	  Else                      → ignore (disjoint)
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
// Masks both sides defensively in case inputs aren't canonical.
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

// ParseAndValidate parses a CIDR string and validates it's not blocked.
// Returns the canonicalized prefix.
func ParseAndValidate(cidrStr string) (netip.Prefix, error) {
	p, err := netip.ParsePrefix(cidrStr)
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("invalid CIDR %q: %w", cidrStr, err)
	}

	// Reject IPv4-mapped IPv6 addresses (e.g., ::ffff:10.0.0.0/104).
	// Note: zones are already rejected by netip.ParsePrefix per go.dev/issue/51899.
	if p.Addr().Is4In6() {
		return netip.Prefix{}, fmt.Errorf("invalid CIDR %q: IPv4-mapped IPv6 not allowed", cidrStr)
	}

	p = p.Masked() // Canonicalize: 10.1.2.3/24 → 10.1.2.0/24

	_, err = ComputeExcept(p)
	if err != nil {
		return netip.Prefix{}, err
	}
	return p, nil
}

// ParsePrefix parses a CIDR string and returns the canonicalized prefix.
// Does NOT validate against blocked ranges - use ParseAndValidate for that.
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
