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
)

const (
	FilesystemSnapshotFinalizer = "filesystemsnapshot.isola.run/cleanup"

	// Condition reasons
	FSSnapshotReasonPending     = "Pending"
	FSSnapshotReasonJobCreated  = "JobCreated"
	FSSnapshotReasonJobRunning  = "JobRunning"
	FSSnapshotReasonSucceeded   = "Succeeded"
	FSSnapshotReasonFailed      = "Failed"
	FSSnapshotReasonJobNotFound = "JobNotFound"
)

// Job helper functions

func isJobComplete(job *batchv1.Job) bool {
	if job == nil {
		return false
	}
	for _, condition := range job.Status.Conditions {
		if condition.Type == batchv1.JobComplete && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func isJobFailed(job *batchv1.Job) bool {
	if job == nil {
		return false
	}
	for _, condition := range job.Status.Conditions {
		if condition.Type == batchv1.JobFailed && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func getJobConditionMessage(job *batchv1.Job, conditionType batchv1.JobConditionType) string {
	if job == nil {
		return ""
	}
	for _, cond := range job.Status.Conditions {
		if cond.Type == conditionType && cond.Status == corev1.ConditionTrue {
			return cond.Message
		}
	}
	return ""
}

// FilesystemSnapshotReconciler reconciles a FilesystemSnapshot object
type FilesystemSnapshotReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
	Clock    Clock // Clock interface for time operations, allows mocking in tests
}

// clock returns the reconciler's Clock, defaulting to RealClock if not set
func (r *FilesystemSnapshotReconciler) clock() Clock {
	if r.Clock != nil {
		return r.Clock
	}
	return RealClock{}
}

// +kubebuilder:rbac:groups=sandbox.isola.run,resources=filesystemsnapshots,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=sandbox.isola.run,resources=filesystemsnapshots/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=sandbox.isola.run,resources=filesystemsnapshots/finalizers,verbs=update
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete

func (r *FilesystemSnapshotReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx).WithValues("filesystemsnapshot", req.Name, "namespace", req.Namespace)

	log.Info("Reconciling FilesystemSnapshot")

	snapshot := &sandboxv1alpha1.FilesystemSnapshot{}
	if err := r.Get(ctx, req.NamespacedName, snapshot); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("FilesystemSnapshot not found")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get FilesystemSnapshot")
		return ctrl.Result{}, err
	}

	// DeepCopy to allow patching only the diff
	baseSnapshot := snapshot.DeepCopy()

	if snapshot.Status.Conditions == nil {
		snapshot.Status.Conditions = []metav1.Condition{}
	}

	// Handle deletion
	if !snapshot.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(snapshot, FilesystemSnapshotFinalizer) {
			// Finalizer cleanup logic would go here if needed
			controllerutil.RemoveFinalizer(snapshot, FilesystemSnapshotFinalizer)
			if err := r.Update(ctx, snapshot); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(snapshot, FilesystemSnapshotFinalizer) {
		controllerutil.AddFinalizer(snapshot, FilesystemSnapshotFinalizer)
		if err := r.Update(ctx, snapshot); err != nil {
			return ctrl.Result{}, err
		}
		baseSnapshot = snapshot.DeepCopy()
	}

	// If already completed, nothing to do
	if snapshot.Status.Phase == sandboxv1alpha1.FilesystemSnapshotPhaseSucceeded ||
		snapshot.Status.Phase == sandboxv1alpha1.FilesystemSnapshotPhaseFailed {
		return ctrl.Result{}, nil
	}

	// Set start time if not set
	if snapshot.Status.StartTime == nil {
		now := metav1.NewTime(r.clock().Now())
		snapshot.Status.StartTime = &now
		snapshot.Status.Phase = sandboxv1alpha1.FilesystemSnapshotPhasePending
	}

	// Get or create the snapshotter job
	job, err := r.getSnapshotterJob(ctx, snapshot)
	if err != nil {
		return ctrl.Result{}, err
	}

	if job == nil {
		// Create the job
		if err := r.createSnapshotterJob(ctx, snapshot, baseSnapshot); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: time.Second}, nil
	}

	// Update status based on job state
	return r.reconcileStatus(ctx, snapshot, baseSnapshot, job)
}

func (r *FilesystemSnapshotReconciler) getSnapshotterJob(ctx context.Context, snapshot *sandboxv1alpha1.FilesystemSnapshot) (*batchv1.Job, error) {
	jobName := snapshot.GetSnapshotterJobName()

	job := &batchv1.Job{}
	if err := r.Get(ctx, types.NamespacedName{Name: jobName, Namespace: snapshot.Namespace}, job); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}

	return job, nil
}

