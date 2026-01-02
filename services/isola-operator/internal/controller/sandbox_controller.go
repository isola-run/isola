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
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	nodev1 "k8s.io/api/node/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
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
	// todo benl: maybe keep just sandbox.Status.Snapshot and no snapshotting condition on sandbox (consider custom CRD for snapshotting)
	SandboxFilesystemSnapshotCondition = "FilesystemSnapshot"
)

const (
	CondReasonTemplateNotFound = "TemplateNotFound"

	CondReasonPodPending      = "PodPending"
	CondReasonPodRunning      = "PodRunning"
	CondReasonPodFailed       = "PodFailed"
	CondReasonPodSucceeded    = "PodSucceeded"
	CondReasonSandboxTimedOut = "TimedOut"

	// Snapshot-related reasons
	CondReasonSnapshottingInProgress = "SnapshottingInProgress"
	CondReasonSnapshotComplete       = "SnapshotComplete"
	CondReasonSnapshotFailed         = "SnapshotFailed"
	CondReasonSnapshotTimeout        = "SnapshotTimeout"
	CondReasonInvalidRuntime         = "InvalidRuntime"
)

const defaultSnapshotTimeoutSeconds int64 = 300

type SandboxReconciler struct {
	client.Client
	Scheme                *runtime.Scheme
	Recorder              record.EventRecorder
	AgentImage            string
	SharedVolumeMountPath string
	Clock                 Clock // Clock interface for time operations, allows mocking in tests
}

const (
	// Shared volume name for communication between sandbox container and agent sidecar
	sharedVolumeName = "sandbox-shared"
	// Default mount path for shared volume
	defaultSharedVolumeMountPath = "/sandbox-shared"
	agentContainerName           = "isola-agent"

	// Field index for efficient lookup of sandboxes by templateRef
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
		Name:  "isola-agent",
		Image: r.AgentImage,
		RestartPolicy: rp,
		// RunAsUser 0 (root) is needed to read /proc/<pid>/environ of other users' processes
		// and to access /proc/<pid>/root when using shared PID namespace.
		SecurityContext: &corev1.SecurityContext{
			RunAsUser: ptr.To(int64(0)),
		},
	}
}

