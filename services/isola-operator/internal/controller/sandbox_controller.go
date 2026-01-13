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

	sandboxv1alpha1 "github.com/omereli/dev-isola/services/isola-operator/api/v1alpha1"
	"github.com/omereli/dev-isola/services/isola-operator/internal/controller/network"
	"github.com/omereli/dev-isola/services/isola-operator/internal/controller/podutil"
	"github.com/omereli/dev-isola/services/isola-operator/internal/controller/snapshot"
	"k8s.io/client-go/tools/record"
)

const (
	// Summary condition
	SandboxReadyCondition = "Ready"

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
	CondReasonNetworkPolicyApplied    = "NetworkPolicyApplied"
	CondReasonNetworkPolicyFailed     = "NetworkPolicyFailed"
	CondReasonNetworkConfigNotApplied = "NetworkConfigNotApplied"
	CondReasonNetworkPolicyMissing    = "NetworkPolicyMissing"
	CondReasonInvalidNetworkPolicy    = "InvalidNetworkPolicy"
	CondReasonPodDeletedForSafety     = "PodDeletedForSafety"
	CondReasonNetworkTemplateDeleting = "NetworkTemplateDeleting"
	CondReasonNoNetworkPolicy         = "NoNetworkPolicy"
	CondReasonNetworkTemplateNotFound = "NetworkTemplateNotFound"
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
	Scheme           *runtime.Scheme
	Recorder         record.EventRecorder
	AgentImage       string
	RuntimeClassName string // RuntimeClassName to use for sandbox pods (e.g. "gvisor"). Empty means use cluster default.
	Clock            Clock  // Clock interface for time operations, allows mocking in tests
}

const (
	agentContainerName = "isola-agent"

	// Field index for efficient lookup of sandboxes by templates references
	sandboxTemplateRefField        = ".spec.templateRef.name"
	sandboxNetworkTemplateRefField = ".spec.network.effectiveTemplateName"

	// Condition reason for owned NetworkTemplate errors
	CondReasonOwnedTemplateError = "OwnedTemplateError"

	NetworkTemplateFinalizer = "sandbox.isola.run/network-template-cleanup"
)

// clock returns the reconciler's Clock, defaulting to RealClock if not set
func (r *SandboxReconciler) clock() Clock {
	if r.Clock != nil {
		return r.Clock
	}
	return RealClock{}
}

func isNetworkTemplateReady(networkTemplate *sandboxv1alpha1.NetworkTemplate) bool {
	if networkTemplate == nil {
		return false
	}

	readyCond := meta.FindStatusCondition(networkTemplate.Status.Conditions,
		string(sandboxv1alpha1.NetworkTemplateReady))
	return readyCond != nil && readyCond.Status == metav1.ConditionTrue
}

func (r *SandboxReconciler) buildAgentContainer() corev1.Container {
	rp := corev1.ContainerRestartPolicyAlways
	return corev1.Container{
		Name:          agentContainerName,
		Image:         r.AgentImage,
		RestartPolicy: &rp,
		// RunAsUser 0 (root) is needed to read /proc/<pid>/environ of other users' processes
		// and to access /proc/<pid>/root when using shared PID namespace.
		SecurityContext: &corev1.SecurityContext{
			RunAsUser: ptr.To(int64(0)),
		},
	}
}

