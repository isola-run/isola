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
	"errors"
	"fmt"
	"maps"
	"net/netip"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	nodev1 "k8s.io/api/node/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	sandboxv1alpha1 "github.com/omereli/dev-isola/services/isola-operator/api/v1alpha1"
	"k8s.io/client-go/tools/record"
)

const (
	// Summary condition
	SandboxReadyCondition = "Ready"

	SandboxTemplateReadyCondition = "TemplateReady"
	SandboxPodReadyCondition      = "PodReady"
	SandboxNetworkReadyCondition  = "NetworkConfigured"
	// todo benl: maybe keep just sandbox.Status.Snapshot and no snapshotting condition on sandbox (consider custom CRD for snapshotting)
	SandboxFilesystemSnapshotCondition = "FilesystemSnapshot"
)

const (
	CondReasonTemplateNotFound = "TemplateNotFound"
	CondReasonTemplateResolved = "TemplateResolved"

	CondReasonPodPending        = "PodPending"
	CondReasonPodRunning        = "PodRunning"
	CondReasonPodFailed         = "PodFailed"
	CondReasonPodSucceeded      = "PodSucceeded"
	CondReasonPodCreating       = "PodCreating"
	CondReasonPodCreationFailed = "PodCreationFailed"
	CondReasonSandboxTimedOut   = "TimedOut"
	CondReasonDeleting          = "Deleting"
	CondReasonReconciling       = "Reconciling"

	// Snapshot-related reasons
	CondReasonSnapshottingInProgress = "SnapshottingInProgress"
	CondReasonSnapshotComplete       = "SnapshotComplete"
	CondReasonSnapshotFailed         = "SnapshotFailed"
	CondReasonSnapshotTimeout        = "SnapshotTimeout"
	CondReasonInvalidRuntime         = "InvalidRuntime"

	// NetworkPolicy-related reasons
	CondReasonNetworkPolicyApplied = "NetworkPolicyApplied"
	CondReasonNetworkPolicyFailed  = "NetworkPolicyFailed"
	CondReasonNetworkConfigured    = "NetworkConfigured"
)

const defaultActiveDeadlineSeconds int64 = 300

const SandboxFinalizer = "sandbox.isola.run/cleanup"

type CleanupTrigger string

const (
	CleanupTriggerTimeout  CleanupTrigger = "Timeout"
	CleanupTriggerDeletion CleanupTrigger = "Deletion"
)

type SandboxReconciler struct {
	client.Client
	Scheme           *runtime.Scheme
	Recorder         record.EventRecorder
	AgentImage       string
	RuntimeClassName string // RuntimeClassName to use for sandbox pods (e.g. "gvisor"). Empty means use cluster default.
	Clock            Clock  // Clock interface for time operations, allows mocking in tests
}

const (
	agentContainerName = "isola-agent"

	// Field index for efficient lookup of sandboxes by templates references
	sandboxTemplateRefField = ".spec.templateRef.name"
)

// clock returns the reconciler's Clock, defaulting to RealClock if not set
func (r *SandboxReconciler) clock() Clock {
	if r.Clock != nil {
		return r.Clock
	}
	return RealClock{}
}


func (r *SandboxReconciler) buildAgentContainer() corev1.Container {
	rp := corev1.ContainerRestartPolicyAlways
	return corev1.Container{
		Name:          agentContainerName,
		Image:         r.AgentImage,
		RestartPolicy: &rp,
		// RunAsUser 0 (root) is needed to read /proc/<pid>/environ of other users' processes
		// and to access /proc/<pid>/root when using shared PID namespace.
		SecurityContext: &corev1.SecurityContext{
			RunAsUser: ptr.To(int64(0)),
		},
	}
}

func (r *SandboxReconciler) injectSidecar(sandboxPod *corev1.Pod) error {
	if len(sandboxPod.Spec.Containers) != 1 {
		// todo: remove this assumption
		return fmt.Errorf("sandbox pod must have exactly one container")
	}

	// todo benl: Mark with sandboxPod.Spec.Containers[i].Name
	// Mark the first container as the main container so the agent can discover it via /proc/<pid>/environ.
	// Note: a single main container is supported. The agent's findMarkedProcess() returns the first PID it finds with the ISOLA_MAIN_CONTAINER marker.
	sandboxPod.Spec.Containers[0].Env = append(sandboxPod.Spec.Containers[0].Env, corev1.EnvVar{
		Name:  "ISOLA_MAIN_CONTAINER",
		Value: "true",
	})

	agentContainer := r.buildAgentContainer()
	sandboxPod.Spec.InitContainers = append(sandboxPod.Spec.InitContainers, agentContainer)
	return nil
}