func (r *SandboxReconciler) injectSidecar(sandboxPod *corev1.Pod) error {
	if len(sandboxPod.Spec.Containers) != 0 {
		// todo: remove this assumption
		return fmt.Errorf("Sandbox pod must have exactly one container")
	}

	// Mark the first container as the main container so the agent can discover it via /proc/<pid>/environ.
	// Note: a single main container is supported. The agent's findMarkedProcess() returns the first PID it finds with the ISOLA_MAIN_CONTAINER marker.
	sandboxPod.Spec.Containers[0].Env = append(sandboxPod.Spec.Containers[0].Env, corev1.EnvVar{
		Name:  "ISOLA_MAIN_CONTAINER",
		Value: "true",
	})

	// Add agent sidecar container
	agentContainer := r.buildAgentContainer()
	sandboxPod.Spec.Containers = append(sandboxPod.Spec.InitContainers, agentContainer)
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

func describePodContainerState(pod *corev1.Pod) string {
	if pod == nil || len(pod.Status.ContainerStatuses) == 0 {
		return "container status unavailable"
	}

	cs := pod.Status.ContainerStatuses[0]
	if cs.State.Terminated != nil {
		term := cs.State.Terminated
		return fmt.Sprintf("terminated: reason=%s exitCode=%d message=%s", term.Reason, term.ExitCode, term.Message)
	}
	if cs.State.Waiting != nil {
		wait := cs.State.Waiting
		return fmt.Sprintf("waiting: reason=%s message=%s", wait.Reason, wait.Message)
	}
	if cs.State.Running != nil {
		return "running"
	}
	return "unknown container state"
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
)

func (r *SandboxReconciler) verifySnapshottingCapability(ctx context.Context, sandbox *sandboxv1alpha1.Sandbox, sandboxPod *corev1.Pod) (string, error) {
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
	}

	// todo benl: why this exists? ("sandbox-id")
	if sandbox.Labels != nil {
		if sandboxID, exists := sandbox.Labels["sandbox-id"]; exists {
			labels["sandbox-id"] = sandboxID
		}
	}

	if template.Spec.PodTemplate.Labels != nil {
		for k, v := range template.Spec.PodTemplate.Labels {
			labels[k] = v
		}
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

	// Enable shared PID namespace so the isola agent can locate the main container and access it's filesystem via /proc/<pid>/root
	sandboxPod.Spec.ShareProcessNamespace = ptr.To(true)

	// todo benl: implement api to restore pod from snapshot (make sure they are compatible)
	// if sandboxPod.Annotations == nil {
	// 	sandboxPod.Annotations = map[string]string{}
	// }

	// sandboxPod.Annotations["dev.gvisor.tar.rootfs.upper.todobenl"] = "/tmp/rootfs-sandbox-870e5846-1766869560.tar"

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
				Reason:             "PodCreationFailed",
				Message:            err.Error(),
				ObservedGeneration: sandbox.Generation,
			},
			{
				Type:               SandboxReadyCondition,
				Status:             metav1.ConditionFalse,
				Reason:             "PodCreationFailed",
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
			Reason:             "PodCreating",
			Message:            "Creating sandbox Pod",
			ObservedGeneration: sandbox.Generation,
		},
		{
			Type:               SandboxReadyCondition,
			Status:             metav1.ConditionFalse,
			Reason:             "Reconciling",
			Message:            "Waiting for Pod to be created/ready",
			ObservedGeneration: sandbox.Generation,
		},
	}); err != nil {
		log.Error(err, "Failed to update Sandbox status")
		return err
	}

	return nil
}

