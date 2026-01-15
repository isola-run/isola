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
	"net/url"
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

	snapshotpkg "github.com/omereli/dev-isola/pkg/snapshot"
	sandboxv1alpha1 "github.com/omereli/dev-isola/services/isola-operator/api/v1alpha1"
	"github.com/omereli/dev-isola/services/isola-operator/internal/controller/podutil"
	"github.com/omereli/dev-isola/services/isola-operator/internal/controller/snapshot"
)

const (
	defaultActiveDeadlineSecondsSnapshot int64 = 300
	defaultTTLSecondsAfterFinished       int32 = 300

	// LabelSandboxName is the label key used for discovering RootfsSnapshots by sandbox.
	// This enables the Sandbox controller to find all snapshots for a given sandbox,
	// regardless of whether they were created by the controller or manually by a user.
	LabelSandboxName = "sandbox.isola.run/sandbox-name"
)

type RootfsSnapshotReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
	Clock    Clock

	// BucketURL is the bucket URL for snapshot storage (e.g., s3://bucket?region=us-east-1)
	BucketURL string
	// CredentialSecretName is the optional Secret name for bucket credentials
	CredentialSecretName string
	// UploaderImage is the container image for the uploader sidecar
	UploaderImage string
	// SnapshotServiceAccount is the ServiceAccount for snapshot jobs
	SnapshotServiceAccount string
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

	deleteAt := snap.Status.CompletedAt.Add(ttl)
	now := r.clock().Now()

	return deleteAt.Sub(now)
}

// ensureLabels ensures the RootfsSnapshot has the sandbox discovery label set.
// This label enables the Sandbox controller to find all snapshots for a sandbox
// via a label selector, regardless of who created the snapshot.
func (r *RootfsSnapshotReconciler) ensureLabels(ctx context.Context, snap *sandboxv1alpha1.RootfsSnapshot) (updated bool, err error) {
	expected := snap.Spec.SandboxName
	if expected == "" {
		return false, nil
	}

	current := ""
	if snap.Labels != nil {
		current = snap.Labels[LabelSandboxName]
	}

	if current == expected {
		return false, nil
	}

	if snap.Labels == nil {
		snap.Labels = make(map[string]string)
	}
	snap.Labels[LabelSandboxName] = expected

	if err := r.Update(ctx, snap); err != nil {
		return false, err
	}
	return true, nil
}

// +kubebuilder:rbac:groups=sandbox.isola.run,resources=rootfssnapshots,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=sandbox.isola.run,resources=rootfssnapshots/status,verbs=get;update;patch
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

	// Ensure the sandbox discovery label is set. This allows the Sandbox controller
	// to find all snapshots for a sandbox via label selector.
	if updated, err := r.ensureLabels(ctx, snap); err != nil {
		log.Error(err, "Failed to ensure labels")
		return ctrl.Result{}, err
	} else if updated {
		// Labels were updated, requeue to continue with fresh object
		return ctrl.Result{Requeue: true}, nil
	}

	isComplete := snap.Status.CompletedAt != nil
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

	if r.BucketURL == "" {
		log.Info("Snapshot storage not configured: ISOLA_SNAPSHOT_BUCKET_URL is required")
		return r.setFailed(ctx, snap, "Snapshot storage not configured: ISOLA_SNAPSHOT_BUCKET_URL is required")
	}

	sandboxPodName := snapshot.GetSandboxPodName(snap.Spec.SandboxName)
	sandboxPod := &corev1.Pod{}
	if err := r.Get(ctx, types.NamespacedName{Name: sandboxPodName, Namespace: snap.Namespace}, sandboxPod); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("Pod not found", "pod", sandboxPodName)
			return r.setFailed(ctx, snap, fmt.Sprintf("Pod %q not found", sandboxPodName))
		}
		return ctrl.Result{}, err
	}

	supported, retryable, err := snapshot.CheckRootfsSnapshotSupport(ctx, r.Client, sandboxPod)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !supported {
		if retryable {
			log.Info("Pod not ready for snapshotting, will retry")
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}
		return r.setFailed(ctx, snap, "Runtime does not support rootfs snapshotting")
	}

	containersToSnapshot := snap.Spec.ContainerNames
	if len(containersToSnapshot) == 0 {
		containersToSnapshot = filterSnapshotableContainers(sandboxPod)
	}
	if len(containersToSnapshot) == 0 {
		return r.setFailed(ctx, snap, "No containers found to snapshot")
	}

	// Get or create the snapshot job (single job for the first container for now)
	// TODO: support multiple containers by iterating
	containerName := containersToSnapshot[0]
	return r.reconcileSnapshotJob(ctx, snap, sandboxPod, containerName)
}

