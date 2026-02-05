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
	"encoding/json"
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
	"k8s.io/client-go/tools/events"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	sandboxv1alpha1 "github.com/isola-ai/isola-sb/api/v1alpha1"
	"github.com/isola-ai/isola-sb/internal/operator/controller/podutil"
	"github.com/isola-ai/isola-sb/internal/operator/controller/snapshot"
	snapshotpkg "github.com/isola-ai/isola-sb/internal/snapshot"
)

const (
	defaultActiveDeadlineSecondsCheckpoint int64 = 300
	defaultTTLSecondsAfterFinishedChkpt    int32 = 300
)

// defaultCheckpointSizeLimit is used for the checkpoint output directory.
var defaultCheckpointSizeLimit = resource.MustParse("2Gi")

type GvisorCheckpointReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder events.EventRecorder
	Clock    Clock

	// BucketURL is the bucket URL for checkpoint storage (e.g., s3://bucket?region=us-east-1)
	BucketURL string
	// CredentialSecretName is the optional Secret name for bucket credentials
	CredentialSecretName string
	// UploaderImage is the container image for the checkpoint uploader
	UploaderImage string
	// CheckpointServiceAccount is the ServiceAccount for checkpoint jobs
	CheckpointServiceAccount string
	// ImagePullSecrets for pulling uploader images from private registries
	ImagePullSecrets []corev1.LocalObjectReference

	// Enabled controls whether checkpoint capability is enabled
	// When false, reconciliation fails fast with "checkpoint not configured"
	Enabled bool
	// GvisorRunscPath is the path to the runsc binary on cluster nodes
	GvisorRunscPath string
	// GvisorRunscRoot is the root directory where runsc stores runtime state
	GvisorRunscRoot string
}

func (r *GvisorCheckpointReconciler) clock() Clock {
	if r.Clock != nil {
		return r.Clock
	}
	return RealClock{}
}

func getCheckpointTTLSeconds(chkpt *sandboxv1alpha1.GvisorCheckpoint) time.Duration {
	ttl := defaultTTLSecondsAfterFinishedChkpt
	if chkpt.Spec.TTLSecondsAfterFinished != nil {
		ttl = *chkpt.Spec.TTLSecondsAfterFinished
	}

	return time.Duration(ttl) * time.Second
}

func (r *GvisorCheckpointReconciler) ttlLeft(chkpt *sandboxv1alpha1.GvisorCheckpoint) (ttlLeft time.Duration) {
	ttl := getCheckpointTTLSeconds(chkpt)

	deleteAt := chkpt.Status.CompletedAt.Add(ttl)
	now := r.clock().Now()

	return deleteAt.Sub(now)
}

// +kubebuilder:rbac:groups=sandbox.isola.run,resources=gvisorcheckpoints,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=sandbox.isola.run,resources=gvisorcheckpoints/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups=node.k8s.io,resources=runtimeclasses,verbs=get;list;watch

