// Copyright The Isola Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package controller

import (
	"context"
	"fmt"
	"maps"
	"path/filepath"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	sandboxv1alpha1 "github.com/isola-run/isola/api/v1alpha1"
	"github.com/isola-run/isola/internal/constants"
	netbuilder "github.com/isola-run/isola/internal/operator/controller/network"
	"github.com/isola-run/isola/internal/operator/controller/podutil"
	"github.com/isola-run/isola/internal/operator/controller/snapshot"
	internalversion "github.com/isola-run/isola/internal/version"
	"k8s.io/client-go/tools/events"
)

const (
	SandboxPodReadyCondition       = "PodReady"
	SandboxNetworkReadyCondition   = "NetworkConfigured"
	SandboxRootfsSnapshotCondition = "RootfsSnapshot"
)

const (
	CondReasonPodPending        = "PodPending"
	CondReasonPodRunning        = "PodRunning"
	CondReasonPodFailed         = "PodFailed"
	CondReasonPodSucceeded      = "PodSucceeded"
	CondReasonPodCreating       = "PodCreating"
	CondReasonPodCreationFailed = "PodCreationFailed"
	CondReasonDeleting          = "Deleting"
	CondReasonReconciling       = "Reconciling"

	// RootfsSnapshot-related reasons
	CondReasonRootfsSnapshottingInProgress = "RootfsSnapshottingInProgress"
	CondReasonRootfsSnapshotComplete       = "RootfsSnapshotComplete"
	CondReasonRootfsSnapshotFailed         = "RootfsSnapshotFailed"
	CondReasonRootfsSnapshotTimeout        = "RootfsSnapshotTimeout"
	CondReasonInvalidRuntime               = "InvalidRuntime"

	CondReasonStartupTimeoutExceeded = "StartupTimeoutExceeded"
	CondReasonTimeout                = "Timeout"

	// Restore-related reasons
	CondReasonRootfsRestoreConfigError = "RootfsRestoreConfigurationError"
	CondReasonNoRootfsSnapshot         = "NoRootfsSnapshot"

	// NetworkPolicy-related reasons
	CondReasonNetworkPolicyApplied = "NetworkPolicyApplied"
	CondReasonNetworkPolicyFailed  = "NetworkPolicyFailed"
)

const defaultTimeoutSeconds int64 = 300

const SandboxFinalizer = "sandbox.isola.run/cleanup"

type SandboxReconciler struct {
	client.Client
	Scheme                        *runtime.Scheme
	Recorder                      events.EventRecorder
	SandboxSidecarImage           string
	SandboxSidecarImagePullPolicy corev1.PullPolicy
	RuntimeClassName              string                        // RuntimeClassName to use for sandbox pods (e.g. "gvisor"). Empty means use cluster default.
	PriorityClassName             string                        // PriorityClassName to use for sandbox pods. Empty means use cluster default.
	ImagePullSecrets              []corev1.LocalObjectReference // ImagePullSecrets for pulling sandbox-sidecar images from private registries.
	Clock                         Clock                         // Clock interface for time operations, allows mocking in tests
	RootfsSnapshotHostMountPath   string                        // Host path where rootfs snapshot tars are NFS-mounted (e.g., /mnt/isola-snapshots)
}

const (
	sandboxSidecarContainerName = "sandbox-sidecar"

	// Trust boundary label: identifies untrusted sandbox pods for NetworkPolicy selection
	LabelSandbox = "isola.run/sandbox"

	// Network labels for pod selection by Helm-installed NetworkPolicies
	LabelAllowIPv4Internet = "isola.run/allow-ipv4-internet-egress"
	LabelAllowIPv6Internet = "isola.run/allow-ipv6-internet-egress"
	LabelAllowClusterDNS   = "isola.run/allow-cluster-dns"
)

func (r *SandboxReconciler) clock() Clock {
	if r.Clock != nil {
		return r.Clock
	}
	return RealClock{}
}

func buildNetworkLabels(network *sandboxv1alpha1.Network) map[string]string {
	labels := make(map[string]string)
	if network == nil {
		return labels
	}
	if network.AllowInternetEgress != nil && *network.AllowInternetEgress {
		labels[LabelAllowIPv4Internet] = "true"
		if network.AllowIPv6Egress != nil && *network.AllowIPv6Egress {
			labels[LabelAllowIPv6Internet] = "true"
		}
	}
	if network.AllowClusterDNS != nil && *network.AllowClusterDNS {
		labels[LabelAllowClusterDNS] = "true"
	}
	return labels
}

