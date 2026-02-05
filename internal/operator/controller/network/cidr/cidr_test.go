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
	"net/netip"
	"testing"

	. "github.com/onsi/gomega"
)

func TestPrefixContains(t *testing.T) {
	tests := []struct {
		name     string
		outer    string
		inner    string
		expected bool
	}{
		{
			name:     "0.0.0.0/0 contains everything IPv4",
			outer:    "0.0.0.0/0",
			inner:    "10.0.0.0/8",
			expected: true,
		},
		{
			name:     "10.0.0.0/8 contains 10.1.2.0/24",
			outer:    "10.0.0.0/8",
			inner:    "10.1.2.0/24",
			expected: true,
		},
		{
			name:     "10.0.0.0/8 does not contain 192.168.0.0/16",
			outer:    "10.0.0.0/8",
			inner:    "192.168.0.0/16",
			expected: false,
		},
		{
			name:     "smaller network cannot contain larger",
			outer:    "10.1.2.0/24",
			inner:    "10.0.0.0/8",
			expected: false,
		},
		{
			name:     "same prefix contains itself",
			outer:    "10.0.0.0/8",
			inner:    "10.0.0.0/8",
			expected: true,
		},
		{
			name:     "IPv6 ::/0 contains fc00::/7",
			outer:    "::/0",
			inner:    "fc00::/7",
			expected: true,
		},
		{
			name:     "fc00::/7 contains fc00::/8",
			outer:    "fc00::/7",
			inner:    "fc00::/8",
			expected: true,
		},
		{
			name:     "IPv4 cannot contain IPv6",
			outer:    "0.0.0.0/0",
			inner:    "::/0",
			expected: false,
		},
		{
			name:     "IPv6 cannot contain IPv4",
			outer:    "::/0",
			inner:    "0.0.0.0/0",
			expected: false,
		},
		{
			name:     "non-canonical input still works (masks internally)",
			outer:    "10.1.2.3/8", // Should be treated as 10.0.0.0/8
			inner:    "10.5.6.7/24",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			outer := netip.MustParsePrefix(tt.outer)
			inner := netip.MustParsePrefix(tt.inner)
			result := prefixContains(outer, inner)
			g.Expect(result).To(Equal(tt.expected))
		})
	}
}

func TestComputeExcept(t *testing.T) {
	tests := []struct {
		name           string
		allowed        string
		expectError    bool
		errorContains  string
		expectedExcept []string
	}{
		{
			name:          "allowed inside blocked - error",
			allowed:       "10.1.2.0/24",
			expectError:   true,
			errorContains: "inside blocked range 10.0.0.0/8",
		},
		{
			name:          "allowed equals blocked - error",
			allowed:       "10.0.0.0/8",
			expectError:   true,
			errorContains: "equals blocked range",
		},
		{
			name:          "172.16.0.0/12 equals blocked - error",
			allowed:       "172.16.0.0/12",
			expectError:   true,
			errorContains: "equals blocked range",
		},
		{
			name:          "169.254.169.254/32 inside blocked - error",
			allowed:       "169.254.169.254/32",
			expectError:   true,
			errorContains: "inside blocked range 169.254.0.0/16",
		},
		{
			name:           "0.0.0.0/0 - except all blocked IPv4",
			allowed:        "0.0.0.0/0",
			expectError:    false,
			expectedExcept: []string{"10.0.0.0/8", "100.64.0.0/10", "169.254.0.0/16", "172.16.0.0/12", "192.168.0.0/16", "198.18.0.0/15", "240.0.0.0/4", "34.118.224.0/20", "224.0.0.0/4"},
		},
		{
			name:           "public CIDR - no except",
			allowed:        "8.8.0.0/16",
			expectError:    false,
			expectedExcept: nil,
		},
		{
			name:           "128.0.0.0/1 - upper half of IPv4",
			allowed:        "128.0.0.0/1",
			expectError:    false,
			expectedExcept: []string{"169.254.0.0/16", "172.16.0.0/12", "192.168.0.0/16", "198.18.0.0/15", "240.0.0.0/4", "224.0.0.0/4"},
		},
		{
			name:           "::/0 - except all blocked IPv6",
			allowed:        "::/0",
			expectError:    false,
			expectedExcept: []string{"fc00::/7", "fe80::/10", "2600:2d00:0:4::/64", "ff00::/8"},
		},
		{
			name:           "public IPv6 - no except",
			allowed:        "2001:db8::/32",
			expectError:    false,
			expectedExcept: nil,
		},
		{
			name:          "fc00::/8 inside fc00::/7 - error",
			allowed:       "fc00::/8",
			expectError:   true,
			errorContains: "inside blocked range fc00::/7",
		},
		{
			name:          "fc00::/7 equals blocked IPv6 - error",
			allowed:       "fc00::/7",
			expectError:   true,
			errorContains: "equals blocked range",
		},
		{
			name:          "fe80::/10 equals blocked IPv6 - error",
			allowed:       "fe80::/10",
			expectError:   true,
			errorContains: "equals blocked range",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			prefix := netip.MustParsePrefix(tt.allowed)
			except, err := ComputeExcept(prefix)

			if tt.expectError {
				g.Expect(err).To(HaveOccurred())
				g.Expect(err.Error()).To(ContainSubstring(tt.errorContains))
				return
			}

			g.Expect(err).ToNot(HaveOccurred())

			if tt.expectedExcept == nil {
				g.Expect(except).To(BeEmpty())
			} else {
				// Convert to strings for comparison
				var exceptStrs []string
				for _, p := range except {
					exceptStrs = append(exceptStrs, p.String())
				}
				g.Expect(exceptStrs).To(ConsistOf(tt.expectedExcept))
			}
		})
	}
}

