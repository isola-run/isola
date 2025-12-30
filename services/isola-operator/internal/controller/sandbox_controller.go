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
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	sandboxv1alpha1 "github.com/omereli/dev-isola/services/isola-operator/api/v1alpha1"
	"k8s.io/client-go/tools/record"
)

const SandboxFinalizer = "isola.run/sandbox-finalizer"

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

	CondReasonPodPending = "PodPending"
	CondReasonPodRunning = "PodRunning"

	// Snapshot-related reasons
	CondReasonSnapshottingInProgress = "SnapshottingInProgress"
	CondReasonSnapshotComplete       = "SnapshotComplete"
	CondReasonSnapshotFailed         = "SnapshotFailed"
	CondReasonSnapshotTimeout        = "SnapshotTimeout"
	CondReasonInvalidRuntime         = "InvalidRuntime"
)


// SandboxReconciler reconciles a Sandbox object
type SandboxReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
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

	sandboxPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      getSandboxPodName(sandbox),
			Namespace: sandbox.Namespace,
		},
		// todo benl: copy labels, annotations as well?
		Spec: template.Spec.PodTemplate.Spec,
	}
	// todo benl: implement api to restore pod from snapshot (make sure they are compatible)
	// if sandboxPod.Annotations == nil {
	// 	sandboxPod.Annotations = map[string]string{}
	// }

	// sandboxPod.Annotations["dev.gvisor.tar.rootfs.upper.todobenl"] = "/tmp/rootfs-sandbox-870e5846-1766869560.tar"

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
	timestamp := time.Now().Unix()
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

	// todo benl: inject clock for testability instead of using .Until that uses .Now() internally
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
	podReady := isPodReady(sandboxPod)

	var lastError error

	if podReady {
		if err := r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
			{
				Type:               SandboxPodReadyCondition,
				Status:             metav1.ConditionTrue,
				Reason:             "PodRunning",
				Message:            "Pod is running",
				ObservedGeneration: sandbox.Generation,
			},
			{
				Type:               SandboxReadyCondition,
				Status:             metav1.ConditionTrue,
				Reason:             "PodRunning",
				Message:            "Pod is running",
				ObservedGeneration: sandbox.Generation,
			},
		}); err != nil {
			lastError = err
		}
	} else {
		if err := r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
			{
				Type:               SandboxPodReadyCondition,
				Status:             metav1.ConditionFalse,
				Reason:             "PodPending",
				Message:            "Pod is not ready yet",
				ObservedGeneration: sandbox.Generation,
			},
			{
				Type:               SandboxReadyCondition,
				Status:             metav1.ConditionFalse,
				Reason:             "PodPending",
				Message:            "Pod is not ready yet",
				ObservedGeneration: sandbox.Generation,
			},
		}); err != nil {
			lastError = err
		}
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
// +kubebuilder:rbac:groups=sandbox.isola.run,resources=sandboxes/finalizers,verbs=update
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

	// todo benl: if we set finalizers, k8s will set sandbox.ObjectMeta.DeletionTimestamp for us to cleanup with finalizers. currently no finalizers
	// relying on sandbox resource owning other objects like pods

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

	// finalization logic:
	// {optional sandbox timeout} -> Delete sandbox -> Finalizers run snapshotting if needed (have until the sandbox deletionGracePeriodSeconds)
	// if shutdown policy is simply delete, no need to apply a finalizer for cleanups
	hasNonTrivialCleanup := template.Spec.ShutdownPolicy != nil && template.Spec.ShutdownPolicy.Policy != sandboxv1alpha1.ShutdownPolicyDelete
	// set finalizers before creating any resource:
	if hasNonTrivialCleanup {
		if !controllerutil.ContainsFinalizer(sandbox, SandboxFinalizer) {
			controllerutil.AddFinalizer(sandbox, SandboxFinalizer)
			if err := r.Update(ctx, sandbox); err != nil {
				log.Error(err, "Failed to add finalizer")
				return ctrl.Result{}, err
			}
		}
	}

	sandboxDeleted := !sandbox.DeletionTimestamp.IsZero()
	if hasNonTrivialCleanup && sandboxDeleted {
		return r.finalizeSandbox(ctx, template, sandbox, baseSandbox, nil)
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
	if optionalTimeoutAt != nil && time.Now().After(optionalTimeoutAt.Time) {
		log.Info("Sandbox timed out")
		if err := r.Delete(ctx, sandbox); err != nil {
			log.Error(err, "Failed to delete sandbox")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	var requeueAfter time.Duration
	if optionalTimeoutAt != nil {
		requeueAfter = time.Until(optionalTimeoutAt.Time)
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

func (r *SandboxReconciler) handleFilesystemSnapshot(
	ctx context.Context,
	sandbox *sandboxv1alpha1.Sandbox,
	baseSandbox *sandboxv1alpha1.Sandbox,
	sandboxPod *corev1.Pod,
) (ctrl.Result, error) {
	log := logf.FromContext(ctx).WithValues("sandbox", sandbox.Name, "namespace", sandbox.Namespace)

	reason, err := r.verifySnapshottingCapability(ctx, sandbox, sandboxPod)
	if err != nil {
		log.Error(err, "Failed to validate snapshotting support")
		return ctrl.Result{}, err
	}

	if reason == ReasonFSSnapshotSupported {
		snapshotterPod, err := r.getFilesystemSnapshotterPod(ctx, sandbox)
		if err != nil {
			if apierrors.IsNotFound(err) {
				// todo benl: if snapshotter pod is deleted, we'll recreate it. Is it always the correct behavior? (e.g. it's done snapshotting!)
				// currently we only snapshot on cleanup (and then deletion) of parent sandbox
				return r.CreateSnapshotterPod(ctx, sandbox, baseSandbox, sandboxPod)
			}
			return ctrl.Result{}, err
		}
		// todo benl: this is not a good way probably (pod might have been GCed) - temp solution to resolve finalizer. Should probably have a state machine in the sandbox resource status
		isSnapshotterPodTerminated := isPodTerminated(snapshotterPod)
		if isSnapshotterPodTerminated {
			controllerutil.RemoveFinalizer(sandbox, SandboxFinalizer)
			return ctrl.Result{}, r.Update(ctx, sandbox)
		}
		return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
	} else {
		log.Info("unable to perform filesystem snapshot", "reason", reason)
		r.Recorder.Event(sandbox, corev1.EventTypeWarning, reason, "Unable to perform filesystem snapshot")

		// best-effort condition update
		_ = r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
			{
			Type:               SandboxFilesystemSnapshotCondition,
			Status:             metav1.ConditionFalse,
			Reason:             reason,
			Message:            "Unable to perform filesystem snapshot",
			ObservedGeneration: sandbox.Generation,
		},
	})
		// todo benl: this removes finalizer even though finalizeSandbox might have other finalization operations in the future...
		controllerutil.RemoveFinalizer(sandbox, SandboxFinalizer)
		return ctrl.Result{}, r.Update(ctx, sandbox)
	}
}

// sandboxPod may be nil if we had to finalize before pod was created
func (r *SandboxReconciler) finalizeSandbox(
	ctx context.Context,
	template *sandboxv1alpha1.SandboxTemplate,
	sandbox *sandboxv1alpha1.Sandbox,
	baseSandbox *sandboxv1alpha1.Sandbox,
	sandboxPod *corev1.Pod,
) (ctrl.Result, error) {
	log := logf.FromContext(ctx).WithValues("sandbox", sandbox.Name, "namespace", sandbox.Namespace)
	hasNonTrivialCleanup := template.Spec.ShutdownPolicy != nil && template.Spec.ShutdownPolicy.Policy != sandboxv1alpha1.ShutdownPolicyDelete

	if hasNonTrivialCleanup && sandboxPod == nil {
		log.Info(fmt.Sprintf("Even though sandbox has a non-trivial shutdown policy of %s, the sandbox pod did not start yet and thus it is skipped", template.Spec.ShutdownPolicy.Policy))
		r.Recorder.Event(sandbox, corev1.EventTypeWarning, "ShutdownPolicySkipped", "Sandbox pod did not start by the time the sandbox was deleted")
		controllerutil.RemoveFinalizer(sandbox, SandboxFinalizer)
		return ctrl.Result{}, r.Update(ctx, sandbox)
	}

	if hasNonTrivialCleanup {
		// currently FilesystemSnapshot is the only non-trivial shutdown policy of a sandbox
		return r.handleFilesystemSnapshot(ctx, sandbox, baseSandbox, sandboxPod)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *SandboxReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Recorder = mgr.GetEventRecorderFor("sandbox-controller")
	return ctrl.NewControllerManagedBy(mgr).
		For(&sandboxv1alpha1.Sandbox{}).
		// Pod owned by Sandbox via SetControllerReference will trigger sandbox_controller re-reconcile on pod changes:
		Owns(&corev1.Pod{}).
		Named("sandbox").
		Complete(r)
}