func (r *SandboxReconciler) buildSandboxSidecarContainer() corev1.Container {
	rp := corev1.ContainerRestartPolicyAlways
	return corev1.Container{
		Name:            sandboxSidecarContainerName,
		Image:           r.SandboxSidecarImage,
		ImagePullPolicy: r.SandboxSidecarImagePullPolicy,
		RestartPolicy:   &rp,
		// CPU & memory: gVisor runs one sentry process in the pod cgroup.
		// The pod cgroup's CPU/memory limits are the sum of all container limits in the spec,
		// but only if ALL containers declare limits (kubelet cpuLimitsDeclared/memoryLimitsDeclared).
		// If any container is missing a limit, the missing limit is effectively infinite,
		// so not setting the sidecar's would unbound the pod. Setting them to anything bigger
		// would add on top of the user-defined values, surprising the user.
		// Requests work the same way (summed across containers) but a missing request is 0,
		// not infinite. We keep near-zero requests for the same reason as limits.
		// A side effect is that the pod is never classified as BestEffort (first to be evicted
		// under node pressure) even when the user sets nothing, since QoS only considers
		// CPU and memory.
		// Near-zero values so the effective pod budget ~= user containers.
		//
		// Ephemeral storage: deliberately omitted. Unlike CPU/memory, a missing ephemeral
		// storage limit contributes 0 to the pod total (not infinity). Kubelet eviction sums
		// only defined limits, so a 1Mi sidecar limit would cap the entire pod at 1Mi when
		// the user sets no limit. If the user does set a limit, the sidecar's 1Mi adds
		// nothing useful. Same applies to requests.
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1m"),
				corev1.ResourceMemory: resource.MustParse("1Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1m"),
				corev1.ResourceMemory: resource.MustParse("1Mi"),
			},
		},
		// CAP_SYS_PTRACE is required by gVisor's ContextCanTrace check (task_files.go)
		// that guards /proc/<pid>/root, /proc/<pid>/cwd, and /proc/<pid>/environ.
		// These are accessed to find the container's PID, resolve its working directory,
		// read its environment, and supply the chroot path for SysProcAttr.Chroot.
		//
		// CAP_SYS_CHROOT (in the default capability set) is required for the chroot to the target
		// container filesystem view.
		SecurityContext: &corev1.SecurityContext{
			RunAsUser: ptr.To(int64(0)),
			Capabilities: &corev1.Capabilities{
				Add: []corev1.Capability{"SYS_PTRACE"},
			},
		},
	}
}

// Mark each container with its name so the sidecar can discover it via /proc/<pid>/environ.
func markContainers(sandboxPod *corev1.Pod) {
	for i := range sandboxPod.Spec.Containers {
		sandboxPod.Spec.Containers[i].Env = append(sandboxPod.Spec.Containers[i].Env, corev1.EnvVar{
			Name:  constants.IsolaContainerNameEnv,
			Value: sandboxPod.Spec.Containers[i].Name,
		})
	}
}

func (r *SandboxReconciler) injectSandboxSidecar(sandboxPod *corev1.Pod) {
	sidecarContainer := r.buildSandboxSidecarContainer()
	sandboxPod.Spec.InitContainers = append(sandboxPod.Spec.InitContainers, sidecarContainer)
}

// markSandboxSucceeded sets Succeeded=True and Ready=False if the sandbox is not already terminal.
// Returns nil without patching if Succeeded is already set (one-way latch).
func (r *SandboxReconciler) markSandboxSucceeded(ctx context.Context, baseSandbox *sandboxv1alpha1.Sandbox, sandbox *sandboxv1alpha1.Sandbox, reason, message string) error {
	if existing := meta.FindStatusCondition(sandbox.Status.Conditions, sandboxv1alpha1.SandboxSucceededCondition); existing != nil {
		return nil
	}
	return r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
		{
			Type:               sandboxv1alpha1.SandboxReadyCondition,
			Status:             metav1.ConditionFalse,
			Reason:             reason,
			Message:            message,
			ObservedGeneration: sandbox.Generation,
		},
		{
			Type:               sandboxv1alpha1.SandboxSucceededCondition,
			Status:             metav1.ConditionTrue,
			Reason:             reason,
			Message:            message,
			ObservedGeneration: sandbox.Generation,
		},
	})
}

// markSandboxFailed sets Succeeded=False and Ready=False if the sandbox is not already terminal.
// Returns nil without patching if Succeeded is already set (one-way latch).
func (r *SandboxReconciler) markSandboxFailed(ctx context.Context, baseSandbox *sandboxv1alpha1.Sandbox, sandbox *sandboxv1alpha1.Sandbox, reason, message string) error {
	if existing := meta.FindStatusCondition(sandbox.Status.Conditions, sandboxv1alpha1.SandboxSucceededCondition); existing != nil {
		return nil
	}
	return r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
		{
			Type:               sandboxv1alpha1.SandboxReadyCondition,
			Status:             metav1.ConditionFalse,
			Reason:             reason,
			Message:            message,
			ObservedGeneration: sandbox.Generation,
		},
		{
			Type:               sandboxv1alpha1.SandboxSucceededCondition,
			Status:             metav1.ConditionFalse,
			Reason:             reason,
			Message:            message,
			ObservedGeneration: sandbox.Generation,
		},
	})
}

func (r *SandboxReconciler) patchStatus(ctx context.Context, baseSandbox *sandboxv1alpha1.Sandbox, newSandbox *sandboxv1alpha1.Sandbox, newConditions []metav1.Condition) error {
	if newSandbox.Status.Conditions == nil {
		newSandbox.Status.Conditions = []metav1.Condition{}
	}

	for _, cond := range newConditions {
		meta.SetStatusCondition(&newSandbox.Status.Conditions, cond)
	}

	return r.Status().Patch(ctx, newSandbox, client.MergeFrom(baseSandbox))
}

