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

const containerName = "sandbox"

func sandboxToResponse(sb *sandboxv1alpha1.Sandbox) SandboxResponse {
	resp := SandboxResponse{
		ID:                sb.Name,
		Status:            conditionsToStatus(sb.Status.Conditions),
		CreationTimestamp: sb.CreationTimestamp.UTC().Format(time.RFC3339),
	}

	if len(sb.Spec.PodTemplate.Spec.Containers) > 0 {
		c := sb.Spec.PodTemplate.Spec.Containers[0]
		resp.PodTemplate.Container.Image = c.Image
		resp.PodTemplate.Container.Env = envVarsToMap(c.Env)
		resp.PodTemplate.Container.Resources = containerResourcesToSpec(c.Resources)
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

func mapToEnvVars(m map[string]string) []corev1.EnvVar {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	envVars := make([]corev1.EnvVar, 0, len(keys))
	for _, k := range keys {
		envVars = append(envVars, corev1.EnvVar{Name: k, Value: m[k]})
	}
	return envVars
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

	rest := &NetworkSpec{
		AllowAllInternet:   n.AllowAllInternet,
		AllowClusterDNS:    n.AllowClusterDNS,
		AllowedEgressCIDRs: n.AllowedEgressCIDRs,
		Nameservers:        n.Nameservers,
	}

	if rest.AllowAllInternet == nil && rest.AllowClusterDNS == nil &&
		len(rest.AllowedEgressCIDRs) == 0 && len(rest.Nameservers) == 0 {
		return nil
	}
	return rest
}

func requestToSandboxCR(req CreateSandboxRequest, name, namespace string) (*sandboxv1alpha1.Sandbox, error) {
	c := req.PodTemplate.Container
	container := corev1.Container{
		Name:  containerName,
		Image: c.Image,
	}

	if c.Resources != nil {
		limits, err := restResourceListToK8s(c.Resources.Limits)
		if err != nil {
			return nil, fmt.Errorf("invalid resource limits: %w", err)
		}
		requests, err := restResourceListToK8s(c.Resources.Requests)
		if err != nil {
			return nil, fmt.Errorf("invalid resource requests: %w", err)
		}
		container.Resources = corev1.ResourceRequirements{
			Limits:   limits,
			Requests: requests,
		}
	}

	container.Env = mapToEnvVars(c.Env)

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

// restResourceListToK8s parses a REST ResourceList into a K8s ResourceList.
// Returns nil if src is nil. Returns error on invalid quantities.
func restResourceListToK8s(src *ResourceList) (corev1.ResourceList, error) {
	if src == nil {
		return nil, nil
	}

	rl := corev1.ResourceList{}

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

	if len(rl) == 0 {
		return nil, nil
	}
	return rl, nil
}

func restNetworkToCRD(n *NetworkSpec) *sandboxv1alpha1.NetworkSpec {
	if n == nil {
		return nil
	}

	return &sandboxv1alpha1.NetworkSpec{
		AllowAllInternet:   n.AllowAllInternet,
		AllowClusterDNS:    n.AllowClusterDNS,
		AllowedEgressCIDRs: n.AllowedEgressCIDRs,
		Nameservers:        n.Nameservers,
	}
}