func (r *SandboxReconciler) injectSidecar(sandboxPod *corev1.Pod) error {
	if len(sandboxPod.Spec.Containers) != 1 {
		// todo: remove this assumption
		return fmt.Errorf("sandbox pod must have exactly one container")
	}

	// todo benl: Mark with sandboxPod.Spec.Containers[i].Name
	// Mark the first container as the main container so the agent can discover it via /proc/<pid>/environ.
	// Note: a single main container is supported. The agent's findMarkedProcess() returns the first PID it finds with the ISOLA_MAIN_CONTAINER marker.
	sandboxPod.Spec.Containers[0].Env = append(sandboxPod.Spec.Containers[0].Env, corev1.EnvVar{
		Name:  "ISOLA_MAIN_CONTAINER",
		Value: "true",
	})

	agentContainer := r.buildAgentContainer()
	sandboxPod.Spec.InitContainers = append(sandboxPod.Spec.InitContainers, agentContainer)
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

// todo benl: get sandbox if create failed with already exists?
func (r *SandboxReconciler) CreateSandboxPod(ctx context.Context, sandbox *sandboxv1alpha1.Sandbox, baseSandbox *sandboxv1alpha1.Sandbox, template *sandboxv1alpha1.SandboxTemplate, networkTemplate *sandboxv1alpha1.NetworkTemplate) error {
	log := logf.FromContext(ctx).WithValues("sandbox", sandbox.Name, "namespace", sandbox.Namespace)
	// todo benl reduce verbose logging
	log.Info("Creating Pod")

	labels := map[string]string{
		"app":                          "isola-sandbox",
		"sandbox.isola.run/id":         sandbox.Name,
		"app.kubernetes.io/managed-by": "isola-operator",
		"cluster-autoscaler.kubernetes.io/safe-to-evict": "false",
	}

	// Add network-template label if network config is specified.
	// This label is required for the created resource(s) (e.g. NetworkPolicy) to select this pod.
	if networkTemplate != nil {
		labels[network.NetworkTemplateLabelKey] = networkTemplate.Name
	}

	// todo benl: why this exists? ("sandbox-id")
	if sandbox.Labels != nil {
		if sandboxID, exists := sandbox.Labels["sandbox-id"]; exists {
			labels["sandbox-id"] = sandboxID
		}
	}

	if template.Spec.PodTemplate.Labels != nil {
		maps.Copy(labels, template.Spec.PodTemplate.Labels)
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

	// Set hostname and subdomain to enable DNS-based addressing via headless service.
	// Both are required for DNS to work: <hostname>.<subdomain>.<namespace>.svc.cluster.local
	sandboxPod.Spec.Hostname = getSandboxPodName(sandbox)
	sandboxPod.Spec.Subdomain = "sandbox-agents"

	// Enable shared PID namespace so the isola agent can locate the main container and access it's filesystem via /proc/<pid>/root
	sandboxPod.Spec.ShareProcessNamespace = ptr.To(true)

	// Set high priority to prevent preemption by applicative non-sandbox pods
	sandboxPod.Spec.PriorityClassName = "isola-sandbox"

	// todo benl: implement api to restore pod from snapshot (make sure they are compatible)
	// if sandboxPod.Annotations == nil {
	// 	sandboxPod.Annotations = map[string]string{}
	// }

	// sandboxPod.Annotations["dev.gvisor.tar.rootfs.upper.todobenl"] = "/tmp/rootfs-sandbox-870e5846-1766869560.tar"

	// Configure DNS for sandbox pods based on NetworkTemplate settings.
	if err := configureDNS(sandboxPod, networkTemplate); err != nil {
		log.Error(err, "Failed to configure DNS")
		return err
	}

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

// configureDNS sets up DNS configuration for the sandbox pod based on the NetworkTemplate.
// - DNSPolicy None: Uses only the specified nameservers. If empty, uses 127.0.0.1 sink with fast-fail options.
// - DNSPolicy ClusterFirst: Uses cluster DNS with optional additional nameservers.
func configureDNS(sandboxPod *corev1.Pod, networkTemplate *sandboxv1alpha1.NetworkTemplate) error {
	dnsPolicy := networkTemplate.Spec.DNSPolicy

	switch dnsPolicy {
	case corev1.DNSNone:
		sandboxPod.Spec.DNSPolicy = corev1.DNSNone
		nameservers := networkTemplate.Spec.Nameservers
		dnsOptions := []corev1.PodDNSConfigOption{
			// ndots:1 - external domains are tried directly without search domain suffix
			{Name: "ndots", Value: ptr.To("1")},
		}
		if len(nameservers) == 0 {
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

	case corev1.DNSClusterFirst, "":
		sandboxPod.Spec.DNSPolicy = corev1.DNSClusterFirst
		if len(networkTemplate.Spec.Nameservers) > 0 {
			if sandboxPod.Spec.DNSConfig == nil {
				sandboxPod.Spec.DNSConfig = &corev1.PodDNSConfig{}
			}
			sandboxPod.Spec.DNSConfig.Nameservers = networkTemplate.Spec.Nameservers
		}
		// When no additional nameservers, don't modify DNSConfig - use pod template defaults

	default:
		return fmt.Errorf("unsupported DNS policy: %q", dnsPolicy)
	}

	return nil
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

func getRootfsSnapshotName(sandbox *sandboxv1alpha1.Sandbox) string {
	return sandbox.Name + "-rootfs-snapshot"
}

func (r *SandboxReconciler) getRootfsSnapshot(ctx context.Context, sandbox *sandboxv1alpha1.Sandbox) (*sandboxv1alpha1.RootfsSnapshot, error) {
	snapshotName := getRootfsSnapshotName(sandbox)
	snap := &sandboxv1alpha1.RootfsSnapshot{}
	if err := r.Get(ctx, types.NamespacedName{Name: snapshotName, Namespace: sandbox.Namespace}, snap); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return snap, nil
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

// EnsureOwnedNetworkTemplateFromSpec creates or updates a sandbox-owned NetworkTemplate
// for sandboxes with embedded network spec (network.spec is set).
// The created NetworkTemplate is owned by the sandbox via OwnerReference
// and will be garbage-collected when the sandbox is deleted.
func (r *SandboxReconciler) EnsureOwnedNetworkTemplateFromSpec(
	ctx context.Context,
	sandbox *sandboxv1alpha1.Sandbox,
	baseSandbox *sandboxv1alpha1.Sandbox,
) (*sandboxv1alpha1.NetworkTemplate, error) {
	log := logf.FromContext(ctx).WithValues("sandbox", sandbox.Name, "namespace", sandbox.Namespace)

	if !sandbox.HasNetworkSpec() {
		return nil, nil
	}

	templateName := sandbox.GetOwnedNetworkTemplateName()
	log = log.WithValues("ownedNetworkTemplate", templateName)

	existing := &sandboxv1alpha1.NetworkTemplate{}
	err := r.Get(ctx, types.NamespacedName{Name: templateName, Namespace: sandbox.Namespace}, existing)

	if err == nil {
		// Template exists - verify ownership
		if !metav1.IsControlledBy(existing, sandbox) {
			log.Error(nil, "NetworkTemplate exists but is not owned by this sandbox")
			return nil, fmt.Errorf("NetworkTemplate %q exists but is not owned by sandbox %q", templateName, sandbox.Name)
		}
		return existing, nil
	}

	if !apierrors.IsNotFound(err) {
		log.Error(err, "Failed to get owned NetworkTemplate")
		return nil, err
	}

	// Create sandbox-owned NetworkTemplate
	log.Info("Creating owned NetworkTemplate")
	ownedTemplate := &sandboxv1alpha1.NetworkTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      templateName,
			Namespace: sandbox.Namespace,
			Labels: map[string]string{
				"sandbox.isola.run/owner": sandbox.Name,
				"sandbox.isola.run/owned": "true",
			},
		},
		Spec: *sandbox.Spec.Network.Spec,
	}

	if err := controllerutil.SetControllerReference(sandbox, ownedTemplate, r.Scheme); err != nil {
		log.Error(err, "Failed to set controller reference on owned NetworkTemplate")
		return nil, err
	}

	if err := r.Create(ctx, ownedTemplate); err != nil {
		if apierrors.IsAlreadyExists(err) {
			// Race condition - another reconcile created it, refetch
			if err := r.Get(ctx, types.NamespacedName{Name: templateName, Namespace: sandbox.Namespace}, existing); err != nil {
				return nil, err
			}
			return existing, nil
		}
		log.Error(err, "Failed to create owned NetworkTemplate")
		return nil, err
	}

	log.Info("Owned NetworkTemplate created")
	return ownedTemplate, nil
}

// EnsureNetworkTemplate fetches the NetworkTemplate for the sandbox.
// For embedded specs (network.spec), creates a sandbox-owned NetworkTemplate.
func (r *SandboxReconciler) EnsureNetworkTemplate(ctx context.Context, sandbox *sandboxv1alpha1.Sandbox, baseSandbox *sandboxv1alpha1.Sandbox) (*sandboxv1alpha1.NetworkTemplate, ctrl.Result, error) {
	log := logf.FromContext(ctx).WithValues("sandbox", sandbox.Name, "namespace", sandbox.Namespace)

	// For embedded specs, create/update the sandbox-owned NetworkTemplate first
	if sandbox.HasNetworkSpec() {
		_, err := r.EnsureOwnedNetworkTemplateFromSpec(ctx, sandbox, baseSandbox)
		if err != nil {
			if patchErr := r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
				{
					Type:               SandboxNetworkReadyCondition,
					Status:             metav1.ConditionFalse,
					Reason:             CondReasonOwnedTemplateError,
					Message:            err.Error(),
					ObservedGeneration: sandbox.Generation,
				},
				{
					Type:               SandboxReadyCondition,
					Status:             metav1.ConditionFalse,
					Reason:             CondReasonOwnedTemplateError,
					Message:            err.Error(),
					ObservedGeneration: sandbox.Generation,
				},
			}); patchErr != nil {
				log.Error(patchErr, "Failed to update Sandbox status")
				return nil, ctrl.Result{}, patchErr
			}
			return nil, ctrl.Result{}, err
		}
	}

	templateName := sandbox.GetNetworkTemplateName()
	log = log.WithValues("networkTemplate", templateName)

	networkTemplate := &sandboxv1alpha1.NetworkTemplate{}
	if err := r.Get(ctx, types.NamespacedName{Name: templateName, Namespace: sandbox.Namespace}, networkTemplate); err != nil {
		if apierrors.IsNotFound(err) {
			log.Error(err, "NetworkTemplate not found")
			if patchErr := r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
				{
					Type:               SandboxNetworkReadyCondition,
					Status:             metav1.ConditionFalse,
					Reason:             CondReasonNetworkTemplateNotFound,
					Message:            fmt.Sprintf("NetworkTemplate %q not found", templateName),
					ObservedGeneration: sandbox.Generation,
				},
				{
					Type:               SandboxReadyCondition,
					Status:             metav1.ConditionFalse,
					Reason:             CondReasonNetworkTemplateNotFound,
					Message:            fmt.Sprintf("NetworkTemplate %q not found", templateName),
					ObservedGeneration: sandbox.Generation,
				},
			}); patchErr != nil {
				log.Error(patchErr, "Failed to update Sandbox status")
				return nil, ctrl.Result{}, patchErr
			}
			// Wait for the NetworkTemplate to be created
			return nil, ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get NetworkTemplate")
		return nil, ctrl.Result{}, err
	}

	if !networkTemplate.DeletionTimestamp.IsZero() {
		log.Info("NetworkTemplate is being deleted, cannot use")
		if patchErr := r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
			{
				Type:               SandboxNetworkReadyCondition,
				Status:             metav1.ConditionFalse,
				Reason:             CondReasonNetworkTemplateDeleting,
				Message:            fmt.Sprintf("NetworkTemplate %q is being deleted", templateName),
				ObservedGeneration: sandbox.Generation,
			},
			{
				Type:               SandboxReadyCondition,
				Status:             metav1.ConditionFalse,
				Reason:             CondReasonNetworkTemplateDeleting,
				Message:            fmt.Sprintf("NetworkTemplate %q is being deleted", templateName),
				ObservedGeneration: sandbox.Generation,
			},
		}); patchErr != nil {
			log.Error(patchErr, "Failed to update Sandbox status")
			return nil, ctrl.Result{}, patchErr
		}
		// Requeue to check again - template will be fully deleted soon
		return nil, ctrl.Result{RequeueAfter: time.Second}, nil
	}

	return networkTemplate, ctrl.Result{}, nil
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
	networkTemplate *sandboxv1alpha1.NetworkTemplate,
) error {
	var conditions []metav1.Condition

	podCondition := r.determinePodCondition(sandbox, sandboxPod)
	conditions = append(conditions, podCondition)

	networkCondition := r.determineNetworkCondition(sandbox, networkTemplate)
	conditions = append(conditions, networkCondition)

	rootfsSnapshot, err := r.getRootfsSnapshot(ctx, sandbox)
	if err != nil {
		return err
	}
	snapshotCondition := r.determineSnapshotCondition(sandbox, rootfsSnapshot)
	conditions = append(conditions, snapshotCondition)

	readyCondition := r.determineReadyCondition(sandbox, sandboxPod, networkTemplate)
	conditions = append(conditions, readyCondition)

	return r.patchStatus(ctx, baseSandbox, sandbox, conditions)
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

func (r *SandboxReconciler) determineSnapshotCondition(sandbox *sandboxv1alpha1.Sandbox, rootfsSnapshot *sandboxv1alpha1.RootfsSnapshot) metav1.Condition {
	if rootfsSnapshot == nil {
		return metav1.Condition{
			Type:               SandboxRootfsSnapshotCondition,
			Status:             metav1.ConditionFalse,
			Reason:             "NotSnapshotting",
			Message:            "No rootfs snapshot in progress",
			ObservedGeneration: sandbox.Generation,
		}
	}

	// Check the RootfsSnapshot's Ready condition
	readyCond := meta.FindStatusCondition(rootfsSnapshot.Status.Conditions, string(sandboxv1alpha1.RootfsSnapshotReady))
	if readyCond == nil {
		return metav1.Condition{
			Type:               SandboxRootfsSnapshotCondition,
			Status:             metav1.ConditionFalse,
			Reason:             CondReasonSnapshottingInProgress,
			Message:            "Rootfs snapshot is initializing",
			ObservedGeneration: sandbox.Generation,
		}
	}

	if readyCond.Status == metav1.ConditionTrue {
		return metav1.Condition{
			Type:               SandboxRootfsSnapshotCondition,
			Status:             metav1.ConditionTrue,
			Reason:             CondReasonSnapshotComplete,
			Message:            "Rootfs snapshot completed",
			ObservedGeneration: sandbox.Generation,
		}
	}

	// Check if it failed
	if readyCond.Reason == sandboxv1alpha1.ReasonRootfsSnapshotFailed {
		return metav1.Condition{
			Type:               SandboxRootfsSnapshotCondition,
			Status:             metav1.ConditionFalse,
			Reason:             CondReasonSnapshotFailed,
			Message:            readyCond.Message,
			ObservedGeneration: sandbox.Generation,
		}
	}

	// Still in progress
	return metav1.Condition{
		Type:               SandboxRootfsSnapshotCondition,
		Status:             metav1.ConditionFalse,
		Reason:             CondReasonSnapshottingInProgress,
		Message:            "Rootfs snapshot is in progress",
		ObservedGeneration: sandbox.Generation,
	}
}

func (r *SandboxReconciler) determineNetworkCondition(sandbox *sandboxv1alpha1.Sandbox, networkTemplate *sandboxv1alpha1.NetworkTemplate) metav1.Condition {
	if networkTemplate == nil {
		return metav1.Condition{
			Type:               SandboxNetworkReadyCondition,
			Status:             metav1.ConditionFalse,
			Reason:             CondReasonNetworkTemplateNotFound,
			Message:            "NetworkTemplate not found",
			ObservedGeneration: sandbox.Generation,
		}
	}

	if isNetworkTemplateReady(networkTemplate) {
		return metav1.Condition{
			Type:               SandboxNetworkReadyCondition,
			Status:             metav1.ConditionTrue,
			Reason:             CondReasonNetworkPolicyApplied,
			Message:            "network configuration applied",
			ObservedGeneration: sandbox.Generation,
		}
	}

	return metav1.Condition{
		Type:               SandboxNetworkReadyCondition,
		Status:             metav1.ConditionFalse,
		Reason:             CondReasonNetworkConfigNotApplied,
		Message:            "Waiting for network configuration to be applied",
		ObservedGeneration: sandbox.Generation,
	}
}

// determineReadyCondition returns the aggregate Ready condition.
// Sandbox is ready when pod is ready AND network is configured (if network template exists).
func (r *SandboxReconciler) determineReadyCondition(sandbox *sandboxv1alpha1.Sandbox, sandboxPod *corev1.Pod, networkTemplate *sandboxv1alpha1.NetworkTemplate) metav1.Condition {
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

	if !isNetworkTemplateReady(networkTemplate) {
		return metav1.Condition{
			Type:               SandboxReadyCondition,
			Status:             metav1.ConditionFalse,
			Reason:             CondReasonNetworkConfigNotApplied,
			Message:            "Waiting for network configuration to be applied",
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
// +kubebuilder:rbac:groups=sandbox.isola.run,resources=networktemplates,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=sandbox.isola.run,resources=networktemplates/finalizers,verbs=update
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

	networkTemplate, result, err := r.EnsureNetworkTemplate(ctx, sandbox, baseSandbox)
	if err != nil {
		return result, err
	}

	if networkTemplate == nil {
		log.Info("NetworkTemplate not found, waiting")
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
	} else {
		timeUntilTimeout = 0 // ctrl.Result{0} is effectively ctrl.Result{} (no scheduled requeue)
	}

	if sandboxPod == nil {
		if err := r.CreateSandboxPod(ctx, sandbox, baseSandbox, template, networkTemplate); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.reconcileSandboxStatus(ctx, sandbox, baseSandbox, nil, networkTemplate); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: timeUntilTimeout}, nil
	}

	if err := r.reconcileSandboxStatus(ctx, sandbox, baseSandbox, sandboxPod, networkTemplate); err != nil {
		return ctrl.Result{}, err
	}

	// If network template exists but isn't ready, requeue sooner to check again
	if networkTemplate != nil && !isNetworkTemplateReady(networkTemplate) {
		return ctrl.Result{RequeueAfter: time.Second}, nil
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

	// Pre-check runtime support
	supported, retryable, err := snapshot.CheckRootfsSnapshotSupport(ctx, r.Client, sandboxPod)
	if err != nil {
		log.Error(err, "Failed to validate snapshotting support")
		return ctrl.Result{}, false, err
	}

	if !supported {
		if retryable {
			if err := r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
				{
					Type:               SandboxRootfsSnapshotCondition,
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

	// Get or create RootfsSnapshot
	rootfsSnapshot, err := r.getRootfsSnapshot(ctx, sandbox)
	if err != nil {
		return ctrl.Result{}, false, err
	}

	if rootfsSnapshot == nil {
		// Create the RootfsSnapshot
		rootfsSnapshot = &sandboxv1alpha1.RootfsSnapshot{
			ObjectMeta: metav1.ObjectMeta{
				Name:      getRootfsSnapshotName(sandbox),
				Namespace: sandbox.Namespace,
			},
			Spec: sandboxv1alpha1.RootfsSnapshotSpec{
				SandboxName:           sandbox.Name,
				ActiveDeadlineSeconds: &activeDeadlineSeconds,
				// ContainerNames empty = snapshot all non-init containers
			},
		}

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

		log.Info("Created RootfsSnapshot", "name", rootfsSnapshot.Name)
		r.Recorder.Event(sandbox, corev1.EventTypeNormal, "RootfsSnapshotCreated", "Created RootfsSnapshot resource")

		if err := r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
			{
				Type:               SandboxRootfsSnapshotCondition,
				Status:             metav1.ConditionFalse,
				Reason:             CondReasonSnapshottingInProgress,
				Message:            "RootfsSnapshot created, waiting for completion",
				ObservedGeneration: sandbox.Generation,
			},
		}); err != nil {
			return ctrl.Result{}, false, err
		}

		return ctrl.Result{RequeueAfter: time.Second}, false, nil
	}

	// Check RootfsSnapshot status
	readyCond := meta.FindStatusCondition(rootfsSnapshot.Status.Conditions, string(sandboxv1alpha1.RootfsSnapshotReady))

	if readyCond != nil && readyCond.Status == metav1.ConditionTrue {
		log.Info("Rootfs snapshot succeeded")
		r.Recorder.Event(sandbox, corev1.EventTypeNormal, "SnapshotSucceeded", "Rootfs snapshot completed")
		if err := r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
			{
				Type:               SandboxRootfsSnapshotCondition,
				Status:             metav1.ConditionTrue,
				Reason:             CondReasonSnapshotComplete,
				Message:            "Rootfs snapshot completed",
				ObservedGeneration: sandbox.Generation,
			},
		}); err != nil {
			return ctrl.Result{}, false, err
		}
		return ctrl.Result{}, true, nil
	}

	if readyCond != nil && readyCond.Reason == sandboxv1alpha1.ReasonRootfsSnapshotFailed {
		log.Info("Rootfs snapshot failed", "message", readyCond.Message)
		r.Recorder.Event(sandbox, corev1.EventTypeWarning, "SnapshotFailed", readyCond.Message)
		if err := r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
			{
				Type:               SandboxRootfsSnapshotCondition,
				Status:             metav1.ConditionFalse,
				Reason:             CondReasonSnapshotFailed,
				Message:            readyCond.Message,
				ObservedGeneration: sandbox.Generation,
			},
		}); err != nil {
			return ctrl.Result{}, false, err
		}
		return ctrl.Result{}, true, nil
	}

	// Still in progress - check deadline again
	if r.clock().Now().After(snapshotDeadline) {
		log.Info("Rootfs snapshot timed out before completion", "deadline", snapshotDeadline)
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
		return ctrl.Result{}, true, nil
	}

	if err := r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
		{
			Type:               SandboxRootfsSnapshotCondition,
			Status:             metav1.ConditionFalse,
			Reason:             CondReasonSnapshottingInProgress,
			Message:            "Rootfs snapshot is running",
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

	// Field index for sandbox networkTemplateRef lookups
	if err := mgr.GetFieldIndexer().IndexField(
		context.Background(),
		&sandboxv1alpha1.Sandbox{},
		sandboxNetworkTemplateRefField,
		extractNetworkTemplateRefName,
	); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&sandboxv1alpha1.Sandbox{}).
		Owns(&corev1.Pod{}).
		Owns(&sandboxv1alpha1.RootfsSnapshot{}).
		// Watch SandboxTemplate changes to reconcile affected sandboxes
		Watches(
			&sandboxv1alpha1.SandboxTemplate{},
			handler.EnqueueRequestsFromMapFunc(r.findSandboxesForTemplate),
		).
		// Watch NetworkTemplate changes to reconcile affected sandboxes.
		// When NetworkTemplate Ready condition changes, sandboxes are requeued
		// to check if they can now proceed with pod creation.
		Watches(
			&sandboxv1alpha1.NetworkTemplate{},
			handler.EnqueueRequestsFromMapFunc(r.findSandboxesForNetworkTemplate),
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

func extractNetworkTemplateRefName(obj client.Object) []string {
	sandbox, ok := obj.(*sandboxv1alpha1.Sandbox)
	if !ok {
		return nil
	}

	return []string{sandbox.GetNetworkTemplateName()}
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

func (r *SandboxReconciler) findSandboxesForNetworkTemplate(ctx context.Context, networkTemplate client.Object) []reconcile.Request {
	// Use field index for efficient lookup (only sandboxes with explicit network config are indexed)
	sandboxList := &sandboxv1alpha1.SandboxList{}
	if err := r.List(ctx, sandboxList,
		client.InNamespace(networkTemplate.GetNamespace()),
		client.MatchingFields{sandboxNetworkTemplateRefField: networkTemplate.GetName()},
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
