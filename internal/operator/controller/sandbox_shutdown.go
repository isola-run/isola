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
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	sandboxv1alpha1 "github.com/isola-ai/isola-sb/api/v1alpha1"
	"github.com/isola-ai/isola-sb/internal/operator/controller/podutil"
	"github.com/isola-ai/isola-sb/internal/operator/controller/snapshot"
)

func getShutdownSnapshotName(sandbox *sandboxv1alpha1.Sandbox) string {
	return sandbox.Name + "-shutdown"
}

func (r *SandboxReconciler) getShutdownSnapshot(ctx context.Context, sandbox *sandboxv1alpha1.Sandbox) (*sandboxv1alpha1.RootfsSnapshot, error) {
	snap := &sandboxv1alpha1.RootfsSnapshot{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      getShutdownSnapshotName(sandbox),
		Namespace: sandbox.Namespace,
	}, snap)
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return snap, nil
}

func (r *SandboxReconciler) calculateTimeout(ctx context.Context, sandbox *sandboxv1alpha1.Sandbox, template *sandboxv1alpha1.SandboxTemplate, sandboxPod *corev1.Pod) (optionalTimeoutAt *metav1.Time) {
	log := logf.FromContext(ctx).WithValues("sandbox", sandbox.Name, "namespace", sandbox.Namespace)
	// todo benl: update sandbox condition(s) here?
	if template == nil || template.Spec.TimeoutSeconds == nil {
		return nil
	}

	var startTime time.Time
	if sandboxPod != nil && sandboxPod.Status.StartTime != nil {
		// sandboxPod.Status.StartTime set once when the pod is first scheduled onto a node (survives pod restarts)
		// it is probably closer to user intent, so if exists we use that time
		log.Info("deduced start time from pod", "startTime", sandboxPod.Status.StartTime.Time)
		startTime = sandboxPod.Status.StartTime.Time
	} else {
		log.Info("deduced start time from sandbox", "startTime", sandbox.CreationTimestamp.Time)
		startTime = sandbox.CreationTimestamp.Time
	}

	timeoutAt := startTime.Add(time.Duration(*template.Spec.TimeoutSeconds) * time.Second)

	log.Info("calculated sandbox timeout", "timeoutAt", timeoutAt)
	return &metav1.Time{Time: timeoutAt}
}

func (r *SandboxReconciler) ensureTimeout(ctx context.Context, sandbox *sandboxv1alpha1.Sandbox, baseSandbox *sandboxv1alpha1.Sandbox, template *sandboxv1alpha1.SandboxTemplate, sandboxPod *corev1.Pod) (optionalTimeoutAt *metav1.Time, err error) {
	log := logf.FromContext(ctx).WithValues("sandbox", sandbox.Name, "namespace", sandbox.Namespace)
	optionalTimeoutAt = r.calculateTimeout(ctx, sandbox, template, sandboxPod)
	if optionalTimeoutAt == nil {
		// no timeout is configured
		return nil, nil
	}

	// once the sandboxPod is created, timeout might change compared to the one calculated based on sandbox creation time
	if sandbox.Status.TimeoutAt == nil || sandbox.Status.TimeoutAt.Time.Before(optionalTimeoutAt.Time) {
		sandbox.Status.TimeoutAt = optionalTimeoutAt

		if err := r.Status().Patch(ctx, sandbox, client.MergeFrom(baseSandbox)); err != nil {
			log.Error(err, "Failed to patch sandbox TimeoutAt")
			return optionalTimeoutAt, err
		}
		log.Info("persisted timeoutAt", "timeoutAt", optionalTimeoutAt)
	}

	return optionalTimeoutAt, nil
}

