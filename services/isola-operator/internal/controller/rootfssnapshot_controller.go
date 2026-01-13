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

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	sandboxv1alpha1 "github.com/omereli/dev-isola/services/isola-operator/api/v1alpha1"
	"github.com/omereli/dev-isola/services/isola-operator/internal/controller/podutil"
	"github.com/omereli/dev-isola/services/isola-operator/internal/controller/snapshot"
)

const (
	RootfsSnapshotFinalizer = "rootfssnapshot.isola.run/cleanup"

	defaultActiveDeadlineSecondsSnapshot int64 = 300
)

type RootfsSnapshotReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
	Clock    Clock
}

func (r *RootfsSnapshotReconciler) clock() Clock {
	if r.Clock != nil {
		return r.Clock
	}
	return RealClock{}
}

func (r *RootfsSnapshotReconciler) patchStatus(ctx context.Context, baseSnap *sandboxv1alpha1.RootfsSnapshot, snap *sandboxv1alpha1.RootfsSnapshot, conditions []metav1.Condition) error {
	if snap.Status.Conditions == nil {
		snap.Status.Conditions = []metav1.Condition{}
	}
	for _, cond := range conditions {
		meta.SetStatusCondition(&snap.Status.Conditions, cond)
	}
	return r.Status().Patch(ctx, snap, client.MergeFrom(baseSnap))
}

// +kubebuilder:rbac:groups=sandbox.isola.run,resources=rootfssnapshots,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=sandbox.isola.run,resources=rootfssnapshots/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=sandbox.isola.run,resources=rootfssnapshots/finalizers,verbs=update
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups=node.k8s.io,resources=runtimeclasses,verbs=get;list;watch

