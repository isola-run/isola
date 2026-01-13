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

	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	sandboxv1alpha1 "github.com/omereli/dev-isola/services/isola-operator/api/v1alpha1"
	"github.com/omereli/dev-isola/services/isola-operator/internal/controller/network"
)

type NetworkTemplateReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

const (
	CondReasonNetworkTemplateReady       = "NetworkPolicyApplied"
	CondReasonNetworkTemplateInvalid     = "InvalidNetworkTemplate"
	CondReasonNetworkTemplatePolicyError = "NetworkPolicyError"
)

// +kubebuilder:rbac:groups=sandbox.isola.run,resources=networktemplates,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=sandbox.isola.run,resources=networktemplates/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=sandbox.isola.run,resources=networktemplates/finalizers,verbs=update
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;update;patch;delete

func (r *NetworkTemplateReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx).WithValues("networktemplate", req.NamespacedName)

	networkTemplate := &sandboxv1alpha1.NetworkTemplate{}
	if err := r.Get(ctx, req.NamespacedName, networkTemplate); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get NetworkTemplate")
		return ctrl.Result{}, err
	}

	baseTemplate := networkTemplate.DeepCopy()

	if !networkTemplate.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, networkTemplate)
	}

	if err := r.ensureFinalizer(ctx, networkTemplate); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.reconcileNetworkPolicy(ctx, networkTemplate, baseTemplate); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *NetworkTemplateReconciler) ensureFinalizer(ctx context.Context, networkTemplate *sandboxv1alpha1.NetworkTemplate) error {
	log := logf.FromContext(ctx).WithValues("networktemplate", networkTemplate.Name, "namespace", networkTemplate.Namespace)

	if controllerutil.ContainsFinalizer(networkTemplate, NetworkTemplateFinalizer) {
		return nil
	}

	log.Info("Adding finalizer to NetworkTemplate")
	controllerutil.AddFinalizer(networkTemplate, NetworkTemplateFinalizer)
	if err := r.Update(ctx, networkTemplate); err != nil {
		log.Error(err, "Failed to add finalizer to NetworkTemplate")
		return err
	}

	return nil
}

