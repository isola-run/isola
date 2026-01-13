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

package snapshot

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	nodev1 "k8s.io/api/node/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Reasons returned by CheckRootfsSnapshotSupport
const (
	ReasonSupported            = "Supported"
	ReasonPodDoesNotExist      = "PodDoesNotExist"
	ReasonRuntimeClassMissing  = "RuntimeClassMissing"
	ReasonRuntimeUnsupported   = "RuntimeUnsupported"
	ReasonRuntimeClassNotFound = "RuntimeClassNotFound"
	ReasonPodNotReady          = "PodNotReady"
)

// GetSandboxPodName returns the pod name for a sandbox.
func GetSandboxPodName(sandboxName string) string {
	return sandboxName + "-pod"
}

// CheckRootfsSnapshotSupport validates the pod can be snapshotted.
// Checks that the pod exists, is ready, and uses a gvisor/runsc runtime.
// Returns (supported bool, reason string, err error).
// The reason is one of the Reason* constants.
// An error is only returned for unexpected failures (e.g., API errors).
func CheckRootfsSnapshotSupport(ctx context.Context, c client.Client, pod *corev1.Pod) (bool, string, error) {
	if pod == nil {
		return false, ReasonPodDoesNotExist, nil
	}
	if !isPodReady(pod) {
		return false, ReasonPodNotReady, nil
	}

	runtimeClassName := pod.Spec.RuntimeClassName
	if runtimeClassName == nil || *runtimeClassName == "" {
		return false, ReasonRuntimeClassMissing, nil
	}

	runtimeClass := &nodev1.RuntimeClass{}
	if err := c.Get(ctx, types.NamespacedName{Name: *runtimeClassName}, runtimeClass); err != nil {
		return false, ReasonRuntimeClassNotFound, err
	}

	if runtimeClass.Handler == "runsc" || runtimeClass.Handler == "gvisor" {
		return true, ReasonSupported, nil
	}

	return false, ReasonRuntimeUnsupported, nil
}

// isPodReady returns true if the pod is running and has the Ready condition set to True.
func isPodReady(pod *corev1.Pod) bool {
	if pod == nil {
		return false
	}
	if pod.Status.Phase != corev1.PodRunning {
		return false
	}
	for i := range pod.Status.Conditions {
		c := pod.Status.Conditions[i]
		if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

// ExtractContainerID gets the container ID for a named container.
// Returns the ID without the containerd:// prefix.
// The containerName must match a container in the pod's status.
func ExtractContainerID(pod *corev1.Pod, containerName string) (string, error) {
	if pod == nil {
		return "", fmt.Errorf("pod is nil")
	}
	if pod.Status.ContainerStatuses == nil {
		return "", fmt.Errorf("pod has no container statuses")
	}

	for _, cs := range pod.Status.ContainerStatuses {
		if cs.Name == containerName {
			if cs.ContainerID == "" {
				return "", fmt.Errorf("container %q has no containerID yet", containerName)
			}
			parts := strings.SplitN(cs.ContainerID, "://", 2)
			if len(parts) != 2 {
				return "", fmt.Errorf("unexpected containerID format for %q: %s", containerName, cs.ContainerID)
			}
			return parts[1], nil
		}
	}

	return "", fmt.Errorf("container %q not found in pod status", containerName)
}

// GetSnapshotableContainers returns names of all non-init containers in the pod.
// These are the containers that can be snapshotted.
func GetSnapshotableContainers(pod *corev1.Pod) []string {
	if pod == nil {
		return nil
	}

	containers := make([]string, 0, len(pod.Spec.Containers))
	for _, c := range pod.Spec.Containers {
		containers = append(containers, c.Name)
	}
	return containers
}
