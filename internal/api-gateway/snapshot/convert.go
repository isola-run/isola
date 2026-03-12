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

package snapshot

import (
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	sandboxv1alpha1 "github.com/isola-ai/isola/api/v1alpha1"
)

// maxK8sNameLen is the maximum length of a Kubernetes object name.
const maxK8sNameLen = 253

// generateNamePrefix builds a GenerateName prefix from sandbox name and snapshot name,
// truncating to stay within K8s name limits (253 chars minus 5 for the random suffix).
func generateNamePrefix(sandboxName, snapshotName string) string {
	prefix := sandboxName + "-" + snapshotName + "-"
	maxPrefix := maxK8sNameLen - 5
	if len(prefix) > maxPrefix {
		prefix = prefix[:maxPrefix]
		prefix = strings.TrimRight(prefix, "-")
		prefix += "-"
	}
	return prefix
}

func requestToRootfsSnapshotCR(req CreateRootfsSnapshotRequest, sandboxName, namespace string) *sandboxv1alpha1.RootfsSnapshot {
	return &sandboxv1alpha1.RootfsSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: generateNamePrefix(sandboxName, req.SnapshotName),
			Namespace:    namespace,
		},
		Spec: sandboxv1alpha1.RootfsSnapshotSpec{
			SandboxName:             sandboxName,
			SnapshotName:            req.SnapshotName,
			Container:               req.Container,
			ActiveDeadlineSeconds:   req.ActiveDeadlineSeconds,
			TTLSecondsAfterFinished: req.TTLSecondsAfterFinished,
		},
	}
}

func rootfsSnapshotToResponse(s *sandboxv1alpha1.RootfsSnapshot) RootfsSnapshotResponse {
	resp := RootfsSnapshotResponse{
		ID:                      s.Name,
		SandboxID:               s.Spec.SandboxName,
		SnapshotName:            s.Spec.SnapshotName,
		Container:               s.Spec.Container,
		Status:                  rootfsSnapshotStatus(s),
		FailureMessage:          rootfsSnapshotFailureMessage(s),
		CreationTimestamp:       s.CreationTimestamp.UTC().Format(time.RFC3339),
		ActiveDeadlineSeconds:   s.Spec.ActiveDeadlineSeconds,
		TTLSecondsAfterFinished: s.Spec.TTLSecondsAfterFinished,
	}

	if s.Status.SnapshotKey != "" {
		resp.SnapshotKey = &s.Status.SnapshotKey
	}

	if s.Status.StartTime != nil {
		t := s.Status.StartTime.UTC().Format(time.RFC3339)
		resp.StartTime = &t
	}

	if s.Status.CompletionTime != nil {
		t := s.Status.CompletionTime.UTC().Format(time.RFC3339)
		resp.CompletionTime = &t
	}

	return resp
}

// rootfsSnapshotStatus derives the user-facing status from CRD conditions.
// Checks Failed before Complete defensively.
func rootfsSnapshotStatus(s *sandboxv1alpha1.RootfsSnapshot) string {
	failed := meta.FindStatusCondition(s.Status.Conditions, string(sandboxv1alpha1.RootfsSnapshotFailed))
	if failed != nil && failed.Status == metav1.ConditionTrue {
		return "failed"
	}

	complete := meta.FindStatusCondition(s.Status.Conditions, string(sandboxv1alpha1.RootfsSnapshotComplete))
	if complete != nil && complete.Status == metav1.ConditionTrue {
		return "complete"
	}

	if s.Status.StartTime != nil {
		return "inProgress"
	}

	return "pending"
}

func rootfsSnapshotFailureMessage(s *sandboxv1alpha1.RootfsSnapshot) *string {
	failed := meta.FindStatusCondition(s.Status.Conditions, string(sandboxv1alpha1.RootfsSnapshotFailed))
	if failed != nil && failed.Status == metav1.ConditionTrue && failed.Message != "" {
		return &failed.Message
	}
	return nil
}
