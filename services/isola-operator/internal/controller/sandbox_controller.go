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
	"os"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
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
	Scheme               *runtime.Scheme
	Recorder             record.EventRecorder
	AgentImage           string
	SharedVolumeMountPath string
}

const (
	// Shared volume name for communication between sandbox container and agent sidecar
	sharedVolumeName = "sandbox-shared"
	// Default mount path for shared volume
	defaultSharedVolumeMountPath = "/sandbox-shared"
)

// buildAgentContainer creates the agent sidecar container spec
func (r *SandboxReconciler) buildAgentContainer(sandboxID string) corev1.Container {
	mountPath := r.SharedVolumeMountPath
	if mountPath == "" {
		mountPath = defaultSharedVolumeMountPath
	}

	env := []corev1.EnvVar{
		{
			Name:  "SANDBOX_ID",
			Value: sandboxID,
		},
		{
			Name:  "SHARED_DIR",
			Value: mountPath,
		},
		{
			Name:  "SANDBOX_DATA_PATH",
			Value: mountPath,
		},
	}

	// Pass through S3 configuration from operator's environment to agent sidecar
	// These are optional - agent will work without them but S3 delete functionality will be disabled
	if bucketName := os.Getenv("BUCKET_NAME"); bucketName != "" {
		env = append(env, corev1.EnvVar{
			Name:  "BUCKET_NAME",
			Value: bucketName,
		})
	}
	if endpointURL := os.Getenv("ENDPOINT_URL"); endpointURL != "" {
		env = append(env, corev1.EnvVar{
			Name:  "ENDPOINT_URL",
			Value: endpointURL,
		})
	}
	if region := os.Getenv("REGION"); region != "" {
		env = append(env, corev1.EnvVar{
			Name:  "REGION",
			Value: region,
		})
	}
	if awsAccessKey := os.Getenv("AWS_ACCESS_KEY_ID"); awsAccessKey != "" {
		env = append(env, corev1.EnvVar{
			Name:  "AWS_ACCESS_KEY_ID",
			Value: awsAccessKey,
		})
	}
	if awsSecretKey := os.Getenv("AWS_SECRET_ACCESS_KEY"); awsSecretKey != "" {
		env = append(env, corev1.EnvVar{
			Name:  "AWS_SECRET_ACCESS_KEY",
			Value: awsSecretKey,
		})
	}

	return corev1.Container{
		Name:  "isola-agent",
		Image: r.AgentImage,
		Env:   env,
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      sharedVolumeName,
				MountPath: mountPath,
			},
		},
	}
}

// injectSidecar injects the agent sidecar container and shared volume into the pod spec
func (r *SandboxReconciler) injectSidecar(pod *corev1.Pod, sandboxID string) {
	mountPath := r.SharedVolumeMountPath
	if mountPath == "" {
		mountPath = defaultSharedVolumeMountPath
	}

	// Add shared volume to pod
	sharedVolume := corev1.Volume{
		Name: sharedVolumeName,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	}
	pod.Spec.Volumes = append(pod.Spec.Volumes, sharedVolume)

	// Add shared volume mount to all existing containers
	for i := range pod.Spec.Containers {
		pod.Spec.Containers[i].VolumeMounts = append(
			pod.Spec.Containers[i].VolumeMounts,
			corev1.VolumeMount{
				Name:      sharedVolumeName,
				MountPath: mountPath,
			},
		)
	}

	// Add agent sidecar container
	agentContainer := r.buildAgentContainer(sandboxID)
	pod.Spec.Containers = append(pod.Spec.Containers, agentContainer)
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
func (r *SandboxReconciler) CreateSandboxPod(ctx context.Context, sandbox *sandboxv1alpha1.Sandbox, baseSandbox *sandboxv1alpha1.Sandbox, template *sandboxv1alpha1.SandboxTemplate, podName string, podNamespace string) (ctrl.Result, error) {
	log := logf.FromContext(ctx).WithValues("sandbox", sandbox.Name, "namespace", sandbox.Namespace)
	log.Info("Creating Pod")

	// Create labels for the pod
	labels := map[string]string{
		"app":                       "isola-sandbox",
		"sandbox.isola.run/id":      sandbox.Name,
		"app.kubernetes.io/managed-by": "isola-operator",
	}

	// Copy sandbox-id label from Sandbox CR to pod if it exists
	if sandbox.Labels != nil {
		if sandboxID, exists := sandbox.Labels["sandbox-id"]; exists {
			labels["sandbox-id"] = sandboxID
		}
	}

	// Merge with template labels if any
	if template.Spec.PodTemplate.Labels != nil {
		for k, v := range template.Spec.PodTemplate.Labels {
			labels[k] = v
		}
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: podNamespace,
			Labels:    labels,
		},
		// todo benl: copy annotations as well?
		Spec: template.Spec.PodTemplate.Spec,
	}

	// Inject agent sidecar and shared volume
	r.injectSidecar(pod, sandbox.Name)

	// Set Pod's object owner reference to the Sandbox object
	if err := controllerutil.SetControllerReference(sandbox, pod, r.Scheme); err != nil {
		log.Error(err, "Failed to set controller reference")
		return ctrl.Result{}, err
	}

	if err := r.Create(ctx, pod); err != nil {
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
		return ctrl.Result{}, err
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
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *SandboxReconciler) EnsureTemplate(ctx context.Context, sandbox *sandboxv1alpha1.Sandbox, baseSandbox *sandboxv1alpha1.Sandbox) (*sandboxv1alpha1.SandboxTemplate, ctrl.Result, error) {
	log := logf.FromContext(ctx).WithValues("sandbox", sandbox.Name, "namespace", sandbox.Namespace)
	template := &sandboxv1alpha1.SandboxTemplate{}

	if err := r.Get(ctx, types.NamespacedName{Name: sandbox.Spec.TemplateRef.Name, Namespace: sandbox.Namespace}, template); err != nil {
		if errors.IsNotFound(err) {

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

// +kubebuilder:rbac:groups=sandbox.isola.run,resources=sandboxes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=sandbox.isola.run,resources=sandboxes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=sandbox.isola.run,resources=sandboxes/finalizers,verbs=update
// +kubebuilder:rbac:groups=sandbox.isola.run,resources=sandboxtemplates,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch

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

	template, result, err := r.EnsureTemplate(ctx, sandbox, baseSandbox)
	if err != nil {
		return result, err
	}
	if template == nil {
		return ctrl.Result{}, nil
	}

	podName := sandbox.Name + "-pod"
	podNamespace := sandbox.Namespace

	pod := &corev1.Pod{}
	if err := r.Get(ctx, types.NamespacedName{Name: podName, Namespace: podNamespace}, pod); err != nil {
		if errors.IsNotFound(err) {
			return r.CreateSandboxPod(ctx, sandbox, baseSandbox, template, podName, podNamespace)
		}
		return ctrl.Result{}, err
	}

	podReady := isPodReady(pod)

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
			log.Error(err, "Failed to update Sandbox status")
			return ctrl.Result{}, err
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
			log.Error(err, "Failed to update Sandbox status")
			return ctrl.Result{}, err
		}
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
	r.Recorder = mgr.GetEventRecorderFor("sandbox-controller")
	return ctrl.NewControllerManagedBy(mgr).
		For(&sandboxv1alpha1.Sandbox{}).
		// Pod owned by Sandbox via SetControllerReference will trigger sandbox_controller re-reconcile on pod changes:
		Owns(&corev1.Pod{}).
		Named("sandbox").
		Complete(r)
}