// finalizeSandbox executes the shutdown policy and prepares for deletion.
// Returns:
// - ctrl.Result: may request a requeue if another reconciliation is needed.
// - bool: whether cleanup completed (sandbox can be deleted).
// - error: if something went wrong.
func (r *SandboxReconciler) finalizeSandbox(
	ctx context.Context,
	sandbox *sandboxv1alpha1.Sandbox,
	baseSandbox *sandboxv1alpha1.Sandbox,
	template *sandboxv1alpha1.SandboxTemplate,
) (ctrl.Result, bool, error) {
	log := logf.FromContext(ctx).WithValues("sandbox", sandbox.Name, "namespace", sandbox.Namespace)

	log.Info("Executing shutdown policy for deletion")

	sandboxPod, err := r.getSandboxPod(ctx, sandbox)
	if err != nil {
		return ctrl.Result{}, false, err
	}

	snapshotDeadline := r.calculateSnapshotDeadline(template)

	result, cleanupDone, err := r.executeShutdownPolicy(
		ctx, sandbox, baseSandbox, template, sandboxPod, snapshotDeadline, CleanupTriggerDeletion,
	)
	if err != nil {
		return result, false, err
	}
	if !cleanupDone {
		return result, cleanupDone, nil
	}

	controllerutil.RemoveFinalizer(sandbox, SandboxFinalizer)
	if err := r.Update(ctx, sandbox); err != nil {
		log.Error(err, "Failed to remove finalizer")
		return ctrl.Result{}, false, err
	}

	return ctrl.Result{}, true, nil
}

// executeShutdownPolicy executes the shutdown policy for a sandbox being cleaned up.
// trigger indicates whether this is due to timeout or user-initiated deletion.
// snapshotDeadline is the deadline by which snapshotting must complete.
func (r *SandboxReconciler) executeShutdownPolicy(
	ctx context.Context,
	sandbox *sandboxv1alpha1.Sandbox,
	baseSandbox *sandboxv1alpha1.Sandbox,
	template *sandboxv1alpha1.SandboxTemplate,
	sandboxPod *corev1.Pod,
	snapshotDeadline time.Time,
	trigger CleanupTrigger,
) (ctrl.Result, bool, error) {
	log := logf.FromContext(ctx).WithValues("sandbox", sandbox.Name, "namespace", sandbox.Namespace, "trigger", trigger)

	// Determine reason and message based on trigger
	var reason, message string
	if trigger == CleanupTriggerTimeout {
		reason = CondReasonSandboxTimedOut
		message = "Sandbox timed out"
	} else {
		reason = CondReasonDeleting
		message = "Sandbox being deleted"
	}

	if template.Spec.ShutdownPolicy == nil || template.Spec.ShutdownPolicy.Policy == sandboxv1alpha1.ShutdownPolicyDelete {
		if err := r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
			{
				Type:               SandboxReadyCondition,
				Status:             metav1.ConditionFalse,
				Reason:             reason,
				Message:            message + "; deleting",
				ObservedGeneration: sandbox.Generation,
			},
		}); err != nil {
			return ctrl.Result{}, false, err
		}
		return ctrl.Result{}, true, nil
	}

	if err := r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
		{
			Type:               SandboxReadyCondition,
			Status:             metav1.ConditionFalse,
			Reason:             CondReasonSnapshottingInProgress,
			Message:            message + "; executing shutdown policy",
			ObservedGeneration: sandbox.Generation,
		},
	}); err != nil {
		return ctrl.Result{}, false, err
	}

	switch template.Spec.ShutdownPolicy.Policy {
	case sandboxv1alpha1.ShutdownPolicySnapshotRootfs:
		return r.handleRootfsSnapshot(ctx, sandbox, baseSandbox, sandboxPod, snapshotDeadline, r.getActiveDeadlineSeconds(template))
	default:
		log.Info("Unknown shutdown policy; proceeding with deletion", "policy", template.Spec.ShutdownPolicy.Policy)
		return ctrl.Result{}, true, nil
	}
}

func (r *SandboxReconciler) getActiveDeadlineSeconds(template *sandboxv1alpha1.SandboxTemplate) int64 {
	if template != nil && template.Spec.ShutdownPolicy != nil && template.Spec.ShutdownPolicy.ActiveDeadlineSeconds != nil {
		return *template.Spec.ShutdownPolicy.ActiveDeadlineSeconds
	}
	return defaultActiveDeadlineSeconds
}

func (r *SandboxReconciler) calculateSnapshotDeadline(template *sandboxv1alpha1.SandboxTemplate) time.Time {
	return r.clock().Now().Add(time.Duration(r.getActiveDeadlineSeconds(template)) * time.Second)
}

