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
	"maps"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
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

	sandboxv1alpha1 "github.com/isola-ai/isola-sb/api/v1alpha1"
	netbuilder "github.com/isola-ai/isola-sb/internal/operator/controller/network"
	"github.com/isola-ai/isola-sb/internal/operator/controller/podutil"
	"github.com/isola-ai/isola-sb/internal/operator/controller/snapshot"
	"k8s.io/client-go/tools/record"
)

const (
	// Summary condition
	SandboxReadyCondition = "Ready"

	// kstatus standard conditions (abnormal-true pattern)
	SandboxReconcilingCondition = "Reconciling"
	SandboxStalledCondition     = "Stalled"

	SandboxTemplateReadyCondition  = "TemplateReady"
	SandboxPodReadyCondition       = "PodReady"
	SandboxNetworkReadyCondition   = "NetworkConfigured"
	SandboxRootfsSnapshotCondition = "RootfsSnapshot"
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
	Scheme              *runtime.Scheme
	Recorder            record.EventRecorder
	SandboxSidecarImage string
	RuntimeClassName    string                        // RuntimeClassName to use for sandbox pods (e.g. "gvisor"). Empty means use cluster default.
	PriorityClassName   string                        // PriorityClassName to use for sandbox pods. Empty means use cluster default.
	ImagePullSecrets    []corev1.LocalObjectReference // ImagePullSecrets for pulling sandbox-sidecar images from private registries.
	Clock               Clock                         // Clock interface for time operations, allows mocking in tests
}

const (
	sandboxSidecarContainerName = "sandbox-sidecar"

	// Field index for efficient lookup of sandboxes by templates references
	sandboxTemplateRefField = ".spec.templateRef.name"

	// Network labels for pod selection by Helm-installed NetworkPolicies
	LabelAllowInternet   = "isola.run/allow-internet"
	LabelAllowClusterDNS = "isola.run/allow-cluster-dns"
)

func (r *SandboxReconciler) clock() Clock {
	if r.Clock != nil {
		return r.Clock
	}
	return RealClock{}
}

func buildNetworkLabels(network *sandboxv1alpha1.NetworkSpec) map[string]string {
	labels := make(map[string]string)
	if network == nil {
		return labels
	}
	if network.AllowAllInternet {
		labels[LabelAllowInternet] = "true"
	}
	if network.AllowClusterDNS {
		labels[LabelAllowClusterDNS] = "true"
	}
	return labels
}

func (r *SandboxReconciler) buildSandboxSidecarContainer() corev1.Container {
	rp := corev1.ContainerRestartPolicyAlways
	return corev1.Container{
		Name:          sandboxSidecarContainerName,
		Image:         r.SandboxSidecarImage,
		RestartPolicy: &rp,
		// RunAsUser 0 (root) is needed to read /proc/<pid>/environ of other users' processes
		// and to access /proc/<pid>/root when using shared PID namespace.
		SecurityContext: &corev1.SecurityContext{
			RunAsUser: ptr.To(int64(0)),
		},
	}
}

func (r *SandboxReconciler) injectSandboxSidecar(sandboxPod *corev1.Pod) error {
	if len(sandboxPod.Spec.Containers) != 1 {
		// todo: remove this assumption
		return fmt.Errorf("sandbox pod must have exactly one container")
	}

	// todo benl: Mark with sandboxPod.Spec.Containers[i].Name
	// Mark the first container as the main container so the sidecar can discover it via /proc/<pid>/environ.
	// Note: a single main container is supported. The sidecar's findMarkedProcess() returns the first PID it finds with the ISOLA_MAIN_CONTAINER marker.
	sandboxPod.Spec.Containers[0].Env = append(sandboxPod.Spec.Containers[0].Env, corev1.EnvVar{
		Name:  "ISOLA_MAIN_CONTAINER",
		Value: "true",
	})

	sidecarContainer := r.buildSandboxSidecarContainer()
	sandboxPod.Spec.InitContainers = append(sandboxPod.Spec.InitContainers, sidecarContainer)
	return nil
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

// setReconciling sets the kstatus Reconciling condition to True with the given reason/message.
// Per kstatus "abnormal-true" pattern, Reconciling=True signals InProgress status.
func (r *SandboxReconciler) setReconciling(ctx context.Context, baseSandbox, sandbox *sandboxv1alpha1.Sandbox, reason, message string) error {
	return r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
		{
			Type:               SandboxReconcilingCondition,
			Status:             metav1.ConditionTrue,
			Reason:             reason,
			Message:            message,
			ObservedGeneration: sandbox.Generation,
		},
	})
}

