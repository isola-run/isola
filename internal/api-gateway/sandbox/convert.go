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

const containerName = "sandbox"

func sandboxToResponse(sb *sandboxv1alpha1.Sandbox) SandboxResponse {
	resp := SandboxResponse{
		SandboxID:         sb.Name,
		Status:            apigateway.ConditionsToStatus(sb.Status.Conditions),
		CreationTimestamp: sb.CreationTimestamp.UTC().Format(time.RFC3339),
	}

	if len(sb.Spec.PodTemplate.Spec.Containers) > 0 {
		c := sb.Spec.PodTemplate.Spec.Containers[0]
		resp.PodTemplate.Container.Image = c.Image
		resp.PodTemplate.Container.Command = c.Command
		resp.PodTemplate.Container.Resources = containerResourcesToSpec(c.Resources)
	}

	resp.TimeoutSeconds = sb.Spec.TimeoutSeconds
	resp.StartupTimeoutSeconds = sb.Spec.StartupTimeoutSeconds
	resp.Network = crdNetworkToREST(sb.Spec.Network)
	resp.RootfsSnapshotSources = crdRootfsSnapshotSourcesToREST(sb.Spec.RootfsSnapshotSources)

	return resp
}

func sandboxToSummary(sb *sandboxv1alpha1.Sandbox) SandboxSummary {
	return SandboxSummary{
		SandboxID:         sb.Name,
		Status:            apigateway.ConditionsToStatus(sb.Status.Conditions),
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

func containerResourcesToSpec(r corev1.ResourceRequirements) *ResourcesSpec {
	spec := &ResourcesSpec{
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

func crdNetworkToREST(n *sandboxv1alpha1.NetworkSpec) *NetworkSpec {
	if n == nil {
		return nil
	}

	return &NetworkSpec{
		AllowInternetEgress: n.AllowInternetEgress,
		AllowClusterDNS:     n.AllowClusterDNS,
		AllowIPv6Egress:     n.AllowIPv6Egress,
		AllowedEgressCIDRs:  n.AllowedEgressCIDRs,
		Nameservers:         n.Nameservers,
	}
}

func requestToSandboxCR(req CreateSandboxRequest, name, namespace string) (*sandboxv1alpha1.Sandbox, error) {
	c := req.PodTemplate.Container
	container := corev1.Container{
		Name:    containerName,
		Image:   c.Image,
		Command: c.Command,
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
			TimeoutSeconds:        req.TimeoutSeconds,
			StartupTimeoutSeconds: req.StartupTimeoutSeconds,
		},
	}

	sb.Spec.Network = restNetworkToCRD(req.Network)
	sb.Spec.RootfsSnapshotSources = restRootfsSnapshotSourcesToCRD(req.RootfsSnapshotSources)

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

func restRootfsSnapshotSourcesToCRD(sources []RootfsSnapshotSource) []sandboxv1alpha1.RootfsSnapshotSource {
	if len(sources) == 0 {
		return nil
	}
	out := make([]sandboxv1alpha1.RootfsSnapshotSource, len(sources))
	for i, s := range sources {
		out[i] = sandboxv1alpha1.RootfsSnapshotSource{
			SnapshotName:  s.SnapshotName,
			ContainerName: s.ContainerName,
		}
	}
	return out
}

func crdRootfsSnapshotSourcesToREST(sources []sandboxv1alpha1.RootfsSnapshotSource) []RootfsSnapshotSource {
	if len(sources) == 0 {
		return nil
	}
	out := make([]RootfsSnapshotSource, len(sources))
	for i, s := range sources {
		out[i] = RootfsSnapshotSource{
			SnapshotName:  s.SnapshotName,
			ContainerName: s.ContainerName,
		}
	}
	return out
}

func restNetworkToCRD(n *NetworkSpec) *sandboxv1alpha1.NetworkSpec {
	if n == nil {
		return nil
	}

	return &sandboxv1alpha1.NetworkSpec{
		AllowInternetEgress: n.AllowInternetEgress,
		AllowClusterDNS:     n.AllowClusterDNS,
		AllowIPv6Egress:     n.AllowIPv6Egress,
		AllowedEgressCIDRs:  n.AllowedEgressCIDRs,
		Nameservers:         n.Nameservers,
	}
}