func (r *SandboxReconciler) handleRootfsSnapshot(
	ctx context.Context,
	sandbox *sandboxv1alpha1.Sandbox,
	baseSandbox *sandboxv1alpha1.Sandbox,
	sandboxPod *corev1.Pod,
	snapshotDeadline time.Time,
	activeDeadlineSeconds int64,
) (ctrl.Result, bool, error) {
	log := logf.FromContext(ctx).WithValues("sandbox", sandbox.Name, "namespace", sandbox.Namespace)

	now := r.clock().Now()
	if now.After(snapshotDeadline) {
		log.Info("Rootfs snapshot timed out", "deadline", snapshotDeadline)
		if err := r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
			{
				Type:               SandboxRootfsSnapshotCondition,
				Status:             metav1.ConditionFalse,
				Reason:             CondReasonSnapshotTimeout,
				Message:            "Rootfs snapshot did not complete before deadline",
				ObservedGeneration: sandbox.Generation,
			},
		}); err != nil {
			return ctrl.Result{}, false, err
		}
		// return true for cleanupDone so the sandbox gets deleted and as a result
		// the rootfssnapshot due to it being owned by the sandbox
		return ctrl.Result{}, true, nil
	}

	if sandboxPod == nil {
		log.Info("Skipping rootfs snapshot because sandbox pod is missing")
		if err := r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
			{
				Type:               SandboxRootfsSnapshotCondition,
				Status:             metav1.ConditionFalse,
				Reason:             CondReasonSnapshotFailed,
				Message:            "Sandbox pod no longer exists; snapshot skipped",
				ObservedGeneration: sandbox.Generation,
			},
		}); err != nil {
			return ctrl.Result{}, false, err
		}
		return ctrl.Result{}, true, nil
	}

	if !podutil.IsPodReady(sandboxPod) {
		log.Info("Unable to perform rootfs snapshot: pod not ready")
		r.Recorder.Event(sandbox, corev1.EventTypeWarning, "PodNotReady", "Unable to perform rootfs snapshot: pod not ready")

		if err := r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
			{
				Type:               SandboxRootfsSnapshotCondition,
				Status:             metav1.ConditionFalse,
				Reason:             CondReasonSnapshotFailed,
				Message:            "Sandbox pod is not ready",
				ObservedGeneration: sandbox.Generation,
			},
		}); err != nil {
			return ctrl.Result{}, false, err
		}
		return ctrl.Result{}, true, nil
	}

	supported, err := snapshot.CheckRootfsSnapshotSupport(ctx, r.Client, sandboxPod)
	if err != nil {
		log.Error(err, "Failed to validate snapshotting support")
		return ctrl.Result{}, false, err
	}

	if !supported {
		log.Info("Unable to perform rootfs snapshot: runtime not supported")
		r.Recorder.Event(sandbox, corev1.EventTypeWarning, "RuntimeNotSupported", "Unable to perform rootfs snapshot")

		if err := r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
			{
				Type:               SandboxRootfsSnapshotCondition,
				Status:             metav1.ConditionFalse,
				Reason:             CondReasonInvalidRuntime,
				Message:            "Runtime does not support rootfs snapshotting",
				ObservedGeneration: sandbox.Generation,
			},
		}); err != nil {
			return ctrl.Result{}, false, err
		}
		return ctrl.Result{}, true, nil
	}

	snap, err := r.getShutdownSnapshot(ctx, sandbox)
	if err != nil {
		return ctrl.Result{}, false, err
	}

	if snap == nil {
		return r.createShutdownSnapshot(ctx, sandbox, baseSandbox, activeDeadlineSeconds)
	}

	snapshotName := snap.Name
	if snap.Status.CompletedAt == nil {
		log.Info("Snapshot in progress, waiting", "snapshot", snapshotName)
		if err := r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
			{
				Type:               SandboxRootfsSnapshotCondition,
				Status:             metav1.ConditionFalse,
				Reason:             CondReasonSnapshottingInProgress,
				Message:            fmt.Sprintf("Snapshot %q is running", snapshotName),
				ObservedGeneration: sandbox.Generation,
			},
		}); err != nil {
			return ctrl.Result{}, false, err
		}

		requeueAfter := r.clock().Until(snapshotDeadline)
		if requeueAfter <= 0 {
			requeueAfter = time.Second
		} else if requeueAfter > 5*time.Second {
			requeueAfter = 5 * time.Second
		}
		return ctrl.Result{RequeueAfter: requeueAfter}, false, nil
	}

	readyCond := meta.FindStatusCondition(snap.Status.Conditions, string(sandboxv1alpha1.RootfsSnapshotComplete))
	if readyCond != nil && readyCond.Status == metav1.ConditionTrue {
		log.Info("Snapshot completed successfully", "snapshot", snapshotName)
		r.Recorder.Event(sandbox, corev1.EventTypeNormal, "SnapshotSucceeded", fmt.Sprintf("Snapshot %q completed", snapshotName))
		if err := r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
			{
				Type:               SandboxRootfsSnapshotCondition,
				Status:             metav1.ConditionTrue,
				Reason:             CondReasonSnapshotComplete,
				Message:            fmt.Sprintf("Snapshot %q completed", snapshotName),
				ObservedGeneration: sandbox.Generation,
			},
		}); err != nil {
			return ctrl.Result{}, false, err
		}
		return ctrl.Result{}, true, nil
	}

	// Completed but failed - proceed with deletion anyway
	message := "Snapshot failed"
	if readyCond != nil && readyCond.Message != "" {
		message = readyCond.Message
	}
	log.Info("Snapshot failed, proceeding with deletion", "snapshot", snapshotName)
	r.Recorder.Event(sandbox, corev1.EventTypeWarning, "SnapshotFailed", message)
	if err := r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
		{
			Type:               SandboxRootfsSnapshotCondition,
			Status:             metav1.ConditionFalse,
			Reason:             CondReasonSnapshotFailed,
			Message:            message,
			ObservedGeneration: sandbox.Generation,
		},
	}); err != nil {
		return ctrl.Result{}, false, err
	}
	return ctrl.Result{}, true, nil
}

