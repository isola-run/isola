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

// sandboxState tracks the state observed and actions taken during reconciliation.
// This is passed to patchStatusDefer to compute final conditions.
type sandboxState struct {
	// Observed state
	Template *sandboxv1alpha1.SandboxTemplate
	Pod      *corev1.Pod

	// Error states (mutually exclusive with observed state being set)
	TemplateNotFound bool

	// Actions/mutations that occurred
	NetworkApplied bool
	NetworkError   string // Non-empty if network policy failed

	// Lifecycle flags
	IsDeleting     bool
	IsSnapshotting bool

	// Fatal errors during reconciliation
	FatalError  string
	FatalReason string
}

// condition helpers
func newCondition(t string, status metav1.ConditionStatus, reason, msg string, gen int64) metav1.Condition {
	return metav1.Condition{
		Type:               t,
		Status:             status,
		Reason:             reason,
		Message:            msg,
		ObservedGeneration: gen,
		LastTransitionTime: metav1.NewTime(time.Now()),
	}
}

// computeConditions returns the complete set of conditions based on the current state.
// Implements kstatus abnormal-true pattern: Reconciling/Stalled are ABSENT when healthy.
func computeConditions(s *sandboxState, sandbox *sandboxv1alpha1.Sandbox) []metav1.Condition {
	gen := sandbox.Generation
	var conditions []metav1.Condition

	// Determine if we're in a reconciling or stalled state
	var isReconciling, isStalled bool
	var reconcilingReason, reconcilingMsg string
	var stalledReason, stalledMsg string

	// 1. TemplateReady condition
	if s.TemplateNotFound {
		conditions = append(conditions, newCondition(
			SandboxTemplateReadyCondition, metav1.ConditionFalse,
			CondReasonTemplateNotFound, "SandboxTemplate not found", gen))
		isStalled, stalledReason, stalledMsg = true, CondReasonTemplateNotFound, "SandboxTemplate not found"
	} else if s.Template != nil {
		conditions = append(conditions, newCondition(
			SandboxTemplateReadyCondition, metav1.ConditionTrue,
			CondReasonTemplateResolved, "Template resolved", gen))
	}

	// 2. NetworkConfigured condition
	if s.NetworkError != "" {
		conditions = append(conditions, newCondition(
			SandboxNetworkReadyCondition, metav1.ConditionFalse,
			CondReasonNetworkPolicyFailed, s.NetworkError, gen))
		isStalled, stalledReason, stalledMsg = true, CondReasonNetworkPolicyFailed, s.NetworkError
	} else if s.NetworkApplied {
		conditions = append(conditions, newCondition(
			SandboxNetworkReadyCondition, metav1.ConditionTrue,
			CondReasonNetworkPolicyApplied, "Network configured", gen))
	}

	// 3. PodReady condition
	podCond := computePodCondition(s, sandbox)
	if podCond != nil {
		conditions = append(conditions, *podCond)
		// Determine reconciling/stalled from pod state
		if podCond.Status == metav1.ConditionFalse {
			switch podCond.Reason {
			case CondReasonPodFailed, CondReasonPodSucceeded, CondReasonPodCreationFailed,
				CondReasonSidecarInjectionFail, CondReasonPodDeleted:
				isStalled, stalledReason, stalledMsg = true, podCond.Reason, podCond.Message
			case CondReasonPodPending, CondReasonPodCreating:
				isReconciling, reconcilingReason, reconcilingMsg = true, CondReasonReconciling, "Waiting for pod"
			}
		}
	} else if s.Template != nil && s.NetworkError == "" && !s.IsDeleting {
		// Pod should be created but doesn't exist yet
		isReconciling, reconcilingReason, reconcilingMsg = true, CondReasonPodCreating, "Creating pod"
	}

	// 4. Snapshotting overrides
	if s.IsSnapshotting {
		isReconciling, isStalled = true, false
		reconcilingReason, reconcilingMsg = CondReasonSnapshottingInProgress, "Snapshot in progress"
	}

	// 5. Ready condition (aggregate)
	conditions = append(conditions, computeReadyCondition(s, sandbox, gen))

	// 6. kstatus abnormal-true conditions (only add when True, absent otherwise)
	if isStalled {
		conditions = append(conditions, newCondition(
			SandboxStalledCondition, metav1.ConditionTrue, stalledReason, stalledMsg, gen))
	} else if isReconciling {
		conditions = append(conditions, newCondition(
			SandboxReconcilingCondition, metav1.ConditionTrue, reconcilingReason, reconcilingMsg, gen))
	}

	return conditions
}

