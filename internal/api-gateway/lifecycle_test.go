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

package apigateway

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/isola-run/isola/internal/operator/controller"
)

var _ = Describe("ConditionsToStatus", func() {
	readyTrue := func(reason string) []metav1.Condition {
		return []metav1.Condition{{
			Type:               controller.SandboxReadyCondition,
			Status:             metav1.ConditionTrue,
			Reason:             reason,
			LastTransitionTime: metav1.Now(),
		}}
	}

	readyFalse := func(reason string) []metav1.Condition {
		return []metav1.Condition{{
			Type:               controller.SandboxReadyCondition,
			Status:             metav1.ConditionFalse,
			Reason:             reason,
			LastTransitionTime: metav1.Now(),
		}}
	}

	DescribeTable("maps Ready=True to running",
		func(reason string) {
			Expect(ConditionsToStatus(readyTrue(reason))).To(Equal(StatusRunning))
		},
		Entry("PodRunning", controller.CondReasonPodRunning),
		Entry("arbitrary reason", "AnythingElse"),
	)

	DescribeTable("maps Ready=False reasons to starting",
		func(reason string) {
			Expect(ConditionsToStatus(readyFalse(reason))).To(Equal(StatusStarting))
		},
		Entry("PodPending", controller.CondReasonPodPending),
		Entry("PodCreating", controller.CondReasonPodCreating),
		Entry("Reconciling", controller.CondReasonReconciling),
		Entry("NetworkPolicyApplied", controller.CondReasonNetworkPolicyApplied),
	)

	DescribeTable("maps Ready=False reasons to running",
		func(reason string) {
			Expect(ConditionsToStatus(readyFalse(reason))).To(Equal(StatusRunning))
		},
		Entry("PodRunning", controller.CondReasonPodRunning),
		Entry("RootfsSnapshottingInProgress", controller.CondReasonRootfsSnapshottingInProgress),
	)

	DescribeTable("maps Ready=False reasons to terminating",
		func(reason string) {
			Expect(ConditionsToStatus(readyFalse(reason))).To(Equal(StatusTerminating))
		},
		Entry("Deleting", controller.CondReasonDeleting),
		Entry("RootfsSnapshotComplete", controller.CondReasonRootfsSnapshotComplete),
	)

	DescribeTable("maps Ready=False reasons to failed",
		func(reason string) {
			Expect(ConditionsToStatus(readyFalse(reason))).To(Equal(StatusFailed))
		},
		Entry("PodFailed", controller.CondReasonPodFailed),
		Entry("PodCreationFailed", controller.CondReasonPodCreationFailed),
		Entry("InvalidRuntime", controller.CondReasonInvalidRuntime),
		Entry("NetworkPolicyFailed", controller.CondReasonNetworkPolicyFailed),
		Entry("RootfsSnapshotFailed", controller.CondReasonRootfsSnapshotFailed),
		Entry("RootfsSnapshotTimeout", controller.CondReasonRootfsSnapshotTimeout),
		Entry("RootfsRestoreConfigurationError", controller.CondReasonRootfsRestoreConfigError),
		Entry("StartupTimeoutExceeded", controller.CondReasonStartupTimeoutExceeded),
	)

	It("maps PodSucceeded to stopped", func() {
		Expect(ConditionsToStatus(readyFalse(controller.CondReasonPodSucceeded))).To(Equal(StatusStopped))
	})

	It("returns unknown for nil conditions", func() {
		Expect(ConditionsToStatus(nil)).To(Equal(StatusUnknown))
	})

	It("returns unknown for empty conditions", func() {
		Expect(ConditionsToStatus([]metav1.Condition{})).To(Equal(StatusUnknown))
	})

	It("returns unknown for unrecognized reason", func() {
		Expect(ConditionsToStatus(readyFalse("SomethingNew"))).To(Equal(StatusUnknown))
	})

	It("ignores non-Ready conditions", func() {
		conditions := []metav1.Condition{
			{
				Type:               controller.SandboxPodReadyCondition,
				Status:             metav1.ConditionTrue,
				Reason:             controller.CondReasonPodRunning,
				LastTransitionTime: metav1.Now(),
			},
		}
		Expect(ConditionsToStatus(conditions)).To(Equal(StatusUnknown))
	})
})
