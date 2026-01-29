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
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	sandboxv1alpha1 "github.com/isola-ai/isola-sb/api/v1alpha1"
	netbuilder "github.com/isola-ai/isola-sb/internal/operator/controller/network"
)

type SandboxReconciler struct {
	client.Client
	Scheme              *runtime.Scheme
	Recorder            record.EventRecorder
	SandboxSidecarImage string
	RuntimeClassName    string                        // RuntimeClassName to use for sandbox pods (e.g. "gvisor"). Empty means use cluster default.
	PriorityClassName   string                        // PriorityClassName to use for sandbox pods. Empty means use cluster default.
	ImagePullSecrets    []corev1.LocalObjectReference // ImagePullSecrets for pulling sandbox-sidecar images from private registries.
	Clock               Clock                         // Clock interface for time operations, allows mocking in tests
}

func (r *SandboxReconciler) clock() Clock {
	if r.Clock != nil {
		return r.Clock
	}
	return RealClock{}
}

// +kubebuilder:rbac:groups=sandbox.isola.run,resources=sandboxes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=sandbox.isola.run,resources=sandboxes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=sandbox.isola.run,resources=sandboxes/finalizers,verbs=update
// +kubebuilder:rbac:groups=sandbox.isola.run,resources=sandboxtemplates,verbs=get;list;watch
// +kubebuilder:rbac:groups=sandbox.isola.run,resources=rootfssnapshots,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;update;patch;delete
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

	if err := r.ensureCustomNetworkPolicy(ctx, sandbox, baseSandbox); err != nil {
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
	} else { // no timeout
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

// ensureCustomNetworkPolicy creates a custom NetworkPolicy for sandboxes
// that need more than the static Helm-installed policies (custom CIDRs, pod rules, or DNS).
func (r *SandboxReconciler) ensureCustomNetworkPolicy(
	ctx context.Context,
	sandbox *sandboxv1alpha1.Sandbox,
	baseSandbox *sandboxv1alpha1.Sandbox,
) error {
	log := logf.FromContext(ctx).WithValues("sandbox", sandbox.Name, "namespace", sandbox.Namespace)

	if !sandbox.NeedsCustomNetworkPolicy() {
		return nil
	}

	desiredNP, err := netbuilder.BuildCustomNetworkPolicy(sandbox.Name, sandbox.Namespace, sandbox.Spec.Network)
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
		}
		return err
	}

	if desiredNP == nil {
		// No custom policy needed
		return nil
	}

	existingNP := &networkingv1.NetworkPolicy{}
	policyName := sandbox.GetCustomNetworkPolicyName()
	err = r.Get(ctx, types.NamespacedName{Name: policyName, Namespace: sandbox.Namespace}, existingNP)

	if err == nil {
		// Policy exists - no update needed (network config is immutable)
		return nil
	}

	if !apierrors.IsNotFound(err) {
		log.Error(err, "Failed to get existing custom NetworkPolicy")
		return err
	}

	log.Info("Creating custom NetworkPolicy", "policyName", policyName)
	if err := controllerutil.SetControllerReference(sandbox, desiredNP, r.Scheme); err != nil {
		log.Error(err, "Failed to set controller reference on custom NetworkPolicy")
		return err
	}

	if err := r.Create(ctx, desiredNP); err != nil {
		if apierrors.IsAlreadyExists(err) {
			// Race condition - policy already created
			return nil
		}
		log.Error(err, "Failed to create custom NetworkPolicy")
		return err
	}

	log.Info("Custom NetworkPolicy created")
	return nil
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
		Owns(&sandboxv1alpha1.RootfsSnapshot{}).
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