func isPodReady(pod *corev1.Pod) bool {
	if pod == nil {
		return false
	}
	if pod.Status.Phase != corev1.PodRunning {
		return false
	}
	for i := range pod.Status.Conditions {
		c := pod.Status.Conditions[i]
		if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func isPodTerminated(pod *corev1.Pod) bool {
	if pod == nil {
		return false
	}
	return pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed
}

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

// extractContainerID extracts the container ID from a pod's container status
// Returns the raw ID without the containerd:// prefix
func extractContainerID(sandboxPod *corev1.Pod) (string, error) {
	if sandboxPod == nil {
		return "", fmt.Errorf("pod is nil")
	}
	if sandboxPod.Status.ContainerStatuses == nil {
		return "", fmt.Errorf("pod has no container statuses")
	}
	// todo benl: currently we only allow and assume a single application container in the pod
	if len(sandboxPod.Status.ContainerStatuses) != 1 {
		return "", fmt.Errorf("pod has %d container statuses, expected 1", len(sandboxPod.Status.ContainerStatuses))
	}
	cs := sandboxPod.Status.ContainerStatuses[0]
	// containerID format: containerd://abc123...
	if cs.ContainerID == "" {
		return "", fmt.Errorf("container has no containerID yet")
	}
	parts := strings.SplitN(cs.ContainerID, "://", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("unexpected containerID format: %s", cs.ContainerID)
	}
	return parts[1], nil
}

var (
	ErrSandboxPodNotFound  = errors.New("sandbox pod not found")
	ErrSandboxPodNotReady  = errors.New("sandbox pod is not ready")
	ErrRuntimeClassMissing = errors.New("runtimeClassName is not set")
	ErrRuntimeUnsupported  = errors.New("runtime class does not support snapshotting")
)

const (
	ReasonFSSnapshotSupported           = "SnapshotSupported"
	ReasonFSSnapshotPodDoesNotExist     = "PodDoesNotExist"
	ReasonFSSnapshotRuntimeClassMissing = "RuntimeClassMissing"
	ReasonFSSnapshotRuntimeUnsupported  = "RuntimeUnsupported"
	ReasonFSSnapshotTargetNotFound      = "TargetNotFound"
	ReasonFSSnapshotPodNotReady         = "SnapshotPodNotReady"
	ReasonFSSnapshotSnapshotting        = "Snapshotting"
	ReasonFSSnapshotNotSnapshotting     = "NotSnapshotting"
)

func (r *SandboxReconciler) verifySnapshottingCapability(ctx context.Context, sandboxPod *corev1.Pod) (string, error) {
	if sandboxPod == nil {
		// can't snapshot if pod doesn't exist
		return ReasonFSSnapshotPodDoesNotExist, nil
	}
	if !isPodReady(sandboxPod) {
		// can't snapshot if pod is not ready
		return ReasonFSSnapshotPodNotReady, nil
	}

	runtimeClassName := sandboxPod.Spec.RuntimeClassName
	if runtimeClassName == nil || *runtimeClassName == "" {
		// default runtime class (can't assume it's snapshottable)
		return ReasonFSSnapshotRuntimeClassMissing, nil
	}

	// 3. Resolve the RuntimeClass to check the actual Handler
	runtimeClass := &nodev1.RuntimeClass{}
	if err := r.Get(ctx, types.NamespacedName{Name: *runtimeClassName}, runtimeClass); err != nil {
		return ReasonFSSnapshotTargetNotFound, err
	}

	if runtimeClass.Handler == "runsc" || runtimeClass.Handler == "gvisor" {
		return ReasonFSSnapshotSupported, nil
	}

	return ReasonFSSnapshotRuntimeUnsupported, nil
}

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

// todo benl: get sandbox if create failed with already exists?
func (r *SandboxReconciler) CreateSandboxPod(ctx context.Context, sandbox *sandboxv1alpha1.Sandbox, baseSandbox *sandboxv1alpha1.Sandbox, template *sandboxv1alpha1.SandboxTemplate) error {
	log := logf.FromContext(ctx).WithValues("sandbox", sandbox.Name, "namespace", sandbox.Namespace)
	// todo benl reduce verbose logging
	log.Info("Creating Pod")

	labels := map[string]string{
		"app":                          "isola-sandbox",
		"sandbox.isola.run/id":         sandbox.Name,
		"app.kubernetes.io/managed-by": "isola-operator",
		"cluster-autoscaler.kubernetes.io/safe-to-evict": "false",
	}

	// Add network capability labels based on sandbox network config.
	// These labels trigger the pre-installed shared NetworkPolicies.
	for k, v := range sandbox.GetNetworkLabels() {
		labels[k] = v
	}

	// todo benl: why this exists? ("sandbox-id")
	if sandbox.Labels != nil {
		if sandboxID, exists := sandbox.Labels["sandbox-id"]; exists {
			labels["sandbox-id"] = sandboxID
		}
	}

	if template.Spec.PodTemplate.Labels != nil {
		maps.Copy(labels, template.Spec.PodTemplate.Labels)
	}

	sandboxPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      getSandboxPodName(sandbox),
			Namespace: sandbox.Namespace,
			Labels:    labels,
		},
		// todo benl: copy annotations as well?
		Spec: template.Spec.PodTemplate.Spec,
	}

	// Set RuntimeClassName if configured (e.g. "gvisor" for sandboxed execution)
	if r.RuntimeClassName != "" {
		sandboxPod.Spec.RuntimeClassName = &r.RuntimeClassName

		// Configure gvisor overlay2 for rootfs ("root"), backed by a file ("self").
		// References:
		//   - https://github.com/google/gvisor/issues/3494 (per-sandbox flag overrides)
		//   - https://github.com/google/gvisor/commit/a53b22ad5283b00b766178eff847c3193c1293b7 (overlay2 self medium)
		// Note: containerd must have pod_annotations=["dev.gvisor.*"] configured to pass this through.
		if sandboxPod.Annotations == nil {
			sandboxPod.Annotations = map[string]string{}
		}
		sandboxPod.Annotations["dev.gvisor.flag.overlay2"] = "root:self"
	}

	// Set hostname and subdomain to enable DNS-based addressing via headless service.
	// Both are required for DNS to work: <hostname>.<subdomain>.<namespace>.svc.cluster.local
	sandboxPod.Spec.Hostname = getSandboxPodName(sandbox)
	sandboxPod.Spec.Subdomain = "sandbox-agents"

	// Enable shared PID namespace so the isola agent can locate the main container and access it's filesystem via /proc/<pid>/root
	sandboxPod.Spec.ShareProcessNamespace = ptr.To(true)

	// Set high priority to prevent preemption by applicative non-sandbox pods
	sandboxPod.Spec.PriorityClassName = "isola-sandbox"

	// todo benl: implement api to restore pod from snapshot (make sure they are compatible)
	// if sandboxPod.Annotations == nil {
	// 	sandboxPod.Annotations = map[string]string{}
	// }

	// sandboxPod.Annotations["dev.gvisor.tar.rootfs.upper.todobenl"] = "/tmp/rootfs-sandbox-870e5846-1766869560.tar"

	// Configure DNS for sandbox pods based on NetworkConfig settings.
	if err := configureDNS(sandboxPod, sandbox.Spec.Network); err != nil {
		log.Error(err, "Failed to configure DNS")
		return err
	}

	if err := r.injectSidecar(sandboxPod); err != nil {
		log.Error(err, "Failed to inject sidecar")
		return err
	}
	// Set Pod's object owner reference to the Sandbox object
	if err := controllerutil.SetControllerReference(sandbox, sandboxPod, r.Scheme); err != nil {
		log.Error(err, "Failed to set controller reference")
		return err
	}

	if err := r.Create(ctx, sandboxPod); err != nil {
		log.Error(err, "Failed creating Pod")

		// not checking err here, best effort status patch and return the create error
		_ = r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
			{
				Type:               SandboxPodReadyCondition,
				Status:             metav1.ConditionFalse,
				Reason:             CondReasonPodCreationFailed,
				Message:            err.Error(),
				ObservedGeneration: sandbox.Generation,
			},
			{
				Type:               SandboxReadyCondition,
				Status:             metav1.ConditionFalse,
				Reason:             CondReasonPodCreationFailed,
				Message:            err.Error(),
				ObservedGeneration: sandbox.Generation,
			},
		})
		return err
	}

	log.Info("Pod created")

	// todo benl: this doesn't print anything - rbac issues?
	r.Recorder.Event(sandbox, corev1.EventTypeNormal, "PodCreated", "Sandbox Pod created")

	if err := r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
		{
			Type:               SandboxPodReadyCondition,
			Status:             metav1.ConditionFalse,
			Reason:             CondReasonPodCreating,
			Message:            "Creating sandbox Pod",
			ObservedGeneration: sandbox.Generation,
		},
		{
			Type:               SandboxReadyCondition,
			Status:             metav1.ConditionFalse,
			Reason:             CondReasonReconciling,
			Message:            "Waiting for Pod to be created/ready",
			ObservedGeneration: sandbox.Generation,
		},
	}); err != nil {
		log.Error(err, "Failed to update Sandbox status")
		return err
	}

	return nil
}