func (r *GvisorCheckpointReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	log.Info("Reconciling GvisorCheckpoint")

	chkpt := &sandboxv1alpha1.GvisorCheckpoint{}
	if err := r.Get(ctx, req.NamespacedName, chkpt); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Create base copy for status patches
	baseChkpt := chkpt.DeepCopy()

	isComplete := chkpt.Status.CompletedAt != nil
	if isComplete {
		ttlLeft := r.ttlLeft(chkpt)
		if ttlLeft <= 0 {
			if err := r.Delete(ctx, chkpt); err != nil && !apierrors.IsNotFound(err) {
				log.Error(err, "Failed to delete GvisorCheckpoint after TTL")
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, nil
		} else {
			return ctrl.Result{RequeueAfter: ttlLeft}, nil
		}
	}

	if !r.Enabled {
		log.Info("GvisorCheckpoint capability disabled - runtime type is clusterDefault or checkpoint.enabled is false")
		return r.setFailed(ctx, baseChkpt, chkpt, "GvisorCheckpoint capability is not enabled. Set operator.sandboxRuntime.type=gvisor and operator.sandboxRuntime.gvisor.checkpoint.enabled=true in Helm values.")
	}

	if r.BucketURL == "" {
		log.Info("GvisorCheckpoint storage not configured: ISOLA_CHECKPOINT_BUCKET_URL is required")
		return r.setFailed(ctx, baseChkpt, chkpt, "GvisorCheckpoint storage not configured: ISOLA_CHECKPOINT_BUCKET_URL is required")
	}

	sandboxPodName := podutil.GetSandboxPodName(chkpt.Spec.SandboxName)
	sandboxPod := &corev1.Pod{}
	if err := r.Get(ctx, types.NamespacedName{Name: sandboxPodName, Namespace: chkpt.Namespace}, sandboxPod); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("Pod not found", "pod", sandboxPodName)
			return r.setFailed(ctx, baseChkpt, chkpt, fmt.Sprintf("Pod %q not found", sandboxPodName))
		}
		return ctrl.Result{}, err
	}

	if !podutil.IsPodReady(sandboxPod) {
		return r.setFailed(ctx, baseChkpt, chkpt, "Sandbox pod is not ready")
	}

	supported, err := snapshot.CheckGvisorCheckpointSupport(ctx, r.Client, sandboxPod)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !supported {
		return r.setFailed(ctx, baseChkpt, chkpt, "Runtime does not support gvisor checkpointing")
	}

	containerName := chkpt.Spec.ContainerName
	if containerName == "" {
		return r.setFailed(ctx, baseChkpt, chkpt, "Container name is required")
	}

	return r.reconcileCheckpointJob(ctx, baseChkpt, chkpt, sandboxPod, containerName)
}

func (r *GvisorCheckpointReconciler) reconcileCheckpointJob(
	ctx context.Context,
	baseChkpt, chkpt *sandboxv1alpha1.GvisorCheckpoint,
	pod *corev1.Pod,
	containerName string,
) (ctrl.Result, error) {
	log := logf.FromContext(ctx).WithValues("container", containerName)

	jobName := podutil.GetCheckpointJobName(chkpt.Name)
	job := &batchv1.Job{}
	err := r.Get(ctx, types.NamespacedName{Name: jobName, Namespace: chkpt.Namespace}, job)

	if apierrors.IsNotFound(err) {
		containerID, err := podutil.ExtractContainerID(pod, containerName)
		if err != nil {
			log.Error(err, "Failed to extract container ID")
			return r.setFailed(ctx, baseChkpt, chkpt, fmt.Sprintf("Failed to extract container ID: %v", err))
		}

		err = r.createCheckpointJob(ctx, chkpt, pod, containerName, containerID)
		if err != nil {
			return ctrl.Result{}, err
		}

		log.Info("Created checkpoint job", "job", jobName)
		r.Recorder.Eventf(chkpt, nil, corev1.EventTypeNormal, "JobCreated", "Created", "Created checkpoint job for container %s", containerName)

		return r.setInProgress(ctx, baseChkpt, chkpt, containerName, containerID)
	}
	if err != nil {
		return ctrl.Result{}, err
	}

	// Job exists, check status
	if podutil.IsJobComplete(job) {
		log.Info("Checkpoint job completed", "job", jobName)

		// Read the upload result from the job pod's termination message
		result, err := r.getUploadResult(ctx, job)
		if err != nil {
			log.Error(err, "Failed to read upload result from termination message")
			r.Recorder.Eventf(chkpt, nil, corev1.EventTypeWarning, "TerminationLogReadFailed", "ReadFailed", "%s", err.Error())
			r.deleteJob(ctx, job)
			return r.setFailed(ctx, baseChkpt, chkpt, fmt.Sprintf("Failed to read upload result: %v", err))
		}

		r.Recorder.Eventf(chkpt, nil, corev1.EventTypeNormal, "CheckpointComplete", "Completed", "Checkpoint completed successfully")

		// Delete the job now that we've read the results
		r.deleteJob(ctx, job)

		return r.setSucceeded(ctx, baseChkpt, chkpt, result)
	}

	if podutil.IsJobFailed(job) {
		message := "Checkpoint job failed"
		if condMsg := podutil.GetJobConditionMessage(job, batchv1.JobFailed); condMsg != "" {
			message = fmt.Sprintf("Checkpoint job failed: %s", condMsg)
		}
		log.Info(message, "job", jobName)
		r.Recorder.Eventf(chkpt, nil, corev1.EventTypeWarning, "CheckpointFailed", "Failed", "%s", message)

		// Delete the job - no point keeping failed jobs until we hit the checkpoint TTL
		r.deleteJob(ctx, job)

		return r.setFailed(ctx, baseChkpt, chkpt, message)
	}

	// Still running - job watch will trigger reconciliation when status changes
	return ctrl.Result{}, nil
}

