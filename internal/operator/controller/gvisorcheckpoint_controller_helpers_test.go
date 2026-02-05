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
	"encoding/json"

	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	sandboxv1alpha1 "github.com/isola-ai/isola-sb/api/v1alpha1"
	snapshotpkg "github.com/isola-ai/isola-sb/internal/snapshot"
)

// Helper functions for gvisorcheckpoint controller tests

func createCheckpointPod(ctx context.Context, name, runtimeClassName string, containers []corev1.Container, containerStatuses []corev1.ContainerStatus) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
		Spec: corev1.PodSpec{
			RuntimeClassName: &runtimeClassName,
			NodeName:         "test-node",
			Containers:       containers,
		},
	}
	ExpectWithOffset(1, k8sClient.Create(ctx, pod)).To(Succeed())
	pod.Status.Phase = corev1.PodRunning
	pod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
	pod.Status.ContainerStatuses = containerStatuses
	ExpectWithOffset(1, k8sClient.Status().Update(ctx, pod)).To(Succeed())
}

func createCheckpointPodNotReady(ctx context.Context, name, runtimeClassName string, containers []corev1.Container) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
		Spec: corev1.PodSpec{
			RuntimeClassName: &runtimeClassName,
			NodeName:         "test-node",
			Containers:       containers,
		},
	}
	ExpectWithOffset(1, k8sClient.Create(ctx, pod)).To(Succeed())
	pod.Status.Phase = corev1.PodPending
	pod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionFalse, Reason: "ContainersNotReady"}}
	ExpectWithOffset(1, k8sClient.Status().Update(ctx, pod)).To(Succeed())
}

func deleteCheckpointPod(ctx context.Context, name string) {
	pod := &corev1.Pod{}
	err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: testNamespace}, pod)
	if errors.IsNotFound(err) {
		return // Already deleted
	}
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	ExpectWithOffset(1, client.IgnoreNotFound(k8sClient.Delete(ctx, pod))).NotTo(HaveOccurred())
}

func createGvisorCheckpointCR(ctx context.Context, name, sandboxName, containerName string) {
	chkpt := &sandboxv1alpha1.GvisorCheckpoint{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: sandboxv1alpha1.GvisorCheckpointSpec{
			SandboxName:   sandboxName,
			ContainerName: containerName,
		},
	}
	ExpectWithOffset(1, k8sClient.Create(ctx, chkpt)).To(Succeed())
}

func deleteGvisorCheckpointCR(ctx context.Context, name string) {
	chkpt := &sandboxv1alpha1.GvisorCheckpoint{}
	err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: testNamespace}, chkpt)
	if errors.IsNotFound(err) {
		return // Already deleted
	}
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	ExpectWithOffset(1, client.IgnoreNotFound(k8sClient.Delete(ctx, chkpt))).NotTo(HaveOccurred())
}

func getGvisorCheckpointCR(ctx context.Context, name string) *sandboxv1alpha1.GvisorCheckpoint {
	chkpt := &sandboxv1alpha1.GvisorCheckpoint{}
	err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: testNamespace}, chkpt)
	if err != nil {
		return nil
	}
	return chkpt
}

func getCheckpointJob(ctx context.Context, name string) *batchv1.Job {
	job := &batchv1.Job{}
	err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: testNamespace}, job)
	if err != nil {
		return nil
	}
	return job
}

func deleteCheckpointJob(ctx context.Context, name string) {
	job := &batchv1.Job{}
	err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: testNamespace}, job)
	if errors.IsNotFound(err) {
		return // Already deleted
	}
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	propagationPolicy := metav1.DeletePropagationBackground
	ExpectWithOffset(1, client.IgnoreNotFound(k8sClient.Delete(ctx, job, &client.DeleteOptions{PropagationPolicy: &propagationPolicy}))).NotTo(HaveOccurred())
}

func setCheckpointJobComplete(ctx context.Context, name string) {
	job := getCheckpointJob(ctx, name)
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

func setCheckpointJobFailed(ctx context.Context, name, message string) {
	job := getCheckpointJob(ctx, name)
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

// createCheckpointJobPodWithTerminationMessage creates a pod for the job with a termination message
// containing the upload result (simulating what the uploader writes)
func createCheckpointJobPodWithTerminationMessage(ctx context.Context, jobName string, result *snapshotpkg.UploadResult) {
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

func deleteCheckpointJobPod(ctx context.Context, jobName string) {
	pod := &corev1.Pod{}
	err := k8sClient.Get(ctx, types.NamespacedName{Name: jobName + "-pod", Namespace: testNamespace}, pod)
	if errors.IsNotFound(err) {
		return // Already deleted
	}
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	ExpectWithOffset(1, client.IgnoreNotFound(k8sClient.Delete(ctx, pod))).NotTo(HaveOccurred())
}
