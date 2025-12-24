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
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
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
)

const (
	// Summary condition
	SandboxReadyCondition = "Ready"

	SandboxTemplateReadyCondition = "TemplateReady"
	SandboxPodReadyCondition      = "PodReady"
)

const (
	CondReasonTemplateNotFound = "TemplateNotFound"

	CondReasonPodPending = "PodPending"
	CondReasonPodRunning = "PodRunning"
)

// SandboxReconciler reconciles a Sandbox object
type SandboxReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// AgentImage is the container image for the isola-agent sidecar
	AgentImage string
	// ControllerWSURL is the WebSocket URL for the agent to connect to the controller
	ControllerWSURL string
	// SharedVolumeMountPath is the mount path for the shared volume between sandbox and agent
	SharedVolumeMountPath string
}

const (
	// Default values for agent configuration
	DefaultAgentImage           = "isola-agent:dev"
	DefaultControllerWSURL      = "ws://isola-controller.isola-control-plane:8765"
	DefaultSharedVolumeMountPath = "/shared"

	// Volume and container names
	SharedVolumeName   = "shared-data"
	AgentContainerName = "isola-agent"
)

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

// buildAgentContainer creates the isola-agent sidecar container spec
func (r *SandboxReconciler) buildAgentContainer(sandboxName string) corev1.Container {
	agentImage := r.AgentImage
	if agentImage == "" {
		agentImage = DefaultAgentImage
	}

	controllerWSURL := r.ControllerWSURL
	if controllerWSURL == "" {
		controllerWSURL = DefaultControllerWSURL
	}

	sharedVolumeMountPath := r.SharedVolumeMountPath
	if sharedVolumeMountPath == "" {
		sharedVolumeMountPath = DefaultSharedVolumeMountPath
	}

	return corev1.Container{
		Name:  AgentContainerName,
		Image: agentImage,
		Env: []corev1.EnvVar{
			{Name: "SANDBOX_ID", Value: sandboxName},
			{Name: "SANDBOX_NAME", Value: sandboxName},
			{Name: "ISOLA_CONTROLLER_WS_URL", Value: controllerWSURL},
			{Name: "SHARED_VOLUME_PATH", Value: sharedVolumeMountPath},
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      SharedVolumeName,
				MountPath: sharedVolumeMountPath,
			},
		},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("50m"),
				corev1.ResourceMemory: resource.MustParse("64Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("200m"),
				corev1.ResourceMemory: resource.MustParse("128Mi"),
			},
		},
	}
}

// injectSidecar adds the isola-agent sidecar container and shared volume to the pod spec
func (r *SandboxReconciler) injectSidecar(pod *corev1.Pod, sandboxName string) {
	sharedVolumeMountPath := r.SharedVolumeMountPath
	if sharedVolumeMountPath == "" {
		sharedVolumeMountPath = DefaultSharedVolumeMountPath
	}

	// Add shared volume to pod
	sharedVolume := corev1.Volume{
		Name: SharedVolumeName,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	}
	pod.Spec.Volumes = append(pod.Spec.Volumes, sharedVolume)

	// Add volume mount to all existing containers
	for i := range pod.Spec.Containers {
		pod.Spec.Containers[i].VolumeMounts = append(pod.Spec.Containers[i].VolumeMounts, corev1.VolumeMount{
			Name:      SharedVolumeName,
			MountPath: sharedVolumeMountPath,
		})
	}

	// Add agent sidecar container
	agentContainer := r.buildAgentContainer(sandboxName)
	pod.Spec.Containers = append(pod.Spec.Containers, agentContainer)
}

