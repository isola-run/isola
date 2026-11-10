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
	"bufio"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/onsi/gomega"
)

func TestHelmInternetEgressBlockedCIDRsMatchGoPolicy(t *testing.T) {
	tests := []struct {
		name     string
		template string
		blocked  []netip.Prefix
	}{
		{
			name:     "IPv4",
			template: "sandbox-allow-ipv4-internet-egress-networkpolicy.yaml",
			blocked:  BlockedV4,
		},
		{
			name:     "IPv6",
			template: "sandbox-allow-ipv6-internet-egress-networkpolicy.yaml",
			blocked:  BlockedV6,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			path := filepath.Join("..", "..", "..", "..", "..", "charts", "isola", "templates", "operator", tt.template)
			got, err := readHelmExceptCIDRs(path)
			g.Expect(err).NotTo(HaveOccurred())

			want := make([]string, 0, len(tt.blocked))
			for _, prefix := range tt.blocked {
				want = append(want, prefix.String())
			}
			g.Expect(got).To(ConsistOf(want), "Helm internet-egress exceptions must match the Go blocked-CIDR policy")
		})
	}
}

func readHelmExceptCIDRs(path string) ([]string, error) {
	f, err := os.Open(path) //nolint:gosec // path is a repository-owned test fixture
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var (
		cidrs        []string
		inExceptList bool
		exceptIndent int
	)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		indent := len(line) - len(strings.TrimLeft(line, " "))

		if trimmed == "except:" {
			inExceptList = true
			exceptIndent = indent
			continue
		}
		if !inExceptList {
			continue
		}
		if trimmed != "" && indent <= exceptIndent {
			break
		}
		if !strings.HasPrefix(trimmed, "- ") {
			continue
		}

		fields := strings.Fields(strings.TrimPrefix(trimmed, "- "))
		if len(fields) == 0 {
			continue
		}
		prefix, err := netip.ParsePrefix(fields[0])
		if err != nil {
			return nil, fmt.Errorf("parse CIDR %q in %s: %w", fields[0], path, err)
		}
		cidrs = append(cidrs, prefix.String())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if !inExceptList || len(cidrs) == 0 {
		return nil, fmt.Errorf("no except CIDRs found in %s", path)
	}
	return cidrs, nil
}
