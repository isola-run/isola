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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	sandboxv1alpha1 "github.com/isola-ai/isola-sb/api/v1alpha1"
)

const (
	// Condition types for SandboxIngress
	SandboxIngressReadyCondition        = "Ready"
	SandboxIngressSandboxReadyCondition = "SandboxReady"
	SandboxIngressServiceCondition      = "ServiceReady"
	SandboxIngressHTTPRouteCondition    = "HTTPRouteReady"

	// Condition reasons
	CondReasonSandboxNotFound   = "SandboxNotFound"
	CondReasonSandboxNotReady   = "SandboxNotReady"
	CondReasonSandboxReady      = "SandboxReady"
	CondReasonServiceCreated    = "ServiceCreated"
	CondReasonServiceFailed     = "ServiceFailed"
	CondReasonHTTPRouteCreated  = "HTTPRouteCreated"
	CondReasonHTTPRouteFailed   = "HTTPRouteFailed"
	CondReasonIngressReady      = "IngressReady"
	CondReasonIngressNotReady   = "IngressNotReady"
	CondReasonGatewayNotEnabled = "GatewayNotEnabled"

	// Field index for efficient lookup of ingresses by sandbox
	ingressSandboxRefField = ".spec.sandboxRef"

	// Finalizer for cleanup
	SandboxIngressFinalizer = "sandboxingress.isola.run/cleanup"
)

// HTTPRoute GVK for Gateway API
var httpRouteGVK = schema.GroupVersionKind{
	Group:   "gateway.networking.k8s.io",
	Version: "v1",
	Kind:    "HTTPRoute",
}

// SandboxIngressReconciler reconciles a SandboxIngress object.
type SandboxIngressReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// IngressDomain is the base domain for sandbox ingress URLs.
	// Example: "sandboxes.example.com" -> URLs will be https://<sandbox-id>.sandboxes.example.com
	IngressDomain string

	// GatewayName is the name of the Gateway resource to attach HTTPRoutes to.
	GatewayName string

	// GatewayNamespace is the namespace where the Gateway resource lives.
	GatewayNamespace string
}

// +kubebuilder:rbac:groups=sandbox.isola.run,resources=sandboxingresses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=sandbox.isola.run,resources=sandboxingresses/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=sandbox.isola.run,resources=sandboxingresses/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes,verbs=get;list;watch;create;update;patch;delete