// CreateSnapshotterPod creates a privileged pod to snapshot the sandbox container's filesystem
func (r *SandboxReconciler) CreateSnapshotterPod(
	ctx context.Context,
	sandbox *sandboxv1alpha1.Sandbox,
	baseSandbox *sandboxv1alpha1.Sandbox,
	sandboxPod *corev1.Pod,
) (ctrl.Result, error) {
	// todo benl: reduce linux capabilities of snapshot pod to only what is needed
	log := logf.FromContext(ctx).WithValues("sandbox", sandbox.Name, "namespace", sandbox.Namespace)

	snapshotterPodName := getFilesystemSnapshotterPodName(sandbox)
	nodeName := sandboxPod.Spec.NodeName
	timestamp := r.clock().Now().Unix()
	snapshotPath := fmt.Sprintf("/tmp/rootfs-%s-%d.tar", sandbox.Name, timestamp)

	containerID, err := extractContainerID(sandboxPod)
	if err != nil {
		log.Error(err, "Failed to extract container ID")
		return ctrl.Result{}, err
	}

	log.Info("Creating filesystem snapshotter pod", "pod", snapshotterPodName, "node", nodeName)

	privileged := false
	hostPathDirectory := corev1.HostPathDirectory
	hostPathFile := corev1.HostPathFile

	// todo benl: add labels / annotations
	// todo benl: create a minimal image (possibly with runsc backed in with a fixed version)
	snapshotPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      snapshotterPodName,
			Namespace: sandbox.Namespace,
		},
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
	}

	// Set owner reference to sandbox for cleanup
	if err := controllerutil.SetControllerReference(sandbox, snapshotPod, r.Scheme); err != nil {
		log.Error(err, "Failed to set controller reference for snapshot pod")
		return ctrl.Result{}, err
	}

	if err := r.Create(ctx, snapshotPod); err != nil {
		if apierrors.IsAlreadyExists(err) {
			log.Info("Snapshotter pod already exists")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to create snapshotter pod")
		return ctrl.Result{}, err
	}

	log.Info("Snapshotter pod created", "snapshotPod", snapshotterPodName)

	r.Recorder.Event(sandbox, corev1.EventTypeNormal, "SnapshottingStarted", "Snapshotter pod created")

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

func getFilesystemSnapshotterPodName(sandbox *sandboxv1alpha1.Sandbox) string {
	return sandbox.Name + "-fssnapshotter"
}

func (r *SandboxReconciler) getFilesystemSnapshotterPod(ctx context.Context, sandbox *sandboxv1alpha1.Sandbox) (*corev1.Pod, error) {
	podName := getFilesystemSnapshotterPodName(sandbox)
	podNamespace := sandbox.Namespace

	snapshotterPod := &corev1.Pod{}
	if err := r.Get(ctx, types.NamespacedName{Name: podName, Namespace: podNamespace}, snapshotterPod); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}

	return snapshotterPod, nil
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

	meta.SetStatusCondition(&sandbox.Status.Conditions, metav1.Condition{
		Type:               SandboxTemplateReadyCondition,
		Status:             metav1.ConditionTrue,
		Reason:             "TemplateOK",
		Message:            "Template resolved",
		ObservedGeneration: sandbox.Generation,
	})

	return template, ctrl.Result{}, nil
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
		log.Info("deduced start time from sandbox", "startTime", sandbox.ObjectMeta.CreationTimestamp.Time)
		startTime = sandbox.ObjectMeta.CreationTimestamp.Time
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

func (r *SandboxReconciler) reconcileSandboxStatus(ctx context.Context, sandbox *sandboxv1alpha1.Sandbox, baseSandbox *sandboxv1alpha1.Sandbox, sandboxPod *corev1.Pod) error {
	var lastError error
	var conditions []metav1.Condition

	if isPodReady(sandboxPod) {
		conditions = []metav1.Condition{
			{
				Type:               SandboxPodReadyCondition,
				Status:             metav1.ConditionTrue,
				Reason:             CondReasonPodRunning,
				Message:            "Pod is running",
				ObservedGeneration: sandbox.Generation,
			},
			{
				Type:               SandboxReadyCondition,
				Status:             metav1.ConditionTrue,
				Reason:             CondReasonPodRunning,
				Message:            "Pod is running",
				ObservedGeneration: sandbox.Generation,
			},
		}
	} else if isPodTerminated(sandboxPod) {
		reason := CondReasonPodFailed
		if sandboxPod.Status.Phase == corev1.PodSucceeded {
			reason = CondReasonPodSucceeded
		}
		message := describePodContainerState(sandboxPod)
		conditions = []metav1.Condition{
			{
				Type:               SandboxPodReadyCondition,
				Status:             metav1.ConditionFalse,
				Reason:             reason,
				Message:            message,
				ObservedGeneration: sandbox.Generation,
			},
			{
				Type:               SandboxReadyCondition,
				Status:             metav1.ConditionFalse,
				Reason:             reason,
				Message:            message,
				ObservedGeneration: sandbox.Generation,
			},
		}
	} else {
		conditions = []metav1.Condition{
			{
				Type:               SandboxPodReadyCondition,
				Status:             metav1.ConditionFalse,
				Reason:             CondReasonPodPending,
				Message:            "Pod is not ready yet",
				ObservedGeneration: sandbox.Generation,
			},
			{
				Type:               SandboxReadyCondition,
				Status:             metav1.ConditionFalse,
				Reason:             CondReasonPodPending,
				Message:            "Pod is not ready yet",
				ObservedGeneration: sandbox.Generation,
			},
		}
	}

	if err := r.patchStatus(ctx, baseSandbox, sandbox, conditions); err != nil {
		lastError = err
	}

	snapshotterPod, err := r.getFilesystemSnapshotterPod(ctx, sandbox)
	if err != nil {
		lastError = err
	}

	if snapshotterPod == nil {
		return lastError
	}

	isSnapshotterPodReady := isPodReady(snapshotterPod)

	if isSnapshotterPodReady {
		if err := r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
			{
				Type:               SandboxFilesystemSnapshotCondition,
				Status:             metav1.ConditionTrue,
				Reason:             ReasonFSSnapshotSnapshotting,
				Message:            "Filesystem snapshot is being taken",
				ObservedGeneration: sandbox.Generation,
			},
		}); err != nil {
			lastError = err
		}
	} else {
		// todo benl: use isPodTerminated(snapshotterPod)
		if err := r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
			{
				Type:               SandboxFilesystemSnapshotCondition,
				Status:             metav1.ConditionFalse,
				Reason:             ReasonFSSnapshotPodNotReady,
				Message:            "Filesystem snapshotter pod is not ready yet",
				ObservedGeneration: sandbox.Generation,
			},
		}); err != nil {
			lastError = err
		}
	}

	return lastError
}