// clearReconciling removes the Reconciling condition to signal reconciliation is complete.
// Per kstatus "abnormal-true" pattern, absence of Reconciling means Current status.
func (r *SandboxReconciler) clearReconciling(ctx context.Context, baseSandbox, sandbox *sandboxv1alpha1.Sandbox) error {
	meta.RemoveStatusCondition(&sandbox.Status.Conditions, SandboxReconcilingCondition)
	sandbox.Status.ObservedGeneration = sandbox.Generation
	return r.Status().Patch(ctx, sandbox, client.MergeFrom(baseSandbox))
}

// setStalled sets the kstatus Stalled condition to True with the given reason/message.
// Per kstatus "abnormal-true" pattern, Stalled=True signals Failed status.
func (r *SandboxReconciler) setStalled(ctx context.Context, baseSandbox, sandbox *sandboxv1alpha1.Sandbox, reason, message string) error {
	// When stalled, we're no longer reconciling
	meta.RemoveStatusCondition(&sandbox.Status.Conditions, SandboxReconcilingCondition)
	sandbox.Status.ObservedGeneration = sandbox.Generation
	meta.SetStatusCondition(&sandbox.Status.Conditions, metav1.Condition{
		Type:               SandboxStalledCondition,
		Status:             metav1.ConditionTrue,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: sandbox.Generation,
	})
	return r.Status().Patch(ctx, sandbox, client.MergeFrom(baseSandbox))
}

// clearStalled removes the Stalled condition.
func (r *SandboxReconciler) clearStalled(ctx context.Context, baseSandbox, sandbox *sandboxv1alpha1.Sandbox) error {
	meta.RemoveStatusCondition(&sandbox.Status.Conditions, SandboxStalledCondition)
	return r.Status().Patch(ctx, sandbox, client.MergeFrom(baseSandbox))
}

// finalizeStatus updates observedGeneration and clears Reconciling to signal completion.
// Call this at the end of a successful reconciliation loop.
func (r *SandboxReconciler) finalizeStatus(ctx context.Context, baseSandbox, sandbox *sandboxv1alpha1.Sandbox) error {
	meta.RemoveStatusCondition(&sandbox.Status.Conditions, SandboxReconcilingCondition)
	meta.RemoveStatusCondition(&sandbox.Status.Conditions, SandboxStalledCondition)
	sandbox.Status.ObservedGeneration = sandbox.Generation
	return r.Status().Patch(ctx, sandbox, client.MergeFrom(baseSandbox))
}

