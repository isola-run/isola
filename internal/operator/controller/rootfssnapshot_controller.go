// Copyright The Isola Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
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

	sandboxv1alpha1 "github.com/isola-run/isola/api/v1alpha1"
	"github.com/isola-run/isola/internal/operator/controller/podutil"
	"github.com/isola-run/isola/internal/operator/controller/snapshot"
	snapshotpkg "github.com/isola-run/isola/internal/snapshot"
)

const (
	defaultTimeoutSecondsSnapshot  int64 = 300
	defaultTTLSecondsAfterFinished int32 = 300
)

// defaultRootfssnapshotSizeLimit is used when the container has no ephemeral storage limit.
var defaultRootfssnapshotSizeLimit = resource.MustParse("1Gi")

type RootfsSnapshotReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder events.EventRecorder
	Clock    Clock

	// BucketURL is the bucket URL for rootfs snapshot storage (e.g., s3://bucket?region=us-east-1)
	BucketURL string
	// CredentialSecretName is the optional Secret name for bucket credentials
	CredentialSecretName string
	// UploaderImage is the container image for the uploader sidecar
	UploaderImage string
	// UploaderImagePullPolicy is the pull policy for the uploader container
	UploaderImagePullPolicy corev1.PullPolicy
	// SnapshotServiceAccount is the ServiceAccount for rootfs snapshot jobs
	SnapshotServiceAccount string
	// ImagePullSecrets for pulling uploader images from private registries
	ImagePullSecrets []corev1.LocalObjectReference

	// Enabled controls whether rootfs snapshot capability is enabled
	// When false, reconciliation fails fast with "rootfs snapshot not configured"
	Enabled bool
	// GvisorRunscPath is the path to the runsc binary on cluster nodes
	GvisorRunscPath string
	// GvisorRunscRoot is the root directory where runsc stores runtime state
	GvisorRunscRoot string
}

func (r *RootfsSnapshotReconciler) clock() Clock {
	if r.Clock != nil {
		return r.Clock
	}
	return RealClock{}
}

func getTTLSeconds(snap *sandboxv1alpha1.RootfsSnapshot) time.Duration {
	ttl := defaultTTLSecondsAfterFinished
	if snap.Spec.TTLSecondsAfterFinished != nil {
		ttl = *snap.Spec.TTLSecondsAfterFinished
	}

	return time.Duration(ttl) * time.Second
}

func (r *RootfsSnapshotReconciler) ttlLeft(snap *sandboxv1alpha1.RootfsSnapshot) (ttlLeft time.Duration) {
	ttl := getTTLSeconds(snap)

	deleteAt := snap.Status.CompletionTime.Add(ttl)
	now := r.clock().Now()

	return deleteAt.Sub(now)
}

// +kubebuilder:rbac:groups=sandbox.isola.run,resources=rootfssnapshots,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=sandbox.isola.run,resources=rootfssnapshots/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups=node.k8s.io,resources=runtimeclasses,verbs=get;list;watch

func (r *RootfsSnapshotReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	log.Info("Reconciling RootfsSnapshot")

	snap := &sandboxv1alpha1.RootfsSnapshot{}
	if err := r.Get(ctx, req.NamespacedName, snap); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Create base copy for status patches
	baseSnap := snap.DeepCopy()

	isComplete := snap.Status.CompletionTime != nil
	if isComplete {
		ttlLeft := r.ttlLeft(snap)
		if ttlLeft <= 0 {
			if err := r.Delete(ctx, snap); err != nil && !apierrors.IsNotFound(err) {
				log.Error(err, "Failed to delete RootfsSnapshot after TTL")
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, nil
		} else {
			return ctrl.Result{RequeueAfter: ttlLeft}, nil
		}
	}

	if !r.Enabled {
		log.Info("RootfsSnapshot capability disabled (--rootfssnapshot-enabled is not set)")
		return r.setFailed(ctx, baseSnap, snap, "RootfsSnapshot capability is not enabled. Set operator.sandboxRuntime.type=gvisor and operator.sandboxRuntime.gvisor.rootfssnapshot.enabled=true in Helm values.")
	}

	if r.BucketURL == "" {
		log.Info("RootfsSnapshot storage not configured: --rootfssnapshot-bucket-url is required")
		return r.setFailed(ctx, baseSnap, snap, "RootfsSnapshot storage not configured: --rootfssnapshot-bucket-url is required")
	}

	sandboxPodName := podutil.GetSandboxPodName(snap.Spec.SandboxName)
	sandboxPod := &corev1.Pod{}
	if err := r.Get(ctx, types.NamespacedName{Name: sandboxPodName, Namespace: snap.Namespace}, sandboxPod); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("Pod not found", "pod", sandboxPodName)
			return r.setFailed(ctx, baseSnap, snap, fmt.Sprintf("Pod %q not found", sandboxPodName))
		}
		return ctrl.Result{}, err
	}

	if !podutil.IsPodReady(sandboxPod) {
		return r.setFailed(ctx, baseSnap, snap, "Sandbox pod is not ready")
	}

	supported, err := snapshot.CheckRootfsSnapshotSupport(ctx, r.Client, sandboxPod)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !supported {
		return r.setFailed(ctx, baseSnap, snap, "Runtime does not support rootfs snapshotting")
	}

	var containerName string
	if snap.Spec.ContainerName != "" {
		containerName = snap.Spec.ContainerName
		targetContainerInPod := slices.ContainsFunc(sandboxPod.Spec.Containers, func(c corev1.Container) bool {
			return c.Name == containerName
		})

		if !targetContainerInPod {
			return r.setFailed(ctx, baseSnap, snap, fmt.Sprintf("Container %q not found in sandbox pod", containerName))
		}
	} else if len(sandboxPod.Spec.Containers) == 1 {
		containerName = sandboxPod.Spec.Containers[0].Name
	} else if len(sandboxPod.Spec.Containers) > 1 {
		containerName = sandboxPod.Spec.Containers[0].Name
		log.Info("Multiple containers found in sandbox pod, defaulting to first container", "containerName", containerName)
	} else { // len(sandboxPod.Spec.Containers) == 0
		return r.setFailed(ctx, baseSnap, snap, "No containers found in sandbox pod")
	}

	// Key path: rootfssnapshots/<namespace>/<snapshotName>.tar
	return r.reconcileSnapshotJob(ctx, baseSnap, snap, sandboxPod, containerName, snap.Spec.SnapshotName)
}

