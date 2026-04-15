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

package podutil

import (
	"regexp"
	"strings"
	"testing"

	. "github.com/onsi/gomega"
)

// dns1123Subdomain matches a single DNS-1123 label (which all our generated
// names must be valid as, since labels are the strictest constraint).
var dns1123Label = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

func TestChildName_ShortParent_PlainConcat(t *testing.T) {
	g := NewWithT(t)

	// 22-char nanoid (matches sandboxIDLength in api-gateway/sandbox).
	parent := "abc1234567890123456789"
	g.Expect(parent).To(HaveLen(22))

	// Golden: short parents must produce the same name as the previous
	// plain-concatenation implementation, so existing sandboxes keep the
	// same child resource names.
	g.Expect(ChildName(parent, "-pod")).To(Equal(parent + "-pod"))
	g.Expect(ChildName(parent, "-custom-netpol")).To(Equal(parent + "-custom-netpol"))
	g.Expect(ChildName(parent, "-termination")).To(Equal(parent + "-termination"))
	g.Expect(ChildName(parent, "-job")).To(Equal(parent + "-job"))
}

func TestChildName_AtBoundary_PlainConcat(t *testing.T) {
	g := NewWithT(t)

	// parent length + suffix length == 63: still plain concat.
	parent := strings.Repeat("a", 59)
	got := ChildName(parent, "-pod")
	g.Expect(got).To(HaveLen(63))
	g.Expect(got).To(Equal(parent + "-pod"))
}

func TestChildName_OverBoundary_HashesParent(t *testing.T) {
	g := NewWithT(t)

	parent := strings.Repeat("a", 60) // 60 + 4 = 64 > 63: triggers hash branch.
	got := ChildName(parent, "-pod")

	g.Expect(len(got)).To(BeNumerically("<=", 63))
	g.Expect(got).To(HaveSuffix("-pod"))
	g.Expect(dns1123Label.MatchString(got)).To(BeTrue(), "result %q is not a valid DNS-1123 label", got)
	// Should not just be a naive truncation of the parent.
	g.Expect(got).NotTo(Equal(parent[:59] + "-pod"))
}

func TestChildName_DistinctLongParents_ProduceDistinctNames(t *testing.T) {
	g := NewWithT(t)

	// Two parents that share the first many characters; without a hash a
	// naive truncation would collide.
	a := strings.Repeat("a", 60) + "-one"
	b := strings.Repeat("a", 60) + "-two"

	g.Expect(ChildName(a, "-pod")).NotTo(Equal(ChildName(b, "-pod")))
}

func TestChildName_LongSuffix_HashesPair(t *testing.T) {
	g := NewWithT(t)

	// Suffix longer than head (31): exercises the second branch.
	parent := strings.Repeat("a", 50)
	suffix := "-" + strings.Repeat("b", 40)

	got := ChildName(parent, suffix)
	g.Expect(len(got)).To(BeNumerically("<=", 63))
	g.Expect(dns1123Label.MatchString(got)).To(BeTrue(), "result %q is not a valid DNS-1123 label", got)
}

func TestChildName_TrailingNonAlnumStripped(t *testing.T) {
	g := NewWithT(t)

	// makeValidName must trim a trailing non-alphanumeric character. We
	// can't easily produce one through ChildName itself with our usual
	// suffixes, so test the helper directly.
	g.Expect(makeValidName("foo.")).To(Equal("foo"))
	g.Expect(makeValidName("foo-")).To(Equal("foo"))
	g.Expect(makeValidName("foo.-.")).To(Equal("foo"))
	g.Expect(makeValidName("foo")).To(Equal("foo"))
	g.Expect(makeValidName("")).To(Equal(""))
}

func TestChildName_AllHelpersBoundedAt63(t *testing.T) {
	g := NewWithT(t)

	// Pathologically long parent (would never come from our APIs today,
	// but verifies the invariant that derived names always fit).
	parent := strings.Repeat("x", 200)

	for _, name := range []string{
		GetSandboxPodName(parent),
		GetCustomNetworkPolicyName(parent),
		GetTerminationSnapshotName(parent),
		GetSnapshotJobName(parent),
	} {
		g.Expect(len(name)).To(BeNumerically("<=", 63), "name %q exceeds 63 chars", name)
		g.Expect(dns1123Label.MatchString(name)).To(BeTrue(), "name %q is not a valid DNS-1123 label", name)
	}
}