func (r *RootfsSnapshotReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx).WithValues("rootfssnapshot", req.NamespacedName)

	log.Info("Reconciling RootfsSnapshot")

	snap := &sandboxv1alpha1.RootfsSnapshot{}
	if err := r.Get(ctx, req.NamespacedName, snap); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	baseSnap := snap.DeepCopy()

	// Handle TTL cleanup for finished snapshots
	ttlResult, ttlDone := r.handleTTLCleanup(ctx, snap)
	if ttlDone {
		return ttlResult, nil
	}

	// Handle deletion
	if !snap.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, snap)
	}

	// Ensure finalizer
	if !controllerutil.ContainsFinalizer(snap, RootfsSnapshotFinalizer) {
		controllerutil.AddFinalizer(snap, RootfsSnapshotFinalizer)
		if err := r.Update(ctx, snap); err != nil {
			return ctrl.Result{}, err
		}
		baseSnap = snap.DeepCopy()
	}

	// Check if already completed
	readyCond := meta.FindStatusCondition(snap.Status.Conditions, string(sandboxv1alpha1.RootfsSnapshotReady))
	if readyCond != nil && (readyCond.Reason == sandboxv1alpha1.ReasonRootfsSnapshotSucceeded ||
		readyCond.Reason == sandboxv1alpha1.ReasonRootfsSnapshotFailed) {
		// Already finished - return TTL requeue result if TTL is set
		return ttlResult, nil
	}

	// Get the sandbox pod
	podName := snapshot.GetSandboxPodName(snap.Spec.SandboxName)
	pod := &corev1.Pod{}
	if err := r.Get(ctx, types.NamespacedName{Name: podName, Namespace: snap.Namespace}, pod); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("Pod not found", "pod", podName)
			return r.setFailedStatus(ctx, snap, baseSnap,
				sandboxv1alpha1.ReasonRootfsSnapshotFailed,
				fmt.Sprintf("Pod %q not found", podName))
		}
		return ctrl.Result{}, err
	}

	// Check runtime support
	supported, retryable, err := snapshot.CheckRootfsSnapshotSupport(ctx, r.Client, pod)
	if err != nil {
		log.Error(err, "Failed to check runtime support")
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	if !supported {
		if retryable {
			log.Info("Pod not ready for snapshotting, will retry")
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}
		log.Info("Runtime not supported for snapshotting")
		return r.setRuntimeNotSupported(ctx, snap, baseSnap)
	}

	// Runtime is supported - set condition
	if err := r.patchStatus(ctx, baseSnap, snap, []metav1.Condition{
		{
			Type:               string(sandboxv1alpha1.RootfsSnapshotRuntimeSupported),
			Status:             metav1.ConditionTrue,
			Reason:             sandboxv1alpha1.ReasonRuntimeSupported,
			Message:            "Runtime supports rootfs snapshotting",
			ObservedGeneration: snap.Generation,
		},
	}); err != nil {
		return ctrl.Result{}, err
	}
	baseSnap = snap.DeepCopy()

	// Determine which containers to snapshot
	containerNames := snap.Spec.ContainerNames
	if len(containerNames) == 0 {
		containerNames = snapshot.GetSnapshotableContainers(pod)
	}

	if len(containerNames) == 0 {
		log.Info("No containers to snapshot")
		return r.setFailedStatus(ctx, snap, baseSnap,
			sandboxv1alpha1.ReasonRootfsSnapshotFailed,
			"No containers found to snapshot")
	}

	// Initialize container snapshot statuses if needed
	if len(snap.Status.ContainerSnapshots) == 0 {
		snap.Status.ContainerSnapshots = make([]sandboxv1alpha1.ContainerSnapshotStatus, 0, len(containerNames))
		for _, name := range containerNames {
			snap.Status.ContainerSnapshots = append(snap.Status.ContainerSnapshots, sandboxv1alpha1.ContainerSnapshotStatus{
				ContainerName: name,
				Conditions:    []metav1.Condition{},
			})
		}
		if err := r.patchStatus(ctx, baseSnap, snap, nil); err != nil {
			return ctrl.Result{}, err
		}
		baseSnap = snap.DeepCopy()
	}

	// Process each container
	nodeName := pod.Spec.NodeName
	allSucceeded := true
	anyFailed := false
	anyPending := false

	for i := range snap.Status.ContainerSnapshots {
		cs := &snap.Status.ContainerSnapshots[i]

		// Check current condition
		readyCond := meta.FindStatusCondition(cs.Conditions, string(sandboxv1alpha1.ContainerSnapshotReady))
		if readyCond != nil && readyCond.Status == metav1.ConditionTrue {
			continue // Already succeeded
		}
		if readyCond != nil && readyCond.Reason == sandboxv1alpha1.ReasonContainerFailed {
			anyFailed = true
			allSucceeded = false
			continue // Already failed
		}

		// Get or create job for this container
		result, err := r.reconcileContainerSnapshot(ctx, snap, baseSnap, pod, cs, nodeName)
		if err != nil {
			return result, err
		}

		// Re-check condition after reconcile
		readyCond = meta.FindStatusCondition(cs.Conditions, string(sandboxv1alpha1.ContainerSnapshotReady))
		if readyCond == nil || readyCond.Status != metav1.ConditionTrue {
			allSucceeded = false
			if readyCond != nil && readyCond.Reason == sandboxv1alpha1.ReasonContainerFailed {
				anyFailed = true
			} else {
				anyPending = true
			}
		}
	}

	// Update aggregate Ready condition
	baseSnap = snap.DeepCopy()
	now := metav1.NewTime(r.clock().Now())

	if allSucceeded {
		snap.Status.CompletedAt = &now
		if err := r.patchStatus(ctx, baseSnap, snap, []metav1.Condition{
			{
				Type:               string(sandboxv1alpha1.RootfsSnapshotReady),
				Status:             metav1.ConditionTrue,
				Reason:             sandboxv1alpha1.ReasonRootfsSnapshotSucceeded,
				Message:            "All container snapshots completed successfully",
				ObservedGeneration: snap.Generation,
			},
		}); err != nil {
			return ctrl.Result{}, err
		}
		r.Recorder.Event(snap, corev1.EventTypeNormal, "SnapshotComplete", "All container rootfs snapshots completed")
		log.Info("All container snapshots completed")
		return ctrl.Result{}, nil
	}

	if anyFailed && !anyPending {
		// All done, but some failed
		snap.Status.CompletedAt = &now
		if err := r.patchStatus(ctx, baseSnap, snap, []metav1.Condition{
			{
				Type:               string(sandboxv1alpha1.RootfsSnapshotReady),
				Status:             metav1.ConditionFalse,
				Reason:             sandboxv1alpha1.ReasonRootfsSnapshotFailed,
				Message:            "One or more container snapshots failed",
				ObservedGeneration: snap.Generation,
			},
		}); err != nil {
			return ctrl.Result{}, err
		}
		r.Recorder.Event(snap, corev1.EventTypeWarning, "SnapshotFailed", "One or more container snapshots failed")
		return ctrl.Result{}, nil
	}

	// Still in progress
	if err := r.patchStatus(ctx, baseSnap, snap, []metav1.Condition{
		{
			Type:               string(sandboxv1alpha1.RootfsSnapshotReady),
			Status:             metav1.ConditionFalse,
			Reason:             sandboxv1alpha1.ReasonRootfsSnapshotInProgress,
			Message:            "Container snapshots in progress",
			ObservedGeneration: snap.Generation,
		},
	}); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
}

