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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	sandboxv1alpha1 "github.com/omereli/dev-isola/services/isola-operator/api/v1alpha1"
)

// SandboxReconciler reconciles a Sandbox object
type SandboxReconciler struct {
	client.Client
	Scheme *runtime.Scheme
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

	// todo benl: set finalizers
	// Add finalizer first if not set to avoid the race condition between init and delete.

	// Check if the Sandbox is being deleted
	if !sandbox.ObjectMeta.DeletionTimestamp.IsZero() {
		// sandbox mark for deletion
		// todo benl: handle deletion
		return ctrl.Result{}, nil
	}

	if sandbox.Spec.TemplateRef == nil {
		log.Info("Sandbox template ref not set")
		return ctrl.Result{}, nil
	}

	template := &sandboxv1alpha1.SandboxTemplate{}
	// todo benl: assuming template is in the same namespace as the sandbox
	if err := r.Get(ctx, types.NamespacedName{Name: sandbox.Spec.TemplateRef.Name, Namespace: req.Namespace}, template); err != nil {
		if errors.IsNotFound(err) {
			log.Info("Sandbox template not found")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get Sandbox template")
		return ctrl.Result{}, err
	}

	podName := sandbox.Name + "-pod"
	podNamespace := sandbox.Namespace

	// check if pod exists
	pod := &corev1.Pod{}
	err := r.Get(ctx, types.NamespacedName{Name: podName, Namespace: podNamespace}, pod)
	if err != nil { // could not get / find the pod
		if errors.IsNotFound(err) {
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
						log.Error(err, "Failed to get Pod")
						return ctrl.Result{}, err
					}
				} else {
					log.Error(err, "Failed to create Pod")
					return ctrl.Result{}, err
				}
			}
			log.Info("Pod created")
			//todo benl: proceeding here doesn't make sense - pod won't be running as this is the object we created in this scope, not the new one
			// we might get from re-reading from the api server. So how can we handld it? how to "subscribe on pod updates"?
		} else {
			log.Error(err, "Failed to get Pod")
			return ctrl.Result{}, err
		}

	}

	// todo benl: is this necessary? why?
	if sandbox.Status.Conditions == nil {
		sandbox.Status.Conditions = []metav1.Condition{}
	}

	// todo benl: refactor the condition update(s), use constants instead of raw strings
	condition := metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionUnknown,
		Reason:             "PLACEHOLDER",
		Message:            "PLACEHOLDER",
		LastTransitionTime: metav1.Now(),
	}

	switch pod.Status.Phase {
	case corev1.PodRunning:
		condition.Status = metav1.ConditionTrue
		condition.Message = "Pod is running"
	case corev1.PodFailed:
		condition.Status = metav1.ConditionFalse
		condition.Reason = "PodFailed"
	case corev1.PodPending:
		condition.Status = metav1.ConditionFalse
		condition.Reason = "PodPending"
	}

	// Simple check to see if we need to update status (simplified for brevity)
	// In production, check existing conditions to avoid unnecessary updates
	sandbox.Status.Conditions = []metav1.Condition{condition}
	sandbox.Status.ObservedGeneration = sandbox.Generation

	log.Info("Updating Sandbox status", "status", condition.Status)
	if err := r.Status().Update(ctx, sandbox); err != nil {
		log.Error(err, "Failed to update Sandbox status")
		return ctrl.Result{}, err
	}

	// check timeout
	hasTimeout := template.Spec.TimeoutSeconds != nil
	if hasTimeout {
		// Use Pod start time if available, otherwise fallback (or wait)
		if pod.Status.StartTime != nil {
			timeoutTimestamp := pod.Status.StartTime.Add(time.Duration(*template.Spec.TimeoutSeconds) * time.Second)
			timeLeft := time.Until(timeoutTimestamp)
			log.Info("Checking sandbox timeout", "timeLeft", timeLeft.String(), "timeout", *template.Spec.TimeoutSeconds)
			if timeLeft <= 0 {
				log.Info("sandbox timed out")
				// todo benl: handle timeout
				return ctrl.Result{}, nil
			} else {
				log.Info("sandbox will time out in:", "time", timeLeft)
				return ctrl.Result{RequeueAfter: timeLeft}, nil
			}
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
