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
	"encoding/json"

	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	nodev1 "k8s.io/api/node/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	sandboxv1alpha1 "github.com/isola-ai/isola/api/v1alpha1"
	snapshotpkg "github.com/isola-ai/isola/internal/snapshot"
)

// Helper functions for rootfssnapshot controller tests

func createRuntimeClassForSnapshot(ctx context.Context, name, handler string) {
	rc := &nodev1.RuntimeClass{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Handler:    handler,
	}
	err := k8sClient.Create(ctx, rc)
	if !errors.IsAlreadyExists(err) {
		ExpectWithOffset(1, err).NotTo(HaveOccurred())
	}
}

func deleteRuntimeClassForSnapshot(ctx context.Context, name string) {
	deleteResource(ctx, name, "", &nodev1.RuntimeClass{})
}

func createSnapshotPodWithStatus(ctx context.Context, name, runtimeClassName string, containers []corev1.Container, phase corev1.PodPhase, readyStatus corev1.ConditionStatus, containerStatuses []corev1.ContainerStatus) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
		Spec: corev1.PodSpec{
			RuntimeClassName: &runtimeClassName,
			Containers:       containers,
		},
	}
	ExpectWithOffset(2, k8sClient.Create(ctx, pod)).To(Succeed())

	// Bind the pod to a node via the binding subresource (mirroring the real scheduler)
	binding := &corev1.Binding{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
		Target:     corev1.ObjectReference{Name: "test-node"},
	}
	ExpectWithOffset(2, k8sClient.SubResource("binding").Create(ctx, pod, binding)).To(Succeed())

	// Re-fetch after binding to get updated spec
	ExpectWithOffset(2, k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: testNamespace}, pod)).To(Succeed())
	pod.Status.Phase = phase
	cond := corev1.PodCondition{Type: corev1.PodReady, Status: readyStatus}
	if readyStatus == corev1.ConditionFalse {
		cond.Reason = "ContainersNotReady"
	}
	pod.Status.Conditions = []corev1.PodCondition{cond}
	pod.Status.ContainerStatuses = containerStatuses
	ExpectWithOffset(2, k8sClient.Status().Update(ctx, pod)).To(Succeed())
}

func createSnapshotPod(ctx context.Context, name, runtimeClassName string, containers []corev1.Container, containerStatuses []corev1.ContainerStatus) {
	createSnapshotPodWithStatus(ctx, name, runtimeClassName, containers, corev1.PodRunning, corev1.ConditionTrue, containerStatuses)
}

func createSnapshotPodNotReady(ctx context.Context, name, runtimeClassName string, containers []corev1.Container) {
	createSnapshotPodWithStatus(ctx, name, runtimeClassName, containers, corev1.PodPending, corev1.ConditionFalse, nil)
}

func deleteSnapshotPod(ctx context.Context, name string) {
	deleteResource(ctx, name, testNamespace, &corev1.Pod{})
}

func createRootfsSnapshotCR(ctx context.Context, name, sandboxName string) {
	createRootfsSnapshotCRWithContainer(ctx, name, sandboxName, "")
}

func createRootfsSnapshotCRWithContainer(ctx context.Context, name, sandboxName, container string) {
	snap := &sandboxv1alpha1.RootfsSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: sandboxv1alpha1.RootfsSnapshotSpec{
			SandboxName:   sandboxName,
			SnapshotName:  name,
			ContainerName: container,
		},
	}
	ExpectWithOffset(2, k8sClient.Create(ctx, snap)).To(Succeed())
}

func deleteRootfsSnapshotCR(ctx context.Context, name string) {
	deleteResource(ctx, name, testNamespace, &sandboxv1alpha1.RootfsSnapshot{})
}

func getRootfsSnapshotCR(ctx context.Context, name string) *sandboxv1alpha1.RootfsSnapshot {
	snap := &sandboxv1alpha1.RootfsSnapshot{}
	err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: testNamespace}, snap)
	if err != nil {
		return nil
	}
	return snap
}

func getSnapshotJob(ctx context.Context, name string) *batchv1.Job {
	job := &batchv1.Job{}
	err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: testNamespace}, job)
	if err != nil {
		return nil
	}
	return job
}

func deleteSnapshotJob(ctx context.Context, name string) {
	job := &batchv1.Job{}
	err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: testNamespace}, job)
	if errors.IsNotFound(err) {
		return // Already deleted
	}
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	propagationPolicy := metav1.DeletePropagationBackground
	ExpectWithOffset(1, client.IgnoreNotFound(k8sClient.Delete(ctx, job, &client.DeleteOptions{PropagationPolicy: &propagationPolicy}))).NotTo(HaveOccurred())
}

func setSnapshotJobComplete(ctx context.Context, name string) {
	job := getSnapshotJob(ctx, name)
	if job == nil {
		return
	}
	now := metav1.Now()
	job.Status.StartTime = &now
	job.Status.CompletionTime = &now
	job.Status.Succeeded = 1
	job.Status.Conditions = []batchv1.JobCondition{
		{Type: batchv1.JobSuccessCriteriaMet, Status: corev1.ConditionTrue},
		{Type: batchv1.JobComplete, Status: corev1.ConditionTrue},
	}
	ExpectWithOffset(1, k8sClient.Status().Update(ctx, job)).To(Succeed())
}

func setSnapshotJobFailed(ctx context.Context, name, message string) {
	job := getSnapshotJob(ctx, name)
	if job == nil {
		return
	}
	now := metav1.Now()
	job.Status.StartTime = &now
	job.Status.Failed = 1
	job.Status.Conditions = []batchv1.JobCondition{
		{Type: batchv1.JobFailureTarget, Status: corev1.ConditionTrue},
		{Type: batchv1.JobFailed, Status: corev1.ConditionTrue, Message: message},
	}
	ExpectWithOffset(1, k8sClient.Status().Update(ctx, job)).To(Succeed())
}

// createSnapshotJobPodWithTerminationMessage creates a pod for the job with a termination message
// containing the upload result (simulating what the uploader writes)
func createSnapshotJobPodWithTerminationMessage(ctx context.Context, jobName string, result *snapshotpkg.UploadResult) {
	// Create termination message JSON
	var terminationMessage string
	if result != nil {
		data, err := json.Marshal(result)
		ExpectWithOffset(1, err).NotTo(HaveOccurred())
		terminationMessage = string(data)
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName + "-pod",
			Namespace: testNamespace,
			Labels:    map[string]string{"job-name": jobName},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "uploader", Image: "test"},
			},
			RestartPolicy: corev1.RestartPolicyNever,
		},
	}
	ExpectWithOffset(1, k8sClient.Create(ctx, pod)).To(Succeed())

	// Update status with termination message
	pod.Status.Phase = corev1.PodSucceeded
	pod.Status.ContainerStatuses = []corev1.ContainerStatus{
		{
			Name: "uploader",
			State: corev1.ContainerState{
				Terminated: &corev1.ContainerStateTerminated{
					ExitCode: 0,
					Message:  terminationMessage,
				},
			},
		},
	}
	ExpectWithOffset(1, k8sClient.Status().Update(ctx, pod)).To(Succeed())
}

func deleteSnapshotJobPod(ctx context.Context, jobName string) {
	deleteResource(ctx, jobName+"-pod", testNamespace, &corev1.Pod{})
}