func (r *RootfsSnapshotReconciler) reconcileContainerSnapshot(
	ctx context.Context,
	snap *sandboxv1alpha1.RootfsSnapshot,
	baseSnap *sandboxv1alpha1.RootfsSnapshot,
	pod *corev1.Pod,
	cs *sandboxv1alpha1.ContainerSnapshotStatus,
	nodeName string,
) (ctrl.Result, error) {
	log := logf.FromContext(ctx).WithValues("container", cs.ContainerName)

	jobName := r.getJobName(snap, cs.ContainerName)

	// Check if job exists
	job := &batchv1.Job{}
	err := r.Get(ctx, types.NamespacedName{Name: jobName, Namespace: snap.Namespace}, job)
	if err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, err
	}

	if apierrors.IsNotFound(err) {
		// Need to create the job
		containerID, err := snapshot.ExtractContainerID(pod, cs.ContainerName)
		if err != nil {
			log.Error(err, "Failed to extract container ID")
			r.updateContainerCondition(cs, metav1.ConditionFalse,
				sandboxv1alpha1.ReasonContainerFailed, err.Error(), snap.Generation)
			if err := r.patchStatus(ctx, baseSnap, snap, nil); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, nil
		}

		cs.ContainerID = containerID
		job, err = r.createSnapshotJob(ctx, snap, cs, nodeName, containerID)
		if err != nil {
			return ctrl.Result{}, err
		}

		cs.JobName = job.Name
		cs.SnapshotPath = r.getSnapshotPath(snap.Spec.SandboxName, cs.ContainerName)

		// Set StartedAt if this is the first job
		if snap.Status.StartedAt == nil {
			now := metav1.NewTime(r.clock().Now())
			snap.Status.StartedAt = &now
		}

		r.updateContainerCondition(cs, metav1.ConditionFalse,
			sandboxv1alpha1.ReasonContainerJobCreated, "Snapshot job created", snap.Generation)

		if err := r.patchStatus(ctx, baseSnap, snap, nil); err != nil {
			return ctrl.Result{}, err
		}

		log.Info("Created snapshot job", "job", jobName)
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	// Job exists, check its status
	if podutil.IsJobComplete(job) {
		log.Info("Snapshot job completed", "job", jobName)
		r.updateContainerCondition(cs, metav1.ConditionTrue,
			sandboxv1alpha1.ReasonContainerSucceeded, "Snapshot completed successfully", snap.Generation)
		if err := r.patchStatus(ctx, baseSnap, snap, nil); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	if podutil.IsJobFailed(job) {
		message := "Snapshot job failed"
		if condMsg := podutil.GetJobConditionMessage(job, batchv1.JobFailed); condMsg != "" {
			message = fmt.Sprintf("Snapshot job failed: %s", condMsg)
		}
		log.Info(message, "job", jobName)
		r.updateContainerCondition(cs, metav1.ConditionFalse,
			sandboxv1alpha1.ReasonContainerFailed, message, snap.Generation)
		if err := r.patchStatus(ctx, baseSnap, snap, nil); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Job still running
	r.updateContainerCondition(cs, metav1.ConditionFalse,
		sandboxv1alpha1.ReasonContainerJobRunning, "Snapshot job running", snap.Generation)
	if err := r.patchStatus(ctx, baseSnap, snap, nil); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
}

func (r *RootfsSnapshotReconciler) createSnapshotJob(
	ctx context.Context,
	snap *sandboxv1alpha1.RootfsSnapshot,
	cs *sandboxv1alpha1.ContainerSnapshotStatus,
	nodeName string,
	containerID string,
) (*batchv1.Job, error) {
	log := logf.FromContext(ctx)

	jobName := r.getJobName(snap, cs.ContainerName)
	snapshotPath := r.getSnapshotPath(snap.Spec.SandboxName, cs.ContainerName)

	activeDeadlineSeconds := defaultActiveDeadlineSecondsSnapshot
	if snap.Spec.ActiveDeadlineSeconds != nil {
		activeDeadlineSeconds = *snap.Spec.ActiveDeadlineSeconds
	}

	privileged := false
	hostPathDirectory := corev1.HostPathDirectory
	hostPathFile := corev1.HostPathFile

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: snap.Namespace,
			Labels: map[string]string{
				"sandbox.isola.run/rootfs-snapshot": snap.Name,
				"sandbox.isola.run/container":       cs.ContainerName,
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            ptr.To(int32(0)),
			ActiveDeadlineSeconds:   &activeDeadlineSeconds,
			TTLSecondsAfterFinished: ptr.To(int32(60)),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					NodeSelector: map[string]string{
						"kubernetes.io/hostname": nodeName,
					},
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:    "snapshotter",
							Image:   "debian:stable-slim",
							Command: []string{"/usr/local/bin/runsc"},
							Args:    []string{"--root=/run/containerd/runsc/k8s.io", "tar", "rootfs-upper", "--file", snapshotPath, containerID},
							SecurityContext: &corev1.SecurityContext{
								Privileged: &privileged,
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "runsc-bin",
									MountPath: "/usr/local/bin/runsc",
									ReadOnly:  true,
								},
								{
									Name:      "runsc-state",
									MountPath: "/run/containerd/runsc/k8s.io",
									ReadOnly:  true,
								},
								{
									Name:      "tmp-output",
									MountPath: "/tmp",
								},
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("64Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("500m"),
									corev1.ResourceMemory: resource.MustParse("256Mi"),
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "runsc-bin",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/usr/bin/runsc",
									Type: &hostPathFile,
								},
							},
						},
						{
							Name: "runsc-state",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/run/containerd/runsc/k8s.io",
									Type: &hostPathDirectory,
								},
							},
						},
						{
							Name: "tmp-output",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/tmp",
									Type: &hostPathDirectory,
								},
							},
						},
					},
				},
			},
		},
	}

	// Set owner reference to RootfsSnapshot
	if err := controllerutil.SetControllerReference(snap, job, r.Scheme); err != nil {
		log.Error(err, "Failed to set controller reference for job")
		return nil, err
	}

	if err := r.Create(ctx, job); err != nil {
		if apierrors.IsAlreadyExists(err) {
			// Race condition, job already exists
			existingJob := &batchv1.Job{}
			if err := r.Get(ctx, types.NamespacedName{Name: jobName, Namespace: snap.Namespace}, existingJob); err != nil {
				return nil, err
			}
			return existingJob, nil
		}
		log.Error(err, "Failed to create snapshot job")
		return nil, err
	}

	r.Recorder.Event(snap, corev1.EventTypeNormal, "JobCreated",
		fmt.Sprintf("Created snapshot job for container %s", cs.ContainerName))

	return job, nil
}