func (r *RootfsSnapshotReconciler) reconcileSnapshotJob(
	ctx context.Context,
	snap *sandboxv1alpha1.RootfsSnapshot,
	pod *corev1.Pod,
	containerName string,
) (ctrl.Result, error) {
	log := logf.FromContext(ctx).WithValues("container", containerName)

	// TODO: must bound to 63 chars max
	jobName := fmt.Sprintf("%s-%s", snap.Name, containerName)
	job := &batchv1.Job{}
	err := r.Get(ctx, types.NamespacedName{Name: jobName, Namespace: snap.Namespace}, job)

	if apierrors.IsNotFound(err) {
		containerID, err := podutil.ExtractContainerID(pod, containerName)
		if err != nil {
			log.Error(err, "Failed to extract container ID")
			return r.setFailed(ctx, snap, fmt.Sprintf("Failed to extract container ID: %v", err))
		}

		_, err = r.createSnapshotJob(ctx, snap, pod.Spec.NodeName, containerName, containerID)
		if err != nil {
			return ctrl.Result{}, err
		}

		log.Info("Created snapshot job", "job", jobName)
		r.Recorder.Event(snap, corev1.EventTypeNormal, "JobCreated", fmt.Sprintf("Created snapshot job for container %s", containerName))

		return r.setInProgress(ctx, snap, containerName, containerID)
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
			// Still mark as succeeded since the job completed, but log the error
			r.Recorder.Event(snap, corev1.EventTypeWarning, "TerminationLogReadFailed", err.Error())
		}

		r.Recorder.Event(snap, corev1.EventTypeNormal, "SnapshotComplete", "Snapshot completed successfully")

		// Delete the job now that we've read the results
		r.deleteJob(ctx, job)

		return r.setSucceeded(ctx, snap, result)
	}

	if podutil.IsJobFailed(job) {
		message := "Snapshot job failed"
		if condMsg := podutil.GetJobConditionMessage(job, batchv1.JobFailed); condMsg != "" {
			message = fmt.Sprintf("Snapshot job failed: %s", condMsg)
		}
		log.Info(message, "job", jobName)
		r.Recorder.Event(snap, corev1.EventTypeWarning, "SnapshotFailed", message)

		// Delete the job - no point keeping failed jobs around
		r.deleteJob(ctx, job)

		return r.setFailed(ctx, snap, message)
	}

	// Still running
	return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
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

// buildSnapshotURI constructs the full bucket URI for a snapshot.
func (r *RootfsSnapshotReconciler) buildSnapshotURI(snapshotKey string) string {
	// Parse bucket URL to extract just the scheme and host
	u, err := url.Parse(r.BucketURL)
	if err != nil {
		return fmt.Sprintf("%s/%s", r.BucketURL, snapshotKey)
	}
	return fmt.Sprintf("%s://%s/%s", u.Scheme, u.Host, snapshotKey)
}

