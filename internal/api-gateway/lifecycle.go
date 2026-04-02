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
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/isola-run/isola/internal/operator/controller"
)

// Public lifecycle phases exposed through the REST API.
const (
	StatusStarting    = "starting"
	StatusRunning     = "running"
	StatusTerminating = "terminating"
	StatusFailed      = "failed"
	StatusStopped     = "stopped"
	StatusUnknown     = "unknown"
)

// StatusEnum is the OpenAPI enum value for sandbox status fields.
const StatusEnum = "starting,running,terminating,failed,stopped,unknown"

// ConditionsToStatus projects internal CRD conditions into a public lifecycle phase.
func ConditionsToStatus(conditions []metav1.Condition) string {
	ready := meta.FindStatusCondition(conditions, controller.SandboxReadyCondition)
	if ready == nil {
		return StatusUnknown
	}

	if ready.Status == metav1.ConditionTrue {
		return StatusRunning
	}

	switch ready.Reason {
	case controller.CondReasonPodPending,
		controller.CondReasonPodCreating,
		controller.CondReasonReconciling,
		controller.CondReasonNetworkPolicyApplied:
		return StatusStarting

	case controller.CondReasonPodRunning,
		controller.CondReasonRootfsSnapshottingInProgress:
		return StatusRunning

	case controller.CondReasonDeleting,
		controller.CondReasonRootfsSnapshotComplete:
		return StatusTerminating

	case controller.CondReasonPodFailed,
		controller.CondReasonPodCreationFailed,
		controller.CondReasonInvalidRuntime,
		controller.CondReasonNetworkPolicyFailed,
		controller.CondReasonRootfsSnapshotFailed,
		controller.CondReasonRootfsSnapshotTimeout,
		controller.CondReasonRootfsRestoreConfigError,
		controller.CondReasonStartupTimeoutExceeded:
		return StatusFailed

	case controller.CondReasonPodSucceeded:
		return StatusStopped

	default:
		return StatusUnknown
	}
}
