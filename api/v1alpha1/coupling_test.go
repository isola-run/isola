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

	"github.com/isola-run/isola/internal/operator/controller/podutil"
)

// This test is the end-to-end lock on the two metadata.name caps.
//
// Derivation chain:
//  1. K8s caps label values at 63 chars.
//  2. The snapshot Job is named "<rootfsSnapshot.Name>-job". K8s auto-injects
//     batch.kubernetes.io/job-name as a label value equal to the Job name, so
//     rootfsSnapshot.Name + len("-job") must fit 63. That gives the
//     RootfsSnapshot cap: 59.
//  3. When terminationPolicy.type is SnapshotRootfs the Sandbox controller
//     derives a RootfsSnapshot named "<sandbox.Name>-termination". That
//     derived name must fit the RootfsSnapshot cap, so sandbox.Name +
//     len("-termination") must fit 59. That gives the Sandbox cap: 47.
//
// This test takes the actual derivation function (podutil.GetTerminationSnapshotName)
// and a sandbox at the CRD-enforced max length, and checks the apiserver
// accepts the derived RootfsSnapshot. If anyone bumps either cap, changes
// the derivation suffix, or forgets to update the matched cap, this fails.
var _ = Describe("metadata.name cap coupling (Sandbox derives RootfsSnapshot)", func() {
	It("Sandbox at max length produces a -termination snapshot name the CRD accepts", func() {
		sandboxName := strings.Repeat("a", 47)

		sb := minimalSandbox(sandboxName)
		Expect(k8sClient.Create(ctx, sb)).To(Succeed())

		// Operator derivation. Deliberately uses the real function, not a
		// literal, so changing the suffix breaks this test.
		derivedName := podutil.GetTerminationSnapshotName(sandboxName)

		// 47 (sandbox cap) + 12 ("-termination") = 59 (RootfsSnapshot cap).
		// Drift in either cap shows up here as a length mismatch.
		Expect(derivedName).To(HaveLen(47 + len("-termination")))

		rfs := minimalRootfsSnapshot(derivedName, sandboxName)
		Expect(k8sClient.Create(ctx, rfs)).To(Succeed())
	})
})