// configureDNS sets up DNS configuration for the sandbox pod based on the NetworkConfig.
// - AllowClusterDNS=true: Uses ClusterFirst policy with optional additional nameservers
// - AllowClusterDNS=false with DNS servers: Uses None policy with specified nameservers
// - AllowClusterDNS=false without DNS servers: Uses None policy with sink nameserver (fast-fail)
func configureDNS(sandboxPod *corev1.Pod, networkConfig *sandboxv1alpha1.NetworkConfig) error {
	// Default: deny-all DNS (no cluster DNS, no external DNS)
	if networkConfig == nil {
		sandboxPod.Spec.DNSPolicy = corev1.DNSNone
		sandboxPod.Spec.DNSConfig = &corev1.PodDNSConfig{
			// Sink nameserver - DNS queries will fail fast
			Nameservers: []string{"127.0.0.1"},
			Options: []corev1.PodDNSConfigOption{
				{Name: "timeout", Value: ptr.To("1")},
				{Name: "attempts", Value: ptr.To("1")},
				{Name: "ndots", Value: ptr.To("1")},
			},
		}
		return nil
	}

	if networkConfig.AllowClusterDNS {
		sandboxPod.Spec.DNSPolicy = corev1.DNSClusterFirst
		if len(networkConfig.DNS) > 0 {
			sandboxPod.Spec.DNSConfig = &corev1.PodDNSConfig{
				Nameservers: networkConfig.DNS,
			}
		}
		return nil
	}

	// No cluster DNS - use external DNS servers or sink
	sandboxPod.Spec.DNSPolicy = corev1.DNSNone
	nameservers := networkConfig.DNS
	dnsOptions := []corev1.PodDNSConfigOption{
		// ndots:1 - external domains are tried directly without search domain suffix
		{Name: "ndots", Value: ptr.To("1")},
	}
	if len(nameservers) == 0 {
		// Sink nameserver - DNS queries will fail fast
		nameservers = []string{"127.0.0.1"}
		dnsOptions = []corev1.PodDNSConfigOption{
			{Name: "timeout", Value: ptr.To("1")},
			{Name: "attempts", Value: ptr.To("1")},
			{Name: "ndots", Value: ptr.To("1")},
		}
	}
	sandboxPod.Spec.DNSConfig = &corev1.PodDNSConfig{
		Nameservers: nameservers,
		Options:     dnsOptions,
	}

	return nil
}

