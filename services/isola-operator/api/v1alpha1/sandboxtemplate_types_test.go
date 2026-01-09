/*
Copyright 2025 isola.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSandboxTemplate_DeepCopy(t *testing.T) {
	timeout := int64(3600)
	deadline := int64(300)

	original := &SandboxTemplate{
		TypeMeta: metav1.TypeMeta{
			Kind:       "SandboxTemplate",
			APIVersion: "isola.run/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-template",
			Namespace: "default",
			Labels: map[string]string{
				"env": "test",
			},
		},
		Spec: SandboxTemplateSpec{
			PodTemplate: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "main",
							Image: "busybox:latest",
						},
					},
				},
			},
			TimeoutSeconds: &timeout,
			ShutdownPolicy: &ShutdownPolicy{
				Policy:                ShutdownPolicySnapshotFilesystem,
				ActiveDeadlineSeconds: &deadline,
			},
		},
		Status: SandboxTemplateStatus{
			Conditions: []metav1.Condition{
				{
					Type:   "Available",
					Status: metav1.ConditionTrue,
					Reason: "TemplateValid",
				},
			},
		},
	}

	copied := original.DeepCopy()

	// Verify fields are copied correctly
	assert.Equal(t, original.TypeMeta, copied.TypeMeta)
	assert.Equal(t, original.ObjectMeta.Name, copied.ObjectMeta.Name)
	assert.Equal(t, original.ObjectMeta.Namespace, copied.ObjectMeta.Namespace)
	assert.Equal(t, original.ObjectMeta.Labels, copied.ObjectMeta.Labels)
	assert.Equal(t, original.Spec.TimeoutSeconds, copied.Spec.TimeoutSeconds)
	assert.Equal(t, original.Spec.ShutdownPolicy.Policy, copied.Spec.ShutdownPolicy.Policy)
	assert.Equal(t, original.Spec.ShutdownPolicy.ActiveDeadlineSeconds, copied.Spec.ShutdownPolicy.ActiveDeadlineSeconds)
	assert.Equal(t, len(original.Spec.PodTemplate.Spec.Containers), len(copied.Spec.PodTemplate.Spec.Containers))
	assert.Equal(t, original.Status.Conditions, copied.Status.Conditions)

	// Verify deep copy independence - modifications to copy don't affect original
	copied.ObjectMeta.Name = "modified-name"
	assert.NotEqual(t, original.ObjectMeta.Name, copied.ObjectMeta.Name)
	assert.Equal(t, "test-template", original.ObjectMeta.Name)

	copied.ObjectMeta.Labels["new-key"] = "new-value"
	assert.NotContains(t, original.ObjectMeta.Labels, "new-key")

	newTimeout := int64(7200)
	copied.Spec.TimeoutSeconds = &newTimeout
	assert.Equal(t, int64(3600), *original.Spec.TimeoutSeconds)

	copied.Spec.ShutdownPolicy.Policy = ShutdownPolicyDelete
	assert.Equal(t, ShutdownPolicySnapshotFilesystem, original.Spec.ShutdownPolicy.Policy)

	copied.Spec.PodTemplate.Spec.Containers = append(copied.Spec.PodTemplate.Spec.Containers, corev1.Container{Name: "sidecar"})
	assert.Equal(t, 1, len(original.Spec.PodTemplate.Spec.Containers))

	copied.Status.Conditions[0].Status = metav1.ConditionFalse
	assert.Equal(t, metav1.ConditionTrue, original.Status.Conditions[0].Status)
}

func TestSandboxTemplateList_DeepCopy(t *testing.T) {
	original := &SandboxTemplateList{
		Items: []SandboxTemplate{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "template-1"},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "template-2"},
			},
		},
	}

	copied := original.DeepCopy()

	assert.Equal(t, len(original.Items), len(copied.Items))
	assert.Equal(t, original.Items[0].Name, copied.Items[0].Name)

	// Verify independence
	copied.Items[0].Name = "modified"
	assert.Equal(t, "template-1", original.Items[0].Name)

	copied.Items = append(copied.Items, SandboxTemplate{ObjectMeta: metav1.ObjectMeta{Name: "template-3"}})
	assert.Equal(t, 2, len(original.Items))
}