func (r *RootfsSnapshotReconciler) createSnapshotJob(
	ctx context.Context,
	snap *sandboxv1alpha1.RootfsSnapshot,
	nodeName, containerName, containerID string,
) (*batchv1.Job, error) {
	log := logf.FromContext(ctx)

	jobName := fmt.Sprintf("%s-%s", snap.Name, containerName)
	localSnapshotPath := "/snapshot/rootfs.tar"

	activeDeadlineSeconds := defaultActiveDeadlineSecondsSnapshot
	if snap.Spec.ActiveDeadlineSeconds != nil {
		activeDeadlineSeconds = *snap.Spec.ActiveDeadlineSeconds
	}

	privileged := false
	hostPathDirectory := corev1.HostPathDirectory
	hostPathFile := corev1.HostPathFile

	// Build uploader container env vars
	// The uploader determines the revision by listing the bucket
	uploaderEnv := []corev1.EnvVar{
		{Name: "ISOLA_BUCKET_URL", Value: r.BucketURL},
		{Name: "SNAPSHOT_FILE", Value: localSnapshotPath},
		{Name: "SNAPSHOT_NAMESPACE", Value: snap.Namespace},
		{Name: "SNAPSHOT_SANDBOX_NAME", Value: snap.Spec.SandboxName},
		{Name: "SNAPSHOT_CONTAINER_NAME", Value: containerName},
	}

	// Build envFrom for credential secret (if configured)
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
			Namespace: snap.Namespace,
			Labels: map[string]string{
				"sandbox.isola.run/rootfs-snapshot": snap.Name,
				"sandbox.isola.run/container":       containerName,
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:          ptr.To(int32(0)),
			ActiveDeadlineSeconds: &activeDeadlineSeconds,
			// No TTLSecondsAfterFinished - we delete the Job explicitly after
			// reading results to avoid race conditions. Owner reference to
			// RootfsSnapshot ensures cleanup if the snapshot is deleted.
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					ServiceAccountName: r.SnapshotServiceAccount,
					NodeSelector: map[string]string{
						"kubernetes.io/hostname": nodeName,
					},
					RestartPolicy: corev1.RestartPolicyNever,

					// Init container runs runsc tar to create the snapshot
					InitContainers: []corev1.Container{
						{
							Name:    "snapshotter",
							Image:   "gcr.io/distroless/static:nonroot",
							Command: []string{"/usr/local/bin/runsc"},
							Args:    []string{"--root=/run/containerd/runsc/k8s.io", "tar", "rootfs-upper", "--file", localSnapshotPath, containerID},
							SecurityContext: &corev1.SecurityContext{
								Privileged: &privileged,
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "runsc-bin", MountPath: "/usr/local/bin/runsc", ReadOnly: true},
								{Name: "runsc-state", MountPath: "/run/containerd/runsc/k8s.io", ReadOnly: true},
								{Name: "snapshot-data", MountPath: "/snapshot"},
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

					// Main container uploads the snapshot to the bucket
					Containers: []corev1.Container{
						{
							Name:    "uploader",
							Image:   r.UploaderImage,
							Env:     uploaderEnv,
							EnvFrom: uploaderEnvFrom,
							VolumeMounts: []corev1.VolumeMount{
								{Name: "snapshot-data", MountPath: "/snapshot", ReadOnly: true},
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("64Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("500m"),
									corev1.ResourceMemory: resource.MustParse("512Mi"),
								},
							},
						},
					},

					Volumes: []corev1.Volume{
						{
							Name: "runsc-bin",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{Path: "/usr/bin/runsc", Type: &hostPathFile},
							},
						},
						{
							Name: "runsc-state",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{Path: "/run/containerd/runsc/k8s.io", Type: &hostPathDirectory},
							},
						},
						{
							Name: "snapshot-data",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},
				},
			},
		},
	}

	if err := controllerutil.SetControllerReference(snap, job, r.Scheme); err != nil {
		log.Error(err, "Failed to set controller reference for job")
		return nil, err
	}

	if err := r.Create(ctx, job); err != nil {
		if apierrors.IsAlreadyExists(err) {
			existing := &batchv1.Job{}
			if err := r.Get(ctx, types.NamespacedName{Name: jobName, Namespace: snap.Namespace}, existing); err != nil {
				return nil, err
			}
			return existing, nil
		}
		log.Error(err, "Failed to create snapshot job")
		return nil, err
	}

	return job, nil
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