// todo benl: extract snapshotting to a separate controller that manages the FSSnapshotter CRD
// CreateSnapshotterJob creates a Job to snapshot the sandbox container's filesystem
func (r *SandboxReconciler) CreateSnapshotterJob(
	ctx context.Context,
	sandbox *sandboxv1alpha1.Sandbox,
	baseSandbox *sandboxv1alpha1.Sandbox,
	sandboxPod *corev1.Pod,
	activeDeadlineSeconds int64,
) (ctrl.Result, error) {
	// todo benl: reduce linux capabilities of snapshot job's pod to only what is needed
	log := logf.FromContext(ctx).WithValues("sandbox", sandbox.Name, "namespace", sandbox.Namespace)

	snapshotterJobName := getFilesystemSnapshotterJobName(sandbox)
	nodeName := sandboxPod.Spec.NodeName
	timestamp := r.clock().Now().Unix()
	snapshotPath := fmt.Sprintf("/tmp/rootfs-%s-%d.tar", sandbox.Name, timestamp)

	containerID, err := extractContainerID(sandboxPod)
	if err != nil {
		log.Error(err, "Failed to extract container ID")
		return ctrl.Result{}, err
	}

	log.Info("Creating filesystem snapshotter job", "job", snapshotterJobName, "node", nodeName)

	privileged := false
	hostPathDirectory := corev1.HostPathDirectory
	hostPathFile := corev1.HostPathFile

	// todo benl: add labels / annotations
	// todo benl: create a minimal image (possibly with runsc backed in with a fixed version)
	snapshotJob := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      snapshotterJobName,
			Namespace: sandbox.Namespace,
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
								// todo benl: upload to bucket instead
								{
									Name:      "tmp-output",
									MountPath: "/tmp",
								},
							},
							// todo benl: adjust resources. Large files might lead to OOM since gvisor loads them to memory?
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

	// Set owner reference to sandbox for cleanup
	if err := controllerutil.SetControllerReference(sandbox, snapshotJob, r.Scheme); err != nil {
		log.Error(err, "Failed to set controller reference for snapshot job")
		return ctrl.Result{}, err
	}

	if err := r.Create(ctx, snapshotJob); err != nil {
		if apierrors.IsAlreadyExists(err) {
			log.Info("Snapshotter job already exists")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to create snapshotter job")
		return ctrl.Result{}, err
	}

	log.Info("Snapshotter job created", "snapshotJob", snapshotterJobName)

	r.Recorder.Event(sandbox, corev1.EventTypeNormal, "SnapshottingStarted", "Snapshotter job created")

	if err := r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
		{
			Type:               SandboxFilesystemSnapshotCondition,
			Status:             metav1.ConditionFalse,
			Reason:             CondReasonSnapshottingInProgress,
			Message:            "Filesystem snapshotter in progress",
			ObservedGeneration: sandbox.Generation,
		},
	}); err != nil {
		log.Error(err, "Failed to update Sandbox status for snapshotting")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func getSandboxPodName(sandbox *sandboxv1alpha1.Sandbox) string {
	return sandbox.Name + "-pod"
}

func (r *SandboxReconciler) getSandboxPod(ctx context.Context, sandbox *sandboxv1alpha1.Sandbox) (*corev1.Pod, error) {
	podName := getSandboxPodName(sandbox)
	podNamespace := sandbox.Namespace

	sandboxPod := &corev1.Pod{}
	if err := r.Get(ctx, types.NamespacedName{Name: podName, Namespace: podNamespace}, sandboxPod); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}

	return sandboxPod, nil
}

func getFilesystemSnapshotterJobName(sandbox *sandboxv1alpha1.Sandbox) string {
	return sandbox.Name + "-fssnapshotter"
}

func (r *SandboxReconciler) getFilesystemSnapshotterJob(ctx context.Context, sandbox *sandboxv1alpha1.Sandbox) (*batchv1.Job, error) {
	jobName := getFilesystemSnapshotterJobName(sandbox)
	jobNamespace := sandbox.Namespace

	snapshotterJob := &batchv1.Job{}
	if err := r.Get(ctx, types.NamespacedName{Name: jobName, Namespace: jobNamespace}, snapshotterJob); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}

	return snapshotterJob, nil
}

func (r *SandboxReconciler) EnsureTemplate(ctx context.Context, sandbox *sandboxv1alpha1.Sandbox, baseSandbox *sandboxv1alpha1.Sandbox) (*sandboxv1alpha1.SandboxTemplate, ctrl.Result, error) {
	log := logf.FromContext(ctx).WithValues("sandbox", sandbox.Name, "namespace", sandbox.Namespace)
	template := &sandboxv1alpha1.SandboxTemplate{}

	if err := r.Get(ctx, types.NamespacedName{Name: sandbox.Spec.TemplateRef.Name, Namespace: sandbox.Namespace}, template); err != nil {
		if apierrors.IsNotFound(err) {

			if err := r.patchStatus(
				ctx,
				baseSandbox,
				sandbox,
				[]metav1.Condition{
					{
						Type:               SandboxTemplateReadyCondition,
						Status:             metav1.ConditionFalse,
						Reason:             CondReasonTemplateNotFound,
						Message:            "Sandbox template not found",
						ObservedGeneration: sandbox.Generation,
					},
					{
						Type:               SandboxReadyCondition,
						Status:             metav1.ConditionFalse,
						Reason:             CondReasonTemplateNotFound,
						Message:            "Sandbox template not found",
						ObservedGeneration: sandbox.Generation,
					},
				},
			); err != nil {
				log.Error(err, "Failed to update Sandbox status")
				return nil, ctrl.Result{}, err
			}

			log.Error(err, "Sandbox template not found")
			// todo benl: we'll stop reconciling (steady failed state) - add watch on SandboxTemplate to reconcile the sandbox when template is created
			return nil, ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get Sandbox template")
		return nil, ctrl.Result{}, err
	}

	if err := r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
		{
			Type:               SandboxTemplateReadyCondition,
			Status:             metav1.ConditionTrue,
			Reason:             CondReasonTemplateResolved,
			Message:            "Template resolved",
			ObservedGeneration: sandbox.Generation,
		},
	}); err != nil {
		log.Error(err, "Failed to update Sandbox status")
		return nil, ctrl.Result{}, err
	}

	return template, ctrl.Result{}, nil
}