func (r *SandboxReconciler) createShutdownSnapshot(
	ctx context.Context,
	sandbox *sandboxv1alpha1.Sandbox,
	baseSandbox *sandboxv1alpha1.Sandbox,
	activeDeadlineSeconds int64,
) (ctrl.Result, bool, error) {
	log := logf.FromContext(ctx).WithValues("sandbox", sandbox.Name, "namespace", sandbox.Namespace)

	snapshotName := getShutdownSnapshotName(sandbox)
	rootfsSnapshot := &sandboxv1alpha1.RootfsSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name:      snapshotName,
			Namespace: sandbox.Namespace,
			Labels: map[string]string{
				LabelSandboxName:            sandbox.Name,
				"sandbox.isola.run/trigger": "shutdown",
			},
		},
		Spec: sandboxv1alpha1.RootfsSnapshotSpec{
			SandboxName:           sandbox.Name,
			ActiveDeadlineSeconds: &activeDeadlineSeconds,
		},
	}

	// Set owner reference for cascade delete (if sandbox is force-deleted)
	if err := controllerutil.SetControllerReference(sandbox, rootfsSnapshot, r.Scheme); err != nil {
		log.Error(err, "Failed to set controller reference on RootfsSnapshot")
		return ctrl.Result{}, false, err
	}

	if err := r.Create(ctx, rootfsSnapshot); err != nil {
		if apierrors.IsAlreadyExists(err) {
			// Race condition - another reconcile created it, just requeue
			return ctrl.Result{RequeueAfter: time.Second}, false, nil
		}
		log.Error(err, "Failed to create RootfsSnapshot")
		return ctrl.Result{}, false, err
	}

	log.Info("Created shutdown RootfsSnapshot", "name", rootfsSnapshot.Name)
	r.Recorder.Event(sandbox, corev1.EventTypeNormal, "RootfsSnapshotCreated", fmt.Sprintf("Created RootfsSnapshot %q", rootfsSnapshot.Name))

	if err := r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
		{
			Type:               SandboxRootfsSnapshotCondition,
			Status:             metav1.ConditionFalse,
			Reason:             CondReasonSnapshottingInProgress,
			Message:            fmt.Sprintf("RootfsSnapshot %q created, waiting for completion", rootfsSnapshot.Name),
			ObservedGeneration: sandbox.Generation,
		},
	}); err != nil {
		return ctrl.Result{}, false, err
	}

	return ctrl.Result{RequeueAfter: time.Second}, false, nil
}