// getUploadResult reads the upload result from the job pod's termination message.
// The uploader writes a JSON UploadResult to /dev/termination-log when it completes.
func (r *GvisorCheckpointReconciler) getUploadResult(ctx context.Context, job *batchv1.Job) (*snapshotpkg.UploadResult, error) {
	// Find the pod created by this job
	podList := &corev1.PodList{}
	if err := r.List(ctx, podList,
		client.InNamespace(job.Namespace),
		client.MatchingLabels{"job-name": job.Name},
	); err != nil {
		return nil, fmt.Errorf("failed to list job pods: %w", err)
	}

	if len(podList.Items) == 0 {
		return nil, fmt.Errorf("no pods found for job %s", job.Name)
	}

	// Get the most recent pod (should only be one for BackoffLimit=0)
	pod := &podList.Items[0]
	for i := range podList.Items {
		if podList.Items[i].CreationTimestamp.After(pod.CreationTimestamp.Time) {
			pod = &podList.Items[i]
		}
	}

	// Find the uploader container's termination message
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.Name == "uploader" && cs.State.Terminated != nil {
			message := cs.State.Terminated.Message
			if message == "" {
				return nil, fmt.Errorf("uploader container has no termination message")
			}

			var result snapshotpkg.UploadResult
			if err := json.Unmarshal([]byte(message), &result); err != nil {
				return nil, fmt.Errorf("failed to parse termination message: %w", err)
			}
			return &result, nil
		}
	}

	return nil, fmt.Errorf("uploader container not found or not terminated")
}