// EnsureCustomNetworkPolicy creates a per-sandbox NetworkPolicy for custom CIDR, pod, or DNS rules.
// The policy is owned by the sandbox and garbage-collected on deletion.
// Returns nil if no custom rules are defined.
func (r *SandboxReconciler) EnsureCustomNetworkPolicy(
	ctx context.Context,
	sandbox *sandboxv1alpha1.Sandbox,
	baseSandbox *sandboxv1alpha1.Sandbox,
) error {
	log := logf.FromContext(ctx).WithValues("sandbox", sandbox.Name, "namespace", sandbox.Namespace)

	if !sandbox.HasCustomNetworkRules() {
		return nil
	}

	policyName := sandbox.GetCustomNetworkPolicyName()
	log = log.WithValues("networkPolicy", policyName)

	// Check if policy already exists
	existing := &networkingv1.NetworkPolicy{}
	err := r.Get(ctx, types.NamespacedName{Name: policyName, Namespace: sandbox.Namespace}, existing)
	if err == nil {
		// Policy exists - verify ownership and we're done
		if !metav1.IsControlledBy(existing, sandbox) {
			return fmt.Errorf("NetworkPolicy %q exists but is not owned by sandbox %q", policyName, sandbox.Name)
		}
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}

	// Build the NetworkPolicy for custom rules
	np, err := r.buildCustomNetworkPolicy(sandbox)
	if err != nil {
		log.Error(err, "Failed to build custom NetworkPolicy")
		if patchErr := r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
			{
				Type:               SandboxNetworkReadyCondition,
				Status:             metav1.ConditionFalse,
				Reason:             CondReasonNetworkPolicyFailed,
				Message:            err.Error(),
				ObservedGeneration: sandbox.Generation,
			},
		}); patchErr != nil {
			log.Error(patchErr, "Failed to update Sandbox status")
			return patchErr
		}
		return err
	}

	if err := controllerutil.SetControllerReference(sandbox, np, r.Scheme); err != nil {
		return err
	}

	log.Info("Creating custom NetworkPolicy")
	if err := r.Create(ctx, np); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return nil
		}
		return err
	}

	log.Info("Custom NetworkPolicy created")
	return nil
}

// buildCustomNetworkPolicy creates a NetworkPolicy for custom CIDR, pod, and DNS rules.
func (r *SandboxReconciler) buildCustomNetworkPolicy(sandbox *sandboxv1alpha1.Sandbox) (*networkingv1.NetworkPolicy, error) {
	networkConfig := sandbox.Spec.Network
	if networkConfig == nil {
		return nil, nil
	}

	np := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      sandbox.GetCustomNetworkPolicyName(),
			Namespace: sandbox.Namespace,
			Labels: map[string]string{
				sandboxv1alpha1.LabelSandboxName:    sandbox.Name,
				"app.kubernetes.io/managed-by":     "isola-operator",
				"app.kubernetes.io/component":      "network-policy",
			},
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					sandboxv1alpha1.LabelSandboxName: sandbox.Name,
				},
			},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeEgress,
			},
		},
	}

	var egressRules []networkingv1.NetworkPolicyEgressRule

	// Add DNS server egress rules (allow port 53 to custom DNS servers)
	if len(networkConfig.DNS) > 0 {
		dnsRule, err := buildDNSEgressRule(networkConfig.DNS)
		if err != nil {
			return nil, err
		}
		egressRules = append(egressRules, dnsRule)
	}

	// Add CIDR-based egress rules
	for _, cidrRule := range networkConfig.AllowedCIDRs {
		rule, err := buildCIDREgressRule(cidrRule)
		if err != nil {
			return nil, err
		}
		egressRules = append(egressRules, rule)
	}

	// Add pod-selector based egress rules
	for _, podRule := range networkConfig.AllowedPods {
		rule := buildPodEgressRule(podRule)
		egressRules = append(egressRules, rule)
	}

	np.Spec.Egress = egressRules
	return np, nil
}

// buildDNSEgressRule creates an egress rule allowing DNS traffic to specified servers.
func buildDNSEgressRule(dnsServers []string) (networkingv1.NetworkPolicyEgressRule, error) {
	udpProtocol := corev1.ProtocolUDP
	tcpProtocol := corev1.ProtocolTCP
	port53 := intstr.FromInt(53)

	var peers []networkingv1.NetworkPolicyPeer
	for _, ipStr := range dnsServers {
		addr, err := netip.ParseAddr(ipStr)
		if err != nil {
			return networkingv1.NetworkPolicyEgressRule{}, fmt.Errorf("invalid DNS server IP %q: %w", ipStr, err)
		}
		bits := 32
		if addr.Is6() {
			bits = 128
		}
		prefix := netip.PrefixFrom(addr, bits)
		peers = append(peers, networkingv1.NetworkPolicyPeer{
			IPBlock: &networkingv1.IPBlock{
				CIDR: prefix.String(),
			},
		})
	}

	return networkingv1.NetworkPolicyEgressRule{
		To: peers,
		Ports: []networkingv1.NetworkPolicyPort{
			{Protocol: &udpProtocol, Port: &port53},
			{Protocol: &tcpProtocol, Port: &port53},
		},
	}, nil
}

// buildCIDREgressRule creates an egress rule for a CIDR range.
func buildCIDREgressRule(rule sandboxv1alpha1.CIDREgressRule) (networkingv1.NetworkPolicyEgressRule, error) {
	_, err := netip.ParsePrefix(rule.CIDR)
	if err != nil {
		return networkingv1.NetworkPolicyEgressRule{}, fmt.Errorf("invalid CIDR %q: %w", rule.CIDR, err)
	}

	egressRule := networkingv1.NetworkPolicyEgressRule{
		To: []networkingv1.NetworkPolicyPeer{
			{
				IPBlock: &networkingv1.IPBlock{
					CIDR: rule.CIDR,
				},
			},
		},
	}

	if len(rule.Ports) > 0 {
		for _, p := range rule.Ports {
			protocol := p.Protocol
			if protocol == "" {
				protocol = corev1.ProtocolTCP
			}
			port := intstr.FromInt32(p.Port)
			egressRule.Ports = append(egressRule.Ports, networkingv1.NetworkPolicyPort{
				Protocol: &protocol,
				Port:     &port,
			})
		}
	}

	return egressRule, nil
}