func (r *SandboxReconciler) CreateSandboxPod(ctx context.Context, sandbox *sandboxv1alpha1.Sandbox, baseSandbox *sandboxv1alpha1.Sandbox, template *sandboxv1alpha1.SandboxTemplate) error {
	log := logf.FromContext(ctx).WithValues("sandbox", sandbox.Name, "namespace", sandbox.Namespace)
	// todo benl reduce verbose logging
	log.Info("Creating Pod")

	// Apply template labels first, then override with standard Kubernetes labels.
	// This prevents templates from overriding app.kubernetes.io/*, isola.run/*, etc.
	labels := make(map[string]string)
	if template.Spec.PodTemplate.Labels != nil {
		maps.Copy(labels, template.Spec.PodTemplate.Labels)
	}

	// Standard Kubernetes recommended labels (https://kubernetes.io/docs/concepts/overview/working-with-objects/common-labels/)
	labels["app.kubernetes.io/name"] = "isola-sandbox"
	labels["app.kubernetes.io/instance"] = sandbox.Name
	labels["app.kubernetes.io/component"] = "sandbox"
	labels["app.kubernetes.io/part-of"] = "isola"
	labels["app.kubernetes.io/managed-by"] = "isola-operator"

	labels["sandbox.isola.run/id"] = sandbox.Name
	labels["cluster-autoscaler.kubernetes.io/safe-to-evict"] = "false"

	maps.Copy(labels, buildNetworkLabels(sandbox.Spec.Network))

	// todo benl: why this exists? ("sandbox-id")
	if sandbox.Labels != nil {
		if sandboxID, exists := sandbox.Labels["sandbox-id"]; exists {
			labels["sandbox-id"] = sandboxID
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

	// Enable shared PID namespace so the sidecar can locate the main container and access its filesystem via /proc/<pid>/root
	sandboxPod.Spec.ShareProcessNamespace = ptr.To(true)

	if r.PriorityClassName != "" {
		sandboxPod.Spec.PriorityClassName = r.PriorityClassName
	}

	configureDNS(sandboxPod, sandbox.Spec.Network)

	// Inject imagePullSecrets for private registries (configured via Helm global.imagePullSecrets)
	if len(r.ImagePullSecrets) > 0 {
		sandboxPod.Spec.ImagePullSecrets = append(sandboxPod.Spec.ImagePullSecrets, r.ImagePullSecrets...)
	}

	if err := r.injectSandboxSidecar(sandboxPod); err != nil {
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

		// Best effort status patch - log but don't override the original create error
		if patchErr := r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
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
		}); patchErr != nil {
			log.Error(patchErr, "Failed to patch status after pod creation failure")
		}
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

func configureDNS(sandboxPod *corev1.Pod, network *sandboxv1alpha1.NetworkSpec) {
	allowClusterDNS := network != nil && network.AllowClusterDNS

	if allowClusterDNS {
		sandboxPod.Spec.DNSPolicy = corev1.DNSClusterFirst
		if len(network.Nameservers) > 0 {
			if sandboxPod.Spec.DNSConfig == nil {
				sandboxPod.Spec.DNSConfig = &corev1.PodDNSConfig{}
			}
			// todo benl: log warn if pod spec has nameservers already?
			sandboxPod.Spec.DNSConfig.Nameservers = network.Nameservers
		}
	} else {
		sandboxPod.Spec.DNSPolicy = corev1.DNSNone

		var nameservers []string
		var dnsOptions []corev1.PodDNSConfigOption
		if network != nil && len(network.Nameservers) > 0 {
			nameservers = network.Nameservers
			dnsOptions = []corev1.PodDNSConfigOption{
				// ndots:1 - external domains are tried directly without search domain suffix
				{Name: "ndots", Value: ptr.To("1")},
			}
		} else {
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
	}
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

func getShutdownSnapshotName(sandbox *sandboxv1alpha1.Sandbox) string {
	return sandbox.Name + "-shutdown"
}

func (r *SandboxReconciler) getShutdownSnapshot(ctx context.Context, sandbox *sandboxv1alpha1.Sandbox) (*sandboxv1alpha1.RootfsSnapshot, error) {
	snap := &sandboxv1alpha1.RootfsSnapshot{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      getShutdownSnapshotName(sandbox),
		Namespace: sandbox.Namespace,
	}, snap)
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return snap, nil
}

func (r *SandboxReconciler) EnsureTemplate(ctx context.Context, sandbox *sandboxv1alpha1.Sandbox, baseSandbox *sandboxv1alpha1.Sandbox) (*sandboxv1alpha1.SandboxTemplate, ctrl.Result, error) {
	log := logf.FromContext(ctx).WithValues("sandbox", sandbox.Name, "namespace", sandbox.Namespace)
	template := &sandboxv1alpha1.SandboxTemplate{}

	if err := r.Get(ctx, types.NamespacedName{Name: sandbox.Spec.TemplateRef.Name, Namespace: sandbox.Namespace}, template); err != nil {
		if apierrors.IsNotFound(err) {
			// Template not found is a stalled state - kstatus will report Failed
			sandbox.Status.ObservedGeneration = sandbox.Generation
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
					{
						Type:               SandboxStalledCondition,
						Status:             metav1.ConditionTrue,
						Reason:             CondReasonTemplateNotFound,
						Message:            fmt.Sprintf("SandboxTemplate %q not found", sandbox.Spec.TemplateRef.Name),
						ObservedGeneration: sandbox.Generation,
					},
					{
						Type:               SandboxReconcilingCondition,
						Status:             metav1.ConditionFalse,
						Reason:             CondReasonTemplateNotFound,
						Message:            "Cannot reconcile without template",
						ObservedGeneration: sandbox.Generation,
					},
				},
			); err != nil {
				log.Error(err, "Failed to update Sandbox status")
				return nil, ctrl.Result{}, err
			}

			log.Error(err, "Sandbox template not found")
			// SandboxTemplate watch will trigger reconciliation when template is created
			return nil, ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get Sandbox template")
		return nil, ctrl.Result{}, err
	}

	// Template found - clear any previous Stalled condition
	if err := r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
		{
			Type:               SandboxTemplateReadyCondition,
			Status:             metav1.ConditionTrue,
			Reason:             CondReasonTemplateResolved,
			Message:            "Template resolved",
			ObservedGeneration: sandbox.Generation,
		},
		{
			Type:               SandboxStalledCondition,
			Status:             metav1.ConditionFalse,
			Reason:             CondReasonTemplateResolved,
			Message:            "Template is available",
			ObservedGeneration: sandbox.Generation,
		},
	}); err != nil {
		log.Error(err, "Failed to update Sandbox status")
		return nil, ctrl.Result{}, err
	}

	return template, ctrl.Result{}, nil
}