func (r *RootfsSnapshotReconciler) getJobName(snap *sandboxv1alpha1.RootfsSnapshot, containerName string) string {
	return fmt.Sprintf("%s-%s", snap.Name, containerName)
}

func (r *RootfsSnapshotReconciler) getSnapshotPath(sandboxName, containerName string) string {
	return fmt.Sprintf("/tmp/rootfs-%s-%s.tar", sandboxName, containerName)
}

func (r *RootfsSnapshotReconciler) updateContainerCondition(
	cs *sandboxv1alpha1.ContainerSnapshotStatus,
	status metav1.ConditionStatus,
	reason, message string,
	generation int64,
) {
	meta.SetStatusCondition(&cs.Conditions, metav1.Condition{
		Type:               string(sandboxv1alpha1.ContainerSnapshotReady),
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: generation,
	})
}

func (r *RootfsSnapshotReconciler) handleDeletion(ctx context.Context, snap *sandboxv1alpha1.RootfsSnapshot) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(snap, RootfsSnapshotFinalizer) {
		return ctrl.Result{}, nil
	}

	// Delete all owned jobs
	for _, cs := range snap.Status.ContainerSnapshots {
		if cs.JobName == "" {
			continue
		}
		job := &batchv1.Job{}
		err := r.Get(ctx, types.NamespacedName{Name: cs.JobName, Namespace: snap.Namespace}, job)
		if err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return ctrl.Result{}, err
		}
		propagationPolicy := metav1.DeletePropagationBackground
		if err := r.Delete(ctx, job, &client.DeleteOptions{PropagationPolicy: &propagationPolicy}); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		log.Info("Deleted snapshot job during cleanup", "job", cs.JobName)
	}

	controllerutil.RemoveFinalizer(snap, RootfsSnapshotFinalizer)
	if err := r.Update(ctx, snap); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *RootfsSnapshotReconciler) handleTTLCleanup(ctx context.Context, snap *sandboxv1alpha1.RootfsSnapshot) (ctrl.Result, bool) {
	if snap.Spec.TTLSecondsAfterFinished == nil {
		return ctrl.Result{}, false
	}

	if snap.Status.CompletedAt == nil {
		return ctrl.Result{}, false
	}

	ttl := time.Duration(*snap.Spec.TTLSecondsAfterFinished) * time.Second
	deleteAt := snap.Status.CompletedAt.Add(ttl)
	now := r.clock().Now()

	if now.After(deleteAt) {
		log := logf.FromContext(ctx)
		log.Info("TTL expired, deleting RootfsSnapshot")
		if err := r.Delete(ctx, snap); err != nil && !apierrors.IsNotFound(err) {
			log.Error(err, "Failed to delete RootfsSnapshot after TTL")
			return ctrl.Result{RequeueAfter: time.Second}, true
		}
		return ctrl.Result{}, true
	}

	// Requeue for TTL
	remaining := deleteAt.Sub(now)
	return ctrl.Result{RequeueAfter: remaining}, false
}