func (r *RootfsSnapshotReconciler) setInProgress(ctx context.Context, snap *sandboxv1alpha1.RootfsSnapshot, containerName, containerID string) (ctrl.Result, error) {
	now := metav1.NewTime(r.clock().Now())
	snap.Status.StartedAt = &now
	snap.Status.ContainerSnapshots = []sandboxv1alpha1.ContainerSnapshotStatus{
		{
			ContainerName: containerName,
			ContainerID:   containerID,
			// SnapshotKey and SnapshotURI will be set when job completes
		},
	}
	meta.SetStatusCondition(&snap.Status.Conditions, metav1.Condition{
		Type:               string(sandboxv1alpha1.RootfsSnapshotReady),
		Status:             metav1.ConditionFalse,
		Reason:             sandboxv1alpha1.ReasonRootfsSnapshotInProgress,
		Message:            "Snapshot job running",
		ObservedGeneration: snap.Generation,
	})
	if err := r.Status().Update(ctx, snap); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
}

func (r *RootfsSnapshotReconciler) setSucceeded(ctx context.Context, snap *sandboxv1alpha1.RootfsSnapshot, result *snapshotpkg.UploadResult) (ctrl.Result, error) {
	now := metav1.NewTime(r.clock().Now())
	snap.Status.CompletedAt = &now

	// Update with actual values from uploader if available
	if result != nil {
		snap.Status.Revision = result.Revision
		if len(snap.Status.ContainerSnapshots) > 0 {
			snap.Status.ContainerSnapshots[0].SnapshotKey = result.SnapshotKey
			snap.Status.ContainerSnapshots[0].SnapshotURI = r.buildSnapshotURI(result.SnapshotKey)
		}
	}

	meta.SetStatusCondition(&snap.Status.Conditions, metav1.Condition{
		Type:               string(sandboxv1alpha1.RootfsSnapshotReady),
		Status:             metav1.ConditionTrue,
		Reason:             sandboxv1alpha1.ReasonRootfsSnapshotSucceeded,
		Message:            "Snapshot completed successfully",
		ObservedGeneration: snap.Generation,
	})
	if err := r.Status().Update(ctx, snap); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: getTTLSeconds(snap)}, nil
}

func (r *RootfsSnapshotReconciler) setFailed(ctx context.Context, snap *sandboxv1alpha1.RootfsSnapshot, message string) (ctrl.Result, error) {
	now := metav1.NewTime(r.clock().Now())
	snap.Status.CompletedAt = &now
	meta.SetStatusCondition(&snap.Status.Conditions, metav1.Condition{
		Type:               string(sandboxv1alpha1.RootfsSnapshotReady),
		Status:             metav1.ConditionFalse,
		Reason:             sandboxv1alpha1.ReasonRootfsSnapshotFailed,
		Message:            message,
		ObservedGeneration: snap.Generation,
	})
	r.Recorder.Event(snap, corev1.EventTypeWarning, "SnapshotFailed", message)
	if err := r.Status().Update(ctx, snap); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: getTTLSeconds(snap)}, nil
}

func filterSnapshotableContainers(pod *corev1.Pod) []string {
	if pod == nil {
		return nil
	}
	names := make([]string, 0, len(pod.Spec.Containers))
	for _, c := range pod.Spec.Containers {
		names = append(names, c.Name)
	}
	return names
}

func (r *RootfsSnapshotReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Recorder = mgr.GetEventRecorderFor("rootfssnapshot-controller")

	return ctrl.NewControllerManagedBy(mgr).
		For(&sandboxv1alpha1.RootfsSnapshot{}).
		Owns(&batchv1.Job{}).
		Named("rootfssnapshot").
		Complete(r)
}