// ensureCustomNetworkPolicy creates or updates a custom NetworkPolicy for sandboxes
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

	if sandboxPod != nil {
		sandbox.Status.PodIP = sandboxPod.Status.PodIP
	}

	networkCondition := r.determineNetworkCondition(sandbox)
	conditions = append(conditions, networkCondition)

	shutdownSnapshot, err := r.getShutdownSnapshot(ctx, sandbox)
	if err != nil {
		return err
	}
	snapshotCondition := r.determineSnapshotCondition(sandbox, shutdownSnapshot)
	conditions = append(conditions, snapshotCondition)

	readyCondition := r.determineReadyCondition(sandbox, sandboxPod)
	conditions = append(conditions, readyCondition)

	// kstatus: Per "abnormal-true" pattern, remove conditions when not in abnormal state
	r.updateKstatusConditions(sandbox, sandboxPod)

	// Always update observedGeneration to indicate we've processed this spec version
	sandbox.Status.ObservedGeneration = sandbox.Generation

	return r.patchStatus(ctx, baseSandbox, sandbox, conditions)
}

// updateKstatusConditions manages Reconciling/Stalled conditions per kstatus abnormal-true pattern.
// Conditions are removed when not in abnormal state, added when abnormal.
func (r *SandboxReconciler) updateKstatusConditions(sandbox *sandboxv1alpha1.Sandbox, sandboxPod *corev1.Pod) {
	// If pod is ready, remove all abnormal conditions
	if podutil.IsPodReady(sandboxPod) {
		meta.RemoveStatusCondition(&sandbox.Status.Conditions, SandboxReconcilingCondition)
		meta.RemoveStatusCondition(&sandbox.Status.Conditions, SandboxStalledCondition)
		return
	}

	// If pod terminated
	if podutil.IsPodTerminated(sandboxPod) {
		meta.RemoveStatusCondition(&sandbox.Status.Conditions, SandboxReconcilingCondition)
		if sandboxPod.Status.Phase == corev1.PodFailed {
			meta.SetStatusCondition(&sandbox.Status.Conditions, metav1.Condition{
				Type:               SandboxStalledCondition,
				Status:             metav1.ConditionTrue,
				Reason:             CondReasonPodFailed,
				Message:            "Sandbox pod has failed",
				ObservedGeneration: sandbox.Generation,
			})
		} else {
			// Pod succeeded - clear stalled if it was set
			meta.RemoveStatusCondition(&sandbox.Status.Conditions, SandboxStalledCondition)
		}
		return
	}

	// Pod is pending or not yet created - set Reconciling, clear Stalled
	meta.RemoveStatusCondition(&sandbox.Status.Conditions, SandboxStalledCondition)
	meta.SetStatusCondition(&sandbox.Status.Conditions, metav1.Condition{
		Type:               SandboxReconcilingCondition,
		Status:             metav1.ConditionTrue,
		Reason:             CondReasonReconciling,
		Message:            "Waiting for sandbox pod to become ready",
		ObservedGeneration: sandbox.Generation,
	})
}