func (r *GvisorCheckpointReconciler) createCheckpointJob(
	ctx context.Context,
	chkpt *sandboxv1alpha1.GvisorCheckpoint,
	sandboxPod *corev1.Pod,
	containerName, containerID string,
) error {
	log := logf.FromContext(ctx)

	jobName := podutil.GetCheckpointJobName(chkpt.Name)
	checkpointDir := "/checkpoint"

	activeDeadlineSeconds := defaultActiveDeadlineSecondsCheckpoint
	if chkpt.Spec.ActiveDeadlineSeconds != nil {
		activeDeadlineSeconds = *chkpt.Spec.ActiveDeadlineSeconds
	}

	hostPathDirectory := corev1.HostPathDirectory
	hostPathFile := corev1.HostPathFile

	uploaderEnv := []corev1.EnvVar{
		{Name: "ISOLA_BUCKET_URL", Value: r.BucketURL},
		{Name: "CHECKPOINT_DIR", Value: checkpointDir},
		{Name: "CHECKPOINT_NAMESPACE", Value: chkpt.Namespace},
		{Name: "CHECKPOINT_SANDBOX_NAME", Value: chkpt.Spec.SandboxName},
		{Name: "CHECKPOINT_CONTAINER_NAME", Value: containerName},
	}

	var uploaderEnvFrom []corev1.EnvFromSource
	if r.CredentialSecretName != "" {
		uploaderEnvFrom = []corev1.EnvFromSource{
			{
				SecretRef: &corev1.SecretEnvSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: r.CredentialSecretName,
					},
				},
			},
		}
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: chkpt.Namespace,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:          ptr.To(int32(0)),
			ActiveDeadlineSeconds: &activeDeadlineSeconds,
			// No TTLSecondsAfterFinished - we delete the Job explicitly after
			// reading results to avoid race conditions. Owner reference to
			// GvisorCheckpoint ensures cleanup if the checkpoint is deleted.
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					ServiceAccountName: r.CheckpointServiceAccount,
					HostPID:            true, // runsc checkpoint needs host PID namespace
					NodeSelector: map[string]string{
						"kubernetes.io/hostname": sandboxPod.Spec.NodeName,
					},
					RestartPolicy:    corev1.RestartPolicyNever,
					ImagePullSecrets: r.ImagePullSecrets,

					// Init container runs runsc checkpoint to create the checkpoint
					InitContainers: []corev1.Container{
						{
							Name:  "checkpointer",
							Image: "gcr.io/distroless/static:nonroot",
							Command: []string{r.GvisorRunscPath},
							Args: []string{
								fmt.Sprintf("--root=%s", r.GvisorRunscRoot),
								"checkpoint",
								"--image-path=" + checkpointDir,
								containerID,
							},
							SecurityContext: &corev1.SecurityContext{
								RunAsUser:  ptr.To(int64(0)), // root needed to read runsc state files
								RunAsGroup: ptr.To(int64(0)),
								Capabilities: &corev1.Capabilities{
									Drop: []corev1.Capability{"ALL"},
									// SYS_PTRACE needed for checkpoint
									Add: []corev1.Capability{"SYS_PTRACE"},
								},
								ReadOnlyRootFilesystem:   ptr.To(true),
								AllowPrivilegeEscalation: ptr.To(false),
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "runsc-bin", MountPath: r.GvisorRunscPath, ReadOnly: true},
								{Name: "runsc-state", MountPath: r.GvisorRunscRoot, ReadOnly: true},
								{Name: "checkpoint-data", MountPath: checkpointDir},
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("256Mi"),
								},
							},
						},
					},

					// Main container uploads the checkpoint to the bucket
					Containers: []corev1.Container{
						{
							Name:    "uploader",
							Image:   r.UploaderImage,
							Env:     uploaderEnv,
							EnvFrom: uploaderEnvFrom,
							SecurityContext: &corev1.SecurityContext{
								RunAsUser:    ptr.To(int64(65534)), // nobody
								RunAsGroup:   ptr.To(int64(65534)),
								RunAsNonRoot: ptr.To(true),
								Capabilities: &corev1.Capabilities{
									Drop: []corev1.Capability{"ALL"},
								},
								ReadOnlyRootFilesystem:   ptr.To(true),
								AllowPrivilegeEscalation: ptr.To(false),
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "checkpoint-data", MountPath: checkpointDir, ReadOnly: true},
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("50m"),
									corev1.ResourceMemory: resource.MustParse("64Mi"),
								},
							},
						},
					},

					Volumes: []corev1.Volume{
						{
							Name: "runsc-bin",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{Path: r.GvisorRunscPath, Type: &hostPathFile},
							},
						},
						{
							Name: "runsc-state",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{Path: r.GvisorRunscRoot, Type: &hostPathDirectory},
							},
						},
						{
							Name: "checkpoint-data",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{
									SizeLimit: &defaultCheckpointSizeLimit,
								},
							},
						},
					},
				},
			},
		},
	}

	if err := controllerutil.SetControllerReference(chkpt, job, r.Scheme); err != nil {
		log.Error(err, "Failed to set controller reference for job")
		return err
	}

	if err := r.Create(ctx, job); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			log.Error(err, "Failed to create checkpoint job")
			return err
		}
	}

	return nil
}

