package handlers

import (
	"fmt"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	sandboxv1alpha1 "github.com/isola-ai/isola-sb/api/v1alpha1"
)

const (
	defaultCPU              = "125m"
	defaultMemory           = "512Mi"
	defaultEphemeralStorage = "1Gi"
	containerName           = "sandbox"
)

func sandboxToResponse(sb *sandboxv1alpha1.Sandbox) SandboxResponse {
	resp := SandboxResponse{
		ID:                sb.Name,
		Status:            conditionsToStatus(sb.Status.Conditions),
		CreationTimestamp: sb.CreationTimestamp.UTC().Format(time.RFC3339),
	}

	if len(sb.Spec.PodTemplate.Spec.Containers) > 0 {
		c := sb.Spec.PodTemplate.Spec.Containers[0]
		resp.Image = c.Image
		resp.Env = envVarsToMap(c.Env)
		resp.Resources = containerResourcesToSpec(c.Resources)
	}

	resp.TimeoutSeconds = sb.Spec.TimeoutSeconds
	resp.Network = crdNetworkToREST(sb.Spec.Network)

	return resp
}

func sandboxToSummary(sb *sandboxv1alpha1.Sandbox) SandboxSummary {
	summary := SandboxSummary{
		ID:                sb.Name,
		Status:            conditionsToStatus(sb.Status.Conditions),
		CreationTimestamp: sb.CreationTimestamp.UTC().Format(time.RFC3339),
	}

	if len(sb.Spec.PodTemplate.Spec.Containers) > 0 {
		summary.Image = sb.Spec.PodTemplate.Spec.Containers[0].Image
	}

	return summary
}

func conditionsToStatus(conditions []metav1.Condition) string {
	ready := meta.FindStatusCondition(conditions, "Ready")
	if ready == nil {
		return "unknown"
	}

	if ready.Status == metav1.ConditionTrue {
		return "running"
	}

	// TODO: remove snapshot-related reasons from Sandbox CRD — they should be
	// encapsulated in the RootfsSnapshot CRD only.
	switch ready.Reason {
	case "PodPending", "PodCreating", "Reconciling", "NetworkPolicyApplied":
		return "creating"
	case "PodRunning", "RootfsSnapshottingInProgress":
		return "running"
	case "Deleting":
		return "shuttingDown"
	case "PodFailed", "PodCreationFailed", "InvalidRuntime",
		"NetworkPolicyFailed", "RootfsSnapshotFailed", "RootfsSnapshotTimeout":
		return "failed"
	case "PodSucceeded", "RootfsSnapshotComplete":
		return "stopped"
	default:
		return "unknown"
	}
}

func envVarsToMap(envVars []corev1.EnvVar) map[string]string {
	if len(envVars) == 0 {
		return nil
	}

	m := make(map[string]string, len(envVars))
	for _, e := range envVars {
		if e.ValueFrom != nil {
			continue
		}
		m[e.Name] = e.Value
	}

	if len(m) == 0 {
		return nil
	}
	return m
}

func containerResourcesToSpec(r corev1.ResourceRequirements) *ResourcesSpec {
	spec := &ResourcesSpec{}
	hasContent := false

	if len(r.Limits) > 0 {
		spec.Limits = resourceListToREST(r.Limits)
		if spec.Limits != nil {
			hasContent = true
		}
	}
	if len(r.Requests) > 0 {
		spec.Requests = resourceListToREST(r.Requests)
		if spec.Requests != nil {
			hasContent = true
		}
	}

	if !hasContent {
		return nil
	}
	return spec
}

func resourceListToREST(rl corev1.ResourceList) *ResourceList {
	if len(rl) == 0 {
		return nil
	}

	out := &ResourceList{}
	hasContent := false

	if q, ok := rl[corev1.ResourceCPU]; ok {
		out.CPU = q.String()
		hasContent = true
	}
	if q, ok := rl[corev1.ResourceMemory]; ok {
		out.Memory = q.String()
		hasContent = true
	}
	if q, ok := rl[corev1.ResourceEphemeralStorage]; ok {
		out.EphemeralStorage = q.String()
		hasContent = true
	}

	if !hasContent {
		return nil
	}
	return out
}

func crdNetworkToREST(n *sandboxv1alpha1.NetworkSpec) *NetworkSpec {
	if n == nil {
		return nil
	}

	rest := &NetworkSpec{}
	hasContent := false

	if n.AllowAllInternet {
		rest.AllowAllInternet = &n.AllowAllInternet
		hasContent = true
	}
	if n.AllowClusterDNS {
		rest.AllowClusterDNS = &n.AllowClusterDNS
		hasContent = true
	}
	if len(n.AllowedEgressCIDRs) > 0 {
		rest.AllowedEgressCIDRs = n.AllowedEgressCIDRs
		hasContent = true
	}
	if len(n.Nameservers) > 0 {
		rest.Nameservers = n.Nameservers
		hasContent = true
	}

	if !hasContent {
		return nil
	}
	return rest
}