func (r *SandboxIngressReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx).WithValues("sandboxingress", req.Name, "namespace", req.Namespace)

	log.Info("Reconciling SandboxIngress")

	// Fetch the SandboxIngress
	ingress := &sandboxv1alpha1.SandboxIngress{}
	if err := r.Get(ctx, req.NamespacedName, ingress); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("SandboxIngress not found, ignoring")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get SandboxIngress")
		return ctrl.Result{}, err
	}

	// Check if ingress is enabled (domain configured)
	if r.IngressDomain == "" {
		log.Info("Ingress domain not configured, skipping")
		return r.updateStatus(ctx, ingress, nil, nil,
			metav1.Condition{
				Type:               SandboxIngressReadyCondition,
				Status:             metav1.ConditionFalse,
				Reason:             CondReasonGatewayNotEnabled,
				Message:            "Ingress is not enabled: no domain configured",
				ObservedGeneration: ingress.Generation,
			})
	}

	baseIngress := ingress.DeepCopy()

	// Handle deletion
	if !ingress.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(ingress, SandboxIngressFinalizer) {
			// Owned resources (Service, HTTPRoute) will be garbage collected
			controllerutil.RemoveFinalizer(ingress, SandboxIngressFinalizer)
			if err := r.Update(ctx, ingress); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(ingress, SandboxIngressFinalizer) {
		controllerutil.AddFinalizer(ingress, SandboxIngressFinalizer)
		if err := r.Update(ctx, ingress); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Get the referenced Sandbox
	sandbox := &sandboxv1alpha1.Sandbox{}
	if err := r.Get(ctx, types.NamespacedName{Name: ingress.Spec.SandboxRef, Namespace: ingress.Namespace}, sandbox); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("Referenced sandbox not found", "sandbox", ingress.Spec.SandboxRef)
			return r.updateStatus(ctx, ingress, baseIngress, nil,
				metav1.Condition{
					Type:               SandboxIngressSandboxReadyCondition,
					Status:             metav1.ConditionFalse,
					Reason:             CondReasonSandboxNotFound,
					Message:            fmt.Sprintf("Sandbox %q not found", ingress.Spec.SandboxRef),
					ObservedGeneration: ingress.Generation,
				},
				metav1.Condition{
					Type:               SandboxIngressReadyCondition,
					Status:             metav1.ConditionFalse,
					Reason:             CondReasonSandboxNotFound,
					Message:            fmt.Sprintf("Sandbox %q not found", ingress.Spec.SandboxRef),
					ObservedGeneration: ingress.Generation,
				})
		}
		return ctrl.Result{}, err
	}

	// Check if sandbox is ready
	sandboxReady := meta.IsStatusConditionTrue(sandbox.Status.Conditions, SandboxReadyCondition)
	if !sandboxReady {
		log.Info("Sandbox not ready", "sandbox", ingress.Spec.SandboxRef)
		return r.updateStatus(ctx, ingress, baseIngress, nil,
			metav1.Condition{
				Type:               SandboxIngressSandboxReadyCondition,
				Status:             metav1.ConditionFalse,
				Reason:             CondReasonSandboxNotReady,
				Message:            fmt.Sprintf("Sandbox %q is not ready", ingress.Spec.SandboxRef),
				ObservedGeneration: ingress.Generation,
			},
			metav1.Condition{
				Type:               SandboxIngressReadyCondition,
				Status:             metav1.ConditionFalse,
				Reason:             CondReasonSandboxNotReady,
				Message:            fmt.Sprintf("Sandbox %q is not ready", ingress.Spec.SandboxRef),
				ObservedGeneration: ingress.Generation,
			})
	}

	// Get sandbox ID from labels
	sandboxID := sandbox.Labels["sandbox-id"]
	if sandboxID == "" {
		sandboxID = sandbox.Name
	}

	// Ensure Service exists
	if err := r.ensureService(ctx, ingress, sandbox); err != nil {
		log.Error(err, "Failed to ensure Service")
		return r.updateStatus(ctx, ingress, baseIngress, nil,
			metav1.Condition{
				Type:               SandboxIngressServiceCondition,
				Status:             metav1.ConditionFalse,
				Reason:             CondReasonServiceFailed,
				Message:            err.Error(),
				ObservedGeneration: ingress.Generation,
			},
			metav1.Condition{
				Type:               SandboxIngressReadyCondition,
				Status:             metav1.ConditionFalse,
				Reason:             CondReasonServiceFailed,
				Message:            err.Error(),
				ObservedGeneration: ingress.Generation,
			})
	}

	// Ensure HTTPRoute exists
	if err := r.ensureHTTPRoute(ctx, ingress, sandboxID); err != nil {
		log.Error(err, "Failed to ensure HTTPRoute")
		return r.updateStatus(ctx, ingress, baseIngress, nil,
			metav1.Condition{
				Type:               SandboxIngressHTTPRouteCondition,
				Status:             metav1.ConditionFalse,
				Reason:             CondReasonHTTPRouteFailed,
				Message:            err.Error(),
				ObservedGeneration: ingress.Generation,
			},
			metav1.Condition{
				Type:               SandboxIngressReadyCondition,
				Status:             metav1.ConditionFalse,
				Reason:             CondReasonHTTPRouteFailed,
				Message:            err.Error(),
				ObservedGeneration: ingress.Generation,
			})
	}

	// Build URL
	url := fmt.Sprintf("https://%s.%s", sandboxID, r.IngressDomain)

	// Update status with success
	return r.updateStatus(ctx, ingress, baseIngress, &url,
		metav1.Condition{
			Type:               SandboxIngressSandboxReadyCondition,
			Status:             metav1.ConditionTrue,
			Reason:             CondReasonSandboxReady,
			Message:            "Sandbox is ready",
			ObservedGeneration: ingress.Generation,
		},
		metav1.Condition{
			Type:               SandboxIngressServiceCondition,
			Status:             metav1.ConditionTrue,
			Reason:             CondReasonServiceCreated,
			Message:            "Service created",
			ObservedGeneration: ingress.Generation,
		},
		metav1.Condition{
			Type:               SandboxIngressHTTPRouteCondition,
			Status:             metav1.ConditionTrue,
			Reason:             CondReasonHTTPRouteCreated,
			Message:            "HTTPRoute created",
			ObservedGeneration: ingress.Generation,
		},
		metav1.Condition{
			Type:               SandboxIngressReadyCondition,
			Status:             metav1.ConditionTrue,
			Reason:             CondReasonIngressReady,
			Message:            fmt.Sprintf("Ingress ready at %s", url),
			ObservedGeneration: ingress.Generation,
		})
}

