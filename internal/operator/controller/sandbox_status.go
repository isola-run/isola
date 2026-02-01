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

package controller

import (
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/isola-ai/isola-sb/internal/operator/controller/podutil"
)

// reconcileState captures all inputs for condition computation.
// Computed fresh each reconcile from cluster state - not persisted.
type reconcileState struct {
	// Template state
	TemplateResolved bool
	TemplateError    string // Non-empty if template lookup failed

	// Network state
	NetworkApplied bool
	NetworkError   string // Non-empty if network policy build/apply failed

	// Pod state (derived from actual pod)
	PodExists bool
	PodPhase  corev1.PodPhase // "", Pending, Running, Succeeded, Failed
	PodReady  bool            // From pod's Ready condition

	// Fatal errors (sidecar injection, pod creation)
	FatalError  string
	FatalReason string // Condition reason for the error

	// Lifecycle state
	IsDeleting     bool // DeletionTimestamp set
	IsSnapshotting bool // Shutdown snapshot in progress
}

// updateFromPod updates state based on pod observation.
func (s *reconcileState) updateFromPod(pod *corev1.Pod) {
	if pod == nil {
		return
	}
	s.PodExists = true
	s.PodPhase = pod.Status.Phase
	s.PodReady = podutil.IsPodReady(pod)
}

func trueCondition(t, reason, msg string, gen int64) metav1.Condition {
	return metav1.Condition{
		Type:               t,
		Status:             metav1.ConditionTrue,
		Reason:             reason,
		Message:            msg,
		ObservedGeneration: gen,
		LastTransitionTime: metav1.NewTime(time.Now()),
	}
}

func falseCondition(t, reason, msg string, gen int64) metav1.Condition {
	return metav1.Condition{
		Type:               t,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            msg,
		ObservedGeneration: gen,
		LastTransitionTime: metav1.NewTime(time.Now()),
	}
}

// computeConditions returns the COMPLETE set of conditions.
// Callers replace sandbox.Status.Conditions entirely.
func computeConditions(s *reconcileState, gen int64) []metav1.Condition {
	conditions := make([]metav1.Condition, 0, 6)

	var isReconciling, isStalled bool
	var reconcilingReason, reconcilingMsg string
	var stalledReason, stalledMsg string

	// 1. TemplateReady
	if s.TemplateResolved {
		conditions = append(conditions, trueCondition(
			SandboxTemplateReadyCondition, CondReasonTemplateResolved, "Template resolved", gen))
	} else if s.TemplateError != "" {
		conditions = append(conditions, falseCondition(
			SandboxTemplateReadyCondition, CondReasonTemplateNotFound, s.TemplateError, gen))
		isStalled, stalledReason, stalledMsg = true, CondReasonTemplateNotFound, s.TemplateError
	}
	// else: template not yet checked, no condition

	// 2. NetworkConfigured
	if s.NetworkApplied {
		conditions = append(conditions, trueCondition(
			SandboxNetworkReadyCondition, CondReasonNetworkPolicyApplied, "Network configured", gen))
	} else if s.NetworkError != "" {
		conditions = append(conditions, falseCondition(
			SandboxNetworkReadyCondition, CondReasonNetworkPolicyFailed, s.NetworkError, gen))
		isStalled, stalledReason, stalledMsg = true, CondReasonNetworkPolicyFailed, s.NetworkError
	}

	// 3. PodReady
	switch {
	case s.FatalError != "":
		conditions = append(conditions, falseCondition(
			SandboxPodReadyCondition, s.FatalReason, s.FatalError, gen))
		isStalled, stalledReason, stalledMsg = true, s.FatalReason, s.FatalError
	case s.PodReady:
		conditions = append(conditions, trueCondition(
			SandboxPodReadyCondition, CondReasonPodRunning, "Pod running", gen))
	case s.PodPhase == corev1.PodFailed:
		conditions = append(conditions, falseCondition(
			SandboxPodReadyCondition, CondReasonPodFailed, "Pod failed", gen))
		isStalled, stalledReason, stalledMsg = true, CondReasonPodFailed, "Pod failed"
	case s.PodPhase == corev1.PodSucceeded:
		conditions = append(conditions, falseCondition(
			SandboxPodReadyCondition, CondReasonPodSucceeded, "Pod completed", gen))
		isStalled, stalledReason, stalledMsg = true, CondReasonPodSucceeded, "Pod terminated unexpectedly"
	case s.PodExists:
		conditions = append(conditions, falseCondition(
			SandboxPodReadyCondition, CondReasonPodPending, "Pod not ready", gen))
		isReconciling, reconcilingReason, reconcilingMsg = true, CondReasonReconciling, "Waiting for pod"
	case s.TemplateResolved && s.NetworkError == "":
		// Pod doesn't exist but should be created
		isReconciling, reconcilingReason, reconcilingMsg = true, CondReasonPodCreating, "Creating pod"
	}

	// 4. Shutdown state overrides
	if s.IsSnapshotting {
		isReconciling, isStalled = true, false
		reconcilingReason, reconcilingMsg = CondReasonSnapshottingInProgress, "Snapshot in progress"
	}

	// 5. Ready (aggregate)
	conditions = append(conditions, computeReadyCondition(s, gen))

	// 6. kstatus abnormal-true conditions
	if isStalled {
		conditions = append(conditions, trueCondition(
			SandboxStalledCondition, stalledReason, stalledMsg, gen))
	} else if isReconciling {
		conditions = append(conditions, trueCondition(
			SandboxReconcilingCondition, reconcilingReason, reconcilingMsg, gen))
	}
	// When healthy: neither condition present (removed by full replacement)

	return conditions
}

func computeReadyCondition(s *reconcileState, gen int64) metav1.Condition {
	switch {
	case s.IsDeleting:
		return falseCondition(SandboxReadyCondition, CondReasonDeleting, "Sandbox is being deleted", gen)
	case s.PodReady:
		return trueCondition(SandboxReadyCondition, CondReasonPodRunning, "Sandbox is ready", gen)
	case s.PodPhase == corev1.PodFailed:
		return falseCondition(SandboxReadyCondition, CondReasonPodFailed, "Pod failed", gen)
	case s.PodPhase == corev1.PodSucceeded:
		return falseCondition(SandboxReadyCondition, CondReasonPodSucceeded, "Pod terminated unexpectedly", gen)
	case s.FatalError != "":
		return falseCondition(SandboxReadyCondition, s.FatalReason, s.FatalError, gen)
	case s.TemplateError != "":
		return falseCondition(SandboxReadyCondition, CondReasonTemplateNotFound, s.TemplateError, gen)
	case s.NetworkError != "":
		return falseCondition(SandboxReadyCondition, CondReasonNetworkPolicyFailed, s.NetworkError, gen)
	default:
		return falseCondition(SandboxReadyCondition, CondReasonReconciling, "Waiting for sandbox to be ready", gen)
	}
}