// +kubebuilder:rbac:groups=sandbox.isola.run,resources=sandboxes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=sandbox.isola.run,resources=sandboxes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=sandbox.isola.run,resources=sandboxtemplates,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=node.k8s.io,resources=runtimeclasses,verbs=get;list;watch
func (r *SandboxReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	//todo benl: pass params by value sometimes, to avoid dereferencing nils by accident
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

	// todo benl: this will make the object never disappear if template doesn't exist (we can't even have a sandbox timeout without the template)
	template, result, err := r.EnsureTemplate(ctx, sandbox, baseSandbox)
	if err != nil {
		return result, err
	}
	if template == nil {
		return ctrl.Result{}, nil
	}

	if !sandbox.DeletionTimestamp.IsZero() {
		log.Info("Sandbox already marked for deletion; skipping further reconciliation")
		return ctrl.Result{}, nil
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
		cleanupResult, cleanupDone, err := r.cleanupTimedOutSandbox(ctx, sandbox, baseSandbox, template, sandboxPod, optionalTimeoutAt.Time)
		if err != nil {
			return cleanupResult, err
		}
		if !cleanupDone {
			return cleanupResult, nil
		}

		if err := r.Delete(ctx, sandbox); err != nil {
			log.Error(err, "Failed to delete sandbox after cleanup")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	var requeueAfter time.Duration
	if optionalTimeoutAt != nil {
		requeueAfter = r.clock().Until(optionalTimeoutAt.Time)
		if requeueAfter <= 0 {
			// in case of some very bad luck where the timeout shifted right after we checked for it
			requeueAfter = time.Second
		}
	} else {
		requeueAfter = 0 // ctrl.Result{0} is effectively ctrl.Result{} (no scheduled requeue)
	}

	if sandboxPod == nil {
		// we'll reconcile again once the pod is up
		if err := r.CreateSandboxPod(ctx, sandbox, baseSandbox, template); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: requeueAfter}, nil
	}

	if err := r.reconcileSandboxStatus(ctx, sandbox, baseSandbox, sandboxPod); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

func (r *SandboxReconciler) cleanupTimedOutSandbox(
	ctx context.Context,
	sandbox *sandboxv1alpha1.Sandbox,
	baseSandbox *sandboxv1alpha1.Sandbox,
	template *sandboxv1alpha1.SandboxTemplate,
	sandboxPod *corev1.Pod,
	timeoutAt time.Time,
) (ctrl.Result, bool, error) {
	log := logf.FromContext(ctx).WithValues("sandbox", sandbox.Name, "namespace", sandbox.Namespace)

	if template.Spec.ShutdownPolicy == nil || template.Spec.ShutdownPolicy.Policy == sandboxv1alpha1.ShutdownPolicyDelete {
		if err := r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
			{
				Type:               SandboxReadyCondition,
				Status:             metav1.ConditionFalse,
				Reason:             CondReasonSandboxTimedOut,
				Message:            "Sandbox timed out; deleting",
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
			Message:            "Sandbox timed out; executing shutdown policy",
			ObservedGeneration: sandbox.Generation,
		},
	}); err != nil {
		return ctrl.Result{}, false, err
	}

	switch template.Spec.ShutdownPolicy.Policy {
	case sandboxv1alpha1.ShutdownPolicySnapshotFilesystem:
		snapshotTimeoutSeconds := defaultSnapshotTimeoutSeconds
		if template.Spec.ShutdownPolicy.SnapshotTimeoutSeconds != nil {
			snapshotTimeoutSeconds = *template.Spec.ShutdownPolicy.SnapshotTimeoutSeconds
		}
		deadline := timeoutAt.Add(time.Duration(snapshotTimeoutSeconds) * time.Second)
		return r.handleFilesystemSnapshot(ctx, sandbox, baseSandbox, sandboxPod, deadline)
	default:
		log.Info("Unknown shutdown policy; proceeding with deletion", "policy", template.Spec.ShutdownPolicy.Policy)
		return ctrl.Result{}, true, nil
	}
}

func (r *SandboxReconciler) handleFilesystemSnapshot(
	ctx context.Context,
	sandbox *sandboxv1alpha1.Sandbox,
	baseSandbox *sandboxv1alpha1.Sandbox,
	sandboxPod *corev1.Pod,
	snapshotDeadline time.Time,
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

	reason, err := r.verifySnapshottingCapability(ctx, sandbox, sandboxPod)
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

	snapshotterPod, err := r.getFilesystemSnapshotterPod(ctx, sandbox)
	if err != nil {
		return ctrl.Result{}, false, err
	}
	if snapshotterPod == nil {
		res, createErr := r.CreateSnapshotterPod(ctx, sandbox, baseSandbox, sandboxPod)
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

	switch snapshotterPod.Status.Phase {
	case corev1.PodSucceeded:
		stateDesc := describePodContainerState(snapshotterPod)
		log.Info("Filesystem snapshot pod succeeded", "state", stateDesc)
		r.Recorder.Event(sandbox, corev1.EventTypeNormal, "SnapshotSucceeded", stateDesc)
		if err := r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
			{
				Type:               SandboxFilesystemSnapshotCondition,
				Status:             metav1.ConditionTrue,
				Reason:             CondReasonSnapshotComplete,
				Message:            fmt.Sprintf("Filesystem snapshot completed (%s)", stateDesc),
				ObservedGeneration: sandbox.Generation,
			},
		}); err != nil {
			return ctrl.Result{}, false, err
		}
		return ctrl.Result{}, true, nil
	case corev1.PodFailed:
		stateDesc := describePodContainerState(snapshotterPod)
		log.Info("Filesystem snapshot pod failed", "state", stateDesc)
		r.Recorder.Event(sandbox, corev1.EventTypeWarning, "SnapshotFailed", stateDesc)
		if err := r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
			{
				Type:               SandboxFilesystemSnapshotCondition,
				Status:             metav1.ConditionFalse,
				Reason:             CondReasonSnapshotFailed,
				Message:            fmt.Sprintf("Filesystem snapshot pod failed (%s)", stateDesc),
				ObservedGeneration: sandbox.Generation,
			},
		}); err != nil {
			return ctrl.Result{}, false, err
		}
		return ctrl.Result{}, true, nil
	default:
		if err := r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
			{
				Type:               SandboxFilesystemSnapshotCondition,
				Status:             metav1.ConditionFalse,
				Reason:             CondReasonSnapshottingInProgress,
				Message:            "Filesystem snapshotter pod is running",
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
}

// SetupWithManager sets up the controller with the Manager.
func (r *SandboxReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Recorder = mgr.GetEventRecorderFor("sandbox-controller")

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
		Watches(
			&sandboxv1alpha1.SandboxTemplate{},
			handler.EnqueueRequestsFromMapFunc(r.findSandboxesForTemplate),
		).
		Named("sandbox").
		Complete(r)
}

func extractTemplateRefName(obj client.Object) []string {
	sandbox, ok := obj.(*sandboxv1alpha1.Sandbox)
	if !ok || sandbox.Spec.TemplateRef == nil || sandbox.Spec.TemplateRef.Name == "" {
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
