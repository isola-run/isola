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

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	nodev1 "k8s.io/api/node/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	sandboxv1alpha1 "github.com/isola-ai/isola-sb/api/v1alpha1"
)

// Helper functions for sandbox controller tests

func createSandbox(ctx context.Context, name, templateRef string) *sandboxv1alpha1.Sandbox {
	sandbox := &sandboxv1alpha1.Sandbox{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: sandboxv1alpha1.SandboxSpec{
			TemplateRef: sandboxv1alpha1.SandboxTemplateReference{
				Name: templateRef,
			},
		},
	}
	ExpectWithOffset(1, k8sClient.Create(ctx, sandbox)).To(Succeed())
	return sandbox
}

func createTemplate(ctx context.Context, name string, opts ...func(*sandboxv1alpha1.SandboxTemplate)) *sandboxv1alpha1.SandboxTemplate {
	template := &sandboxv1alpha1.SandboxTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: sandboxv1alpha1.SandboxTemplateSpec{
			PodTemplate: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:    "sandbox",
							Image:   "busybox:latest",
							Command: []string{"sleep", "infinity"},
						},
					},
				},
			},
		},
	}
	for _, opt := range opts {
		opt(template)
	}
	ExpectWithOffset(1, k8sClient.Create(ctx, template)).To(Succeed())
	return template
}

func createRuntimeClass(ctx context.Context, name, handler string) {
	rc := &nodev1.RuntimeClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Handler: handler,
	}
	ExpectWithOffset(1, k8sClient.Create(ctx, rc)).To(Succeed())
}

func getSandbox(ctx context.Context, name string) *sandboxv1alpha1.Sandbox {
	sandbox := &sandboxv1alpha1.Sandbox{}
	err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: testNamespace}, sandbox)
	if errors.IsNotFound(err) {
		return nil
	}
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	return sandbox
}

func getPod(ctx context.Context, name string) *corev1.Pod {
	pod := &corev1.Pod{}
	err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: testNamespace}, pod)
	if errors.IsNotFound(err) {
		return nil
	}
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	return pod
}

func deleteSandbox(ctx context.Context, name string) {
	sandbox := &sandboxv1alpha1.Sandbox{}
	err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: testNamespace}, sandbox)
	if errors.IsNotFound(err) {
		return // Already deleted
	}
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	ExpectWithOffset(1, client.IgnoreNotFound(k8sClient.Delete(ctx, sandbox))).NotTo(HaveOccurred())
}

func deleteTemplate(ctx context.Context, name string) {
	template := &sandboxv1alpha1.SandboxTemplate{}
	err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: testNamespace}, template)
	if errors.IsNotFound(err) {
		return // Already deleted
	}
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	ExpectWithOffset(1, client.IgnoreNotFound(k8sClient.Delete(ctx, template))).NotTo(HaveOccurred())
}

func deleteRuntimeClass(ctx context.Context, name string) {
	rc := &nodev1.RuntimeClass{}
	err := k8sClient.Get(ctx, types.NamespacedName{Name: name}, rc)
	if errors.IsNotFound(err) {
		return // Already deleted
	}
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	ExpectWithOffset(1, client.IgnoreNotFound(k8sClient.Delete(ctx, rc))).NotTo(HaveOccurred())
}

func deletePod(ctx context.Context, name string) {
	pod := &corev1.Pod{}
	err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: testNamespace}, pod)
	if errors.IsNotFound(err) {
		return // Already deleted
	}
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	ExpectWithOffset(1, client.IgnoreNotFound(k8sClient.Delete(ctx, pod))).NotTo(HaveOccurred())
}

func getRootfsSnapshot(ctx context.Context, name string) *sandboxv1alpha1.RootfsSnapshot {
	snap := &sandboxv1alpha1.RootfsSnapshot{}
	err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: testNamespace}, snap)
	if errors.IsNotFound(err) {
		return nil
	}
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	return snap
}

func getShutdownSnapshot(ctx context.Context, sandboxName string) *sandboxv1alpha1.RootfsSnapshot {
	return getRootfsSnapshot(ctx, sandboxName+"-shutdown")
}

func deleteShutdownSnapshot(ctx context.Context, sandboxName string) {
	snap := getShutdownSnapshot(ctx, sandboxName)
	if snap == nil {
		return // Already deleted
	}
	ExpectWithOffset(1, client.IgnoreNotFound(k8sClient.Delete(ctx, snap))).NotTo(HaveOccurred())
}

