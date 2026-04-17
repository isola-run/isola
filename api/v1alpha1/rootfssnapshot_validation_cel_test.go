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
)

var _ = Describe("RootfsSnapshot CRD validation", func() {

	// RootfsSnapshot metadata.name is capped at 59. The operator creates
	// a snapshot Job named "<rootfsSnapshot.Name>-job"; Kubernetes
	// auto-injects batch.kubernetes.io/job-name on the Job's pod template
	// with value = Job name. Label values cap at 63, so the Job name
	// must fit 63, which gives rootfsSnapshot.Name <= 63 - len("-job") = 59.
	Describe("metadata.name size cap", func() {
		It("accepts 59 characters", func() {
			rfs := minimalRootfsSnapshot(strings.Repeat("a", 59), "sb")
			Expect(k8sClient.Create(ctx, rfs)).To(Succeed())
		})
		It("rejects 60 characters", func() {
			rfs := minimalRootfsSnapshot(strings.Repeat("a", 60), "sb")
			Expect(k8sClient.Create(ctx, rfs)).ToNot(Succeed())
		})
	})

	Describe("spec.sandboxName is immutable", func() {
		It("rejects set to different value", func() {
			rfs := minimalRootfsSnapshot("rfs", "sb-a")
			Expect(k8sClient.Create(ctx, rfs)).To(Succeed())
			rfs.Spec.SandboxName = "sb-b"
			Expect(k8sClient.Update(ctx, rfs)).ToNot(Succeed())
		})
		It("accepts set to same value", func() {
			rfs := minimalRootfsSnapshot("rfs", "sb-a")
			Expect(k8sClient.Create(ctx, rfs)).To(Succeed())
			rfs.Spec.SandboxName = "sb-a"
			Expect(k8sClient.Update(ctx, rfs)).To(Succeed())
		})
	})

	Describe("spec.snapshotName is immutable once set", func() {
		It("accepts unset to set", func() {
			rfs := minimalRootfsSnapshot("rfs", "sb")
			Expect(k8sClient.Create(ctx, rfs)).To(Succeed())
			rfs.Spec.SnapshotName = "snap1"
			Expect(k8sClient.Update(ctx, rfs)).To(Succeed())
		})
		It("rejects set to different", func() {
			rfs := minimalRootfsSnapshot("rfs", "sb")
			rfs.Spec.SnapshotName = "snap1"
			Expect(k8sClient.Create(ctx, rfs)).To(Succeed())
			rfs.Spec.SnapshotName = "snap2"
			Expect(k8sClient.Update(ctx, rfs)).ToNot(Succeed())
		})
		It("rejects set to unset", func() {
			rfs := minimalRootfsSnapshot("rfs", "sb")
			rfs.Spec.SnapshotName = "snap1"
			Expect(k8sClient.Create(ctx, rfs)).To(Succeed())
			rfs.Spec.SnapshotName = ""
			Expect(k8sClient.Update(ctx, rfs)).ToNot(Succeed())
		})
		It("accepts set to same", func() {
			rfs := minimalRootfsSnapshot("rfs", "sb")
			rfs.Spec.SnapshotName = "snap1"
			Expect(k8sClient.Create(ctx, rfs)).To(Succeed())
			rfs.Spec.SnapshotName = "snap1"
			Expect(k8sClient.Update(ctx, rfs)).To(Succeed())
		})
	})

	Describe("spec.containerName is immutable once set", func() {
		It("accepts unset to set", func() {
			rfs := minimalRootfsSnapshot("rfs", "sb")
			Expect(k8sClient.Create(ctx, rfs)).To(Succeed())
			rfs.Spec.ContainerName = "c1"
			Expect(k8sClient.Update(ctx, rfs)).To(Succeed())
		})
		It("rejects set to different", func() {
			rfs := minimalRootfsSnapshot("rfs", "sb")
			rfs.Spec.ContainerName = "c1"
			Expect(k8sClient.Create(ctx, rfs)).To(Succeed())
			rfs.Spec.ContainerName = "c2"
			Expect(k8sClient.Update(ctx, rfs)).ToNot(Succeed())
		})
		It("rejects set to unset", func() {
			rfs := minimalRootfsSnapshot("rfs", "sb")
			rfs.Spec.ContainerName = "c1"
			Expect(k8sClient.Create(ctx, rfs)).To(Succeed())
			rfs.Spec.ContainerName = ""
			Expect(k8sClient.Update(ctx, rfs)).ToNot(Succeed())
		})
		It("accepts set to same", func() {
			rfs := minimalRootfsSnapshot("rfs", "sb")
			rfs.Spec.ContainerName = "c1"
			Expect(k8sClient.Create(ctx, rfs)).To(Succeed())
			rfs.Spec.ContainerName = "c1"
			Expect(k8sClient.Update(ctx, rfs)).To(Succeed())
		})
	})

	// spec.timeoutSeconds has +kubebuilder:default=300, so the apiserver
	// fills it on Create. Post-create the unset state is unreachable from
	// a client, so only the value-to-value transitions are testable.
	Describe("spec.timeoutSeconds is fully immutable", func() {
		It("rejects set to different value", func() {
			rfs := minimalRootfsSnapshot("rfs", "sb")
			Expect(k8sClient.Create(ctx, rfs)).To(Succeed())
			// apiserver defaulted to 300; change it.
			rfs.Spec.TimeoutSeconds = ptr.To[int64](60)
			Expect(k8sClient.Update(ctx, rfs)).ToNot(Succeed())
		})
		It("accepts set to same value", func() {
			rfs := minimalRootfsSnapshot("rfs", "sb")
			rfs.Spec.TimeoutSeconds = ptr.To[int64](120)
			Expect(k8sClient.Create(ctx, rfs)).To(Succeed())
			rfs.Spec.TimeoutSeconds = ptr.To[int64](120)
			Expect(k8sClient.Update(ctx, rfs)).To(Succeed())
		})
	})
})