func (r *NetworkTemplateReconciler) handleDeletion(ctx context.Context, networkTemplate *sandboxv1alpha1.NetworkTemplate) (ctrl.Result, error) {
	log := logf.FromContext(ctx).WithValues("networktemplate", networkTemplate.Name, "namespace", networkTemplate.Namespace)

	if !controllerutil.ContainsFinalizer(networkTemplate, NetworkTemplateFinalizer) {
		return ctrl.Result{}, nil
	}

	// Use field index for efficient lookup of sandboxes using this template
	usingSandboxes := &sandboxv1alpha1.SandboxList{}
	if err := r.List(ctx, usingSandboxes,
		client.InNamespace(networkTemplate.Namespace),
		client.MatchingFields{sandboxNetworkTemplateRefField: networkTemplate.Name},
	); err != nil {
		log.Error(err, "Failed to list sandboxes")
		return ctrl.Result{}, err
	}

	if len(usingSandboxes.Items) > 0 {
		log.Info("NetworkTemplate still in use, cannot remove finalizer", "sandboxCount", len(usingSandboxes.Items))
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	// TODO BENL:
	// there's an inherent TOCTOU issue here where if a new sandbox relies on a network template that is being deleted:
	// sandbox A references NetworkTemplate T
	// A deleted -> T checks and usingSandboxes.Items == 0 -> removes finalizer
	// in the meantime sandbox B was able to Get template T and materialize a pod
	// T is deleted (as there's no finalizer) => B's pod has no effective network policy
	// this is an edge case that results from a template being deleted, so it might be enough to just document it

	log.Info("Removing finalizer from NetworkTemplate")
	controllerutil.RemoveFinalizer(networkTemplate, NetworkTemplateFinalizer)
	if err := r.Update(ctx, networkTemplate); err != nil {
		log.Error(err, "Failed to remove finalizer from NetworkTemplate")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *NetworkTemplateReconciler) reconcileNetworkPolicy(
	ctx context.Context,
	networkTemplate *sandboxv1alpha1.NetworkTemplate,
	baseTemplate *sandboxv1alpha1.NetworkTemplate,
) error {
	log := logf.FromContext(ctx).WithValues("networktemplate", networkTemplate.Name, "namespace", networkTemplate.Namespace)

	existingNP, err := r.getNetworkPolicy(ctx, networkTemplate)
	if err != nil {
		return err
	}

	desiredNP, err := network.BuildNetworkPolicy(networkTemplate)
	if err != nil {
		log.Error(err, "Failed to build NetworkPolicy from template")
		return r.patchStatus(ctx, baseTemplate, networkTemplate, []metav1.Condition{{
			Type:               string(sandboxv1alpha1.NetworkTemplateReady),
			Status:             metav1.ConditionFalse,
			Reason:             CondReasonNetworkTemplateInvalid,
			Message:            err.Error(),
			ObservedGeneration: networkTemplate.Generation,
		}})
	}

	// Note: NetworkTemplate spec is immutable - updates are ignored.
	// To change network rules, create a new NetworkTemplate.
	if existingNP == nil {
		log.Info("Creating NetworkPolicy for NetworkTemplate")
		if err := controllerutil.SetControllerReference(networkTemplate, desiredNP, r.Scheme); err != nil {
			return err
		}
		if err := r.Create(ctx, desiredNP); err != nil {
			if apierrors.IsAlreadyExists(err) {
				log.Info("NetworkPolicy already exists (created by another reconcile)")
				return r.patchStatus(ctx, baseTemplate, networkTemplate, []metav1.Condition{{
					Type:               string(sandboxv1alpha1.NetworkTemplateReady),
					Status:             metav1.ConditionTrue,
					Reason:             CondReasonNetworkTemplateReady,
					Message:            "NetworkPolicy exists",
					ObservedGeneration: networkTemplate.Generation,
				}})
			}
			log.Error(err, "Failed to create NetworkPolicy")
			return r.patchStatus(ctx, baseTemplate, networkTemplate, []metav1.Condition{{
				Type:               string(sandboxv1alpha1.NetworkTemplateReady),
				Status:             metav1.ConditionFalse,
				Reason:             CondReasonNetworkTemplatePolicyError,
				Message:            err.Error(),
				ObservedGeneration: networkTemplate.Generation,
			}})
		}
		log.Info("NetworkPolicy created")
	}

	return r.patchStatus(ctx, baseTemplate, networkTemplate, []metav1.Condition{{
		Type:               string(sandboxv1alpha1.NetworkTemplateReady),
		Status:             metav1.ConditionTrue,
		Reason:             CondReasonNetworkTemplateReady,
		Message:            "NetworkPolicy applied",
		ObservedGeneration: networkTemplate.Generation,
	}})
}

// getNetworkPolicy retrieves the NetworkPolicy for a NetworkTemplate, if it exists.
// Returns nil, nil if the NetworkPolicy doesn't exist.
func (r *NetworkTemplateReconciler) getNetworkPolicy(ctx context.Context, networkTemplate *sandboxv1alpha1.NetworkTemplate) (*networkingv1.NetworkPolicy, error) {
	np := &networkingv1.NetworkPolicy{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      network.GetNetworkPolicyName(networkTemplate.Name),
		Namespace: networkTemplate.Namespace,
	}, np)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return np, nil
}

func (r *NetworkTemplateReconciler) patchStatus(ctx context.Context, baseTemplate *sandboxv1alpha1.NetworkTemplate, networkTemplate *sandboxv1alpha1.NetworkTemplate, newConditions []metav1.Condition) error {
	if networkTemplate.Status.Conditions == nil {
		networkTemplate.Status.Conditions = []metav1.Condition{}
	}

	for _, cond := range newConditions {
		meta.SetStatusCondition(&networkTemplate.Status.Conditions, cond)
	}

	if err := r.Status().Patch(ctx, networkTemplate, client.MergeFrom(baseTemplate)); err != nil {
		return err
	}

	return nil
}

func (r *NetworkTemplateReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Register field index for sandbox networkTemplateRef lookups (if not already done)
	// Note: This may already be registered by SandboxReconciler, which is fine
	_ = mgr.GetFieldIndexer().IndexField(
		context.Background(),
		&sandboxv1alpha1.Sandbox{},
		sandboxNetworkTemplateRefField,
		extractNetworkTemplateRefName,
	)

	return ctrl.NewControllerManagedBy(mgr).
		For(&sandboxv1alpha1.NetworkTemplate{}).
		// Watch NetworkPolicy changes to ensure policies are recreated if deleted out-of-band
		Watches(
			&networkingv1.NetworkPolicy{},
			handler.EnqueueRequestsFromMapFunc(r.findNetworkTemplateForPolicy),
		).
		Named("networktemplate").
		Complete(r)
}

func (r *NetworkTemplateReconciler) findNetworkTemplateForPolicy(ctx context.Context, np client.Object) []reconcile.Request {
	// NetworkPolicies are labeled with the network template name
	templateName := np.GetLabels()[network.NetworkTemplateLabelKey]
	if templateName == "" {
		return nil
	}

	return []reconcile.Request{
		{
			NamespacedName: types.NamespacedName{
				Name:      templateName,
				Namespace: np.GetNamespace(),
			},
		},
	}
}