// buildPodEgressRule creates an egress rule for pod selectors.
func buildPodEgressRule(rule sandboxv1alpha1.PodEgressRule) networkingv1.NetworkPolicyEgressRule {
	egressRule := networkingv1.NetworkPolicyEgressRule{
		To: []networkingv1.NetworkPolicyPeer{
			{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"kubernetes.io/metadata.name": rule.Namespace,
					},
				},
				PodSelector: &rule.PodSelector,
			},
		},
	}

	if len(rule.Ports) > 0 {
		for _, p := range rule.Ports {
			protocol := p.Protocol
			if protocol == "" {
				protocol = corev1.ProtocolTCP
			}
			port := intstr.FromInt32(p.Port)
			egressRule.Ports = append(egressRule.Ports, networkingv1.NetworkPolicyPort{
				Protocol: &protocol,
				Port:     &port,
			})
		}
	}

	return egressRule
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

func (r *SandboxReconciler) reconcileSandboxStatus(
	ctx context.Context,
	sandbox *sandboxv1alpha1.Sandbox,
	baseSandbox *sandboxv1alpha1.Sandbox,
	sandboxPod *corev1.Pod,
) error {
	var conditions []metav1.Condition

	podCondition := r.determinePodCondition(sandbox, sandboxPod)
	conditions = append(conditions, podCondition)

	networkCondition := r.determineNetworkCondition(sandbox)
	conditions = append(conditions, networkCondition)

	snapshotterJob, err := r.getFilesystemSnapshotterJob(ctx, sandbox)
	if err != nil {
		return err
	}
	snapshotCondition := r.determineSnapshotCondition(sandbox, snapshotterJob)
	conditions = append(conditions, snapshotCondition)

	readyCondition := r.determineReadyCondition(sandbox, sandboxPod)
	conditions = append(conditions, readyCondition)

	return r.patchStatus(ctx, baseSandbox, sandbox, conditions)
}