func (r *RootfsSnapshotReconciler) reconcileSnapshotJob(
	ctx context.Context,
	baseSnap, snap *sandboxv1alpha1.RootfsSnapshot,
	pod *corev1.Pod,
	containerName, snapshotName string,
) (ctrl.Result, error) {
	log := logf.FromContext(ctx).WithValues("container", containerName)

	jobName := podutil.GetSnapshotJobName(snap.Name)
	job := &batchv1.Job{}
	err := r.Get(ctx, types.NamespacedName{Name: jobName, Namespace: snap.Namespace}, job)

	if apierrors.IsNotFound(err) {
		containerID, err := podutil.ExtractContainerID(pod, containerName)
		if err != nil {
			log.Error(err, "Failed to extract container ID")
			return r.setFailed(ctx, baseSnap, snap, fmt.Sprintf("Failed to extract container ID: %v", err))
		}

		err = r.createSnapshotJob(ctx, snap, pod, containerName, containerID, snapshotName)
		if err != nil {
			return ctrl.Result{}, err
		}
		rootfsSnapshotCreatedTotal.Inc()

		log.Info("Created snapshot job", "job", jobName)
		r.Recorder.Eventf(snap, nil, corev1.EventTypeNormal, "JobCreated", "Created", "Created snapshot job for container %s", containerName)

		return r.setInProgress(ctx, baseSnap, snap, containerID)
	}
	if err != nil {
		return ctrl.Result{}, err
	}

	// Job exists, check status
	if podutil.IsJobComplete(job) {
		log.Info("Snapshot job completed", "job", jobName)

		// Read the upload result from the job pod's termination message
		result, err := r.getUploadResult(ctx, job)
		if err != nil {
			log.Error(err, "Failed to read upload result from termination message")
			r.Recorder.Eventf(snap, nil, corev1.EventTypeWarning, "TerminationLogReadFailed", "ReadFailed", "%s", err.Error())
			r.deleteJob(ctx, job)
			return r.setFailed(ctx, baseSnap, snap, fmt.Sprintf("Failed to read upload result: %v", err))
		}

		// Delete the job now that we've read the results
		r.deleteJob(ctx, job)

		return r.setSucceeded(ctx, baseSnap, snap, result)
	}

	if podutil.IsJobFailed(job) {
		message := "Snapshot job failed"
		if condMsg := podutil.GetJobConditionMessage(job, batchv1.JobFailed); condMsg != "" {
			message = fmt.Sprintf("Snapshot job failed: %s", condMsg)
		}
		log.Info(message, "job", jobName)

		// Delete the job - no point keeping failed jobs until we hit to snapshotter TTL
		r.deleteJob(ctx, job)

		return r.setFailed(ctx, baseSnap, snap, message)
	}

	// Still running - job watch will trigger reconciliation when status changes
	return ctrl.Result{}, nil
}

