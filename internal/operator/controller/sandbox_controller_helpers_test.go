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

	sandboxv1alpha1 "github.com/isola-run/isola/api/v1alpha1"
)

// Helper functions for sandbox controller tests

//nolint:unparam // return value not used today but is the natural API for a create helper
func createSandbox(ctx context.Context, name string, opts ...func(*sandboxv1alpha1.Sandbox)) *sandboxv1alpha1.Sandbox {
	sandbox := &sandboxv1alpha1.Sandbox{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: sandboxv1alpha1.SandboxSpec{
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
		opt(sandbox)
	}
	ExpectWithOffset(1, k8sClient.Create(ctx, sandbox)).To(Succeed())
	return sandbox
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
	ExpectWithOffset(1, k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: testNamespace}, sandbox)).To(Succeed())
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
	if err != nil {
		return nil
	}
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

func setRootfsSnapshotReady(ctx context.Context, name string, ready bool, reason, message string) {
	snap := getRootfsSnapshot(ctx, name)
	if snap == nil {
		return
	}
	status := metav1.ConditionTrue
	if !ready {
		status = metav1.ConditionFalse
	}
	meta.SetStatusCondition(&snap.Status.Conditions, metav1.Condition{
		Type:               sandboxv1alpha1.RootfsSnapshotSucceededCondition,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: snap.Generation,
	})
	now := metav1.Now()
	snap.Status.CompletionTime = &now
	ExpectWithOffset(1, k8sClient.Status().Update(ctx, snap)).To(Succeed())
}

func setShutdownSnapshotReady(ctx context.Context, sandboxName string, ready bool, reason, message string) {
	setRootfsSnapshotReady(ctx, sandboxName+"-shutdown", ready, reason, message)
}

func createSandboxWithNetwork(ctx context.Context, name string, network *sandboxv1alpha1.NetworkSpec) {
	createSandbox(ctx, name, func(s *sandboxv1alpha1.Sandbox) {
		s.Spec.Network = network
	})
}

func hasConditionWithReason(sandbox *sandboxv1alpha1.Sandbox, condType string, status metav1.ConditionStatus, reason string) bool {
	cond := meta.FindStatusCondition(sandbox.Status.Conditions, condType)
	return cond != nil && cond.Status == status && cond.Reason == reason
}

// bindPodToNode assigns a node to an existing pod via the binding subresource,
// mirroring what the real Kubernetes scheduler does. This works in envtest
// (which has no scheduler) and preserves the original pod object.
func bindPodToNode(ctx context.Context, podName string) *corev1.Pod {
	const nodeName = "test-node"

	pod := getPod(ctx, podName)
	ExpectWithOffset(1, pod).NotTo(BeNil())

	binding := &corev1.Binding{
		ObjectMeta: metav1.ObjectMeta{Name: podName, Namespace: testNamespace},
		Target:     corev1.ObjectReference{Name: nodeName},
	}
	ExpectWithOffset(1, k8sClient.SubResource("binding").Create(ctx, pod, binding)).To(Succeed())

	// Re-fetch to get the updated spec with NodeName set
	pod = getPod(ctx, podName)
	ExpectWithOffset(1, pod).NotTo(BeNil())
	return pod
}

// makePodReady updates pod status to make it appear ready
func makePodReady(ctx context.Context, pod *corev1.Pod, containerID string, clock Clock) {
	pod.Status.Phase = corev1.PodRunning
	pod.Status.StartTime = &metav1.Time{Time: clock.Now()}
	pod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue, LastTransitionTime: metav1.Time{Time: clock.Now()}}}
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