// setRootfsSnapshotTerminal sets a RootfsSnapshot to a terminal state (Succeeded, Failed, DeadlineExceeded).
// Terminal states always have CompletedAt set.
func setRootfsSnapshotTerminal(ctx context.Context, name string, succeeded bool, reason, message string) {
	snap := getRootfsSnapshot(ctx, name)
	if snap == nil {
		return
	}
	status := metav1.ConditionTrue
	if !succeeded {
		status = metav1.ConditionFalse
	}
	meta.SetStatusCondition(&snap.Status.Conditions, metav1.Condition{
		Type:               string(sandboxv1alpha1.RootfsSnapshotComplete),
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: snap.Generation,
	})
	now := metav1.Now()
	snap.Status.CompletedAt = &now
	ExpectWithOffset(1, k8sClient.Status().Update(ctx, snap)).To(Succeed())
}

func setShutdownSnapshotTerminal(ctx context.Context, sandboxName string, succeeded bool, reason, message string) {
	setRootfsSnapshotTerminal(ctx, sandboxName+"-shutdown", succeeded, reason, message)
}

func createSandboxWithNetwork(ctx context.Context, name, templateRef string, network *sandboxv1alpha1.NetworkSpec) {
	sandbox := &sandboxv1alpha1.Sandbox{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: sandboxv1alpha1.SandboxSpec{
			TemplateRef: sandboxv1alpha1.SandboxTemplateReference{
				Name: templateRef,
			},
			Network: network,
		},
	}
	ExpectWithOffset(1, k8sClient.Create(ctx, sandbox)).To(Succeed())
}

func hasConditionWithReason(sandbox *sandboxv1alpha1.Sandbox, condType string, status metav1.ConditionStatus, reason string) bool {
	cond := meta.FindStatusCondition(sandbox.Status.Conditions, condType)
	return cond != nil && cond.Status == status && cond.Reason == reason
}

// recreatePodWithNodeName deletes the existing pod and creates a new one with NodeName set
// This is needed because Kubernetes doesn't allow updating NodeName on existing pods
func recreatePodWithNodeName(ctx context.Context, podName, nodeName string, runtimeClassName *string) *corev1.Pod {
	// Get the existing pod to copy labels
	existingPod := getPod(ctx, podName)
	ExpectWithOffset(1, existingPod).NotTo(BeNil())
	labels := existingPod.Labels
	ExpectWithOffset(1, k8sClient.Delete(ctx, existingPod)).To(Succeed())

	// Create new pod with NodeName
	newPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: testNamespace,
			Labels:    labels,
		},
		Spec: corev1.PodSpec{
			RuntimeClassName: runtimeClassName,
			NodeName:         nodeName,
			Containers: []corev1.Container{
				{Name: "sandbox", Image: "busybox:latest", Command: []string{"sleep", "infinity"}},
			},
		},
	}
	ExpectWithOffset(1, k8sClient.Create(ctx, newPod)).To(Succeed())
	return newPod
}

// makePodReady updates pod status to make it appear ready
func makePodReady(ctx context.Context, pod *corev1.Pod, containerID string) {
	pod.Status.Phase = corev1.PodRunning
	pod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
	pod.Status.ContainerStatuses = []corev1.ContainerStatus{
		{Name: "sandbox", ContainerID: containerID, Ready: true, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
	}
	ExpectWithOffset(1, k8sClient.Status().Update(ctx, pod)).To(Succeed())
}

// doReconcile is a convenience helper that reconciles the sandbox.
// The controller adds the finalizer and proceeds with work in a single reconcile.
func doReconcile(ctx context.Context, reconciler *SandboxReconciler, name string) (reconcile.Result, error) {
	req := reconcile.Request{NamespacedName: types.NamespacedName{Name: name, Namespace: testNamespace}}
	return reconciler.Reconcile(ctx, req)
}

func getNetworkPolicy(ctx context.Context, name string) *networkingv1.NetworkPolicy {
	np := &networkingv1.NetworkPolicy{}
	err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: testNamespace}, np)
	if errors.IsNotFound(err) {
		return nil
	}
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	return np
}

func deleteNetworkPolicy(ctx context.Context, name string) {
	np := &networkingv1.NetworkPolicy{}
	err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: testNamespace}, np)
	if errors.IsNotFound(err) {
		return // Already deleted
	}
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	ExpectWithOffset(1, client.IgnoreNotFound(k8sClient.Delete(ctx, np))).NotTo(HaveOccurred())
}