// determinePodCondition returns the PodReady condition based on the sandbox pod state.
func (r *SandboxReconciler) determinePodCondition(sandbox *sandboxv1alpha1.Sandbox, sandboxPod *corev1.Pod) metav1.Condition {
	if podutil.IsPodReady(sandboxPod) {
		return metav1.Condition{
			Type:               SandboxPodReadyCondition,
			Status:             metav1.ConditionTrue,
			Reason:             CondReasonPodRunning,
			Message:            "Pod is running",
			ObservedGeneration: sandbox.Generation,
		}
	}

	if podutil.IsPodTerminated(sandboxPod) {
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

func (r *SandboxReconciler) determineSnapshotCondition(sandbox *sandboxv1alpha1.Sandbox, snap *sandboxv1alpha1.RootfsSnapshot) metav1.Condition {
	if snap == nil {
		return metav1.Condition{
			Type:               SandboxRootfsSnapshotCondition,
			Status:             metav1.ConditionFalse,
			Reason:             "NoSnapshot",
			Message:            "No shutdown snapshot exists",
			ObservedGeneration: sandbox.Generation,
		}
	}

	if snap.Status.CompletedAt == nil {
		return metav1.Condition{
			Type:               SandboxRootfsSnapshotCondition,
			Status:             metav1.ConditionFalse,
			Reason:             CondReasonSnapshottingInProgress,
			Message:            fmt.Sprintf("Snapshot %q is in progress", snap.Name),
			ObservedGeneration: sandbox.Generation,
		}
	}

	readyCond := meta.FindStatusCondition(snap.Status.Conditions, string(sandboxv1alpha1.RootfsSnapshotComplete))
	if readyCond == nil {
		return metav1.Condition{
			Type:               SandboxRootfsSnapshotCondition,
			Status:             metav1.ConditionFalse,
			Reason:             CondReasonSnapshottingInProgress,
			Message:            fmt.Sprintf("Snapshot %q status unknown", snap.Name),
			ObservedGeneration: sandbox.Generation,
		}
	}

	if readyCond.Status == metav1.ConditionTrue {
		message := fmt.Sprintf("Snapshot %q completed", snap.Name)
		if snap.Status.Revision > 0 {
			message = fmt.Sprintf("Snapshot %q completed (revision %d)", snap.Name, snap.Status.Revision)
		}
		return metav1.Condition{
			Type:               SandboxRootfsSnapshotCondition,
			Status:             metav1.ConditionTrue,
			Reason:             CondReasonSnapshotComplete,
			Message:            message,
			ObservedGeneration: sandbox.Generation,
		}
	}

	return metav1.Condition{
		Type:               SandboxRootfsSnapshotCondition,
		Status:             metav1.ConditionFalse,
		Reason:             CondReasonSnapshotFailed,
		Message:            fmt.Sprintf("Snapshot %q failed: %s", snap.Name, readyCond.Message),
		ObservedGeneration: sandbox.Generation,
	}
}

func (r *SandboxReconciler) determineNetworkCondition(sandbox *sandboxv1alpha1.Sandbox) metav1.Condition {
	// Network configuration is now static (Helm-installed policies + optional custom policy).
	// The network is considered ready once the pod is created (policies apply immediately).
	return metav1.Condition{
		Type:               SandboxNetworkReadyCondition,
		Status:             metav1.ConditionTrue,
		Reason:             CondReasonNetworkPolicyApplied,
		Message:            "Network configuration applied",
		ObservedGeneration: sandbox.Generation,
	}
}

// determineReadyCondition returns the aggregate Ready condition.
// Sandbox is ready when pod is ready.
func (r *SandboxReconciler) determineReadyCondition(sandbox *sandboxv1alpha1.Sandbox, sandboxPod *corev1.Pod) metav1.Condition {
	if podutil.IsPodTerminated(sandboxPod) {
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

	if !podutil.IsPodReady(sandboxPod) {
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
	case sandboxv1alpha1.ShutdownPolicySnapshotRootfs:
		return r.handleRootfsSnapshot(ctx, sandbox, baseSandbox, sandboxPod, snapshotDeadline, r.getActiveDeadlineSeconds(template))
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

func (r *SandboxReconciler) handleRootfsSnapshot(
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
		log.Info("Rootfs snapshot timed out", "deadline", snapshotDeadline)
		if err := r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
			{
				Type:               SandboxRootfsSnapshotCondition,
				Status:             metav1.ConditionFalse,
				Reason:             CondReasonSnapshotTimeout,
				Message:            "Rootfs snapshot did not complete before deadline",
				ObservedGeneration: sandbox.Generation,
			},
		}); err != nil {
			return ctrl.Result{}, false, err
		}
		// return true for cleanupDone so the sandbox gets deleted and as a result
		// the rootfssnapshot due to it being owned by the sandboxed
		return ctrl.Result{}, true, nil
	}

	if sandboxPod == nil {
		log.Info("Skipping rootfs snapshot because sandbox pod is missing")
		if err := r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
			{
				Type:               SandboxRootfsSnapshotCondition,
				Status:             metav1.ConditionFalse,
				Reason:             CondReasonSnapshotFailed,
				Message:            "Sandbox pod no longer exists; snapshot skipped",
				ObservedGeneration: sandbox.Generation,
			},
		}); err != nil {
			return ctrl.Result{}, false, err
		}
		return ctrl.Result{}, true, nil
	}

	if !podutil.IsPodReady(sandboxPod) {
		log.Info("Unable to perform rootfs snapshot: pod not ready")
		r.Recorder.Event(sandbox, corev1.EventTypeWarning, "PodNotReady", "Unable to perform rootfs snapshot: pod not ready")

		if err := r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
			{
				Type:               SandboxRootfsSnapshotCondition,
				Status:             metav1.ConditionFalse,
				Reason:             CondReasonSnapshotFailed,
				Message:            "Sandbox pod is not ready",
				ObservedGeneration: sandbox.Generation,
			},
		}); err != nil {
			return ctrl.Result{}, false, err
		}
		return ctrl.Result{}, true, nil
	}

	supported, err := snapshot.CheckRootfsSnapshotSupport(ctx, r.Client, sandboxPod)
	if err != nil {
		log.Error(err, "Failed to validate snapshotting support")
		return ctrl.Result{}, false, err
	}

	if !supported {
		log.Info("Unable to perform rootfs snapshot: runtime not supported")
		r.Recorder.Event(sandbox, corev1.EventTypeWarning, "RuntimeNotSupported", "Unable to perform rootfs snapshot")

		if err := r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
			{
				Type:               SandboxRootfsSnapshotCondition,
				Status:             metav1.ConditionFalse,
				Reason:             CondReasonInvalidRuntime,
				Message:            "Runtime does not support rootfs snapshotting",
				ObservedGeneration: sandbox.Generation,
			},
		}); err != nil {
			return ctrl.Result{}, false, err
		}
		return ctrl.Result{}, true, nil
	}

	snap, err := r.getShutdownSnapshot(ctx, sandbox)
	if err != nil {
		return ctrl.Result{}, false, err
	}

	if snap == nil {
		return r.createShutdownSnapshot(ctx, sandbox, baseSandbox, activeDeadlineSeconds)
	}

	snapshotName := snap.Name
	if snap.Status.CompletedAt == nil {
		log.Info("Snapshot in progress, waiting", "snapshot", snapshotName)
		if err := r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
			{
				Type:               SandboxRootfsSnapshotCondition,
				Status:             metav1.ConditionFalse,
				Reason:             CondReasonSnapshottingInProgress,
				Message:            fmt.Sprintf("Snapshot %q is running", snapshotName),
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

	readyCond := meta.FindStatusCondition(snap.Status.Conditions, string(sandboxv1alpha1.RootfsSnapshotComplete))
	if readyCond != nil && readyCond.Status == metav1.ConditionTrue {
		log.Info("Snapshot completed successfully", "snapshot", snapshotName)
		r.Recorder.Event(sandbox, corev1.EventTypeNormal, "SnapshotSucceeded", fmt.Sprintf("Snapshot %q completed", snapshotName))
		if err := r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
			{
				Type:               SandboxRootfsSnapshotCondition,
				Status:             metav1.ConditionTrue,
				Reason:             CondReasonSnapshotComplete,
				Message:            fmt.Sprintf("Snapshot %q completed", snapshotName),
				ObservedGeneration: sandbox.Generation,
			},
		}); err != nil {
			return ctrl.Result{}, false, err
		}
		return ctrl.Result{}, true, nil
	}

	// Completed but failed - proceed with deletion anyway
	message := "Snapshot failed"
	if readyCond != nil && readyCond.Message != "" {
		message = readyCond.Message
	}
	log.Info("Snapshot failed, proceeding with deletion", "snapshot", snapshotName)
	r.Recorder.Event(sandbox, corev1.EventTypeWarning, "SnapshotFailed", message)
	if err := r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
		{
			Type:               SandboxRootfsSnapshotCondition,
			Status:             metav1.ConditionFalse,
			Reason:             CondReasonSnapshotFailed,
			Message:            message,
			ObservedGeneration: sandbox.Generation,
		},
	}); err != nil {
		return ctrl.Result{}, false, err
	}
	return ctrl.Result{}, true, nil
}

