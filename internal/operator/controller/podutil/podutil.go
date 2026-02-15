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

package podutil

import (
	"fmt"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
)

func IsPodReady(pod *corev1.Pod) bool {
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

// IsPodTerminated returns true if the pod has finished (Succeeded or Failed).
func IsPodTerminated(pod *corev1.Pod) bool {
	if pod == nil {
		return false
	}
	return pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed
}

func GetSandboxPodName(sandboxName string) string {
	return sandboxName + "-pod"
}

func GetCustomNetworkPolicyName(sandboxName string) string {
	return sandboxName + "-custom-netpol"
}

func GetShutdownSnapshotName(sandboxName string) string {
	return sandboxName + "-shutdown"
}

func GetSnapshotJobName(snapshotName, containerName string) string {
	return snapshotName + "-" + containerName
}

// GetSnapshotContainerNames returns the names of containers to snapshot.
// Returns the user-specified list if non-empty, otherwise all non-init
// containers from the pod excluding the sandbox-sidecar.
func GetSnapshotContainerNames(pod *corev1.Pod, specified []string) []string {
	if len(specified) > 0 {
		return specified
	}
	var names []string
	for _, c := range pod.Spec.Containers {
		if c.Name != "sandbox-sidecar" {
			names = append(names, c.Name)
		}
	}
	return names
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

func IsJobComplete(job *batchv1.Job) bool {
	if job == nil {
		return false
	}
	for _, condition := range job.Status.Conditions {
		if condition.Type == batchv1.JobComplete && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func IsJobFailed(job *batchv1.Job) bool {
	if job == nil {
		return false
	}
	for _, condition := range job.Status.Conditions {
		if condition.Type == batchv1.JobFailed && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func GetJobConditionMessage(job *batchv1.Job, conditionType batchv1.JobConditionType) string {
	if job == nil {
		return ""
	}
	for _, cond := range job.Status.Conditions {
		if cond.Type == conditionType && cond.Status == corev1.ConditionTrue {
			return cond.Message
		}
	}
	return ""
}