// determinePodCondition returns the PodReady condition based on the sandbox pod state.
func (r *SandboxReconciler) determinePodCondition(sandbox *sandboxv1alpha1.Sandbox, sandboxPod *corev1.Pod) metav1.Condition {
	if isPodReady(sandboxPod) {
		return metav1.Condition{
			Type:               SandboxPodReadyCondition,
			Status:             metav1.ConditionTrue,
			Reason:             CondReasonPodRunning,
			Message:            "Pod is running",
			ObservedGeneration: sandbox.Generation,
		}
	}

	if isPodTerminated(sandboxPod) {
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

func (r *SandboxReconciler) determineSnapshotCondition(sandbox *sandboxv1alpha1.Sandbox, snapshotterJob *batchv1.Job) metav1.Condition {
	if snapshotterJob == nil {
		return metav1.Condition{
			Type:               SandboxFilesystemSnapshotCondition,
			Status:             metav1.ConditionFalse,
			Reason:             ReasonFSSnapshotNotSnapshotting,
			Message:            "No filesystem snapshot in progress",
			ObservedGeneration: sandbox.Generation,
		}
	}

	if isJobComplete(snapshotterJob) {
		return metav1.Condition{
			Type:               SandboxFilesystemSnapshotCondition,
			Status:             metav1.ConditionTrue,
			Reason:             CondReasonSnapshotComplete,
			Message:            "Filesystem snapshot completed",
			ObservedGeneration: sandbox.Generation,
		}
	}

	if isJobFailed(snapshotterJob) {
		message := "Filesystem snapshot job failed"
		if condMsg := getJobConditionMessage(snapshotterJob, batchv1.JobFailed); condMsg != "" {
			message = fmt.Sprintf("Filesystem snapshot job failed: %s", condMsg)
		}
		return metav1.Condition{
			Type:               SandboxFilesystemSnapshotCondition,
			Status:             metav1.ConditionFalse,
			Reason:             CondReasonSnapshotFailed,
			Message:            message,
			ObservedGeneration: sandbox.Generation,
		}
	}

	return metav1.Condition{
		Type:               SandboxFilesystemSnapshotCondition,
		Status:             metav1.ConditionFalse,
		Reason:             ReasonFSSnapshotSnapshotting,
		Message:            "Filesystem snapshot job is running",
		ObservedGeneration: sandbox.Generation,
	}
}

// determineNetworkCondition returns the NetworkConfigured condition.
// With the label-based pattern, network is always configured via shared policies.
func (r *SandboxReconciler) determineNetworkCondition(sandbox *sandboxv1alpha1.Sandbox) metav1.Condition {
	// Network is always configured - labels are applied to pod and shared policies handle the rest.
	// Custom policies (if any) are created before pod creation.
	return metav1.Condition{
		Type:               SandboxNetworkReadyCondition,
		Status:             metav1.ConditionTrue,
		Reason:             CondReasonNetworkConfigured,
		Message:            "Network configuration applied",
		ObservedGeneration: sandbox.Generation,
	}
}

// determineReadyCondition returns the aggregate Ready condition.
// Sandbox is ready when pod is ready (network is always configured via labels).
func (r *SandboxReconciler) determineReadyCondition(sandbox *sandboxv1alpha1.Sandbox, sandboxPod *corev1.Pod) metav1.Condition {
	if isPodTerminated(sandboxPod) {
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

	if !isPodReady(sandboxPod) {
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

// +kubebuilder:rbac:groups=sandbox.isola.run,resources=sandboxes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=sandbox.isola.run,resources=sandboxes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=sandbox.isola.run,resources=sandboxes/finalizers,verbs=update
// +kubebuilder:rbac:groups=sandbox.isola.run,resources=sandboxtemplates,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=node.k8s.io,resources=runtimeclasses,verbs=get;list;watch
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;update;patch;delete

func (r *SandboxReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// todo benl: pass params by value sometimes, to avoid dereferencing nils by accident
	// todo benl: add r.RecordEvent for events (observability)
	log := logf.FromContext(ctx).WithValues("sandbox", req.Name, "namespace", req.Namespace)

	log.Info("Reconciling Sandbox")

	sandbox := &sandboxv1alpha1.Sandbox{}
	if err := r.Get(ctx, req.NamespacedName, sandbox); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("Sandbox not found")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get Sandbox")
		return ctrl.Result{}, err
	}

	log.Info("Sandbox found")

	// DeepCopy to allow patching only the diff between the new sandbox and the old one
	baseSandbox := sandbox.DeepCopy()

	if sandbox.Status.Conditions == nil {
		sandbox.Status.Conditions = []metav1.Condition{}
	}

	sandboxDeleted := !sandbox.DeletionTimestamp.IsZero()
	noFinalizer := !controllerutil.ContainsFinalizer(sandbox, SandboxFinalizer)

	if sandboxDeleted && noFinalizer {
		return ctrl.Result{}, nil
	}

	// Add finalizer first, before any other operations
	if !sandboxDeleted && noFinalizer {
		log.Info("Adding finalizer to sandbox")
		controllerutil.AddFinalizer(sandbox, SandboxFinalizer)
		if err := r.Update(ctx, sandbox); err != nil {
			log.Error(err, "Failed to add finalizer")
			return ctrl.Result{}, err
		}
		// Update baseSandbox to reflect the finalizer change for subsequent patches
		baseSandbox = sandbox.DeepCopy()
	}

	template, result, err := r.EnsureTemplate(ctx, sandbox, baseSandbox)
	if err != nil {
		return result, err
	}

	if sandboxDeleted && !noFinalizer {
		if template == nil {
			// Template not found during deletion - can't determine shutdown policy
			// Remove finalizer and allow deletion to proceed
			log.Info("Template not found during deletion; removing finalizer without executing shutdown policy")
			controllerutil.RemoveFinalizer(sandbox, SandboxFinalizer)
			if err := r.Update(ctx, sandbox); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, nil
		}

		res, _, err := r.finalizeSandbox(ctx, sandbox, baseSandbox, template)
		return res, err
	}

	if template == nil {
		log.Info("Template not found, waiting", "template", sandbox.Spec.TemplateRef.Name)
		return ctrl.Result{}, nil
	}

	// Create per-sandbox NetworkPolicy for custom rules (if any)
	if err := r.EnsureCustomNetworkPolicy(ctx, sandbox, baseSandbox); err != nil {
		return ctrl.Result{}, err
	}

	sandboxPod, err := r.getSandboxPod(ctx, sandbox)
	if err != nil {
		return ctrl.Result{}, err
	}

	// whether we created the pod or not, check for sandbox timeout before proceeding:
	optionalTimeoutAt, err := r.ensureTimeout(ctx, sandbox, baseSandbox, template, sandboxPod)
	if err != nil {
		return ctrl.Result{}, err
	}

	if optionalTimeoutAt != nil && r.clock().Now().After(optionalTimeoutAt.Time) {
		log.Info("Sandbox timed out")

		res, cleanupDone, err := r.finalizeSandbox(ctx, sandbox, baseSandbox, template)
		if err != nil {
			return res, err
		}

		if cleanupDone {
			if err := r.Delete(ctx, sandbox); err != nil {
				log.Error(err, "Failed to delete sandbox after cleanup")
				return ctrl.Result{}, err
			}
		}
		// if cleanUp is not done, return res that might ask for a requeue in the future
		return res, nil
	}

	var timeUntilTimeout time.Duration
	if optionalTimeoutAt != nil {
		timeUntilTimeout = r.clock().Until(optionalTimeoutAt.Time)
		if timeUntilTimeout <= 0 {
			// in case of some very bad luck where the timeout shifted right after we checked for it
			timeUntilTimeout = time.Millisecond
		}
	} else {
		timeUntilTimeout = 0 // ctrl.Result{0} is effectively ctrl.Result{} (no scheduled requeue)
	}

	if sandboxPod == nil {
		if err := r.CreateSandboxPod(ctx, sandbox, baseSandbox, template); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.reconcileSandboxStatus(ctx, sandbox, baseSandbox, nil); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: timeUntilTimeout}, nil
	}

	if err := r.reconcileSandboxStatus(ctx, sandbox, baseSandbox, sandboxPod); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: timeUntilTimeout}, nil
}

// finalize the sandbox according to the shutdown policy and have it ready to be deleted.
// Returns:
// ctrl.Result: a result object that might ask for a requeue if another reconciliation would be required.
// bool: whether the cleanup was fully completed (and thus the sandbox can be deleted) or not.
// an error if something went wrong.
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
	case sandboxv1alpha1.ShutdownPolicySnapshotFilesystem:
		return r.handleFilesystemSnapshot(ctx, sandbox, baseSandbox, sandboxPod, snapshotDeadline, r.getActiveDeadlineSeconds(template))
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

func (r *SandboxReconciler) handleFilesystemSnapshot(
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
		log.Info("Filesystem snapshot timed out", "deadline", snapshotDeadline)
		if err := r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
			{
				Type:               SandboxFilesystemSnapshotCondition,
				Status:             metav1.ConditionFalse,
				Reason:             CondReasonSnapshotTimeout,
				Message:            "Filesystem snapshot did not complete before deadline",
				ObservedGeneration: sandbox.Generation,
			},
		}); err != nil {
			return ctrl.Result{}, false, err
		}
		return ctrl.Result{}, true, nil
	}

	if sandboxPod == nil {
		log.Info("Skipping filesystem snapshot because sandbox pod is missing")
		if err := r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
			{
				Type:               SandboxFilesystemSnapshotCondition,
				Status:             metav1.ConditionFalse,
				Reason:             ReasonFSSnapshotPodDoesNotExist,
				Message:            "Sandbox pod no longer exists; snapshot skipped",
				ObservedGeneration: sandbox.Generation,
			},
		}); err != nil {
			return ctrl.Result{}, false, err
		}
		return ctrl.Result{}, true, nil
	}

	reason, err := r.verifySnapshottingCapability(ctx, sandboxPod)
	if err != nil {
		log.Error(err, "Failed to validate snapshotting support")
		return ctrl.Result{}, false, err
	}

	if reason != ReasonFSSnapshotSupported {
		if reason == ReasonFSSnapshotPodNotReady {
			if err := r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
				{
					Type:               SandboxFilesystemSnapshotCondition,
					Status:             metav1.ConditionFalse,
					Reason:             CondReasonSnapshottingInProgress,
					Message:            "Waiting for sandbox pod to become ready for snapshotting",
					ObservedGeneration: sandbox.Generation,
				},
			}); err != nil {
				return ctrl.Result{}, false, err
			}
			return ctrl.Result{RequeueAfter: time.Second}, false, nil
		}

		log.Info("Unable to perform filesystem snapshot", "reason", reason)
		r.Recorder.Event(sandbox, corev1.EventTypeWarning, reason, "Unable to perform filesystem snapshot")

		if err := r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
			{
				Type:               SandboxFilesystemSnapshotCondition,
				Status:             metav1.ConditionFalse,
				Reason:             reason,
				Message:            "Unable to perform filesystem snapshot",
				ObservedGeneration: sandbox.Generation,
			},
		}); err != nil {
			return ctrl.Result{}, false, err
		}
		return ctrl.Result{}, true, nil
	}

	snapshotterJob, err := r.getFilesystemSnapshotterJob(ctx, sandbox)
	if err != nil {
		return ctrl.Result{}, false, err
	}
	if snapshotterJob == nil {
		res, createErr := r.CreateSnapshotterJob(ctx, sandbox, baseSandbox, sandboxPod, activeDeadlineSeconds)
		if res.RequeueAfter == 0 {
			res.RequeueAfter = time.Second
		}
		return res, false, createErr
	}

	// Avoid waiting forever if we are close to the deadline.
	if r.clock().Now().After(snapshotDeadline) {
		log.Info("Filesystem snapshot timed out before completion", "deadline", snapshotDeadline)
		if err := r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
			{
				Type:               SandboxFilesystemSnapshotCondition,
				Status:             metav1.ConditionFalse,
				Reason:             CondReasonSnapshotTimeout,
				Message:            "Filesystem snapshot did not complete before deadline",
				ObservedGeneration: sandbox.Generation,
			},
		}); err != nil {
			return ctrl.Result{}, false, err
		}
		return ctrl.Result{}, true, nil
	}

	if isJobComplete(snapshotterJob) {
		log.Info("Filesystem snapshot job succeeded")
		r.Recorder.Event(sandbox, corev1.EventTypeNormal, "SnapshotSucceeded", "Filesystem snapshot job completed")
		if err := r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
			{
				Type:               SandboxFilesystemSnapshotCondition,
				Status:             metav1.ConditionTrue,
				Reason:             CondReasonSnapshotComplete,
				Message:            "Filesystem snapshot completed",
				ObservedGeneration: sandbox.Generation,
			},
		}); err != nil {
			return ctrl.Result{}, false, err
		}
		return ctrl.Result{}, true, nil
	}

	if isJobFailed(snapshotterJob) {
		failureMessage := "Filesystem snapshot job failed"
		if condMsg := getJobConditionMessage(snapshotterJob, batchv1.JobFailed); condMsg != "" {
			failureMessage = fmt.Sprintf("Filesystem snapshot job failed: %s", condMsg)
		}
		log.Info(failureMessage)
		r.Recorder.Event(sandbox, corev1.EventTypeWarning, "SnapshotFailed", failureMessage)
		if err := r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
			{
				Type:               SandboxFilesystemSnapshotCondition,
				Status:             metav1.ConditionFalse,
				Reason:             CondReasonSnapshotFailed,
				Message:            failureMessage,
				ObservedGeneration: sandbox.Generation,
			},
		}); err != nil {
			return ctrl.Result{}, false, err
		}
		return ctrl.Result{}, true, nil
	}

	if err := r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
		{
			Type:               SandboxFilesystemSnapshotCondition,
			Status:             metav1.ConditionFalse,
			Reason:             CondReasonSnapshottingInProgress,
			Message:            "Filesystem snapshotter job is running",
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

// SetupWithManager sets up the controller with the Manager.
func (r *SandboxReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Recorder = mgr.GetEventRecorderFor("sandbox-controller")

	// Field index for sandbox templateRef lookups
	if err := mgr.GetFieldIndexer().IndexField(
		context.Background(),
		&sandboxv1alpha1.Sandbox{},
		sandboxTemplateRefField,
		extractTemplateRefName,
	); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&sandboxv1alpha1.Sandbox{}).
		Owns(&corev1.Pod{}).
		Owns(&batchv1.Job{}).
		Owns(&networkingv1.NetworkPolicy{}).
		// Watch SandboxTemplate changes to reconcile affected sandboxes
		Watches(
			&sandboxv1alpha1.SandboxTemplate{},
			handler.EnqueueRequestsFromMapFunc(r.findSandboxesForTemplate),
		).
		Named("sandbox").
		Complete(r)
}

func extractTemplateRefName(obj client.Object) []string {
	sandbox, ok := obj.(*sandboxv1alpha1.Sandbox)
	if !ok || sandbox.Spec.TemplateRef.Name == "" {
		return nil
	}
	return []string{sandbox.Spec.TemplateRef.Name}
}

func (r *SandboxReconciler) findSandboxesForTemplate(ctx context.Context, template client.Object) []reconcile.Request {
	sandboxList := &sandboxv1alpha1.SandboxList{}
	if err := r.List(ctx, sandboxList,
		client.InNamespace(template.GetNamespace()),
		client.MatchingFields{sandboxTemplateRefField: template.GetName()},
	); err != nil {
		return nil
	}

	requests := make([]reconcile.Request, 0, len(sandboxList.Items))
	for _, sandbox := range sandboxList.Items {
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      sandbox.Name,
				Namespace: sandbox.Namespace,
			},
		})
	}
	return requests
}
