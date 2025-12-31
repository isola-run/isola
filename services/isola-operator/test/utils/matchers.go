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

package utils

import (
	"fmt"

	"github.com/onsi/gomega/format"
	"github.com/onsi/gomega/types"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// HaveCondition returns a matcher that checks if an object has a condition with the given type and status
func HaveCondition(condType string, status metav1.ConditionStatus) types.GomegaMatcher {
	return &conditionMatcher{
		condType:    condType,
		status:      status,
		checkReason: false,
	}
}

// HaveConditionWithReason returns a matcher that checks condition type, status, and reason
func HaveConditionWithReason(condType string, status metav1.ConditionStatus, reason string) types.GomegaMatcher {
	return &conditionMatcher{
		condType:    condType,
		status:      status,
		reason:      reason,
		checkReason: true,
	}
}

type conditionMatcher struct {
	condType    string
	status      metav1.ConditionStatus
	reason      string
	checkReason bool
}

func (m *conditionMatcher) Match(actual interface{}) (bool, error) {
	conditions, err := extractConditions(actual)
	if err != nil {
		return false, err
	}

	for _, cond := range conditions {
		if cond.Type == m.condType {
			if cond.Status != m.status {
				return false, nil
			}
			if m.checkReason && cond.Reason != m.reason {
				return false, nil
			}
			return true, nil
		}
	}
	return false, nil
}

func (m *conditionMatcher) FailureMessage(actual interface{}) string {
	conditions, _ := extractConditions(actual)
	if m.checkReason {
		return fmt.Sprintf("Expected condition %q with status %q and reason %q\nActual conditions: %s",
			m.condType, m.status, m.reason, format.Object(conditions, 1))
	}
	return fmt.Sprintf("Expected condition %q with status %q\nActual conditions: %s",
		m.condType, m.status, format.Object(conditions, 1))
}

func (m *conditionMatcher) NegatedFailureMessage(actual interface{}) string {
	if m.checkReason {
		return fmt.Sprintf("Expected condition %q NOT to have status %q and reason %q",
			m.condType, m.status, m.reason)
	}
	return fmt.Sprintf("Expected condition %q NOT to have status %q", m.condType, m.status)
}

// ConditionsGetter is an interface for objects that have conditions
type ConditionsGetter interface {
	GetConditions() []metav1.Condition
}

func extractConditions(actual interface{}) ([]metav1.Condition, error) {
	if getter, ok := actual.(ConditionsGetter); ok {
		return getter.GetConditions(), nil
	}

	// Try to extract from common status patterns
	switch v := actual.(type) {
	case []metav1.Condition:
		return v, nil
	default:
		return nil, fmt.Errorf("expected object with conditions, got %T", actual)
	}
}

// HaveOwnerReference returns a matcher that checks if an object has an owner reference to the given object
func HaveOwnerReference(owner client.Object) types.GomegaMatcher {
	return &ownerReferenceMatcher{
		ownerName: owner.GetName(),
		ownerUID:  owner.GetUID(),
	}
}

type ownerReferenceMatcher struct {
	ownerName string
	ownerUID  k8stypes.UID
}

func (m *ownerReferenceMatcher) Match(actual interface{}) (bool, error) {
	obj, ok := actual.(client.Object)
	if !ok {
		return false, fmt.Errorf("expected client.Object, got %T", actual)
	}

	for _, ref := range obj.GetOwnerReferences() {
		if ref.Name == m.ownerName && ref.UID == m.ownerUID {
			return true, nil
		}
	}
	return false, nil
}

func (m *ownerReferenceMatcher) FailureMessage(actual interface{}) string {
	obj, _ := actual.(client.Object)
	return fmt.Sprintf("Expected owner reference to %q (UID: %s)\nActual owner references: %+v",
		m.ownerName, m.ownerUID, obj.GetOwnerReferences())
}

func (m *ownerReferenceMatcher) NegatedFailureMessage(actual interface{}) string {
	return fmt.Sprintf("Expected NOT to have owner reference to %q (UID: %s)",
		m.ownerName, m.ownerUID)
}

// HaveLabel returns a matcher that checks if an object has a specific label with a value
func HaveLabel(key, value string) types.GomegaMatcher {
	return &labelMatcher{key: key, value: value}
}

type labelMatcher struct {
	key   string
	value string
}

func (m *labelMatcher) Match(actual interface{}) (bool, error) {
	obj, ok := actual.(client.Object)
	if !ok {
		return false, fmt.Errorf("expected client.Object, got %T", actual)
	}

	labels := obj.GetLabels()
	if labels == nil {
		return false, nil
	}

	actualValue, exists := labels[m.key]
	return exists && actualValue == m.value, nil
}

func (m *labelMatcher) FailureMessage(actual interface{}) string {
	obj, _ := actual.(client.Object)
	return fmt.Sprintf("Expected label %q=%q\nActual labels: %+v", m.key, m.value, obj.GetLabels())
}

func (m *labelMatcher) NegatedFailureMessage(actual interface{}) string {
	return fmt.Sprintf("Expected NOT to have label %q=%q", m.key, m.value)
}

// HaveInitContainer returns a matcher that checks if a pod has an init container with the given name
func HaveInitContainer(name string) types.GomegaMatcher {
	return &initContainerMatcher{name: name}
}

type initContainerMatcher struct {
	name string
}

func (m *initContainerMatcher) Match(actual interface{}) (bool, error) {
	pod, ok := actual.(PodSpecGetter)
	if !ok {
		return false, fmt.Errorf("expected object with PodSpec, got %T", actual)
	}

	for _, c := range pod.GetPodSpec().InitContainers {
		if c.Name == m.name {
			return true, nil
		}
	}
	return false, nil
}

func (m *initContainerMatcher) FailureMessage(actual interface{}) string {
	pod, _ := actual.(PodSpecGetter)
	var names []string
	for _, c := range pod.GetPodSpec().InitContainers {
		names = append(names, c.Name)
	}
	return fmt.Sprintf("Expected init container %q\nActual init containers: %v", m.name, names)
}

func (m *initContainerMatcher) NegatedFailureMessage(actual interface{}) string {
	return fmt.Sprintf("Expected NOT to have init container %q", m.name)
}

// PodSpecGetter is an interface for objects that have a PodSpec
type PodSpecGetter interface {
	GetPodSpec() corev1.PodSpec
}

// HaveVolume returns a matcher that checks if a pod has a volume with the given name
func HaveVolume(name string) types.GomegaMatcher {
	return &volumeMatcher{name: name}
}

type volumeMatcher struct {
	name string
}

func (m *volumeMatcher) Match(actual interface{}) (bool, error) {
	pod, ok := actual.(PodSpecGetter)
	if !ok {
		return false, fmt.Errorf("expected object with PodSpec, got %T", actual)
	}

	spec := pod.GetPodSpec()
	// This is a simplified matcher - in practice we'd need to handle different types
	_ = spec
	return false, fmt.Errorf("volumeMatcher not fully implemented - use pod.Spec.Volumes directly")
}

func (m *volumeMatcher) FailureMessage(actual interface{}) string {
	return fmt.Sprintf("Expected volume %q", m.name)
}

func (m *volumeMatcher) NegatedFailureMessage(actual interface{}) string {
	return fmt.Sprintf("Expected NOT to have volume %q", m.name)
}

// ContainEvent returns a matcher that checks if an event channel contains an event with the given reason
func ContainEvent(reason string) types.GomegaMatcher {
	return &eventMatcher{reason: reason}
}

type eventMatcher struct {
	reason string
}

func (m *eventMatcher) Match(actual interface{}) (bool, error) {
	ch, ok := actual.(chan string)
	if !ok {
		return false, fmt.Errorf("expected chan string, got %T", actual)
	}

	// Non-blocking check of channel
	select {
	case event := <-ch:
		// Check if event contains reason
		return containsSubstring(event, m.reason), nil
	default:
		return false, nil
	}
}

func (m *eventMatcher) FailureMessage(actual interface{}) string {
	return fmt.Sprintf("Expected event with reason %q", m.reason)
}

func (m *eventMatcher) NegatedFailureMessage(actual interface{}) string {
	return fmt.Sprintf("Expected NOT to find event with reason %q", m.reason)
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s[1:], substr) || s[:len(substr)] == substr)
}
