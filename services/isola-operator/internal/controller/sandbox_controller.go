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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	sandboxv1alpha1 "github.com/omereli/dev-isola/dev-isola/services/isola-operator/api/v1alpha1"
)

// SandboxReconciler reconciles a Sandbox object
type SandboxReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=sandbox.isola.run,resources=sandboxes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=sandbox.isola.run,resources=sandboxes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=sandbox.isola.run,resources=sandboxes/finalizers,verbs=update

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
	log := logf.FromContext(ctx).WithValues("Sandbox", "name", req.Name, "namespace", req.Namespace)

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

	// todo benl: take spec as-is from template for now. Think about how we:
	// * force some fields (?) like RestartPolicy
	// * override some fields (user defined? global policy?)
	// * tweak some fields? (E.g. more ephermal storage needed for snapshotting?)
	pod := &corev1.Pod{
		Spec: template.Spec.PodTemplate.Spec,
	}

	// Set Pod's object owner reference to the Sandbox object
	controllerutil.SetControllerReference(sandbox, pod, r.Scheme)

	log.Info("Creating Pod")
	// todo benl: handle pod creation failure, handle race between 2 controllers creating the same pod
	if err := r.Create(ctx, pod); err != nil {
		log.Error(err, "Failed to create Pod")
		return ctrl.Result{}, err
	}
	log.Info("Pod created")

	// check timeout
	hasTimeout := template.Spec.TimeoutSeconds != nil
	if hasTimeout {
		timeLeft := time.Until(pod.Status.StartTime.Add(time.Duration(*template.Spec.TimeoutSeconds) * time.Second))
		if timeLeft <= 0 {
			log.Info("sandbox timed out")
			// todo benl: handle timeout
			return ctrl.Result{}, nil
		} else {
			log.Info("sandbox will time out in:", "time", timeLeft)
			return ctrl.Result{RequeueAfter: timeLeft}, nil
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *SandboxReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&sandboxv1alpha1.Sandbox{}).
		Named("sandbox").
		Complete(r)
}