// +kubebuilder:rbac:groups=sandbox.isola.run,resources=sandboxes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=sandbox.isola.run,resources=sandboxes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=sandbox.isola.run,resources=sandboxes/finalizers,verbs=update
// +kubebuilder:rbac:groups=sandbox.isola.run,resources=sandboxtemplates,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Sandbox object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.22.4/pkg/reconcile
func (r *SandboxReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// todo benl: add r.RecordEvent for events (observability)
	log := logf.FromContext(ctx).WithValues("sandbox", req.Name, "namespace", req.Namespace)

	log.Info("Reconciling Sandbox")

	sandbox := &sandboxv1alpha1.Sandbox{}
	if err := r.Get(ctx, req.NamespacedName, sandbox); err != nil {
		if errors.IsNotFound(err) {
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

	template := &sandboxv1alpha1.SandboxTemplate{}
	// todo benl: assuming template is in the same namespace as the sandbox
	if err := r.Get(ctx, types.NamespacedName{Name: sandbox.Spec.TemplateRef.Name, Namespace: req.Namespace}, template); err != nil {
		if errors.IsNotFound(err) {
			meta.SetStatusCondition(&sandbox.Status.Conditions, metav1.Condition{
				Type:               SandboxTemplateReadyCondition,
				Status:             metav1.ConditionFalse,
				Reason:             CondReasonTemplateNotFound,
				Message:            "Sandbox template not found",
				ObservedGeneration: sandbox.Generation,
			})

			meta.SetStatusCondition(&sandbox.Status.Conditions, metav1.Condition{
				Type:               SandboxReadyCondition,
				Status:             metav1.ConditionFalse,
				Reason:             CondReasonTemplateNotFound,
				Message:            "Sandbox template not found",
				ObservedGeneration: sandbox.Generation,
			})

			if err := r.Status().Patch(ctx, sandbox, client.MergeFrom(baseSandbox)); err != nil {
				if errors.IsConflict(err) {
					log.Info("Sandbox resource updated by another controller")
					// todo benl: return an error to allow the controller to trigger reconcile again later (with rate limiting)
					return ctrl.Result{}, err
				}
				log.Error(err, "Failed to update Sandbox status")
				return ctrl.Result{}, err
			}
			log.Error(err, "Sandbox template not found")
			// todo benl: we'll stop reconciling (steady failed state) - add watch on SandboxTemplate to reconcile the sandbox when template is created
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get Sandbox template")
		return ctrl.Result{}, err
	}

	meta.SetStatusCondition(&sandbox.Status.Conditions, metav1.Condition{
		Type:               SandboxTemplateReadyCondition,
		Status:             metav1.ConditionTrue,
		Reason:             "TemplateOK",
		Message:            "Template resolved",
		ObservedGeneration: sandbox.Generation,
	})

	podName := sandbox.Name + "-pod"
	podNamespace := sandbox.Namespace

	// check if pod already exists
	pod := &corev1.Pod{}
	err := r.Get(ctx, types.NamespacedName{Name: podName, Namespace: podNamespace}, pod)
	if err != nil {
		if errors.IsNotFound(err) {
			meta.SetStatusCondition(&sandbox.Status.Conditions, metav1.Condition{
				Type:               SandboxPodReadyCondition,
				Status:             metav1.ConditionFalse,
				Reason:             "PodCreating",
				Message:            "Creating sandbox Pod",
				ObservedGeneration: sandbox.Generation,
			})
			meta.SetStatusCondition(&sandbox.Status.Conditions, metav1.Condition{
				Type:               SandboxReadyCondition,
				Status:             metav1.ConditionFalse,
				Reason:             "Reconciling",
				Message:            "Waiting for Pod to be created/ready",
				ObservedGeneration: sandbox.Generation,
			})

			// todo benl: take spec as-is from template for now. Think about how we:
			// * force some fields (?) like RestartPolicy
			// * override some fields (user defined? global policy?)
			// * tweak some fields? (E.g. more ephermal storage needed for snapshotting?)
			pod = &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      podName,
					Namespace: podNamespace,
				},
				Spec: template.Spec.PodTemplate.Spec,
			}

			// Inject isola-agent sidecar container and shared volume
			r.injectSidecar(pod, sandbox.Name)
			log.Info("Injected isola-agent sidecar", "agentImage", r.AgentImage, "controllerWSURL", r.ControllerWSURL)

			// Set Pod's object owner reference to the Sandbox object
			if err := controllerutil.SetControllerReference(sandbox, pod, r.Scheme); err != nil {
				log.Error(err, "Failed to set controller reference")
				return ctrl.Result{}, err
			}

			log.Info("Creating Pod")
			// todo benl: handle pod creation failure, handle race between 2 controllers creating the same pod
			if err := r.Create(ctx, pod); err != nil {
				if errors.IsAlreadyExists(err) {
					log.Info("Pod already exists")
					// Someone beat us to it: treat as success and continue by fetching it.
					if err := r.Get(ctx, types.NamespacedName{Name: podName, Namespace: podNamespace}, pod); err != nil {
						log.Error(err, "Failed to get sandbox pod")
						return ctrl.Result{}, err
					}
				} else {
					meta.SetStatusCondition(&sandbox.Status.Conditions, metav1.Condition{
						Type:               SandboxPodReadyCondition,
						Status:             metav1.ConditionFalse,
						Reason:             "PodCreationFailed",
						Message:            err.Error(),
						ObservedGeneration: sandbox.Generation,
					})
					meta.SetStatusCondition(&sandbox.Status.Conditions, metav1.Condition{
						Type:               SandboxReadyCondition,
						Status:             metav1.ConditionFalse,
						Reason:             "PodCreationFailed",
						Message:            err.Error(),
						ObservedGeneration: sandbox.Generation,
					})
					if patchErr := r.Status().Patch(ctx, sandbox, client.MergeFrom(baseSandbox)); patchErr != nil {
						log.Error(patchErr, "Failed to update Sandbox status after pod creation failure")
					}
					log.Error(err, "Failed to create sandbox pod")
					return ctrl.Result{}, err
				}
			}
			log.Info("Pod created")
			if err := r.Status().Patch(ctx, sandbox, client.MergeFrom(baseSandbox)); err != nil {
				if errors.IsConflict(err) {
					log.Info("Sandbox resource updated by another controller")
					// todo benl: return an error to allow the controller to trigger reconcile again later (with rate limiting)
					return ctrl.Result{}, err
				}
				log.Error(err, "Failed to update Sandbox status")
				return ctrl.Result{}, err
			}
			// reconcile will trigger when pod changes status since our sandbox owns the pod
			return ctrl.Result{}, nil
		} else {
			log.Error(err, "Failed to get sandbox pod")
			return ctrl.Result{}, err
		}

	}

	podReady := isPodReady(pod)

	if podReady {
		// todo benl: when adding more elaborate mechanisms like networkpolicy, change SandboxReady from True here if they are not ready yet
		meta.SetStatusCondition(&sandbox.Status.Conditions, metav1.Condition{
			Type:               SandboxPodReadyCondition,
			Status:             metav1.ConditionTrue,
			Reason:             "PodRunning",
			Message:            "Pod is running",
			ObservedGeneration: sandbox.Generation,
		})
		meta.SetStatusCondition(&sandbox.Status.Conditions, metav1.Condition{
			Type:               SandboxReadyCondition,
			Status:             metav1.ConditionTrue,
			Reason:             "PodRunning",
			Message:            "Pod is running",
			ObservedGeneration: sandbox.Generation,
		})
	} else {
		meta.SetStatusCondition(&sandbox.Status.Conditions, metav1.Condition{
			Type:               SandboxPodReadyCondition,
			Status:             metav1.ConditionFalse,
			Reason:             "PodPending",
			Message:            "Pod is not ready yet",
			ObservedGeneration: sandbox.Generation,
		})
		meta.SetStatusCondition(&sandbox.Status.Conditions, metav1.Condition{
			Type:               SandboxReadyCondition,
			Status:             metav1.ConditionFalse,
			Reason:             "PodPending",
			Message:            "Pod is not ready yet",
			ObservedGeneration: sandbox.Generation,
		})
	}

	log.Info("Updating Sandbox status", "status", podReady)
	if err := r.Status().Patch(ctx, sandbox, client.MergeFrom(baseSandbox)); err != nil {
		if errors.IsConflict(err) {
			log.Info("Sandbox resource updated by another controller")
			// todo benl: return an error to allow the controller to trigger reconcile again later (with rate limiting)
			return ctrl.Result{}, err
		}
		log.Error(err, "Failed to update Sandbox status")
		return ctrl.Result{}, err
	}

	// check timeout
	hasTimeout := template.Spec.TimeoutSeconds != nil
	if hasTimeout {

		var startTime time.Time
		if pod.Status.StartTime != nil {
			// pod.Status.StartTime set once when the pod is first scheduled onto a node (survives pod restarts)
			// it is probably closer to user intent, so if exists we use that time
			log.Info("deduced start time from pod", "startTime", pod.Status.StartTime.Time)
			startTime = pod.Status.StartTime.Time
		} else {
			log.Info("deduced start time from sandbox", "startTime", sandbox.ObjectMeta.CreationTimestamp.Time)
			startTime = sandbox.ObjectMeta.CreationTimestamp.Time
		}
		// todo benl: inject clock for testability instead of using .Until that uses .Now() internally
		timeoutTimestamp := startTime.Add(time.Duration(*template.Spec.TimeoutSeconds) * time.Second)
		timeLeft := time.Until(timeoutTimestamp)

		if timeLeft <= 0 {
			log.Info("sandbox timed out")
			if err := r.Delete(ctx, sandbox); err != nil {
				log.Error(err, "Failed to delete sandbox")
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, nil
		} else {
			log.Info("sandbox will time out in", "timeLeft", timeLeft)
			return ctrl.Result{RequeueAfter: timeLeft}, nil
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *SandboxReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&sandboxv1alpha1.Sandbox{}).
		// Pod owned by Sandbox via SetControllerReference will trigger sandbox_controller re-reconcile on pod changes:
		Owns(&corev1.Pod{}).
		Named("sandbox").
		Complete(r)
}
