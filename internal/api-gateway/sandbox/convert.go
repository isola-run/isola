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

package sandbox

import (
	"fmt"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	sandboxv1alpha1 "github.com/isola-run/isola/api/v1alpha1"
	apigateway "github.com/isola-run/isola/internal/api-gateway"
)

func sandboxToResponse(sb *sandboxv1alpha1.Sandbox) SandboxResponse {
	resp := SandboxResponse{
		ID:                sb.Name,
		Status:            apigateway.SandboxStatus(sb),
		CreationTimestamp: sb.CreationTimestamp.UTC().Format(time.RFC3339),
	}

	rootfsMap := make(map[string]string, len(sb.Spec.RootfsSnapshotSources))
	for _, src := range sb.Spec.RootfsSnapshotSources {
		rootfsMap[src.ContainerName] = src.SnapshotName
	}
	if len(sb.Spec.PodTemplate.Spec.Containers) == 1 {
		if snap, ok := rootfsMap[""]; ok {
			rootfsMap[sb.Spec.PodTemplate.Spec.Containers[0].Name] = snap
		}
	}

	for _, c := range sb.Spec.PodTemplate.Spec.Containers {
		resp.PodTemplate.Containers = append(resp.PodTemplate.Containers, ContainerInfo{
			Name:               c.Name,
			Image:              c.Image,
			Command:            c.Command,
			RootfsSnapshotName: rootfsMap[c.Name],
			Resources:          containerResourcesToSpec(c.Resources),
		})
	}

	resp.TimeoutSeconds = sb.Spec.TimeoutSeconds
	resp.StartupTimeoutSeconds = sb.Spec.StartupTimeoutSeconds
	resp.Network = crdNetworkToREST(sb.Spec.Network)
	resp.TerminationPolicy = crdTerminationPolicyToREST(sb.Spec.TerminationPolicy)

	return resp
}

func sandboxToSummary(sb *sandboxv1alpha1.Sandbox) SandboxSummary {
	return SandboxSummary{
		ID:                sb.Name,
		Status:            apigateway.SandboxStatus(sb),
		CreationTimestamp: sb.CreationTimestamp.UTC().Format(time.RFC3339),
	}
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

func containerResourcesToSpec(r corev1.ResourceRequirements) *ResourceRequirements {
	spec := &ResourceRequirements{
		Limits:   resourceListToREST(r.Limits),
		Requests: resourceListToREST(r.Requests),
	}
	if spec.Limits == nil && spec.Requests == nil {
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

func crdNetworkToREST(n *sandboxv1alpha1.Network) *Network {
	if n == nil {
		return nil
	}

	rest := &Network{
		AllowInternetEgress: n.AllowInternetEgress,
		AllowClusterDNS:     n.AllowClusterDNS,
		AllowIPv6Egress:     n.AllowIPv6Egress,
		AllowedEgressCIDRs:  n.AllowedEgressCIDRs,
		Nameservers:         n.Nameservers,
	}
	if n.EgressRateLimit != nil {
		rest.EgressRateLimit = &EgressRateLimit{
			RateBytesPerSecond: n.EgressRateLimit.RateBytesPerSecond,
		}
	}
	return rest
}

func requestToSandboxCR(req CreateSandboxRequest, name, namespace string) (*sandboxv1alpha1.Sandbox, error) {
	containers := make([]corev1.Container, 0, len(req.PodTemplate.Containers))
	var rootfsSources []sandboxv1alpha1.RootfsSnapshotSource
	for i, c := range req.PodTemplate.Containers {
		containerName := c.Name
		if containerName == "" {
			containerName = fmt.Sprintf("sandbox%d", i)
		}
		container := corev1.Container{
			Name:    containerName,
			Image:   c.Image,
			Command: c.Command,
		}

		if c.Resources != nil {
			limits, err := restResourceListToK8s(c.Resources.Limits)
			if err != nil {
				return nil, fmt.Errorf("container %q: invalid resource limits: %w", containerName, err)
			}
			requests, err := restResourceListToK8s(c.Resources.Requests)
			if err != nil {
				return nil, fmt.Errorf("container %q: invalid resource requests: %w", containerName, err)
			}
			container.Resources = corev1.ResourceRequirements{
				Limits:   limits,
				Requests: requests,
			}
		}

		container.Env = mapToEnvVars(c.Env)
		containers = append(containers, container)

		if c.RootfsSnapshotName != "" {
			rootfsSources = append(rootfsSources, sandboxv1alpha1.RootfsSnapshotSource{
				SnapshotName:  c.RootfsSnapshotName,
				ContainerName: containerName,
			})
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
					Containers: containers,
				},
			},
			TimeoutSeconds:        req.TimeoutSeconds,
			StartupTimeoutSeconds: req.StartupTimeoutSeconds,
			RootfsSnapshotSources: rootfsSources,
		},
	}

	sb.Spec.Network = restNetworkToCRD(req.Network)
	sb.Spec.TerminationPolicy = restTerminationPolicyToCRD(req.TerminationPolicy)
	// snapshotName in the termination policy defaults to the sandbox name if omitted.
	// kubebuilder can't express cross-field defaults, so we set it explicitly before writing the CRD.
	if sb.Spec.TerminationPolicy != nil && sb.Spec.TerminationPolicy.SnapshotRootfs != nil && sb.Spec.TerminationPolicy.SnapshotRootfs.SnapshotName == "" {
		sb.Spec.TerminationPolicy.SnapshotRootfs.SnapshotName = name
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

func restNetworkToCRD(n *Network) *sandboxv1alpha1.Network {
	if n == nil {
		return nil
	}

	crd := &sandboxv1alpha1.Network{
		AllowInternetEgress: n.AllowInternetEgress,
		AllowClusterDNS:     n.AllowClusterDNS,
		AllowIPv6Egress:     n.AllowIPv6Egress,
		AllowedEgressCIDRs:  n.AllowedEgressCIDRs,
		Nameservers:         n.Nameservers,
	}
	if n.EgressRateLimit != nil {
		crd.EgressRateLimit = &sandboxv1alpha1.EgressRateLimit{
			RateBytesPerSecond: n.EgressRateLimit.RateBytesPerSecond,
		}
	}
	return crd
}

func restTerminationPolicyToCRD(rest *TerminationPolicy) *sandboxv1alpha1.TerminationPolicy {
	if rest == nil {
		return nil
	}
	crd := &sandboxv1alpha1.TerminationPolicy{
		Type: sandboxv1alpha1.SandboxTerminationType(rest.Type),
	}
	if rest.SnapshotRootfs != nil {
		crd.SnapshotRootfs = &sandboxv1alpha1.SnapshotRootfsTermination{
			SnapshotName:   rest.SnapshotRootfs.SnapshotName,
			TimeoutSeconds: rest.SnapshotRootfs.TimeoutSeconds,
		}
	}
	return crd
}

func crdTerminationPolicyToREST(tp *sandboxv1alpha1.TerminationPolicy) *TerminationPolicy {
	if tp == nil {
		return nil
	}
	rest := &TerminationPolicy{
		Type: string(tp.Type),
	}
	if tp.SnapshotRootfs != nil {
		rest.SnapshotRootfs = &SnapshotRootfsTermination{
			SnapshotName:   tp.SnapshotRootfs.SnapshotName,
			TimeoutSeconds: tp.SnapshotRootfs.TimeoutSeconds,
		}
	}
	return rest
}
