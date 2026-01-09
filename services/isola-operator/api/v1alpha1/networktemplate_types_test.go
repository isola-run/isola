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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNetworkTemplateSpec_ZeroValue(t *testing.T) {
	spec := NetworkTemplateSpec{}
	assert.Nil(t, spec.AllowedIngress)
	assert.Nil(t, spec.AllowedEgress)
	assert.Nil(t, spec.DNSServers)
}

func TestNetworkTemplate_DeepCopy(t *testing.T) {
	original := &NetworkTemplate{
		TypeMeta: metav1.TypeMeta{
			Kind:       "NetworkTemplate",
			APIVersion: "isola.run/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-template",
			Namespace: "default",
			Labels: map[string]string{
				"app": "test",
			},
		},
		Spec: NetworkTemplateSpec{
			AllowedIngress: []string{"10.0.0.0/8", "192.168.0.0/16"},
			AllowedEgress:  []string{"0.0.0.0/0"},
			DNSServers:     []string{"8.8.8.8", "8.8.4.4"},
		},
		Status: NetworkTemplateStatus{
			Conditions: []metav1.Condition{
				{
					Type:   "Ready",
					Status: metav1.ConditionTrue,
					Reason: "NetworkPolicyApplied",
				},
			},
		},
	}

	copied := original.DeepCopy()

	assert.Equal(t, original.TypeMeta, copied.TypeMeta)
	assert.Equal(t, original.ObjectMeta.Name, copied.ObjectMeta.Name)
	assert.Equal(t, original.ObjectMeta.Namespace, copied.ObjectMeta.Namespace)
	assert.Equal(t, original.ObjectMeta.Labels, copied.ObjectMeta.Labels)
	assert.Equal(t, original.Spec.AllowedIngress, copied.Spec.AllowedIngress)
	assert.Equal(t, original.Spec.AllowedEgress, copied.Spec.AllowedEgress)
	assert.Equal(t, original.Spec.DNSServers, copied.Spec.DNSServers)
	assert.Equal(t, original.Status.Conditions, copied.Status.Conditions)

	// Verify deep copy independence - modifications to copy don't affect original
	copied.ObjectMeta.Name = "modified-name"
	assert.NotEqual(t, original.ObjectMeta.Name, copied.ObjectMeta.Name)

	copied.Spec.AllowedIngress = append(copied.Spec.AllowedIngress, "172.16.0.0/12")
	assert.NotEqual(t, len(original.Spec.AllowedIngress), len(copied.Spec.AllowedIngress))

	copied.Spec.DNSServers[0] = "1.1.1.1"
	assert.NotEqual(t, original.Spec.DNSServers[0], copied.Spec.DNSServers[0])

	copied.ObjectMeta.Labels["new-label"] = "new-value"
	assert.NotContains(t, original.ObjectMeta.Labels, "new-label")

	copied.Status.Conditions[0].Status = metav1.ConditionFalse
	assert.NotEqual(t, original.Status.Conditions[0].Status, copied.Status.Conditions[0].Status)
}

func TestNetworkTemplateList_DeepCopy(t *testing.T) {
	original := &NetworkTemplateList{
		Items: []NetworkTemplate{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "template-1"},
				Spec: NetworkTemplateSpec{
					AllowedEgress: []string{"8.8.8.8/32"},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "template-2"},
			},
		},
	}

	copied := original.DeepCopy()

	assert.Equal(t, len(original.Items), len(copied.Items))
	assert.Equal(t, original.Items[0].Name, copied.Items[0].Name)
	assert.Equal(t, original.Items[0].Spec.AllowedEgress, copied.Items[0].Spec.AllowedEgress)

	// Verify independence
	copied.Items[0].Name = "modified"
	assert.Equal(t, "template-1", original.Items[0].Name)

	copied.Items[0].Spec.AllowedEgress[0] = "1.1.1.1/32"
	assert.Equal(t, "8.8.8.8/32", original.Items[0].Spec.AllowedEgress[0])

	copied.Items = append(copied.Items, NetworkTemplate{ObjectMeta: metav1.ObjectMeta{Name: "template-3"}})
	assert.Equal(t, 2, len(original.Items))
}