// getUploadResult reads the upload result from the job pod's termination message.
// The uploader writes a JSON UploadResult to /dev/termination-log when it completes.
func (r *RootfsSnapshotReconciler) getUploadResult(ctx context.Context, job *batchv1.Job) (*snapshotpkg.UploadResult, error) {
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

// getRootfssnapshotSizeLimit returns the ephemeral storage limit for a container.
// With gVisor's root:self overlay2, the rootfs upper layer is stored on disk
// in the container's root filesystem, so Kubernetes ephemeral storage limits apply directly.
// Falls back to defaultRootfssnapshotSizeLimit if no limit is set.
func (r *RootfsSnapshotReconciler) getRootfssnapshotSizeLimit(pod *corev1.Pod, containerName string) *resource.Quantity {
	for _, c := range pod.Spec.Containers {
		if c.Name == containerName {
			if limit, ok := c.Resources.Limits[corev1.ResourceEphemeralStorage]; ok {
				return &limit
			}
			break
		}
	}
	return &defaultRootfssnapshotSizeLimit
}

func (r *RootfsSnapshotReconciler) createSnapshotJob(
	ctx context.Context,
	snap *sandboxv1alpha1.RootfsSnapshot,
	sandboxPod *corev1.Pod,
	containerName, containerID, snapshotName string,
) error {
	log := logf.FromContext(ctx)

	jobName := podutil.GetSnapshotJobName(snap.Name)
	localSnapshotPath := "/snapshot/rootfs.tar"

	timeoutSeconds := defaultTimeoutSecondsSnapshot
	if snap.Spec.TimeoutSeconds != nil {
		timeoutSeconds = *snap.Spec.TimeoutSeconds
	}

	rootfssnapshotSizeLimit := r.getRootfssnapshotSizeLimit(sandboxPod, containerName)

	hostPathDirectory := corev1.HostPathDirectory
	hostPathFile := corev1.HostPathFile

	uploaderEnv := []corev1.EnvVar{
		{Name: "ISOLA_BUCKET_URL", Value: r.BucketURL},
		{Name: "SNAPSHOT_FILE", Value: localSnapshotPath},
		{Name: "SNAPSHOT_NAME", Value: snapshotName},
		{Name: "SNAPSHOT_NAMESPACE", Value: snap.Namespace},
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

	jobLabels := map[string]string{
		"app.kubernetes.io/name":       "isola-uploader",
		"app.kubernetes.io/instance":   snap.Name,
		"app.kubernetes.io/component":  "rootfssnapshot",
		"app.kubernetes.io/part-of":    "isola",
		"app.kubernetes.io/managed-by": "isola-operator",
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: snap.Namespace,
			Labels:    jobLabels,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:          ptr.To(int32(0)),
			ActiveDeadlineSeconds: &timeoutSeconds,
			// No TTLSecondsAfterFinished - we delete the Job explicitly after
			// reading results to avoid race conditions. Owner reference to
			// RootfsSnapshot ensures cleanup if the snapshot is deleted.
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: jobLabels,
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: r.SnapshotServiceAccount,
					HostPID:            true, // runsc tar needs host PID namespace to verify sandbox is running
					NodeSelector: map[string]string{
						"kubernetes.io/hostname": sandboxPod.Spec.NodeName,
					},
					RestartPolicy:    corev1.RestartPolicyNever,
					ImagePullSecrets: r.ImagePullSecrets,

					// Init container runs runsc tar to create the snapshot
					InitContainers: []corev1.Container{
						{
							Name:    "snapshotter",
							Image:   "gcr.io/distroless/static:nonroot",
							Command: []string{r.GvisorRunscPath},
							Args:    []string{fmt.Sprintf("--root=%s", r.GvisorRunscRoot), "tar", "rootfs-upper", "--file", localSnapshotPath, containerID},
							SecurityContext: &corev1.SecurityContext{
								RunAsUser:  ptr.To(int64(0)), // root needed to read runsc state files
								RunAsGroup: ptr.To(int64(0)),
								Capabilities: &corev1.Capabilities{
									Drop: []corev1.Capability{"ALL"},
								},
								ReadOnlyRootFilesystem:   ptr.To(true),
								AllowPrivilegeEscalation: ptr.To(false),
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "runsc-bin", MountPath: r.GvisorRunscPath, ReadOnly: true},
								{Name: "runsc-state", MountPath: r.GvisorRunscRoot, ReadOnly: true},
								{Name: "snapshot-data", MountPath: "/snapshot"},
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("50m"),
									corev1.ResourceMemory: resource.MustParse("64Mi"),
								},
							},
						},
					},

					// Main container uploads the snapshot to the bucket
					Containers: []corev1.Container{
						{
							Name:            "uploader",
							Image:           r.UploaderImage,
							ImagePullPolicy: r.UploaderImagePullPolicy,
							Env:             uploaderEnv,
							EnvFrom:         uploaderEnvFrom,
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
								{Name: "snapshot-data", MountPath: "/snapshot", ReadOnly: true},
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
							Name: "snapshot-data",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{
									SizeLimit: rootfssnapshotSizeLimit,
								},
							},
						},
					},
				},
			},
		},
	}

	if err := controllerutil.SetControllerReference(snap, job, r.Scheme); err != nil {
		log.Error(err, "Failed to set controller reference for job")
		return err
	}

	if err := r.Create(ctx, job); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			log.Error(err, "Failed to create snapshot job")
			return err
		}
	}

	return nil
}

