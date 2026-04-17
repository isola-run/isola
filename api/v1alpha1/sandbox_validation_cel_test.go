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

package v1alpha1_test

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/utils/ptr"

	sandboxv1alpha1 "github.com/isola-run/isola/api/v1alpha1"
)

// Convention (karpenter pkg/apis/v1/*_validation_cel_test.go): assert only
// Succeed vs ToNot(Succeed). CEL error wording is not a contract and drifts
// across K8s versions.

var _ = Describe("Sandbox CRD validation", func() {

	// Sandbox metadata.name is capped at 47. The number comes from the
	// operator deriving "<sandbox.Name>-termination" as a RootfsSnapshot
	// name when terminationPolicy.type is SnapshotRootfs, and that derived
	// name must fit the RootfsSnapshot.metadata.name cap (59). So
	// 47 = 59 - len("-termination"). See also the end-to-end coupling test.
	Describe("metadata.name size cap", func() {
		// Boundary probe: shift the marker by one and one of these fails.
		It("accepts 47 characters", func() {
			sb := minimalSandbox(strings.Repeat("a", 47))
			Expect(k8sClient.Create(ctx, sb)).To(Succeed())
		})
		It("rejects 48 characters", func() {
			sb := minimalSandbox(strings.Repeat("a", 48))
			Expect(k8sClient.Create(ctx, sb)).ToNot(Succeed())
		})
	})

	Describe("spec.timeoutSeconds is fully immutable", func() {
		It("rejects unset to set", func() {
			sb := minimalSandbox("sb")
			Expect(k8sClient.Create(ctx, sb)).To(Succeed())
			sb.Spec.TimeoutSeconds = ptr.To[int64](60)
			Expect(k8sClient.Update(ctx, sb)).ToNot(Succeed())
		})
		It("rejects set to different value", func() {
			sb := minimalSandbox("sb")
			sb.Spec.TimeoutSeconds = ptr.To[int64](60)
			Expect(k8sClient.Create(ctx, sb)).To(Succeed())
			sb.Spec.TimeoutSeconds = ptr.To[int64](120)
			Expect(k8sClient.Update(ctx, sb)).ToNot(Succeed())
		})
		It("rejects set to unset", func() {
			sb := minimalSandbox("sb")
			sb.Spec.TimeoutSeconds = ptr.To[int64](60)
			Expect(k8sClient.Create(ctx, sb)).To(Succeed())
			sb.Spec.TimeoutSeconds = nil
			Expect(k8sClient.Update(ctx, sb)).ToNot(Succeed())
		})
		It("accepts set to same value", func() {
			sb := minimalSandbox("sb")
			sb.Spec.TimeoutSeconds = ptr.To[int64](60)
			Expect(k8sClient.Create(ctx, sb)).To(Succeed())
			sb.Spec.TimeoutSeconds = ptr.To[int64](60)
			Expect(k8sClient.Update(ctx, sb)).To(Succeed())
		})
	})

	Describe("spec.network is immutable once set", func() {
		It("accepts unset to set", func() {
			sb := minimalSandbox("sb")
			Expect(k8sClient.Create(ctx, sb)).To(Succeed())
			sb.Spec.Network = &sandboxv1alpha1.Network{AllowClusterDNS: ptr.To(true)}
			Expect(k8sClient.Update(ctx, sb)).To(Succeed())
		})
		It("rejects set to different", func() {
			sb := minimalSandbox("sb")
			sb.Spec.Network = &sandboxv1alpha1.Network{AllowClusterDNS: ptr.To(true)}
			Expect(k8sClient.Create(ctx, sb)).To(Succeed())
			sb.Spec.Network.AllowClusterDNS = ptr.To(false)
			Expect(k8sClient.Update(ctx, sb)).ToNot(Succeed())
		})
		It("rejects set to unset", func() {
			sb := minimalSandbox("sb")
			sb.Spec.Network = &sandboxv1alpha1.Network{AllowClusterDNS: ptr.To(true)}
			Expect(k8sClient.Create(ctx, sb)).To(Succeed())
			sb.Spec.Network = nil
			Expect(k8sClient.Update(ctx, sb)).ToNot(Succeed())
		})
		It("accepts set to same", func() {
			sb := minimalSandbox("sb")
			sb.Spec.Network = &sandboxv1alpha1.Network{AllowClusterDNS: ptr.To(true)}
			Expect(k8sClient.Create(ctx, sb)).To(Succeed())
			sb.Spec.Network = &sandboxv1alpha1.Network{AllowClusterDNS: ptr.To(true)}
			Expect(k8sClient.Update(ctx, sb)).To(Succeed())
		})
	})

	Describe("spec.rootfsSnapshotSources is immutable once set", func() {
		It("accepts unset to set", func() {
			sb := minimalSandbox("sb")
			Expect(k8sClient.Create(ctx, sb)).To(Succeed())
			sb.Spec.RootfsSnapshotSources = []sandboxv1alpha1.RootfsSnapshotSource{
				{SnapshotName: "snap1"},
			}
			Expect(k8sClient.Update(ctx, sb)).To(Succeed())
		})
		It("rejects set to different", func() {
			sb := minimalSandbox("sb")
			sb.Spec.RootfsSnapshotSources = []sandboxv1alpha1.RootfsSnapshotSource{
				{SnapshotName: "snap1"},
			}
			Expect(k8sClient.Create(ctx, sb)).To(Succeed())
			sb.Spec.RootfsSnapshotSources[0].SnapshotName = "snap2"
			Expect(k8sClient.Update(ctx, sb)).ToNot(Succeed())
		})
		It("rejects set to unset", func() {
			sb := minimalSandbox("sb")
			sb.Spec.RootfsSnapshotSources = []sandboxv1alpha1.RootfsSnapshotSource{
				{SnapshotName: "snap1"},
			}
			Expect(k8sClient.Create(ctx, sb)).To(Succeed())
			sb.Spec.RootfsSnapshotSources = nil
			Expect(k8sClient.Update(ctx, sb)).ToNot(Succeed())
		})
		It("accepts set to same", func() {
			sb := minimalSandbox("sb")
			sb.Spec.RootfsSnapshotSources = []sandboxv1alpha1.RootfsSnapshotSource{
				{SnapshotName: "snap1"},
			}
			Expect(k8sClient.Create(ctx, sb)).To(Succeed())
			sb.Spec.RootfsSnapshotSources = []sandboxv1alpha1.RootfsSnapshotSource{
				{SnapshotName: "snap1"},
			}
			Expect(k8sClient.Update(ctx, sb)).To(Succeed())
		})
	})

	Describe("spec.terminationPolicy is immutable once set", func() {
		It("accepts unset to set", func() {
			sb := minimalSandbox("sb")
			Expect(k8sClient.Create(ctx, sb)).To(Succeed())
			sb.Spec.TerminationPolicy = &sandboxv1alpha1.TerminationPolicy{
				Type: sandboxv1alpha1.TerminationTypeDelete,
			}
			Expect(k8sClient.Update(ctx, sb)).To(Succeed())
		})
		It("rejects change to a nested snapshotRootfs field", func() {
			sb := minimalSandbox("sb")
			sb.Spec.TerminationPolicy = &sandboxv1alpha1.TerminationPolicy{
				Type: sandboxv1alpha1.TerminationTypeSnapshotRootfs,
				SnapshotRootfs: &sandboxv1alpha1.SnapshotRootfsTermination{
					SnapshotName: "snap1",
				},
			}
			Expect(k8sClient.Create(ctx, sb)).To(Succeed())
			sb.Spec.TerminationPolicy.SnapshotRootfs.SnapshotName = "snap2"
			Expect(k8sClient.Update(ctx, sb)).ToNot(Succeed())
		})
		It("rejects set to unset", func() {
			sb := minimalSandbox("sb")
			sb.Spec.TerminationPolicy = &sandboxv1alpha1.TerminationPolicy{
				Type: sandboxv1alpha1.TerminationTypeDelete,
			}
			Expect(k8sClient.Create(ctx, sb)).To(Succeed())
			sb.Spec.TerminationPolicy = nil
			Expect(k8sClient.Update(ctx, sb)).ToNot(Succeed())
		})
		It("accepts set to same", func() {
			sb := minimalSandbox("sb")
			sb.Spec.TerminationPolicy = &sandboxv1alpha1.TerminationPolicy{
				Type: sandboxv1alpha1.TerminationTypeDelete,
			}
			Expect(k8sClient.Create(ctx, sb)).To(Succeed())
			sb.Spec.TerminationPolicy = &sandboxv1alpha1.TerminationPolicy{
				Type: sandboxv1alpha1.TerminationTypeDelete,
			}
			Expect(k8sClient.Update(ctx, sb)).To(Succeed())
		})
	})

	Describe("TerminationPolicy shape", func() {
		It("rejects type=SnapshotRootfs without snapshotRootfs", func() {
			sb := minimalSandbox("sb")
			sb.Spec.TerminationPolicy = &sandboxv1alpha1.TerminationPolicy{
				Type: sandboxv1alpha1.TerminationTypeSnapshotRootfs,
			}
			Expect(k8sClient.Create(ctx, sb)).ToNot(Succeed())
		})
		It("rejects type=Delete with snapshotRootfs", func() {
			sb := minimalSandbox("sb")
			sb.Spec.TerminationPolicy = &sandboxv1alpha1.TerminationPolicy{
				Type:           sandboxv1alpha1.TerminationTypeDelete,
				SnapshotRootfs: &sandboxv1alpha1.SnapshotRootfsTermination{},
			}
			Expect(k8sClient.Create(ctx, sb)).ToNot(Succeed())
		})
		It("accepts type=SnapshotRootfs with snapshotRootfs", func() {
			sb := minimalSandbox("sb")
			sb.Spec.TerminationPolicy = &sandboxv1alpha1.TerminationPolicy{
				Type:           sandboxv1alpha1.TerminationTypeSnapshotRootfs,
				SnapshotRootfs: &sandboxv1alpha1.SnapshotRootfsTermination{},
			}
			Expect(k8sClient.Create(ctx, sb)).To(Succeed())
		})
	})

	Describe("Network IPv6 gating", func() {
		It("rejects IPv6 nameservers without allowIPv6Egress", func() {
			sb := minimalSandbox("sb")
			sb.Spec.Network = &sandboxv1alpha1.Network{
				Nameservers: []string{"2001:db8::1"},
			}
			Expect(k8sClient.Create(ctx, sb)).ToNot(Succeed())
		})
		It("accepts IPv6 nameservers with allowIPv6Egress=true", func() {
			sb := minimalSandbox("sb")
			sb.Spec.Network = &sandboxv1alpha1.Network{
				AllowIPv6Egress: ptr.To(true),
				Nameservers:     []string{"2001:db8::1"},
			}
			Expect(k8sClient.Create(ctx, sb)).To(Succeed())
		})
		It("rejects IPv6 allowedEgressCIDRs without allowIPv6Egress", func() {
			sb := minimalSandbox("sb")
			sb.Spec.Network = &sandboxv1alpha1.Network{
				AllowedEgressCIDRs: []string{"2001:db8::/32"},
			}
			Expect(k8sClient.Create(ctx, sb)).ToNot(Succeed())
		})
		It("accepts IPv6 allowedEgressCIDRs with allowIPv6Egress=true", func() {
			sb := minimalSandbox("sb")
			sb.Spec.Network = &sandboxv1alpha1.Network{
				AllowIPv6Egress:    ptr.To(true),
				AllowedEgressCIDRs: []string{"2001:db8::/32"},
			}
			Expect(k8sClient.Create(ctx, sb)).To(Succeed())
		})
	})

	Describe("Network format rules", func() {
		It("rejects allowedEgressCIDRs that are not CIDR", func() {
			sb := minimalSandbox("sb")
			sb.Spec.Network = &sandboxv1alpha1.Network{
				AllowedEgressCIDRs: []string{"not-a-cidr"},
			}
			Expect(k8sClient.Create(ctx, sb)).ToNot(Succeed())
		})
		It("rejects nameservers that are not IP", func() {
			sb := minimalSandbox("sb")
			sb.Spec.Network = &sandboxv1alpha1.Network{
				Nameservers: []string{"not-an-ip"},
			}
			Expect(k8sClient.Create(ctx, sb)).ToNot(Succeed())
		})
	})

	Describe("rootfsSnapshotSources shape", func() {
		It("accepts a single source with no containerName", func() {
			sb := minimalSandbox("sb")
			sb.Spec.RootfsSnapshotSources = []sandboxv1alpha1.RootfsSnapshotSource{
				{SnapshotName: "snap1"},
			}
			Expect(k8sClient.Create(ctx, sb)).To(Succeed())
		})
		It("rejects multiple sources missing containerName", func() {
			sb := minimalSandbox("sb")
			sb.Spec.RootfsSnapshotSources = []sandboxv1alpha1.RootfsSnapshotSource{
				{SnapshotName: "snap1"},
				{SnapshotName: "snap2"},
			}
			Expect(k8sClient.Create(ctx, sb)).ToNot(Succeed())
		})
		It("rejects multiple sources with duplicate containerName", func() {
			sb := minimalSandbox("sb")
			sb.Spec.RootfsSnapshotSources = []sandboxv1alpha1.RootfsSnapshotSource{
				{SnapshotName: "snap1", ContainerName: "same"},
				{SnapshotName: "snap2", ContainerName: "same"},
			}
			Expect(k8sClient.Create(ctx, sb)).ToNot(Succeed())
		})
		It("accepts multiple sources with distinct containerNames", func() {
			sb := minimalSandbox("sb")
			sb.Spec.RootfsSnapshotSources = []sandboxv1alpha1.RootfsSnapshotSource{
				{SnapshotName: "snap1", ContainerName: "c1"},
				{SnapshotName: "snap2", ContainerName: "c2"},
			}
			Expect(k8sClient.Create(ctx, sb)).To(Succeed())
		})
	})

	// rootfsSnapshotSources[].snapshotName is interpolated into a host file
	// path by the operator (see injectRootfsRestore). The DNS-1123 subdomain
	// pattern happens to forbid every char that would enable path traversal
	// (separators, null bytes, leading dots). These inputs would all be
	// rejected by the pattern alone, and this test pins that behavior so a
	// future pattern loosening that permits any of these fails here first.
	DescribeTable("rootfsSnapshotSources[].snapshotName rejects characters that would enable path traversal",
		func(bad string) {
			sb := minimalSandbox("sb")
			sb.Spec.RootfsSnapshotSources = []sandboxv1alpha1.RootfsSnapshotSource{
				{SnapshotName: bad},
			}
			Expect(k8sClient.Create(ctx, sb)).ToNot(Succeed())
		},
		Entry("parent-dir marker", ".."),
		Entry("explicit traversal", "../foo"),
		Entry("forward slash", "foo/bar"),
		Entry("backslash", "foo\\bar"),
		Entry("null byte", "foo\x00bar"),
	)
})