func (r *FilesystemSnapshotReconciler) createSnapshotterJob(
	ctx context.Context,
	snapshot *sandboxv1alpha1.FilesystemSnapshot,
	baseSnapshot *sandboxv1alpha1.FilesystemSnapshot,
) error {
	log := logf.FromContext(ctx).WithValues("filesystemsnapshot", snapshot.Name, "namespace", snapshot.Namespace)

	jobName := snapshot.GetSnapshotterJobName()
	log.Info("Creating snapshotter job", "job", jobName, "node", snapshot.Spec.NodeName)

	privileged := false
	hostPathDirectory := corev1.HostPathDirectory
	hostPathFile := corev1.HostPathFile

	activeDeadlineSeconds := snapshot.Spec.ActiveDeadlineSeconds
	if activeDeadlineSeconds == 0 {
		activeDeadlineSeconds = 300
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: snapshot.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by":       "isola-operator",
				"filesystemsnapshot.isola.run/name":  snapshot.Name,
				"sandbox.isola.run/id":               snapshot.Spec.SandboxRef.Name,
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            ptr.To(int32(0)),
			ActiveDeadlineSeconds:   &activeDeadlineSeconds,
			TTLSecondsAfterFinished: ptr.To(int32(60)),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					NodeSelector: map[string]string{
						"kubernetes.io/hostname": snapshot.Spec.NodeName,
					},
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:    "snapshotter",
							Image:   "debian:stable-slim",
							Command: []string{"/usr/local/bin/runsc"},
							Args: []string{
								"--root=/run/containerd/runsc/k8s.io",
								"tar",
								"rootfs-upper",
								"--file",
								snapshot.Spec.SnapshotPath,
								snapshot.Spec.ContainerID,
							},
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

	// Set owner reference to FilesystemSnapshot for cleanup
	if err := controllerutil.SetControllerReference(snapshot, job, r.Scheme); err != nil {
		log.Error(err, "Failed to set controller reference for snapshot job")
		return err
	}

	if err := r.Create(ctx, job); err != nil {
		if apierrors.IsAlreadyExists(err) {
			log.Info("Snapshotter job already exists")
			return nil
		}
		log.Error(err, "Failed to create snapshotter job")
		return err
	}

	log.Info("Snapshotter job created", "job", jobName)
	r.Recorder.Event(snapshot, corev1.EventTypeNormal, "JobCreated", "Snapshotter job created")

	// Update status
	snapshot.Status.JobName = jobName
	snapshot.Status.Phase = sandboxv1alpha1.FilesystemSnapshotPhaseRunning
	snapshot.Status.Message = "Snapshotter job created"

	meta.SetStatusCondition(&snapshot.Status.Conditions, metav1.Condition{
		Type:               string(sandboxv1alpha1.FilesystemSnapshotJobCreated),
		Status:             metav1.ConditionTrue,
		Reason:             FSSnapshotReasonJobCreated,
		Message:            "Snapshotter job created",
		ObservedGeneration: snapshot.Generation,
	})

	if err := r.Status().Patch(ctx, snapshot, client.MergeFrom(baseSnapshot)); err != nil {
		log.Error(err, "Failed to update FilesystemSnapshot status")
		return err
	}

	return nil
}

func (r *FilesystemSnapshotReconciler) reconcileStatus(
	ctx context.Context,
	snapshot *sandboxv1alpha1.FilesystemSnapshot,
	baseSnapshot *sandboxv1alpha1.FilesystemSnapshot,
	job *batchv1.Job,
) (ctrl.Result, error) {
	log := logf.FromContext(ctx).WithValues("filesystemsnapshot", snapshot.Name, "namespace", snapshot.Namespace)

	if isJobComplete(job) {
		log.Info("Snapshotter job completed successfully")
		r.Recorder.Event(snapshot, corev1.EventTypeNormal, "Succeeded", "Filesystem snapshot completed")

		now := metav1.NewTime(r.clock().Now())
		snapshot.Status.CompletionTime = &now
		snapshot.Status.Phase = sandboxv1alpha1.FilesystemSnapshotPhaseSucceeded
		snapshot.Status.Message = "Snapshot completed successfully"

		meta.SetStatusCondition(&snapshot.Status.Conditions, metav1.Condition{
			Type:               string(sandboxv1alpha1.FilesystemSnapshotReady),
			Status:             metav1.ConditionTrue,
			Reason:             FSSnapshotReasonSucceeded,
			Message:            "Filesystem snapshot completed successfully",
			ObservedGeneration: snapshot.Generation,
		})

		if err := r.Status().Patch(ctx, snapshot, client.MergeFrom(baseSnapshot)); err != nil {
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, nil
	}

	if isJobFailed(job) {
		failureMessage := "Filesystem snapshot job failed"
		if condMsg := getJobConditionMessage(job, batchv1.JobFailed); condMsg != "" {
			failureMessage = fmt.Sprintf("Filesystem snapshot job failed: %s", condMsg)
		}
		log.Info(failureMessage)
		r.Recorder.Event(snapshot, corev1.EventTypeWarning, "Failed", failureMessage)

		now := metav1.NewTime(r.clock().Now())
		snapshot.Status.CompletionTime = &now
		snapshot.Status.Phase = sandboxv1alpha1.FilesystemSnapshotPhaseFailed
		snapshot.Status.Message = failureMessage

		meta.SetStatusCondition(&snapshot.Status.Conditions, metav1.Condition{
			Type:               string(sandboxv1alpha1.FilesystemSnapshotReady),
			Status:             metav1.ConditionFalse,
			Reason:             FSSnapshotReasonFailed,
			Message:            failureMessage,
			ObservedGeneration: snapshot.Generation,
		})

		if err := r.Status().Patch(ctx, snapshot, client.MergeFrom(baseSnapshot)); err != nil {
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, nil
	}

	// Job is still running
	snapshot.Status.Phase = sandboxv1alpha1.FilesystemSnapshotPhaseRunning
	snapshot.Status.Message = "Snapshotter job is running"

	meta.SetStatusCondition(&snapshot.Status.Conditions, metav1.Condition{
		Type:               string(sandboxv1alpha1.FilesystemSnapshotReady),
		Status:             metav1.ConditionFalse,
		Reason:             FSSnapshotReasonJobRunning,
		Message:            "Snapshotter job is running",
		ObservedGeneration: snapshot.Generation,
	})

	if err := r.Status().Patch(ctx, snapshot, client.MergeFrom(baseSnapshot)); err != nil {
		return ctrl.Result{}, err
	}

	// Requeue to check job status
	return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *FilesystemSnapshotReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Recorder = mgr.GetEventRecorderFor("filesystemsnapshot-controller")

	return ctrl.NewControllerManagedBy(mgr).
		For(&sandboxv1alpha1.FilesystemSnapshot{}).
		Owns(&batchv1.Job{}).
		Named("filesystemsnapshot").
		Complete(r)
}
