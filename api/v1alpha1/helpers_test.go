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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	sandboxv1alpha1 "github.com/isola-run/isola/api/v1alpha1"
)

// minimalSandbox returns the smallest Sandbox that passes CREATE validation:
// a required PodTemplate with a single container. Callers mutate spec fields
// as needed before Create/Update.
func minimalSandbox(name string) *sandboxv1alpha1.Sandbox {
	return &sandboxv1alpha1.Sandbox{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: sandboxv1alpha1.SandboxSpec{
			PodTemplate: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "c1", Image: "nginx"},
					},
				},
			},
		},
	}
}

// minimalRootfsSnapshot returns the smallest RootfsSnapshot that passes
// CREATE validation: just the required SandboxName.
func minimalRootfsSnapshot(name, sandboxName string) *sandboxv1alpha1.RootfsSnapshot {
	return &sandboxv1alpha1.RootfsSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: sandboxv1alpha1.RootfsSnapshotSpec{
			SandboxName: sandboxName,
		},
	}
}