func (r *RootfsSnapshotReconciler) setFailedStatus(
	ctx context.Context,
	snap *sandboxv1alpha1.RootfsSnapshot,
	baseSnap *sandboxv1alpha1.RootfsSnapshot,
	reason, message string,
) (ctrl.Result, error) {
	now := metav1.NewTime(r.clock().Now())
	snap.Status.CompletedAt = &now
	if err := r.patchStatus(ctx, baseSnap, snap, []metav1.Condition{
		{
			Type:               string(sandboxv1alpha1.RootfsSnapshotReady),
			Status:             metav1.ConditionFalse,
			Reason:             reason,
			Message:            message,
			ObservedGeneration: snap.Generation,
		},
	}); err != nil {
		return ctrl.Result{}, err
	}
	r.Recorder.Event(snap, corev1.EventTypeWarning, "SnapshotFailed", message)
	return ctrl.Result{}, nil
}

func (r *RootfsSnapshotReconciler) setRuntimeNotSupported(
	ctx context.Context,
	snap *sandboxv1alpha1.RootfsSnapshot,
	baseSnap *sandboxv1alpha1.RootfsSnapshot,
) (ctrl.Result, error) {
	message := "Runtime does not support rootfs snapshotting"

	now := metav1.NewTime(r.clock().Now())
	snap.Status.CompletedAt = &now

	if err := r.patchStatus(ctx, baseSnap, snap, []metav1.Condition{
		{
			Type:               string(sandboxv1alpha1.RootfsSnapshotRuntimeSupported),
			Status:             metav1.ConditionFalse,
			Reason:             sandboxv1alpha1.ReasonRuntimeNotSupported,
			Message:            message,
			ObservedGeneration: snap.Generation,
		},
		{
			Type:               string(sandboxv1alpha1.RootfsSnapshotReady),
			Status:             metav1.ConditionFalse,
			Reason:             sandboxv1alpha1.ReasonRootfsSnapshotFailed,
			Message:            message,
			ObservedGeneration: snap.Generation,
		},
	}); err != nil {
		return ctrl.Result{}, err
	}

	r.Recorder.Event(snap, corev1.EventTypeWarning, "RuntimeNotSupported", message)
	return ctrl.Result{}, nil
}

func (r *RootfsSnapshotReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Recorder = mgr.GetEventRecorderFor("rootfssnapshot-controller")

	return ctrl.NewControllerManagedBy(mgr).
		For(&sandboxv1alpha1.RootfsSnapshot{}).
		Owns(&batchv1.Job{}).
		Named("rootfssnapshot").
		Complete(r)
}