func computePodCondition(s *sandboxState, sandbox *sandboxv1alpha1.Sandbox) *metav1.Condition {
	gen := sandbox.Generation

	// Fatal error during creation
	if s.FatalError != "" {
		c := newCondition(SandboxPodReadyCondition, metav1.ConditionFalse, s.FatalReason, s.FatalError, gen)
		return &c
	}

	// Pod exists - compute from actual state
	if s.Pod != nil {
		if podutil.IsPodReady(s.Pod) {
			c := newCondition(SandboxPodReadyCondition, metav1.ConditionTrue, CondReasonPodRunning, "Pod running", gen)
			return &c
		}
		switch s.Pod.Status.Phase {
		case corev1.PodFailed:
			c := newCondition(SandboxPodReadyCondition, metav1.ConditionFalse, CondReasonPodFailed, "Pod failed", gen)
			return &c
		case corev1.PodSucceeded:
			c := newCondition(SandboxPodReadyCondition, metav1.ConditionFalse, CondReasonPodSucceeded, "Pod terminated", gen)
			return &c
		default:
			c := newCondition(SandboxPodReadyCondition, metav1.ConditionFalse, CondReasonPodPending, "Pod not ready", gen)
			return &c
		}
	}

	// Pod not found - was it ever created? (check PodIP as marker)
	if sandbox.Status.PodIP != "" {
		c := newCondition(SandboxPodReadyCondition, metav1.ConditionFalse,
			CondReasonPodDeleted, "Pod was deleted while Sandbox still exists", gen)
		return &c
	}

	// Pod never created yet - no PodReady condition (Reconciling will cover)
	return nil
}

func computeReadyCondition(s *sandboxState, sandbox *sandboxv1alpha1.Sandbox, gen int64) metav1.Condition {
	switch {
	case s.IsDeleting:
		return newCondition(SandboxReadyCondition, metav1.ConditionFalse, CondReasonDeleting, "Sandbox is being deleted", gen)
	case s.Pod != nil && podutil.IsPodReady(s.Pod):
		return newCondition(SandboxReadyCondition, metav1.ConditionTrue, CondReasonPodRunning, "Sandbox is ready", gen)
	case s.Pod != nil && s.Pod.Status.Phase == corev1.PodFailed:
		return newCondition(SandboxReadyCondition, metav1.ConditionFalse, CondReasonPodFailed, "Pod failed", gen)
	case s.Pod != nil && s.Pod.Status.Phase == corev1.PodSucceeded:
		return newCondition(SandboxReadyCondition, metav1.ConditionFalse, CondReasonPodSucceeded, "Pod terminated unexpectedly", gen)
	case s.FatalError != "":
		return newCondition(SandboxReadyCondition, metav1.ConditionFalse, s.FatalReason, s.FatalError, gen)
	case s.TemplateNotFound:
		return newCondition(SandboxReadyCondition, metav1.ConditionFalse, CondReasonTemplateNotFound, "SandboxTemplate not found", gen)
	case s.NetworkError != "":
		return newCondition(SandboxReadyCondition, metav1.ConditionFalse, CondReasonNetworkPolicyFailed, s.NetworkError, gen)
	case s.Pod == nil && sandbox.Status.PodIP != "":
		// Pod was deleted unexpectedly (had IP, now gone)
		return newCondition(SandboxReadyCondition, metav1.ConditionFalse, CondReasonPodDeleted, "Pod was deleted while Sandbox still exists", gen)
	default:
		return newCondition(SandboxReadyCondition, metav1.ConditionFalse, CondReasonReconciling, "Waiting for sandbox to be ready", gen)
	}
}

// patchStatusDefer patches status at reconciliation end.
// Sets ObservedGeneration only when reconcileErr is nil.
func (r *SandboxReconciler) patchStatusDefer(
	ctx context.Context,
	log logr.Logger,
	baseSandbox, sandbox *sandboxv1alpha1.Sandbox,
	state *sandboxState,
	reconcileErr error,
) error {
	// Compute all conditions from current state
	sandbox.Status.Conditions = computeConditions(state, sandbox)

	// Only set ObservedGeneration on successful reconciliation
	if reconcileErr == nil {
		sandbox.Status.ObservedGeneration = sandbox.Generation
	}

	// Skip if nothing changed
	if equality.Semantic.DeepEqual(baseSandbox.Status, sandbox.Status) {
		return nil
	}

	if err := r.Status().Patch(ctx, sandbox, client.MergeFrom(baseSandbox)); err != nil {
		// Ignore NotFound - sandbox may have been deleted during reconcile
		if apierrors.IsNotFound(err) {
			return nil
		}
		log.Error(err, "Failed to patch status")
		return err
	}
	return nil
}