func requestToSandboxCR(req CreateSandboxRequest, name, namespace string) (*sandboxv1alpha1.Sandbox, error) {
	limits, err := buildResourceList(req.Resources, true)
	if err != nil {
		return nil, fmt.Errorf("invalid resource limits: %w", err)
	}

	requests, err := buildResourceList(req.Resources, false)
	if err != nil {
		return nil, fmt.Errorf("invalid resource requests: %w", err)
	}

	// Default requests to limits for any field not explicitly set
	defaultRequestsToLimits(requests, limits)

	container := corev1.Container{
		Name:  containerName,
		Image: req.Image,
		Resources: corev1.ResourceRequirements{
			Limits:   limits,
			Requests: requests,
		},
	}

	if len(req.Env) > 0 {
		keys := make([]string, 0, len(req.Env))
		for k := range req.Env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			container.Env = append(container.Env, corev1.EnvVar{Name: k, Value: req.Env[k]})
		}
	}

	sb := &sandboxv1alpha1.Sandbox{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: sandboxv1alpha1.SandboxSpec{
			PodTemplate: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{container},
				},
			},
			TimeoutSeconds: req.TimeoutSeconds,
		},
	}

	if req.Network != nil {
		sb.Spec.Network = restNetworkToCRD(req.Network)
	}

	return sb, nil
}

func buildResourceList(spec *ResourcesSpec, isLimits bool) (corev1.ResourceList, error) {
	rl := corev1.ResourceList{}

	var src *ResourceList
	if spec != nil {
		if isLimits {
			src = spec.Limits
		} else {
			src = spec.Requests
		}
	}

	// For limits, apply defaults for any unset fields
	if isLimits {
		cpuStr := defaultCPU
		memStr := defaultMemory
		esStr := defaultEphemeralStorage

		if src != nil {
			if src.CPU != "" {
				cpuStr = src.CPU
			}
			if src.Memory != "" {
				memStr = src.Memory
			}
			if src.EphemeralStorage != "" {
				esStr = src.EphemeralStorage
			}
		}

		cpu, err := resource.ParseQuantity(cpuStr)
		if err != nil {
			return nil, fmt.Errorf("invalid cpu %q: %w", cpuStr, err)
		}
		mem, err := resource.ParseQuantity(memStr)
		if err != nil {
			return nil, fmt.Errorf("invalid memory %q: %w", memStr, err)
		}
		es, err := resource.ParseQuantity(esStr)
		if err != nil {
			return nil, fmt.Errorf("invalid ephemeralStorage %q: %w", esStr, err)
		}

		rl[corev1.ResourceCPU] = cpu
		rl[corev1.ResourceMemory] = mem
		rl[corev1.ResourceEphemeralStorage] = es
		return rl, nil
	}

	// For requests, only parse explicitly set fields
	if src == nil {
		return rl, nil
	}

	if src.CPU != "" {
		q, err := resource.ParseQuantity(src.CPU)
		if err != nil {
			return nil, fmt.Errorf("invalid cpu %q: %w", src.CPU, err)
		}
		rl[corev1.ResourceCPU] = q
	}
	if src.Memory != "" {
		q, err := resource.ParseQuantity(src.Memory)
		if err != nil {
			return nil, fmt.Errorf("invalid memory %q: %w", src.Memory, err)
		}
		rl[corev1.ResourceMemory] = q
	}
	if src.EphemeralStorage != "" {
		q, err := resource.ParseQuantity(src.EphemeralStorage)
		if err != nil {
			return nil, fmt.Errorf("invalid ephemeralStorage %q: %w", src.EphemeralStorage, err)
		}
		rl[corev1.ResourceEphemeralStorage] = q
	}

	return rl, nil
}

func defaultRequestsToLimits(requests, limits corev1.ResourceList) {
	for _, res := range []corev1.ResourceName{
		corev1.ResourceCPU,
		corev1.ResourceMemory,
		corev1.ResourceEphemeralStorage,
	} {
		if _, ok := requests[res]; !ok {
			if lim, ok := limits[res]; ok {
				requests[res] = lim
			}
		}
	}
}

func restNetworkToCRD(n *NetworkSpec) *sandboxv1alpha1.NetworkSpec {
	if n == nil {
		return nil
	}

	crd := &sandboxv1alpha1.NetworkSpec{}

	if n.AllowAllInternet != nil {
		crd.AllowAllInternet = *n.AllowAllInternet
	}
	if n.AllowClusterDNS != nil {
		crd.AllowClusterDNS = *n.AllowClusterDNS
	}
	if len(n.AllowedEgressCIDRs) > 0 {
		crd.AllowedEgressCIDRs = n.AllowedEgressCIDRs
	}
	if len(n.Nameservers) > 0 {
		crd.Nameservers = n.Nameservers
	}

	return crd
}