func (r *SandboxReconciler) CreateSandboxPod(ctx context.Context, sandbox *sandboxv1alpha1.Sandbox, baseSandbox *sandboxv1alpha1.Sandbox) error {
	log := logf.FromContext(ctx)
	// todo benl reduce verbose logging
	log.Info("Creating Pod")

	// Apply pod template labels first, then override with our labels.
	// This prevents templates from overriding app.kubernetes.io/* etc.
	labels := make(map[string]string)
	maps.Copy(labels, sandbox.Spec.PodTemplate.Labels)

	// Standard Kubernetes recommended labels (https://kubernetes.io/docs/concepts/overview/working-with-objects/common-labels/)
	labels["app.kubernetes.io/name"] = "isola-sandbox"
	labels["app.kubernetes.io/instance"] = sandbox.Name
	labels["app.kubernetes.io/component"] = "sandbox"
	labels["app.kubernetes.io/part-of"] = "isola"
	labels["app.kubernetes.io/managed-by"] = "isola-operator"
	labels[LabelSandbox] = "true"

	labels["cluster-autoscaler.kubernetes.io/safe-to-evict"] = "false"

	maps.Copy(labels, buildNetworkLabels(sandbox.Spec.Network))

	annotations := make(map[string]string, len(sandbox.Spec.PodTemplate.Annotations))
	maps.Copy(annotations, sandbox.Spec.PodTemplate.Annotations)

	sandboxPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podutil.GetSandboxPodName(sandbox.Name),
			Namespace: sandbox.Namespace,
			Labels:    labels,
			// There's a security gate in runsc/config/flags.go
			// where only flags deemed safe for container authors
			// to set because they don't weaken the sandbox.
			Annotations: annotations,
		},
		Spec: sandbox.Spec.PodTemplate.Spec,
	}

	sandboxPod.Spec.RestartPolicy = corev1.RestartPolicyNever
	sandboxPod.Spec.AutomountServiceAccountToken = ptr.To(false)

	// Inject imagePullSecrets for private registries (configured via Helm global.imagePullSecrets)
	if len(r.ImagePullSecrets) > 0 {
		sandboxPod.Spec.ImagePullSecrets = append(sandboxPod.Spec.ImagePullSecrets, r.ImagePullSecrets...)
	}

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

	if len(sandbox.Spec.RootfsSnapshotSources) > 0 {
		if terminal, err := r.validateRootfsRestoreConfig(ctx); err != nil {
			if terminal {
				if patchErr := r.markSandboxFailed(ctx, baseSandbox, sandbox, CondReasonRootfsRestoreConfigError, err.Error()); patchErr != nil {
					return patchErr
				}
				return reconcile.TerminalError(err)
			}
			if patchErr := r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{{
				Type:               sandboxv1alpha1.SandboxReadyCondition,
				Status:             metav1.ConditionFalse,
				Reason:             CondReasonRootfsRestoreConfigError,
				Message:            err.Error(),
				ObservedGeneration: sandbox.Generation,
			}}); patchErr != nil {
				return patchErr
			}
			return err
		}
		if err := r.injectRootfsRestore(sandbox.Spec.RootfsSnapshotSources, sandboxPod, sandbox.Namespace); err != nil {
			// injectRootfsRestore errors are all spec validation (permanent)
			if patchErr := r.markSandboxFailed(ctx, baseSandbox, sandbox, CondReasonRootfsRestoreConfigError, err.Error()); patchErr != nil {
				return patchErr
			}
			return reconcile.TerminalError(err)
		}
	}

	// Enable shared PID namespace so the sidecar can locate the main container and access its filesystem via /proc/<pid>/root
	sandboxPod.Spec.ShareProcessNamespace = ptr.To(true)

	if r.PriorityClassName != "" {
		sandboxPod.Spec.PriorityClassName = r.PriorityClassName
	}

	// Default to sleep infinity so sandbox containers stay alive
	for i := range sandboxPod.Spec.Containers {
		if len(sandboxPod.Spec.Containers[i].Command) == 0 {
			sandboxPod.Spec.Containers[i].Command = []string{"sleep", "infinity"}
		}
	}

	configureDNS(sandboxPod, sandbox.Spec.Network)

	markContainers(sandboxPod)

	r.injectSandboxSidecar(sandboxPod)

	if err := controllerutil.SetControllerReference(sandbox, sandboxPod, r.Scheme); err != nil {
		log.Error(err, "Failed to set controller reference")
		return err
	}

	if err := r.Create(ctx, sandboxPod); err != nil {
		log.Error(err, "Failed creating Pod")

		// Best effort status patch - log but don't override the original create error.
		if patchErr := r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
			{
				Type:               SandboxPodReadyCondition,
				Status:             metav1.ConditionFalse,
				Reason:             CondReasonPodCreationFailed,
				Message:            err.Error(),
				ObservedGeneration: sandbox.Generation,
			},
			{
				Type:               sandboxv1alpha1.SandboxReadyCondition,
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
	sandboxCreatedTotal.Inc()

	r.Recorder.Eventf(sandbox, nil, corev1.EventTypeNormal, "PodCreated", "Created", "Sandbox Pod created")

	// Capture the sidecar version at pod creation time so consumers (api-gateway)
	// can do capability checks against long-running sandboxes whose sidecar image
	// predates later operator upgrades. Operator and sidecar are released together,
	// so the operator's own version stands in for the sidecar build.
	sandbox.Status.SidecarVersion = internalversion.Get().GitVersion

	if err := r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
		{
			Type:               SandboxPodReadyCondition,
			Status:             metav1.ConditionFalse,
			Reason:             CondReasonPodCreating,
			Message:            "Creating sandbox Pod",
			ObservedGeneration: sandbox.Generation,
		},
		{
			Type:               sandboxv1alpha1.SandboxReadyCondition,
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

func configureDNS(sandboxPod *corev1.Pod, network *sandboxv1alpha1.Network) {
	allowClusterDNS := network != nil && network.AllowClusterDNS != nil && *network.AllowClusterDNS

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
		if ns := netbuilder.EffectiveNameservers(network); len(ns) > 0 {
			nameservers = ns
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

func (r *SandboxReconciler) getSandboxPod(ctx context.Context, sandbox *sandboxv1alpha1.Sandbox) (*corev1.Pod, error) {
	podName := podutil.GetSandboxPodName(sandbox.Name)
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

// getTerminationRootfssnapshot looks up the termination RootfsSnapshot by its
// derived name. Theoretical collision: a user-created RootfsSnapshot of the
// same name would be adopted here without an ownerRef check, letting its
// status drive the sandbox's termination outcome. Tackle later if it bites
// (e.g. verify controller ownerRef points back at this sandbox).
func (r *SandboxReconciler) getTerminationRootfssnapshot(ctx context.Context, sandbox *sandboxv1alpha1.Sandbox) (*sandboxv1alpha1.RootfsSnapshot, error) {
	snap := &sandboxv1alpha1.RootfsSnapshot{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      podutil.GetTerminationSnapshotName(sandbox.Name),
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

// ensureCustomNetworkPolicy creates or updates a custom NetworkPolicy for sandboxes
// that need more than the static Helm-installed policies (custom CIDRs or DNS).
func (r *SandboxReconciler) ensureCustomNetworkPolicy(
	ctx context.Context,
	sandbox *sandboxv1alpha1.Sandbox,
	baseSandbox *sandboxv1alpha1.Sandbox,
) error {
	log := logf.FromContext(ctx)

	desiredNP, err := netbuilder.BuildCustomNetworkPolicy(sandbox.Name, sandbox.Namespace, sandbox.Spec.Network)
	if err != nil {
		log.Error(err, "Failed to build custom NetworkPolicy")
		// Network is immutable, so this error will never resolve on retry.
		if patchErr := r.markSandboxFailed(ctx, baseSandbox, sandbox, CondReasonNetworkPolicyFailed, err.Error()); patchErr != nil {
			return patchErr
		}
		return reconcile.TerminalError(err)
	}

	if desiredNP == nil {
		// No custom policy needed
		return nil
	}

	existingNP := &networkingv1.NetworkPolicy{}
	policyName := podutil.GetCustomNetworkPolicyName(sandbox.Name)
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

func (r *SandboxReconciler) ensureTimeout(ctx context.Context, sandbox *sandboxv1alpha1.Sandbox, baseSandbox *sandboxv1alpha1.Sandbox, sandboxPod *corev1.Pod) (optionalTimeoutAt *metav1.Time, err error) {
	log := logf.FromContext(ctx)
	if sandbox.Spec.TimeoutSeconds == nil {
		return nil, nil
	}

	// Set once: anchor timeout to pod start time. Pod start time is immutable,
	// so this naturally prevents crashlooping pods from pushing the timeout forward.
	if sandbox.Status.TimeoutAt == nil {
		startTime := podutil.PodStartTime(sandboxPod)
		if startTime == nil {
			// pod not started yet, we'll calculate the timeout once acknowledged by the kubelet (will trigger a reconcile with StartTime set)
			return nil, nil
		}

		timeoutAt := startTime.Add(time.Duration(*sandbox.Spec.TimeoutSeconds) * time.Second)
		log.Info("calculated sandbox timeout from pod start time", "startTime", startTime.Time, "timeoutAt", timeoutAt)

		sandbox.Status.TimeoutAt = &metav1.Time{Time: timeoutAt}
		if err := r.Status().Patch(ctx, sandbox, client.MergeFrom(baseSandbox)); err != nil {
			log.Error(err, "Failed to patch sandbox TimeoutAt")
			return nil, err
		}
		log.Info("persisted timeoutAt", "timeoutAt", timeoutAt)
	}

	return sandbox.Status.TimeoutAt, nil
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

	// todo benl: currently, only termination snapshot condition is reflected
	terminationSnapshot, err := r.getTerminationRootfssnapshot(ctx, sandbox)
	if err != nil {
		return err
	}
	snapshotCondition := r.determineRootfssnapshotCondition(sandbox, terminationSnapshot)
	conditions = append(conditions, snapshotCondition)

	readyCondition := r.determineReadyCondition(sandbox, sandboxPod)
	conditions = append(conditions, readyCondition)

	if succeeded := r.determineSucceededCondition(sandbox, sandboxPod); succeeded != nil {
		conditions = append(conditions, *succeeded)
	}

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

func (r *SandboxReconciler) determineRootfssnapshotCondition(sandbox *sandboxv1alpha1.Sandbox, snap *sandboxv1alpha1.RootfsSnapshot) metav1.Condition {
	if snap == nil {
		return metav1.Condition{
			Type:               SandboxRootfsSnapshotCondition,
			Status:             metav1.ConditionFalse,
			Reason:             CondReasonNoRootfsSnapshot,
			Message:            "No termination rootfs snapshot exists",
			ObservedGeneration: sandbox.Generation,
		}
	}

	if snap.Status.CompletionTime == nil {
		return metav1.Condition{
			Type:               SandboxRootfsSnapshotCondition,
			Status:             metav1.ConditionFalse,
			Reason:             CondReasonRootfsSnapshottingInProgress,
			Message:            fmt.Sprintf("RootfsSnapshot %q is running", snap.Name),
			ObservedGeneration: sandbox.Generation,
		}
	}

	succeeded := meta.FindStatusCondition(snap.Status.Conditions, sandboxv1alpha1.RootfsSnapshotSucceededCondition)
	if succeeded != nil && succeeded.Status == metav1.ConditionTrue {
		return metav1.Condition{
			Type:               SandboxRootfsSnapshotCondition,
			Status:             metav1.ConditionTrue,
			Reason:             CondReasonRootfsSnapshotComplete,
			Message:            fmt.Sprintf("RootfsSnapshot %q succeeded", snap.Name),
			ObservedGeneration: sandbox.Generation,
		}
	}
	if succeeded != nil && succeeded.Status == metav1.ConditionFalse {
		return metav1.Condition{
			Type:               SandboxRootfsSnapshotCondition,
			Status:             metav1.ConditionFalse,
			Reason:             CondReasonRootfsSnapshotFailed,
			Message:            fmt.Sprintf("RootfsSnapshot %q failed: %s", snap.Name, succeeded.Message),
			ObservedGeneration: sandbox.Generation,
		}
	}

	return metav1.Condition{
		Type:               SandboxRootfsSnapshotCondition,
		Status:             metav1.ConditionFalse,
		Reason:             CondReasonRootfsSnapshottingInProgress,
		Message:            fmt.Sprintf("RootfsSnapshot %q status unknown", snap.Name),
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
			Type:               sandboxv1alpha1.SandboxReadyCondition,
			Status:             metav1.ConditionFalse,
			Reason:             reason,
			Message:            message,
			ObservedGeneration: sandbox.Generation,
		}
	}

	if !podutil.IsPodReady(sandboxPod) {
		return metav1.Condition{
			Type:               sandboxv1alpha1.SandboxReadyCondition,
			Status:             metav1.ConditionFalse,
			Reason:             CondReasonPodPending,
			Message:            "Pod is not ready yet",
			ObservedGeneration: sandbox.Generation,
		}
	}

	return metav1.Condition{
		Type:               sandboxv1alpha1.SandboxReadyCondition,
		Status:             metav1.ConditionTrue,
		Reason:             CondReasonPodRunning,
		Message:            "Pod is running",
		ObservedGeneration: sandbox.Generation,
	}
}

// determineSucceededCondition returns the Succeeded condition for terminal sandboxes.
// Returns nil when the sandbox is still running (condition should not be set).
// Once set, the condition is preserved (terminal is permanent).
func (r *SandboxReconciler) determineSucceededCondition(sandbox *sandboxv1alpha1.Sandbox, sandboxPod *corev1.Pod) *metav1.Condition {
	// Terminal is permanent — preserve existing Succeeded condition
	if existing := meta.FindStatusCondition(sandbox.Status.Conditions, sandboxv1alpha1.SandboxSucceededCondition); existing != nil {
		return existing
	}

	if sandboxPod != nil && sandboxPod.Status.Phase == corev1.PodSucceeded {
		return &metav1.Condition{
			Type:               sandboxv1alpha1.SandboxSucceededCondition,
			Status:             metav1.ConditionTrue,
			Reason:             CondReasonPodSucceeded,
			Message:            "Pod completed successfully",
			ObservedGeneration: sandbox.Generation,
		}
	}

	if sandboxPod != nil && sandboxPod.Status.Phase == corev1.PodFailed {
		return &metav1.Condition{
			Type:               sandboxv1alpha1.SandboxSucceededCondition,
			Status:             metav1.ConditionFalse,
			Reason:             CondReasonPodFailed,
			Message:            "Pod failed",
			ObservedGeneration: sandbox.Generation,
		}
	}

	return nil
}

// +kubebuilder:rbac:groups=sandbox.isola.run,resources=sandboxes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=sandbox.isola.run,resources=sandboxes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=sandbox.isola.run,resources=sandboxes/finalizers,verbs=update
// +kubebuilder:rbac:groups=sandbox.isola.run,resources=rootfssnapshots,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=node.k8s.io,resources=runtimeclasses,verbs=get;list;watch
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;update;patch;delete

func (r *SandboxReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// todo benl: pass params by value sometimes, to avoid dereferencing nils by accident
	// todo benl: add r.RecordEvent for events (observability)
	log := logf.FromContext(ctx)

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
	hasFinalizer := controllerutil.ContainsFinalizer(sandbox, SandboxFinalizer)

	if sandboxDeleted && !hasFinalizer {
		return ctrl.Result{}, nil
	}

	// Add finalizer first, before any other operations
	if !sandboxDeleted && !hasFinalizer {
		log.Info("Adding finalizer to sandbox")
		controllerutil.AddFinalizer(sandbox, SandboxFinalizer)
		if err := r.Update(ctx, sandbox); err != nil {
			log.Error(err, "Failed to add finalizer")
			return ctrl.Result{}, err
		}
		// Update baseSandbox to reflect the finalizer change for subsequent patches
		baseSandbox = sandbox.DeepCopy()
	}

	if sandboxDeleted && hasFinalizer { // run finalizer logic
		res, _, err := r.finalizeSandbox(ctx, sandbox, baseSandbox)
		return res, err
	}

	if err := r.ensureCustomNetworkPolicy(ctx, sandbox, baseSandbox); err != nil {
		return ctrl.Result{}, err
	}

	sandboxPod, err := r.getSandboxPod(ctx, sandbox)
	if err != nil {
		return ctrl.Result{}, err
	}

	// whether we created the pod or not, check for sandbox timeout before proceeding:
	optionalTimeoutAt, err := r.ensureTimeout(ctx, sandbox, baseSandbox, sandboxPod)
	if err != nil {
		return ctrl.Result{}, err
	}

	if optionalTimeoutAt != nil && r.clock().Now().After(optionalTimeoutAt.Time) {
		log.Info("Sandbox timed out")

		if err := r.markSandboxSucceeded(ctx, baseSandbox, sandbox, CondReasonTimeout, "Sandbox timed out"); err != nil {
			return ctrl.Result{}, err
		}

		res, cleanupDone, err := r.finalizeSandbox(ctx, sandbox, baseSandbox)
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

	// Startup timeout: if the pod exists but is not ready, check if it exceeded the startup deadline
	var startupDeadline time.Time
	hasStartupDeadline := sandboxPod != nil && !podutil.IsPodReady(sandboxPod) && sandbox.Spec.StartupTimeoutSeconds != nil
	if hasStartupDeadline {
		startupDeadline = sandboxPod.CreationTimestamp.Add(
			time.Duration(*sandbox.Spec.StartupTimeoutSeconds) * time.Second)
		if r.clock().Now().After(startupDeadline) {
			log.Info("Startup timeout exceeded", "deadline", startupDeadline)
			msg := fmt.Sprintf("pod not ready within %ds of creation", *sandbox.Spec.StartupTimeoutSeconds)
			if err := r.markSandboxFailed(ctx, baseSandbox, sandbox, CondReasonStartupTimeoutExceeded, msg); err != nil {
				log.Error(err, "Failed to patch startup timeout condition")
			}
			if err := r.Delete(ctx, sandbox); err != nil {
				return ctrl.Result{}, client.IgnoreNotFound(err)
			}
			return ctrl.Result{}, nil
		}
	}

	// todo benl: simplify requeue logic below
	var requeueAfter time.Duration
	if optionalTimeoutAt != nil {
		requeueAfter = r.clock().Until(optionalTimeoutAt.Time)
		if requeueAfter <= 0 {
			requeueAfter = time.Millisecond
		}
	}

	if hasStartupDeadline {
		timeUntilStartup := r.clock().Until(startupDeadline)
		if timeUntilStartup <= 0 {
			timeUntilStartup = time.Millisecond
		}
		if requeueAfter == 0 || timeUntilStartup < requeueAfter {
			requeueAfter = timeUntilStartup
		}
	}

	if sandboxPod == nil {
		if err := r.CreateSandboxPod(ctx, sandbox, baseSandbox); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.reconcileSandboxStatus(ctx, sandbox, baseSandbox, nil); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: requeueAfter}, nil
	}

	if err := r.reconcileSandboxStatus(ctx, sandbox, baseSandbox, sandboxPod); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

// finalize the sandbox according to the termination policy and have it ready to be deleted.
// Returns:
// ctrl.Result: a result object that might ask for a requeue if another reconciliation would be required.
// bool: whether the cleanup was fully completed (and thus the sandbox can be deleted) or not.
// an error if something went wrong.
func (r *SandboxReconciler) finalizeSandbox(
	ctx context.Context,
	sandbox *sandboxv1alpha1.Sandbox,
	baseSandbox *sandboxv1alpha1.Sandbox,
) (ctrl.Result, bool, error) {
	log := logf.FromContext(ctx)

	log.Info("Executing termination policy for deletion")

	sandboxPod, err := r.getSandboxPod(ctx, sandbox)
	if err != nil {
		return ctrl.Result{}, false, err
	}

	snapshotDeadline, err := r.ensureTerminationDeadline(ctx, sandbox, baseSandbox)
	if err != nil {
		return ctrl.Result{}, false, err
	}

	result, cleanupDone, err := r.executeTerminationPolicy(
		ctx, sandbox, baseSandbox, sandboxPod, snapshotDeadline,
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

// executeTerminationPolicy executes the termination policy for a sandbox being cleaned up.
// snapshotDeadline is the deadline by which snapshotting must complete.
func (r *SandboxReconciler) executeTerminationPolicy(
	ctx context.Context,
	sandbox *sandboxv1alpha1.Sandbox,
	baseSandbox *sandboxv1alpha1.Sandbox,
	sandboxPod *corev1.Pod,
	snapshotDeadline time.Time,
) (ctrl.Result, bool, error) {
	log := logf.FromContext(ctx)

	if sandbox.Spec.TerminationPolicy == nil || sandbox.Spec.TerminationPolicy.Type == sandboxv1alpha1.TerminationTypeDelete {
		if err := r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
			{
				Type:               sandboxv1alpha1.SandboxReadyCondition,
				Status:             metav1.ConditionFalse,
				Reason:             CondReasonDeleting,
				Message:            "Sandbox being deleted",
				ObservedGeneration: sandbox.Generation,
			},
		}); err != nil {
			return ctrl.Result{}, false, err
		}
		return ctrl.Result{}, true, nil
	}

	if err := r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
		{
			Type:               sandboxv1alpha1.SandboxReadyCondition,
			Status:             metav1.ConditionFalse,
			Reason:             CondReasonRootfsSnapshottingInProgress,
			Message:            "Sandbox being deleted; executing termination policy",
			ObservedGeneration: sandbox.Generation,
		},
	}); err != nil {
		return ctrl.Result{}, false, err
	}

	switch sandbox.Spec.TerminationPolicy.Type {
	case sandboxv1alpha1.TerminationTypeSnapshotRootfs:
		cfg := sandbox.Spec.TerminationPolicy.SnapshotRootfs
		if cfg == nil {
			log.Info("SnapshotRootfs config missing despite SnapshotRootfs type; proceeding with deletion")
			return ctrl.Result{}, true, nil
		}
		return r.handleRootfsSnapshot(ctx, sandbox, baseSandbox, sandboxPod, snapshotDeadline, cfg)
	default:
		log.Info("Unknown termination policy; proceeding with deletion", "type", sandbox.Spec.TerminationPolicy.Type)
		return ctrl.Result{}, true, nil
	}
}

func (r *SandboxReconciler) getTimeoutSeconds(sandbox *sandboxv1alpha1.Sandbox) int64 {
	if sandbox != nil && sandbox.Spec.TerminationPolicy != nil &&
		sandbox.Spec.TerminationPolicy.SnapshotRootfs != nil &&
		sandbox.Spec.TerminationPolicy.SnapshotRootfs.TimeoutSeconds != nil {
		return *sandbox.Spec.TerminationPolicy.SnapshotRootfs.TimeoutSeconds
	}
	return defaultTimeoutSeconds
}

// ensureTerminationDeadline persists TerminationDeadlineAt on the first call (anchored to
// DeletionTimestamp) and returns the persisted value on subsequent reconciles.
func (r *SandboxReconciler) ensureTerminationDeadline(ctx context.Context, sandbox *sandboxv1alpha1.Sandbox, baseSandbox *sandboxv1alpha1.Sandbox) (time.Time, error) {
	log := logf.FromContext(ctx)

	if sandbox.Status.TerminationDeadlineAt != nil {
		return sandbox.Status.TerminationDeadlineAt.Time, nil
	}

	// Anchor to DeletionTimestamp (when finalization started)
	anchor := r.clock().Now()
	if sandbox.DeletionTimestamp != nil {
		anchor = sandbox.DeletionTimestamp.Time
	}

	deadline := anchor.Add(time.Duration(r.getTimeoutSeconds(sandbox)) * time.Second)
	sandbox.Status.TerminationDeadlineAt = &metav1.Time{Time: deadline}

	if err := r.patchStatus(ctx, baseSandbox, sandbox, nil); err != nil {
		return time.Time{}, fmt.Errorf("failed to persist termination deadline: %w", err)
	}

	log.Info("Termination deadline set", "deadline", deadline)
	return deadline, nil
}

func (r *SandboxReconciler) handleRootfsSnapshot(
	ctx context.Context,
	sandbox *sandboxv1alpha1.Sandbox,
	baseSandbox *sandboxv1alpha1.Sandbox,
	sandboxPod *corev1.Pod,
	snapshotDeadline time.Time,
	cfg *sandboxv1alpha1.SnapshotRootfsTermination,
) (ctrl.Result, bool, error) {
	log := logf.FromContext(ctx)

	now := r.clock().Now()
	if now.After(snapshotDeadline) {
		log.Info("Rootfs snapshot timed out", "deadline", snapshotDeadline)
		if err := r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
			{
				Type:               SandboxRootfsSnapshotCondition,
				Status:             metav1.ConditionFalse,
				Reason:             CondReasonRootfsSnapshotTimeout,
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
				Reason:             CondReasonRootfsSnapshotFailed,
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
		r.Recorder.Eventf(sandbox, nil, corev1.EventTypeWarning, "PodNotReady", "SnapshotBlocked", "Unable to perform rootfs snapshot: pod not ready")

		if err := r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
			{
				Type:               SandboxRootfsSnapshotCondition,
				Status:             metav1.ConditionFalse,
				Reason:             CondReasonRootfsSnapshotFailed,
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
		r.Recorder.Eventf(sandbox, nil, corev1.EventTypeWarning, "RuntimeNotSupported", "SnapshotBlocked", "Unable to perform rootfs snapshot")

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

	snap, err := r.getTerminationRootfssnapshot(ctx, sandbox)
	if err != nil {
		return ctrl.Result{}, false, err
	}

	if snap == nil {
		return r.createTerminationSnapshot(ctx, sandbox, baseSandbox, cfg)
	}

	snapshotName := snap.Name
	if snap.Status.CompletionTime == nil {
		log.Info("Snapshot in progress, waiting", "snapshot", snapshotName)
		if err := r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
			{
				Type:               SandboxRootfsSnapshotCondition,
				Status:             metav1.ConditionFalse,
				Reason:             CondReasonRootfsSnapshottingInProgress,
				Message:            fmt.Sprintf("Snapshot %q is running", snapshotName),
				ObservedGeneration: sandbox.Generation,
			},
		}); err != nil {
			return ctrl.Result{}, false, err
		}

		// RootfsSnapshot watch (via Owns) will trigger reconciliation when snapshot status changes
		requeueAfter := r.clock().Until(snapshotDeadline)
		if requeueAfter <= 0 {
			requeueAfter = time.Second
		}
		return ctrl.Result{RequeueAfter: requeueAfter}, false, nil
	}

	succeededCond := meta.FindStatusCondition(snap.Status.Conditions, sandboxv1alpha1.RootfsSnapshotSucceededCondition)
	if succeededCond != nil && succeededCond.Status == metav1.ConditionTrue {
		log.Info("Snapshot completed successfully", "snapshot", snapshotName)
		r.Recorder.Eventf(sandbox, nil, corev1.EventTypeNormal, "SnapshotSucceeded", "SnapshotCompleted", "Snapshot %q completed", snapshotName)
		if err := r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
			{
				Type:               SandboxRootfsSnapshotCondition,
				Status:             metav1.ConditionTrue,
				Reason:             CondReasonRootfsSnapshotComplete,
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
	if succeededCond != nil && succeededCond.Message != "" {
		message = succeededCond.Message
	}
	log.Info("Snapshot failed, proceeding with deletion", "snapshot", snapshotName)
	r.Recorder.Eventf(sandbox, nil, corev1.EventTypeWarning, "SnapshotFailed", "SnapshotFailed", "%s", message)
	if err := r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
		{
			Type:               SandboxRootfsSnapshotCondition,
			Status:             metav1.ConditionFalse,
			Reason:             CondReasonRootfsSnapshotFailed,
			Message:            message,
			ObservedGeneration: sandbox.Generation,
		},
	}); err != nil {
		return ctrl.Result{}, false, err
	}
	return ctrl.Result{}, true, nil
}

func (r *SandboxReconciler) createTerminationSnapshot(
	ctx context.Context,
	sandbox *sandboxv1alpha1.Sandbox,
	baseSandbox *sandboxv1alpha1.Sandbox,
	cfg *sandboxv1alpha1.SnapshotRootfsTermination,
) (ctrl.Result, bool, error) {
	log := logf.FromContext(ctx)

	snapshotName := cfg.SnapshotName
	if snapshotName == "" {
		snapshotName = sandbox.Name
	}
	timeoutSeconds := defaultTimeoutSeconds
	if cfg.TimeoutSeconds != nil {
		timeoutSeconds = *cfg.TimeoutSeconds
	}
	crName := podutil.GetTerminationSnapshotName(sandbox.Name)
	rootfsSnapshot := &sandboxv1alpha1.RootfsSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name:      crName,
			Namespace: sandbox.Namespace,
		},
		Spec: sandboxv1alpha1.RootfsSnapshotSpec{
			SandboxName:    sandbox.Name,
			SnapshotName:   snapshotName,
			TimeoutSeconds: &timeoutSeconds,
			// TTL must outlive the time between snapshot completion and the next sandbox
			// reconcile (~5s), otherwise the snapshot is deleted before we read the result.
			// Owner-ref cascade handles final cleanup when the sandbox is deleted.
			TTLSecondsAfterFinished: ptr.To(int32(300)),
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

	log.Info("Created termination RootfsSnapshot", "name", rootfsSnapshot.Name)
	r.Recorder.Eventf(sandbox, nil, corev1.EventTypeNormal, "RootfsSnapshotCreated", "Created", "Created RootfsSnapshot %q", rootfsSnapshot.Name)

	if err := r.patchStatus(ctx, baseSandbox, sandbox, []metav1.Condition{
		{
			Type:               SandboxRootfsSnapshotCondition,
			Status:             metav1.ConditionFalse,
			Reason:             CondReasonRootfsSnapshottingInProgress,
			Message:            fmt.Sprintf("RootfsSnapshot %q created, waiting for completion", rootfsSnapshot.Name),
			ObservedGeneration: sandbox.Generation,
		},
	}); err != nil {
		return ctrl.Result{}, false, err
	}

	return ctrl.Result{RequeueAfter: time.Second}, false, nil
}

// validateRootfsRestoreConfig checks that the reconciler is configured for rootfs restore
// (runtime class + host mount path). Called once before processing individual sources.
// Returns (true, err) for permanent misconfigurations, (false, err) for transient failures.
func (r *SandboxReconciler) validateRootfsRestoreConfig(ctx context.Context) (terminal bool, err error) {
	if r.RootfsSnapshotHostMountPath == "" {
		return true, fmt.Errorf("rootfsSnapshotSources requires rootfs snapshot restore to be configured (--rootfssnapshot-host-mount-path)")
	}

	supported, err := snapshot.CheckRuntimeClassSupport(ctx, r.Client, r.RuntimeClassName)
	if err != nil {
		// K8s API error — may be transient, allow retry
		return false, fmt.Errorf("failed to validate runtime for rootfs restore: %w", err)
	}
	if !supported {
		return true, fmt.Errorf("rootfsSnapshotSources requires a gVisor runtime (RuntimeClass %q handler is not runsc/gvisor)", r.RuntimeClassName)
	}

	return false, nil
}

func (r *SandboxReconciler) injectRootfsRestore(sources []sandboxv1alpha1.RootfsSnapshotSource, pod *corev1.Pod, namespace string) error {
	containers := make(map[string]struct{}, len(pod.Spec.Containers))
	for _, c := range pod.Spec.Containers {
		containers[c.Name] = struct{}{}
	}

	// Defense in depth: namespace comes from CR metadata (not user input),
	// but verify it's a safe path component since it's used to construct a host file path.
	if !filepath.IsLocal(namespace) {
		return reconcile.TerminalError(fmt.Errorf(
			"invalid namespace %q: must be a safe local path component", namespace))
	}

	for _, src := range sources {
		name := src.ContainerName
		if name == "" {
			if len(pod.Spec.Containers) != 1 {
				return reconcile.TerminalError(fmt.Errorf(
					"containerName must be specified when sandbox has %d containers", len(pod.Spec.Containers)))
			}
			name = pod.Spec.Containers[0].Name
		}

		if _, ok := containers[name]; !ok {
			return reconcile.TerminalError(fmt.Errorf(
				"container %q not found in sandbox pod", name))
		}

		// Defense in depth: CRD validation enforces the DNS-1123 subdomain pattern, but verify here
		// as well since SnapshotName is used to construct a host file path.
		if !filepath.IsLocal(src.SnapshotName) {
			return reconcile.TerminalError(fmt.Errorf(
				"invalid snapshot name %q: must be a safe local path component", src.SnapshotName))
		}

		key := fmt.Sprintf("dev.gvisor.tar.rootfs.upper.%s", name)
		if _, dup := pod.Annotations[key]; dup {
			return reconcile.TerminalError(fmt.Errorf(
				"duplicate restore target: container %q", name))
		}
		pod.Annotations[key] = fmt.Sprintf("%s/%s/%s.tar", r.RootfsSnapshotHostMountPath, namespace, src.SnapshotName)

		// Add per-container restart rules so kubelet retries exit code 128
		// (OCI runtime start failure, e.g. missing gVisor rootfs tar).
		// Requires the ContainerRestartRules feature gate (K8s 1.34+, enabled by default in 1.35+).
		rp := corev1.ContainerRestartPolicyNever
		for i := range pod.Spec.Containers {
			if pod.Spec.Containers[i].Name == name {
				pod.Spec.Containers[i].RestartPolicy = &rp
				pod.Spec.Containers[i].RestartPolicyRules = []corev1.ContainerRestartRule{{
					Action: corev1.ContainerRestartRuleActionRestart,
					ExitCodes: &corev1.ContainerRestartRuleOnExitCodes{
						Operator: corev1.ContainerRestartRuleOnExitCodesOpIn,
						Values:   []int32{128},
					},
				}}
				break
			}
		}
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *SandboxReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Recorder = mgr.GetEventRecorder("sandbox-controller")

	return ctrl.NewControllerManagedBy(mgr).
		For(&sandboxv1alpha1.Sandbox{}).
		Owns(&corev1.Pod{}).
		Owns(&sandboxv1alpha1.RootfsSnapshot{}).
		Owns(&networkingv1.NetworkPolicy{}).
		Named("sandbox").
		Complete(r)
}
