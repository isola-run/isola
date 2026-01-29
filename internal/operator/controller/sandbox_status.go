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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	sandboxv1alpha1 "github.com/isola-ai/isola-sb/api/v1alpha1"
	"github.com/isola-ai/isola-sb/internal/operator/controller/podutil"
)

func (r *SandboxReconciler) patchStatus(ctx context.Context, baseSandbox *sandboxv1alpha1.Sandbox, newSandbox *sandboxv1alpha1.Sandbox, newConditions []metav1.Condition) error {
	if newSandbox.Status.Conditions == nil {
		newSandbox.Status.Conditions = []metav1.Condition{}
	}

	for _, cond := range newConditions {
		meta.SetStatusCondition(&newSandbox.Status.Conditions, cond)
	}

	if err := r.Status().Patch(ctx, newSandbox, client.MergeFrom(baseSandbox)); err != nil {
		return err
	}

	return nil
}

func (r *SandboxReconciler) reconcileSandboxStatus(
	ctx context.Context,
	sandbox *sandboxv1alpha1.Sandbox,
	baseSandbox *sandboxv1alpha1.Sandbox,
	sandboxPod *corev1.Pod,
) error {
	var conditions []metav1.Condition

	podCondition := r.determinePodCondition(sandbox, sandboxPod)
	conditions = append(conditions, podCondition)

	if sandboxPod != nil {
		sandbox.Status.PodIP = sandboxPod.Status.PodIP
	}

	networkCondition := r.determineNetworkCondition(sandbox)
	conditions = append(conditions, networkCondition)

	// todo benl: currently, only shutdown snapshot condition is reflected
	shutdownSnapshot, err := r.getShutdownSnapshot(ctx, sandbox)
	if err != nil {
		return err
	}
	snapshotCondition := r.determineSnapshotCondition(sandbox, shutdownSnapshot)
	conditions = append(conditions, snapshotCondition)

	readyCondition := r.determineReadyCondition(sandbox, sandboxPod)
	conditions = append(conditions, readyCondition)

	return r.patchStatus(ctx, baseSandbox, sandbox, conditions)
}

// determinePodCondition returns the PodReady condition based on the sandbox pod state.
func (r *SandboxReconciler) determinePodCondition(sandbox *sandboxv1alpha1.Sandbox, sandboxPod *corev1.Pod) metav1.Condition {
	if podutil.IsPodReady(sandboxPod) {
		return metav1.Condition{
			Type:               SandboxPodReadyCondition,
			Status:             metav1.ConditionTrue,
			Reason:             CondReasonPodRunning,
			Message:            "Pod is running",
			ObservedGeneration: sandbox.Generation,
		}
	}

	if podutil.IsPodTerminated(sandboxPod) {
		reason := CondReasonPodFailed
		message := "Pod failed"
		if sandboxPod.Status.Phase == corev1.PodSucceeded {
			reason = CondReasonPodSucceeded
			message = "Pod completed"
		}
		return metav1.Condition{
			Type:               SandboxPodReadyCondition,
			Status:             metav1.ConditionFalse,
			Reason:             reason,
			Message:            message,
			ObservedGeneration: sandbox.Generation,
		}
	}

	return metav1.Condition{
		Type:               SandboxPodReadyCondition,
		Status:             metav1.ConditionFalse,
		Reason:             CondReasonPodPending,
		Message:            "Pod is not ready yet",
		ObservedGeneration: sandbox.Generation,
	}
}

func (r *SandboxReconciler) determineSnapshotCondition(sandbox *sandboxv1alpha1.Sandbox, snap *sandboxv1alpha1.RootfsSnapshot) metav1.Condition {
	if snap == nil {
		return metav1.Condition{
			Type:               SandboxRootfsSnapshotCondition,
			Status:             metav1.ConditionFalse,
			Reason:             "NoSnapshot",
			Message:            "No shutdown snapshot exists",
			ObservedGeneration: sandbox.Generation,
		}
	}

	if snap.Status.CompletedAt == nil {
		return metav1.Condition{
			Type:               SandboxRootfsSnapshotCondition,
			Status:             metav1.ConditionFalse,
			Reason:             CondReasonSnapshottingInProgress,
			Message:            fmt.Sprintf("Snapshot %q is in progress", snap.Name),
			ObservedGeneration: sandbox.Generation,
		}
	}

	readyCond := meta.FindStatusCondition(snap.Status.Conditions, string(sandboxv1alpha1.RootfsSnapshotComplete))
	if readyCond == nil {
		return metav1.Condition{
			Type:               SandboxRootfsSnapshotCondition,
			Status:             metav1.ConditionFalse,
			Reason:             CondReasonSnapshottingInProgress,
			Message:            fmt.Sprintf("Snapshot %q status unknown", snap.Name),
			ObservedGeneration: sandbox.Generation,
		}
	}

	if readyCond.Status == metav1.ConditionTrue {
		message := fmt.Sprintf("Snapshot %q completed", snap.Name)
		if snap.Status.Revision > 0 {
			message = fmt.Sprintf("Snapshot %q completed (revision %d)", snap.Name, snap.Status.Revision)
		}
		return metav1.Condition{
			Type:               SandboxRootfsSnapshotCondition,
			Status:             metav1.ConditionTrue,
			Reason:             CondReasonSnapshotComplete,
			Message:            message,
			ObservedGeneration: sandbox.Generation,
		}
	}

	return metav1.Condition{
		Type:               SandboxRootfsSnapshotCondition,
		Status:             metav1.ConditionFalse,
		Reason:             CondReasonSnapshotFailed,
		Message:            fmt.Sprintf("Snapshot %q failed: %s", snap.Name, readyCond.Message),
		ObservedGeneration: sandbox.Generation,
	}
}

func (r *SandboxReconciler) determineNetworkCondition(sandbox *sandboxv1alpha1.Sandbox) metav1.Condition {
	// Network configuration is now static (Helm-installed policies + optional custom policy).
	// The network is considered ready once the pod is created (policies apply immediately).
	return metav1.Condition{
		Type:               SandboxNetworkReadyCondition,
		Status:             metav1.ConditionTrue,
		Reason:             CondReasonNetworkPolicyApplied,
		Message:            "Network configuration applied",
		ObservedGeneration: sandbox.Generation,
	}
}

// determineReadyCondition returns the aggregate Ready condition.
// Sandbox is ready when pod is ready.
func (r *SandboxReconciler) determineReadyCondition(sandbox *sandboxv1alpha1.Sandbox, sandboxPod *corev1.Pod) metav1.Condition {
	if podutil.IsPodTerminated(sandboxPod) {
		reason := CondReasonPodFailed
		message := "Pod failed"
		if sandboxPod.Status.Phase == corev1.PodSucceeded {
			reason = CondReasonPodSucceeded
			message = "Pod completed"
		}
		return metav1.Condition{
			Type:               SandboxReadyCondition,
			Status:             metav1.ConditionFalse,
			Reason:             reason,
			Message:            message,
			ObservedGeneration: sandbox.Generation,
		}
	}

	if !podutil.IsPodReady(sandboxPod) {
		return metav1.Condition{
			Type:               SandboxReadyCondition,
			Status:             metav1.ConditionFalse,
			Reason:             CondReasonPodPending,
			Message:            "Pod is not ready yet",
			ObservedGeneration: sandbox.Generation,
		}
	}

	return metav1.Condition{
		Type:               SandboxReadyCondition,
		Status:             metav1.ConditionTrue,
		Reason:             CondReasonPodRunning,
		Message:            "Pod is running",
		ObservedGeneration: sandbox.Generation,
	}
}
