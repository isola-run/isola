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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
			outer := netip.MustParsePrefix(tt.outer)
			inner := netip.MustParsePrefix(tt.inner)
			result := prefixContains(outer, inner)
			assert.Equal(t, tt.expected, result)
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
			expectedExcept: []string{"10.0.0.0/8", "100.64.0.0/10", "169.254.0.0/16", "172.16.0.0/12", "192.168.0.0/16", "240.0.0.0/4"},
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
			expectedExcept: []string{"169.254.0.0/16", "172.16.0.0/12", "192.168.0.0/16", "240.0.0.0/4"},
		},
		{
			name:           "::/0 - except all blocked IPv6",
			allowed:        "::/0",
			expectError:    false,
			expectedExcept: []string{"fc00::/7", "fe80::/10"},
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
			prefix := netip.MustParsePrefix(tt.allowed)
			except, err := ComputeExcept(prefix)

			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorContains)
				return
			}

			require.NoError(t, err)

			if tt.expectedExcept == nil {
				assert.Empty(t, except)
			} else {
				// Convert to strings for comparison
				var exceptStrs []string
				for _, p := range except {
					exceptStrs = append(exceptStrs, p.String())
				}
				assert.ElementsMatch(t, tt.expectedExcept, exceptStrs)
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
			prefix, err := ParsePrefix(tt.cidr)

			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorContains)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expectedCIDR, prefix.String())
		})
	}
}

func TestIPv4DoesNotGetIPv6Except(t *testing.T) {
	// Ensure IPv4 allowed never gets IPv6 blocked CIDRs in except
	prefix := netip.MustParsePrefix("0.0.0.0/0")
	except, err := ComputeExcept(prefix)
	require.NoError(t, err)

	for _, p := range except {
		assert.True(t, p.Addr().Is4(), "IPv4 allowed should not have IPv6 in except: %s", p)
	}
}

func TestIPv6DoesNotGetIPv4Except(t *testing.T) {
	// Ensure IPv6 allowed never gets IPv4 blocked CIDRs in except
	prefix := netip.MustParsePrefix("::/0")
	except, err := ComputeExcept(prefix)
	require.NoError(t, err)

	for _, p := range except {
		assert.True(t, p.Addr().Is6(), "IPv6 allowed should not have IPv4 in except: %s", p)
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
			addr, err := ParseDNSServerIP(tt.ip)

			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorContains)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expectedIP, addr.String())
		})
	}
}

func BenchmarkIsBlockedCIDR_IPv4(b *testing.B) {
	testCases := []string{
		"10.1.2.0/24",     // Inside blocked range
		"8.8.8.0/24",      // Public (not blocked)
		"192.168.1.0/24",  // Inside blocked range
		"1.1.1.0/24",      // Public (not blocked)
		"172.16.0.0/16",   // Inside blocked range
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cidr := testCases[i%len(testCases)]
		prefix, _ := ParsePrefix(cidr)
		_, _ = ComputeExcept(prefix)
	}
}

func BenchmarkIsBlockedCIDR_IPv6(b *testing.B) {
	testCases := []string{
		"fc00::/8",        // Inside blocked range
		"2001:db8::/32",   // Public (not blocked)
		"fe80::/64",       // Inside blocked range
		"2606:4700::/32",  // Public (not blocked)
		"fd00::/8",        // Inside blocked range
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cidr := testCases[i%len(testCases)]
		prefix, _ := ParsePrefix(cidr)
		_, _ = ComputeExcept(prefix)
	}
}

func BenchmarkValidateEgressCIDRs_Small(b *testing.B) {
	cidrs := []string{
		"8.8.8.0/24",
		"1.1.1.0/24",
		"0.0.0.0/0",
		"2001:db8::/32",
		"2606:4700::/32",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, cidr := range cidrs {
			prefix, err := ParsePrefix(cidr)
			if err != nil {
				continue
			}
			_, _ = ComputeExcept(prefix)
		}
	}
}

func BenchmarkValidateEgressCIDRs_Large(b *testing.B) {
	cidrs := []string{
		"8.8.8.0/24", "1.1.1.0/24", "8.8.4.0/24", "1.0.0.0/24",
		"208.67.222.0/24", "208.67.220.0/24", "9.9.9.0/24", "149.112.112.0/24",
		"64.6.64.0/24", "64.6.65.0/24", "185.228.168.0/24", "185.228.169.0/24",
		"76.76.19.0/24", "76.223.122.0/24", "94.140.14.0/24", "94.140.15.0/24",
		"216.146.35.0/24", "216.146.36.0/24", "156.154.70.0/24", "156.154.71.0/24",
		"2001:4860:4860::/48", "2606:4700:4700::/48", "2620:fe::/48", "2620:119:35::/48",
		"2620:119:53::/48", "2a0d:2a00:1::/48", "2a0d:2a00:2::/48", "2620:74:1b::/48",
		"2001:608::/32", "2001:678::/32", "2001:67c::/32", "2a01:4f8::/32",
		"2a01:4f9::/32", "2a02:6b8::/32", "2a02:2498::/32", "2a03:2880::/32",
		"2a04:4e42::/32", "2a05:d012::/32", "2a06:98c0::/32", "2a07:1c44::/32",
		"2a09:bac0::/32", "2a0a:e5c0::/32", "2a0b:4d07::/32", "2a0c:b641::/32",
		"2a0d:5600::/32", "2a0e:b107::/32", "2a0f:9400::/32", "2a10:cc40::/32",
		"2a11:fb00::/32", "2a12:4946::/32",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, cidr := range cidrs {
			prefix, err := ParsePrefix(cidr)
			if err != nil {
				continue
			}
			_, _ = ComputeExcept(prefix)
		}
	}
}

func FuzzValidateEgressCIDR(f *testing.F) {
	f.Add("10.0.0.0/8")
	f.Add("8.8.8.8/32")
	f.Add("2001:db8::/32")
	f.Add("invalid")
	f.Add("")
	f.Add("256.1.1.1/8")
	f.Add("10.0.0.0/33")

	f.Fuzz(func(t *testing.T, cidr string) {
		prefix, err := ParsePrefix(cidr)
		if err != nil {
			return
		}
		_, _ = ComputeExcept(prefix)
	})
}