func (r *SandboxReconciler) createShutdownSnapshot(
	ctx context.Context,
	sandbox *sandboxv1alpha1.Sandbox,
	baseSandbox *sandboxv1alpha1.Sandbox,
	activeDeadlineSeconds int64,
) (ctrl.Result, bool, error) {
	log := logf.FromContext(ctx).WithValues("sandbox", sandbox.Name, "namespace", sandbox.Namespace)

	snapshotName := getShutdownSnapshotName(sandbox)
	rootfsSnapshot := &sandboxv1alpha1.RootfsSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name:      snapshotName,
			Namespace: sandbox.Namespace,
			Labels: map[string]string{
				LabelSandboxName:            sandbox.Name,
				"sandbox.isola.run/trigger": "shutdown",
			},
		},
		Spec: sandboxv1alpha1.RootfsSnapshotSpec{
			SandboxName:           sandbox.Name,
			ActiveDeadlineSeconds: &activeDeadlineSeconds,
		},
	}

	// Set owner reference for cascade delete (if sandbox is force-deleted)
	if err := controllerutil.SetControllerReference(sandbox, rootfsSnapshot, r.Scheme); err != nil {
		log.Error(err, "Failed to set controller reference on RootfsSnapshot")
		return ctrl.Result{}, false, err
	}

	if err := r.Create(ctx, rootfsSnapshot); err != nil {
		if apierrors.IsAlreadyExists(err) {
			// Race condition - another reconcile created it, just requeue
			return ctrl.Result{RequeueAfter: time.Second}, false, nil
		}
		log.Error(err, "Failed to create RootfsSnapshot")
		return ctrl.Result{}, false, err
	}

	log.Info("Created shutdown RootfsSnapshot", "name", rootfsSnapshot.Name)
	r.Recorder.Event(sandbox, corev1.EventTypeNormal, "RootfsSnapshotCreated", fmt.Sprintf("Created RootfsSnapshot %q", rootfsSnapshot.Name))

	if err := r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
		{
			Type:               SandboxRootfsSnapshotCondition,
			Status:             metav1.ConditionFalse,
			Reason:             CondReasonSnapshottingInProgress,
			Message:            fmt.Sprintf("RootfsSnapshot %q created, waiting for completion", rootfsSnapshot.Name),
			ObservedGeneration: sandbox.Generation,
		},
	}); err != nil {
		return ctrl.Result{}, false, err
	}

	return ctrl.Result{RequeueAfter: time.Second}, false, nil
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