func (r *SandboxIngressReconciler) updateStatus(
	ctx context.Context,
	ingress *sandboxv1alpha1.SandboxIngress,
	baseIngress *sandboxv1alpha1.SandboxIngress,
	url *string,
	conditions ...metav1.Condition,
) (ctrl.Result, error) {
	if ingress.Status.Conditions == nil {
		ingress.Status.Conditions = []metav1.Condition{}
	}

	for _, cond := range conditions {
		meta.SetStatusCondition(&ingress.Status.Conditions, cond)
	}

	if url != nil {
		ingress.Status.URL = *url
	}

	if baseIngress == nil {
		baseIngress = ingress
	}

	if err := r.Status().Patch(ctx, ingress, client.MergeFrom(baseIngress)); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *SandboxIngressReconciler) ensureService(ctx context.Context, ingress *sandboxv1alpha1.SandboxIngress, sandbox *sandboxv1alpha1.Sandbox) error {
	log := logf.FromContext(ctx)

	serviceName := ingress.GetServiceName()

	// Check if Service exists
	existingSvc := &corev1.Service{}
	err := r.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: ingress.Namespace}, existingSvc)
	if err == nil {
		// Service exists, nothing to do
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}

	// Get sandbox-id label for selector
	sandboxID := sandbox.Labels["sandbox-id"]
	if sandboxID == "" {
		sandboxID = sandbox.Name
	}

	// Create Service
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: ingress.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "isola-operator",
				"sandbox.isola.run/ingress":    ingress.Name,
			},
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"sandbox-id": sandboxID,
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       80,
					TargetPort: intstr.FromInt32(ingress.Spec.ContainerPort),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}

	if err := controllerutil.SetControllerReference(ingress, svc, r.Scheme); err != nil {
		return err
	}

	log.Info("Creating Service", "name", serviceName)
	return r.Create(ctx, svc)
}

func (r *SandboxIngressReconciler) ensureHTTPRoute(ctx context.Context, ingress *sandboxv1alpha1.SandboxIngress, sandboxID string) error {
	log := logf.FromContext(ctx)

	routeName := ingress.GetHTTPRouteName()

	// Check if HTTPRoute exists
	existingRoute := &unstructured.Unstructured{}
	existingRoute.SetGroupVersionKind(httpRouteGVK)
	err := r.Get(ctx, types.NamespacedName{Name: routeName, Namespace: ingress.Namespace}, existingRoute)
	if err == nil {
		// HTTPRoute exists, nothing to do
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}

	// Build hostname
	hostname := fmt.Sprintf("%s.%s", sandboxID, r.IngressDomain)

	// Create HTTPRoute using unstructured
	route := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "gateway.networking.k8s.io/v1",
			"kind":       "HTTPRoute",
			"metadata": map[string]interface{}{
				"name":      routeName,
				"namespace": ingress.Namespace,
				"labels": map[string]interface{}{
					"app.kubernetes.io/managed-by": "isola-operator",
					"sandbox.isola.run/ingress":    ingress.Name,
				},
			},
			"spec": map[string]interface{}{
				"parentRefs": []interface{}{
					map[string]interface{}{
						"name":      r.GatewayName,
						"namespace": r.GatewayNamespace,
					},
				},
				"hostnames": []interface{}{
					hostname,
				},
				"rules": []interface{}{
					map[string]interface{}{
						"backendRefs": []interface{}{
							map[string]interface{}{
								"name": ingress.GetServiceName(),
								"port": int64(80),
							},
						},
					},
				},
			},
		},
	}

	// Set owner reference
	if err := controllerutil.SetControllerReference(ingress, route, r.Scheme); err != nil {
		return err
	}

	log.Info("Creating HTTPRoute", "name", routeName, "hostname", hostname)
	return r.Create(ctx, route)
}

// SetupWithManager sets up the controller with the Manager.
func (r *SandboxIngressReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Field index for sandboxRef lookups
	if err := mgr.GetFieldIndexer().IndexField(
		context.Background(),
		&sandboxv1alpha1.SandboxIngress{},
		ingressSandboxRefField,
		extractSandboxRefName,
	); err != nil {
		return err
	}

	// Create unstructured for HTTPRoute watching
	httpRoute := &unstructured.Unstructured{}
	httpRoute.SetGroupVersionKind(httpRouteGVK)

	return ctrl.NewControllerManagedBy(mgr).
		For(&sandboxv1alpha1.SandboxIngress{}).
		Owns(&corev1.Service{}).
		Owns(httpRoute).
		// Watch Sandbox changes to reconcile affected ingresses
		Watches(
			&sandboxv1alpha1.Sandbox{},
			handler.EnqueueRequestsFromMapFunc(r.findIngressesForSandbox),
		).
		Named("sandboxingress").
		Complete(r)
}

func extractSandboxRefName(obj client.Object) []string {
	ingress, ok := obj.(*sandboxv1alpha1.SandboxIngress)
	if !ok || ingress.Spec.SandboxRef == "" {
		return nil
	}
	return []string{ingress.Spec.SandboxRef}
}

func (r *SandboxIngressReconciler) findIngressesForSandbox(ctx context.Context, sandbox client.Object) []reconcile.Request {
	ingressList := &sandboxv1alpha1.SandboxIngressList{}
	if err := r.List(ctx, ingressList,
		client.InNamespace(sandbox.GetNamespace()),
		client.MatchingFields{ingressSandboxRefField: sandbox.GetName()},
	); err != nil {
		return nil
	}

	requests := make([]reconcile.Request, 0, len(ingressList.Items))
	for _, ingress := range ingressList.Items {
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      ingress.Name,
				Namespace: ingress.Namespace,
			},
		})
	}
	return requests
}
