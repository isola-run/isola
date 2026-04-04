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

package rootfssnapshot

import (
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	sandboxv1alpha1 "github.com/isola-run/isola/api/v1alpha1"
)

func requestToRootfsSnapshotCR(req CreateRootfsSnapshotRequest, name, namespace string) *sandboxv1alpha1.RootfsSnapshot {
	return &sandboxv1alpha1.RootfsSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: sandboxv1alpha1.RootfsSnapshotSpec{
			SandboxName:             req.SandboxID,
			SnapshotName:            req.SnapshotName,
			ContainerName:           req.ContainerName,
			TimeoutSeconds:          req.TimeoutSeconds,
			TTLSecondsAfterFinished: req.TTLSecondsAfterFinished,
		},
	}
}

func rootfsSnapshotToResponse(rs *sandboxv1alpha1.RootfsSnapshot) RootfsSnapshotResponse {
	return RootfsSnapshotResponse{
		ID:                      rs.Name,
		SandboxID:               rs.Spec.SandboxName,
		SnapshotName:            rs.Spec.SnapshotName,
		ContainerName:           rs.Spec.ContainerName,
		TimeoutSeconds:          rs.Spec.TimeoutSeconds,
		TTLSecondsAfterFinished: rs.Spec.TTLSecondsAfterFinished,
		Status:                  snapshotStatus(rs.Status.StartTime, rs.Status.Conditions),
		CreationTimestamp:       rs.CreationTimestamp.UTC().Format(time.RFC3339),
	}
}

func snapshotStatus(startTime *metav1.Time, conditions []metav1.Condition) string {
	succeeded := meta.FindStatusCondition(conditions, sandboxv1alpha1.RootfsSnapshotSucceededCondition)
	if succeeded != nil {
		switch succeeded.Status {
		case metav1.ConditionTrue:
			return "Succeeded"
		case metav1.ConditionFalse:
			return "Failed"
		}
	}

	if startTime != nil {
		return "Running"
	}

	return "Pending"
}
