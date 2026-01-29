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

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	sandboxv1alpha1 "github.com/isola-ai/isola-sb/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

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
