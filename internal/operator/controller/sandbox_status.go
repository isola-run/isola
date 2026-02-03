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
	"context"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	sandboxv1alpha1 "github.com/isola-ai/isola-sb/api/v1alpha1"
	"github.com/isola-ai/isola-sb/internal/operator/controller/podutil"
)

// conditionPtr returns a pointer to a condition.
func conditionPtr(c metav1.Condition) *metav1.Condition { return &c }

type podState struct {
	Pod      *corev1.Pod // nil if not found or error
	NotFound bool        // true = 404, false = other error or success
	GetError error       // non-404 transient error
}

type sandboxTemplateState struct {
	SandboxTemplate *sandboxv1alpha1.SandboxTemplate // nil if not found or error
	NotFound        bool                             // true = 404, false = other error or success
	GetError        error                            // non-404 transient error
}

type reconcileState struct {
	// Template observation
	SandboxTemplateState sandboxTemplateState

	// Pod observation
	PodState podState

	// Network state (set during mutation, not observation)
	NetworkApplied bool
	NetworkError   string // Non-empty if network policy build/apply failed

	// Fatal errors during pod creation/mutation
	FatalError  string
	FatalReason string // Condition reason for the error

	// Lifecycle state
	IsDeleting     bool // DeletionTimestamp set
	IsSnapshotting bool // Shutdown snapshot in progress
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
// Follows Cluster-API pattern: preserves existing conditions on transient errors.
func computeConditions(s *reconcileState, sandbox *sandboxv1alpha1.Sandbox) []metav1.Condition {
	gen := sandbox.Generation
	existing := sandbox.Status.Conditions
	conditions := make([]metav1.Condition, 0, 6)

	var isReconciling, isStalled bool
	var reconcilingReason, reconcilingMsg string
	var stalledReason, stalledMsg string

	// 1. TemplateReady
	if c := computeTemplateCondition(s, gen, existing); c != nil {
		conditions = append(conditions, *c)
		if c.Status == metav1.ConditionFalse && c.Reason == CondReasonTemplateNotFound {
			isStalled, stalledReason, stalledMsg = true, CondReasonTemplateNotFound, c.Message
		}
	}

	// 2. NetworkConfigured
	if c := computeNetworkCondition(s, gen, existing); c != nil {
		conditions = append(conditions, *c)
		if c.Status == metav1.ConditionFalse && c.Reason == CondReasonNetworkPolicyFailed {
			isStalled, stalledReason, stalledMsg = true, CondReasonNetworkPolicyFailed, c.Message
		}
	}

	// 3. PodReady
	if c := computePodCondition(s, sandbox, existing); c != nil {
		conditions = append(conditions, *c)
		if c.Status == metav1.ConditionTrue {
			// Pod is ready - no reconciling/stalled
		} else {
			switch c.Reason {
			case CondReasonPodFailed, CondReasonPodSucceeded, CondReasonPodCreationFailed,
				CondReasonSidecarInjectionFail, CondReasonPodDeleted:
				isStalled, stalledReason, stalledMsg = true, c.Reason, c.Message
			case CondReasonPodPending:
				isReconciling, reconcilingReason, reconcilingMsg = true, CondReasonReconciling, "Waiting for pod"
			}
		}
	} else if s.SandboxTemplateState.SandboxTemplate != nil && s.NetworkError == "" {
		// Pod doesn't exist but should be created
		isReconciling, reconcilingReason, reconcilingMsg = true, CondReasonPodCreating, "Creating pod"
	}

	// 4. Shutdown state overrides
	if s.IsSnapshotting {
		isReconciling, isStalled = true, false
		reconcilingReason, reconcilingMsg = CondReasonSnapshottingInProgress, "Snapshot in progress"
	}

	// 5. Ready (aggregate)
	conditions = append(conditions, computeReadyCondition(s, sandbox, gen))

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

// computeTemplateCondition computes the TemplateReady condition.
// Preserves existing condition on transient errors.
func computeTemplateCondition(s *reconcileState, gen int64, existing []metav1.Condition) *metav1.Condition {
	// 1. Transient error - preserve existing, skip if none
	if s.SandboxTemplateState.GetError != nil {
		if c := findCondition(existing, SandboxTemplateReadyCondition); c != nil {
			return c
		}
		return nil // No existing condition - skip, let retry handle it
	}

	// 2. Template not found (404) - permanent error
	if s.SandboxTemplateState.NotFound {
		return conditionPtr(falseCondition(SandboxTemplateReadyCondition, CondReasonTemplateNotFound,
			"SandboxTemplate not found", gen))
	}

	// 3. Template found
	if s.SandboxTemplateState.SandboxTemplate != nil {
		return conditionPtr(trueCondition(SandboxTemplateReadyCondition, CondReasonTemplateResolved,
			"Template resolved", gen))
	}

	return nil
}

// computeNetworkCondition computes the NetworkConfigured condition.
// Network uses the existing condition as "was configured" marker
// (no separate status field needed - config is immutable).
func computeNetworkCondition(s *reconcileState, gen int64, existing []metav1.Condition) *metav1.Condition {
	if s.NetworkError != "" {
		return conditionPtr(falseCondition(SandboxNetworkReadyCondition, CondReasonNetworkPolicyFailed,
			s.NetworkError, gen))
	}

	if s.NetworkApplied {
		return conditionPtr(trueCondition(SandboxNetworkReadyCondition, CondReasonNetworkPolicyApplied,
			"Network configured", gen))
	}

	// Preserve existing condition on transient errors (e.g., pod get failure before network check)
	if c := findCondition(existing, SandboxNetworkReadyCondition); c != nil {
		return c
	}

	return nil
}

// computePodCondition computes the PodReady condition.
// Preserves existing condition on transient errors.
// Detects pod deletion by checking if PodIP was ever set.
func computePodCondition(s *reconcileState, sandbox *sandboxv1alpha1.Sandbox, existing []metav1.Condition) *metav1.Condition {
	gen := sandbox.Generation

	// 1. Transient error - PRESERVE existing condition, skip if none (retry will sort it out)
	if s.PodState.GetError != nil {
		if c := findCondition(existing, SandboxPodReadyCondition); c != nil {
			return c // Preserve existing
		}
		return nil // No existing condition - skip, let retry handle it
	}

	// 2. Fatal error during creation
	if s.FatalError != "" {
		return conditionPtr(falseCondition(SandboxPodReadyCondition, s.FatalReason, s.FatalError, gen))
	}

	// 3. Pod exists - compute from actual state
	if s.PodState.Pod != nil {
		if podutil.IsPodReady(s.PodState.Pod) {
			return conditionPtr(trueCondition(SandboxPodReadyCondition, CondReasonPodRunning, "Pod running", gen))
		}
		switch s.PodState.Pod.Status.Phase {
		case corev1.PodFailed:
			return conditionPtr(falseCondition(SandboxPodReadyCondition, CondReasonPodFailed, "Pod failed", gen))
		case corev1.PodSucceeded:
			return conditionPtr(falseCondition(SandboxPodReadyCondition, CondReasonPodSucceeded, "Pod terminated", gen))
		default:
			return conditionPtr(falseCondition(SandboxPodReadyCondition, CondReasonPodPending, "Pod not ready", gen))
		}
	}

	// 4. Pod not found - was it EVER created? (use PodIP as marker)
	if s.PodState.NotFound && sandbox.Status.PodIP != "" {
		// Pod GetError existed before (had IP) but now gone - ERROR
		return conditionPtr(falseCondition(SandboxPodReadyCondition, CondReasonPodDeleted,
			"Pod was deleted while Sandbox still exists", gen))
	}

	// 5. Pod never created yet - no condition (Reconciling will cover)
	return nil
}

func computeReadyCondition(s *reconcileState, sandbox *sandboxv1alpha1.Sandbox, gen int64) metav1.Condition {
	switch {
	case s.IsDeleting:
		return falseCondition(SandboxReadyCondition, CondReasonDeleting, "Sandbox is being deleted", gen)
	case s.PodState.Pod != nil && podutil.IsPodReady(s.PodState.Pod):
		return trueCondition(SandboxReadyCondition, CondReasonPodRunning, "Sandbox is ready", gen)
	case s.PodState.Pod != nil && s.PodState.Pod.Status.Phase == corev1.PodFailed:
		return falseCondition(SandboxReadyCondition, CondReasonPodFailed, "Pod failed", gen)
	case s.PodState.Pod != nil && s.PodState.Pod.Status.Phase == corev1.PodSucceeded:
		return falseCondition(SandboxReadyCondition, CondReasonPodSucceeded, "Pod terminated unexpectedly", gen)
	case s.FatalError != "":
		return falseCondition(SandboxReadyCondition, s.FatalReason, s.FatalError, gen)
	case s.SandboxTemplateState.NotFound:
		return falseCondition(SandboxReadyCondition, CondReasonTemplateNotFound, "SandboxTemplate not found", gen)
	case s.NetworkError != "":
		return falseCondition(SandboxReadyCondition, CondReasonNetworkPolicyFailed, s.NetworkError, gen)
	case s.PodState.NotFound && sandbox.Status.PodIP != "":
		return falseCondition(SandboxReadyCondition, CondReasonPodDeleted, "Pod was deleted while Sandbox still exists", gen)
	default:
		return falseCondition(SandboxReadyCondition, CondReasonReconciling, "Waiting for sandbox to be ready", gen)
	}
}

// patchStatusDefer patches status at reconciliation end.
// Sets ObservedGeneration only when reconcileErr is nil.
func (r *SandboxReconciler) patchStatusDefer(
	ctx context.Context,
	log logr.Logger,
	baseSandbox, sandbox *sandboxv1alpha1.Sandbox,
	state *reconcileState,
	reconcileErr error,
) error {
	// Compute conditions (pass sandbox so it can access existing conditions and PodIP)
	sandbox.Status.Conditions = computeConditions(state, sandbox)

	// Only set ObservedGeneration on successful reconciliation
	if reconcileErr == nil {
		sandbox.Status.ObservedGeneration = sandbox.Generation
	}

	// Skip if nothing changed (avoid unnecessary API call)
	if equality.Semantic.DeepEqual(baseSandbox.Status, sandbox.Status) {
		return nil
	}

	if err := r.Status().Patch(ctx, sandbox, client.MergeFrom(baseSandbox)); err != nil {
		// Ignore NotFound errors - sandbox may have been deleted during reconcile (e.g., timeout)
		if apierrors.IsNotFound(err) {
			return nil
		}
		log.Error(err, "Failed to patch status")
		return err
	}
	return nil
}