func (r *RootfsSnapshotReconciler) deleteJob(ctx context.Context, job *batchv1.Job) {
	log := logf.FromContext(ctx)
	if err := r.Delete(ctx, job, client.PropagationPolicy(metav1.DeletePropagationBackground)); err != nil {
		if !apierrors.IsNotFound(err) {
			// Non-fatal: owner reference ensures cleanup when RootfsSnapshot is deleted
			log.Error(err, "Failed to delete job", "job", job.Name)
		}
	}
}

func (r *RootfsSnapshotReconciler) patchStatus(ctx context.Context, base, snap *sandboxv1alpha1.RootfsSnapshot, conditions []metav1.Condition) error {
	if snap.Status.Conditions == nil {
		snap.Status.Conditions = []metav1.Condition{}
	}
	for _, c := range conditions {
		meta.SetStatusCondition(&snap.Status.Conditions, c)
	}
	return r.Status().Patch(ctx, snap, client.MergeFrom(base))
}

func (r *RootfsSnapshotReconciler) setInProgress(ctx context.Context, base, snap *sandboxv1alpha1.RootfsSnapshot, containerID string) (ctrl.Result, error) {
	now := metav1.NewTime(r.clock().Now())
	snap.Status.StartTime = &now
	snap.Status.ContainerID = containerID
	if err := r.patchStatus(ctx, base, snap, nil); err != nil {
		return ctrl.Result{}, err
	}
	// Job watch (via Owns) will trigger reconciliation when job status changes
	return ctrl.Result{}, nil
}

func (r *RootfsSnapshotReconciler) setSucceeded(ctx context.Context, base, snap *sandboxv1alpha1.RootfsSnapshot, result *snapshotpkg.UploadResult) (ctrl.Result, error) {
	now := metav1.NewTime(r.clock().Now())
	snap.Status.CompletionTime = &now

	if result != nil {
		snap.Status.SnapshotKey = result.SnapshotKey
	}

	if err := r.patchStatus(ctx, base, snap, []metav1.Condition{
		{
			Type:               sandboxv1alpha1.RootfsSnapshotSucceededCondition,
			Status:             metav1.ConditionTrue,
			Reason:             sandboxv1alpha1.ReasonRootfsSnapshotSucceeded,
			Message:            "Snapshot completed successfully",
			ObservedGeneration: snap.Generation,
		},
	}); err != nil {
		return ctrl.Result{}, err
	}

	rootfsSnapshotCompletedTotal.WithLabelValues("succeeded").Inc()
	r.Recorder.Eventf(snap, nil, corev1.EventTypeNormal, "SnapshotComplete", "Completed", "Snapshot completed successfully")
	return ctrl.Result{RequeueAfter: getTTLSeconds(snap)}, nil
}

func (r *RootfsSnapshotReconciler) setFailed(ctx context.Context, base, snap *sandboxv1alpha1.RootfsSnapshot, message string) (ctrl.Result, error) {
	now := metav1.NewTime(r.clock().Now())
	snap.Status.CompletionTime = &now

	if err := r.patchStatus(ctx, base, snap, []metav1.Condition{
		{
			Type:               sandboxv1alpha1.RootfsSnapshotSucceededCondition,
			Status:             metav1.ConditionFalse,
			Reason:             sandboxv1alpha1.ReasonRootfsSnapshotFailed,
			Message:            message,
			ObservedGeneration: snap.Generation,
		},
	}); err != nil {
		return ctrl.Result{}, err
	}

	rootfsSnapshotCompletedTotal.WithLabelValues("failed").Inc()
	r.Recorder.Eventf(snap, nil, corev1.EventTypeWarning, "SnapshotFailed", "Failed", "%s", message)
	return ctrl.Result{RequeueAfter: getTTLSeconds(snap)}, nil
}

func (r *RootfsSnapshotReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Recorder = mgr.GetEventRecorder("rootfssnapshot-controller")

	return ctrl.NewControllerManagedBy(mgr).
		For(&sandboxv1alpha1.RootfsSnapshot{}).
		Owns(&batchv1.Job{}).
		Named("rootfssnapshot").
		Complete(r)
}