func TestParsePrefix(t *testing.T) {
	tests := []struct {
		name          string
		cidr          string
		expectError   bool
		errorContains string
		expectedCIDR  string
	}{
		{
			name:         "valid CIDR",
			cidr:         "10.0.0.0/8",
			expectError:  false,
			expectedCIDR: "10.0.0.0/8",
		},
		{
			name:         "canonicalizes",
			cidr:         "10.1.2.3/8",
			expectError:  false,
			expectedCIDR: "10.0.0.0/8",
		},
		{
			name:          "invalid format",
			cidr:          "invalid",
			expectError:   true,
			errorContains: "invalid CIDR",
		},
		{
			name:          "IPv4-mapped IPv6 - error",
			cidr:          "::ffff:10.0.0.0/104",
			expectError:   true,
			errorContains: "IPv4-mapped IPv6 not allowed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			prefix, err := ParsePrefix(tt.cidr)

			if tt.expectError {
				g.Expect(err).To(HaveOccurred())
				g.Expect(err.Error()).To(ContainSubstring(tt.errorContains))
				return
			}

			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(prefix.String()).To(Equal(tt.expectedCIDR))
		})
	}
}

func TestIPv4DoesNotGetIPv6Except(t *testing.T) {
	g := NewWithT(t)
	// Ensure IPv4 allowed never gets IPv6 blocked CIDRs in except
	prefix := netip.MustParsePrefix("0.0.0.0/0")
	except, err := ComputeExcept(prefix)
	g.Expect(err).ToNot(HaveOccurred())

	for _, p := range except {
		g.Expect(p.Addr().Is4()).To(BeTrue(), "IPv4 allowed should not have IPv6 in except: %s", p)
	}
}

func TestIPv6DoesNotGetIPv4Except(t *testing.T) {
	g := NewWithT(t)
	// Ensure IPv6 allowed never gets IPv4 blocked CIDRs in except
	prefix := netip.MustParsePrefix("::/0")
	except, err := ComputeExcept(prefix)
	g.Expect(err).ToNot(HaveOccurred())

	for _, p := range except {
		g.Expect(p.Addr().Is6()).To(BeTrue(), "IPv6 allowed should not have IPv4 in except: %s", p)
	}
}

func TestIsBlocked(t *testing.T) {
	tests := []struct {
		name    string
		addr    string
		blocked bool
	}{
		// Public IPv4 — not blocked
		{"public 8.8.8.8", "8.8.8.8", false},
		{"public 1.1.1.1", "1.1.1.1", false},
		{"public 93.184.216.34", "93.184.216.34", false},

		// Private/blocked IPv4
		{"RFC1918 10.x", "10.0.0.53", true},
		{"RFC1918 172.16.x", "172.16.0.1", true},
		{"RFC1918 192.168.x", "192.168.1.1", true},
		{"CGNAT 100.64.x", "100.64.0.1", true},
		{"link-local 169.254.x", "169.254.169.254", true},
		{"benchmark 198.18.x", "198.18.0.1", true},
		{"class-e 240.x", "240.0.0.1", true},
		{"multicast 224.x", "224.0.0.1", true},

		// Public IPv6 — not blocked
		{"public IPv6 Google DNS", "2001:4860:4860::8888", false},
		{"public IPv6 Cloudflare", "2606:4700:4700::1111", false},

		// Blocked IPv6
		{"ULA fd00::53", "fd00::53", true},
		{"ULA fc00::1", "fc00::1", true},
		{"link-local fe80::1", "fe80::1", true},
		{"multicast ff02::1", "ff02::1", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			addr := netip.MustParseAddr(tt.addr)
			g.Expect(IsBlocked(addr)).To(Equal(tt.blocked))
		})
	}
}

func TestParseDNSServerIP(t *testing.T) {
	tests := []struct {
		name          string
		ip            string
		expectError   bool
		errorContains string
		expectedIP    string
	}{
		{
			name:        "valid IPv4",
			ip:          "8.8.8.8",
			expectError: false,
			expectedIP:  "8.8.8.8",
		},
		{
			name:        "valid IPv4 - Cloudflare",
			ip:          "1.1.1.1",
			expectError: false,
			expectedIP:  "1.1.1.1",
		},
		{
			name:        "valid IPv6",
			ip:          "2001:4860:4860::8888",
			expectError: false,
			expectedIP:  "2001:4860:4860::8888",
		},
		{
			name:        "valid IPv6 - full form",
			ip:          "2001:4860:4860:0000:0000:0000:0000:8888",
			expectError: false,
			expectedIP:  "2001:4860:4860::8888", // Canonicalized
		},
		{
			name:          "invalid - not an IP",
			ip:            "not-an-ip",
			expectError:   true,
			errorContains: "invalid DNS server IP",
		},
		{
			name:          "invalid - CIDR notation",
			ip:            "8.8.8.8/32",
			expectError:   true,
			errorContains: "invalid DNS server IP",
		},
		{
			name:          "invalid - hostname",
			ip:            "dns.google.com",
			expectError:   true,
			errorContains: "invalid DNS server IP",
		},
		{
			name:          "invalid - IPv4-mapped IPv6",
			ip:            "::ffff:8.8.8.8",
			expectError:   true,
			errorContains: "IPv4-mapped IPv6 not allowed",
		},
		{
			name:          "invalid - empty",
			ip:            "",
			expectError:   true,
			errorContains: "invalid DNS server IP",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			addr, err := ParseDNSServerIP(tt.ip)

			if tt.expectError {
				g.Expect(err).To(HaveOccurred())
				g.Expect(err.Error()).To(ContainSubstring(tt.errorContains))
				return
			}

			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(addr.String()).To(Equal(tt.expectedIP))
		})
	}
}