func (r *GvisorCheckpointReconciler) deleteJob(ctx context.Context, job *batchv1.Job) {
	log := logf.FromContext(ctx)
	if err := r.Delete(ctx, job, client.PropagationPolicy(metav1.DeletePropagationBackground)); err != nil {
		if !apierrors.IsNotFound(err) {
			// Non-fatal: owner reference ensures cleanup when GvisorCheckpoint is deleted
			log.Error(err, "Failed to delete job", "job", job.Name)
		}
	}
}

func (r *GvisorCheckpointReconciler) patchStatus(ctx context.Context, base, chkpt *sandboxv1alpha1.GvisorCheckpoint, conditions []metav1.Condition) error {
	if chkpt.Status.Conditions == nil {
		chkpt.Status.Conditions = []metav1.Condition{}
	}
	for _, c := range conditions {
		meta.SetStatusCondition(&chkpt.Status.Conditions, c)
	}
	return r.Status().Patch(ctx, chkpt, client.MergeFrom(base))
}

func (r *GvisorCheckpointReconciler) setInProgress(ctx context.Context, base, chkpt *sandboxv1alpha1.GvisorCheckpoint, containerName, containerID string) (ctrl.Result, error) {
	now := metav1.NewTime(r.clock().Now())
	chkpt.Status.StartedAt = &now
	chkpt.Status.ContainerName = containerName
	chkpt.Status.ContainerID = containerID
	if err := r.patchStatus(ctx, base, chkpt, []metav1.Condition{
		{
			Type:               string(sandboxv1alpha1.GvisorCheckpointComplete),
			Status:             metav1.ConditionFalse,
			Reason:             sandboxv1alpha1.ReasonGvisorCheckpointInProgress,
			Message:            "Checkpoint job running",
			ObservedGeneration: chkpt.Generation,
		},
	}); err != nil {
		return ctrl.Result{}, err
	}
	// Job watch (via Owns) will trigger reconciliation when job status changes
	return ctrl.Result{}, nil
}

func (r *GvisorCheckpointReconciler) setSucceeded(ctx context.Context, base, chkpt *sandboxv1alpha1.GvisorCheckpoint, result *snapshotpkg.UploadResult) (ctrl.Result, error) {
	now := metav1.NewTime(r.clock().Now())
	chkpt.Status.CompletedAt = &now

	if result != nil {
		chkpt.Status.Revision = result.Revision
		chkpt.Status.CheckpointKey = result.SnapshotKey
	}

	if err := r.patchStatus(ctx, base, chkpt, []metav1.Condition{
		{
			Type:               string(sandboxv1alpha1.GvisorCheckpointComplete),
			Status:             metav1.ConditionTrue,
			Reason:             sandboxv1alpha1.ReasonGvisorCheckpointSucceeded,
			Message:            "Checkpoint completed successfully",
			ObservedGeneration: chkpt.Generation,
		},
	}); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: getCheckpointTTLSeconds(chkpt)}, nil
}

func (r *GvisorCheckpointReconciler) setFailed(ctx context.Context, base, chkpt *sandboxv1alpha1.GvisorCheckpoint, message string) (ctrl.Result, error) {
	now := metav1.NewTime(r.clock().Now())
	chkpt.Status.CompletedAt = &now

	r.Recorder.Eventf(chkpt, nil, corev1.EventTypeWarning, "CheckpointFailed", "Failed", "%s", message)
	if err := r.patchStatus(ctx, base, chkpt, []metav1.Condition{
		{
			Type:               string(sandboxv1alpha1.GvisorCheckpointComplete),
			Status:             metav1.ConditionFalse,
			Reason:             sandboxv1alpha1.ReasonGvisorCheckpointFailed,
			Message:            message,
			ObservedGeneration: chkpt.Generation,
		},
	}); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: getCheckpointTTLSeconds(chkpt)}, nil
}

func (r *GvisorCheckpointReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Recorder = mgr.GetEventRecorder("gvisorcheckpoint-controller")

	return ctrl.NewControllerManagedBy(mgr).
		For(&sandboxv1alpha1.GvisorCheckpoint{}).
		Owns(&batchv1.Job{}).
		Named("gvisorcheckpoint").
		Complete(r)
}
