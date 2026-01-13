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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	nodev1 "k8s.io/api/node/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	sandboxv1alpha1 "github.com/omereli/dev-isola/services/isola-operator/api/v1alpha1"
)

var _ = Describe("RootfsSnapshot Controller", func() {
	const (
		testTimeout  = time.Second * 10
		testInterval = time.Millisecond * 250
	)

	var (
		reconciler *RootfsSnapshotReconciler
		fakeClock  *FakeClock
		recorder   *record.FakeRecorder
	)

	// Helper functions
	createRuntimeClassHelper := func(ctx context.Context, name, handler string) {
		rc := &nodev1.RuntimeClass{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Handler:    handler,
		}
		err := k8sClient.Create(ctx, rc)
		if !errors.IsAlreadyExists(err) {
			Expect(err).NotTo(HaveOccurred())
		}
	}

	deleteRuntimeClassHelper := func(ctx context.Context, name string) {
		rc := &nodev1.RuntimeClass{}
		err := k8sClient.Get(ctx, types.NamespacedName{Name: name}, rc)
		if err == nil {
			_ = k8sClient.Delete(ctx, rc)
		}
	}

	createPod := func(ctx context.Context, name, namespace, runtimeClassName, nodeName string, containers []corev1.Container, containerStatuses []corev1.ContainerStatus) *corev1.Pod {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
			Spec: corev1.PodSpec{
				RuntimeClassName: &runtimeClassName,
				NodeName:         nodeName,
				Containers:       containers,
			},
		}
		Expect(k8sClient.Create(ctx, pod)).To(Succeed())
		pod.Status.Phase = corev1.PodRunning
		pod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
		pod.Status.ContainerStatuses = containerStatuses
		Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())
		return pod
	}

	deletePodHelper := func(ctx context.Context, name, namespace string) {
		pod := &corev1.Pod{}
		err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, pod)
		if err == nil {
			_ = k8sClient.Delete(ctx, pod)
		}
	}

	createRootfsSnapshot := func(ctx context.Context, name, namespace, sandboxName string, containerNames []string) *sandboxv1alpha1.RootfsSnapshot {
		snap := &sandboxv1alpha1.RootfsSnapshot{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
			Spec: sandboxv1alpha1.RootfsSnapshotSpec{
				SandboxName:    sandboxName,
				ContainerNames: containerNames,
			},
		}
		Expect(k8sClient.Create(ctx, snap)).To(Succeed())
		return snap
	}

	deleteRootfsSnapshotHelper := func(ctx context.Context, name, namespace string) {
		snap := &sandboxv1alpha1.RootfsSnapshot{}
		err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, snap)
		if err == nil {
			_ = k8sClient.Delete(ctx, snap)
		}
	}

	getRootfsSnapshotHelper := func(ctx context.Context, name, namespace string) *sandboxv1alpha1.RootfsSnapshot {
		snap := &sandboxv1alpha1.RootfsSnapshot{}
		err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, snap)
		if err != nil {
			return nil
		}
		return snap
	}

	getJobHelper := func(ctx context.Context, name, namespace string) *batchv1.Job {
		job := &batchv1.Job{}
		err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, job)
		if err != nil {
			return nil
		}
		return job
	}

	deleteJobHelper := func(ctx context.Context, name, namespace string) {
		job := &batchv1.Job{}
		err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, job)
		if err == nil {
			propagationPolicy := metav1.DeletePropagationBackground
			_ = k8sClient.Delete(ctx, job, &client.DeleteOptions{PropagationPolicy: &propagationPolicy})
		}
	}

	setJobComplete := func(ctx context.Context, name, namespace string) {
		job := getJobHelper(ctx, name, namespace)
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

	setJobFailed := func(ctx context.Context, name, namespace, message string) {
		job := getJobHelper(ctx, name, namespace)
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

	BeforeEach(func() {
		fakeClock = NewFakeClock(time.Now())
		recorder = record.NewFakeRecorder(10)
		reconciler = &RootfsSnapshotReconciler{
			Client:   k8sClient,
			Scheme:   k8sClient.Scheme(),
			Recorder: recorder,
			Clock:    fakeClock,
		}
	})

	Context("Basic Operations", func() {
		It("should do nothing for non-existent RootfsSnapshot", func() {
			result, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "nonexistent", Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(ctrl.Result{}))
		})

		It("should create job on first reconcile", func() {
			snapName := "snap-first"
			sandboxName := "sandbox-first"
			podName := sandboxName + "-pod"
			runtimeClassName := "gvisor-first"

			createRuntimeClassHelper(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassHelper(ctx, runtimeClassName)

			createPod(ctx, podName, testNamespace, runtimeClassName, "test-node",
				[]corev1.Container{{Name: "main", Image: "busybox"}},
				[]corev1.ContainerStatus{{Name: "main", ContainerID: "containerd://abc123", Ready: true}},
			)
			defer deletePodHelper(ctx, podName, testNamespace)

			createRootfsSnapshot(ctx, snapName, testNamespace, sandboxName, nil)
			defer deleteRootfsSnapshotHelper(ctx, snapName, testNamespace)
			defer deleteJobHelper(ctx, snapName+"-main", testNamespace)

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify job was created
			job := getJobHelper(ctx, snapName+"-main", testNamespace)
			Expect(job).NotTo(BeNil())
		})
	})

	Context("Runtime Validation", func() {
		It("should fail when pod does not exist", func() {
			snapName := "snap-no-pod"
			sandboxName := "sandbox-no-pod"

			createRootfsSnapshot(ctx, snapName, testNamespace, sandboxName, nil)
			defer deleteRootfsSnapshotHelper(ctx, snapName, testNamespace)

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			snap := getRootfsSnapshotHelper(ctx, snapName, testNamespace)
			Expect(snap).NotTo(BeNil())

			readyCond := meta.FindStatusCondition(snap.Status.Conditions, string(sandboxv1alpha1.RootfsSnapshotReady))
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCond.Reason).To(Equal(sandboxv1alpha1.ReasonRootfsSnapshotFailed))
		})

		It("should fail when runtime class is not gvisor", func() {
			snapName := "snap-unsupported"
			sandboxName := "sandbox-unsupported"
			podName := sandboxName + "-pod"
			runtimeClassName := "runc-unsupported"

			createRuntimeClassHelper(ctx, runtimeClassName, "runc")
			defer deleteRuntimeClassHelper(ctx, runtimeClassName)

			createPod(ctx, podName, testNamespace, runtimeClassName, "test-node",
				[]corev1.Container{{Name: "main", Image: "busybox"}},
				[]corev1.ContainerStatus{{Name: "main", ContainerID: "containerd://abc123", Ready: true}},
			)
			defer deletePodHelper(ctx, podName, testNamespace)

			createRootfsSnapshot(ctx, snapName, testNamespace, sandboxName, nil)
			defer deleteRootfsSnapshotHelper(ctx, snapName, testNamespace)

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			snap := getRootfsSnapshotHelper(ctx, snapName, testNamespace)
			Expect(snap).NotTo(BeNil())

			readyCond := meta.FindStatusCondition(snap.Status.Conditions, string(sandboxv1alpha1.RootfsSnapshotReady))
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCond.Reason).To(Equal(sandboxv1alpha1.ReasonRootfsSnapshotFailed))
			Expect(readyCond.Message).To(ContainSubstring("Runtime does not support"))
		})
	})

	Context("Single Container Snapshot", func() {
		It("should create job for single container", func() {
			snapName := "snap-single"
			sandboxName := "sandbox-single"
			podName := sandboxName + "-pod"
			runtimeClassName := "gvisor-single"

			createRuntimeClassHelper(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassHelper(ctx, runtimeClassName)

			createPod(ctx, podName, testNamespace, runtimeClassName, "test-node",
				[]corev1.Container{{Name: "sandbox", Image: "busybox"}},
				[]corev1.ContainerStatus{{Name: "sandbox", ContainerID: "containerd://abc123", Ready: true}},
			)
			defer deletePodHelper(ctx, podName, testNamespace)

			createRootfsSnapshot(ctx, snapName, testNamespace, sandboxName, []string{"sandbox"})
			defer deleteRootfsSnapshotHelper(ctx, snapName, testNamespace)

			jobName := snapName + "-sandbox"
			defer deleteJobHelper(ctx, jobName, testNamespace)

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify job was created
			job := getJobHelper(ctx, jobName, testNamespace)
			Expect(job).NotTo(BeNil())
			Expect(job.Spec.Template.Spec.Containers[0].Name).To(Equal("snapshotter"))

			// Verify container snapshot status
			snap := getRootfsSnapshotHelper(ctx, snapName, testNamespace)
			Expect(snap).NotTo(BeNil())
			Expect(snap.Status.ContainerSnapshots).To(HaveLen(1))
			Expect(snap.Status.ContainerSnapshots[0].ContainerName).To(Equal("sandbox"))
			Expect(snap.Status.ContainerSnapshots[0].ContainerID).To(Equal("abc123"))
		})

		It("should mark complete when job succeeds", func() {
			snapName := "snap-success"
			sandboxName := "sandbox-success"
			podName := sandboxName + "-pod"
			runtimeClassName := "gvisor-success"

			createRuntimeClassHelper(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassHelper(ctx, runtimeClassName)

			createPod(ctx, podName, testNamespace, runtimeClassName, "test-node",
				[]corev1.Container{{Name: "main", Image: "busybox"}},
				[]corev1.ContainerStatus{{Name: "main", ContainerID: "containerd://xyz789", Ready: true}},
			)
			defer deletePodHelper(ctx, podName, testNamespace)

			createRootfsSnapshot(ctx, snapName, testNamespace, sandboxName, []string{"main"})
			defer deleteRootfsSnapshotHelper(ctx, snapName, testNamespace)

			jobName := snapName + "-main"
			defer deleteJobHelper(ctx, jobName, testNamespace)

			// First reconcile - creates job
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Mark job complete
			setJobComplete(ctx, jobName, testNamespace)

			// Second reconcile - updates status
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			snap := getRootfsSnapshotHelper(ctx, snapName, testNamespace)
			Expect(snap).NotTo(BeNil())

			readyCond := meta.FindStatusCondition(snap.Status.Conditions, string(sandboxv1alpha1.RootfsSnapshotReady))
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(readyCond.Reason).To(Equal(sandboxv1alpha1.ReasonRootfsSnapshotSucceeded))

			Expect(snap.Status.CompletedAt).NotTo(BeNil())
		})

		It("should mark failed when job fails", func() {
			snapName := "snap-fail"
			sandboxName := "sandbox-fail"
			podName := sandboxName + "-pod"
			runtimeClassName := "gvisor-fail"

			createRuntimeClassHelper(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassHelper(ctx, runtimeClassName)

			createPod(ctx, podName, testNamespace, runtimeClassName, "test-node",
				[]corev1.Container{{Name: "main", Image: "busybox"}},
				[]corev1.ContainerStatus{{Name: "main", ContainerID: "containerd://fail123", Ready: true}},
			)
			defer deletePodHelper(ctx, podName, testNamespace)

			createRootfsSnapshot(ctx, snapName, testNamespace, sandboxName, []string{"main"})
			defer deleteRootfsSnapshotHelper(ctx, snapName, testNamespace)

			jobName := snapName + "-main"
			defer deleteJobHelper(ctx, jobName, testNamespace)

			// First reconcile - creates job
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Mark job failed
			setJobFailed(ctx, jobName, testNamespace, "Container not found")

			// Second reconcile - updates status
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			snap := getRootfsSnapshotHelper(ctx, snapName, testNamespace)
			Expect(snap).NotTo(BeNil())

			readyCond := meta.FindStatusCondition(snap.Status.Conditions, string(sandboxv1alpha1.RootfsSnapshotReady))
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCond.Reason).To(Equal(sandboxv1alpha1.ReasonRootfsSnapshotFailed))

			Expect(snap.Status.CompletedAt).NotTo(BeNil())
		})
	})

	Context("Container Selection", func() {
		It("should snapshot first container when containerNames is empty", func() {
			snapName := "snap-auto"
			sandboxName := "sandbox-auto"
			podName := sandboxName + "-pod"
			runtimeClassName := "gvisor-auto"

			createRuntimeClassHelper(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassHelper(ctx, runtimeClassName)

			createPod(ctx, podName, testNamespace, runtimeClassName, "test-node",
				[]corev1.Container{
					{Name: "app", Image: "busybox"},
					{Name: "sidecar", Image: "busybox"},
				},
				[]corev1.ContainerStatus{
					{Name: "app", ContainerID: "containerd://app123", Ready: true},
					{Name: "sidecar", ContainerID: "containerd://sidecar456", Ready: true},
				},
			)
			defer deletePodHelper(ctx, podName, testNamespace)

			createRootfsSnapshot(ctx, snapName, testNamespace, sandboxName, nil) // nil = use first container
			defer deleteRootfsSnapshotHelper(ctx, snapName, testNamespace)
			defer deleteJobHelper(ctx, snapName+"-app", testNamespace)

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			snap := getRootfsSnapshotHelper(ctx, snapName, testNamespace)
			Expect(snap).NotTo(BeNil())
			Expect(snap.Status.ContainerSnapshots).To(HaveLen(1))
			Expect(snap.Status.ContainerSnapshots[0].ContainerName).To(Equal("app"))

			// Verify job created for first container
			appJob := getJobHelper(ctx, snapName+"-app", testNamespace)
			Expect(appJob).NotTo(BeNil())
		})

		It("should use first specified container when containerNames is provided", func() {
			snapName := "snap-specified"
			sandboxName := "sandbox-specified"
			podName := sandboxName + "-pod"
			runtimeClassName := "gvisor-specified"

			createRuntimeClassHelper(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassHelper(ctx, runtimeClassName)

			createPod(ctx, podName, testNamespace, runtimeClassName, "test-node",
				[]corev1.Container{
					{Name: "a", Image: "busybox"},
					{Name: "b", Image: "busybox"},
				},
				[]corev1.ContainerStatus{
					{Name: "a", ContainerID: "containerd://a111", Ready: true},
					{Name: "b", ContainerID: "containerd://b222", Ready: true},
				},
			)
			defer deletePodHelper(ctx, podName, testNamespace)

			// Request specific containers - controller uses first one
			createRootfsSnapshot(ctx, snapName, testNamespace, sandboxName, []string{"b", "a"})
			defer deleteRootfsSnapshotHelper(ctx, snapName, testNamespace)
			defer deleteJobHelper(ctx, snapName+"-b", testNamespace)

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			snap := getRootfsSnapshotHelper(ctx, snapName, testNamespace)
			Expect(snap).NotTo(BeNil())
			Expect(snap.Status.ContainerSnapshots).To(HaveLen(1))
			Expect(snap.Status.ContainerSnapshots[0].ContainerName).To(Equal("b"))
		})
	})

	Context("TTL Cleanup", func() {
		It("should delete RootfsSnapshot after TTL expires", func() {
			snapName := "snap-ttl"
			sandboxName := "sandbox-ttl"
			podName := sandboxName + "-pod"
			runtimeClassName := "gvisor-ttl"

			createRuntimeClassHelper(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassHelper(ctx, runtimeClassName)

			createPod(ctx, podName, testNamespace, runtimeClassName, "test-node",
				[]corev1.Container{{Name: "main", Image: "busybox"}},
				[]corev1.ContainerStatus{{Name: "main", ContainerID: "containerd://ttl123", Ready: true}},
			)
			defer deletePodHelper(ctx, podName, testNamespace)

			snap := &sandboxv1alpha1.RootfsSnapshot{
				ObjectMeta: metav1.ObjectMeta{Name: snapName, Namespace: testNamespace},
				Spec: sandboxv1alpha1.RootfsSnapshotSpec{
					SandboxName:             sandboxName,
					ContainerNames:          []string{"main"},
					TTLSecondsAfterFinished: func() *int32 { v := int32(10); return &v }(),
				},
			}
			Expect(k8sClient.Create(ctx, snap)).To(Succeed())
			defer deleteRootfsSnapshotHelper(ctx, snapName, testNamespace)

			jobName := snapName + "-main"
			defer deleteJobHelper(ctx, jobName, testNamespace)

			// First reconcile - creates job
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Mark job complete
			setJobComplete(ctx, jobName, testNamespace)

			// Second reconcile - marks complete
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify completion
			snap = getRootfsSnapshotHelper(ctx, snapName, testNamespace)
			Expect(snap).NotTo(BeNil())
			Expect(snap.Status.CompletedAt).NotTo(BeNil())

			// Advance clock past TTL
			fakeClock.Advance(11 * time.Second)

			// Third reconcile - should delete
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify deleted
			Eventually(func() bool {
				snap := getRootfsSnapshotHelper(ctx, snapName, testNamespace)
				return snap == nil || !snap.DeletionTimestamp.IsZero()
			}, testTimeout, testInterval).Should(BeTrue())
		})

		It("should not delete before TTL expires", func() {
			snapName := "snap-ttl-wait"
			sandboxName := "sandbox-ttl-wait"
			podName := sandboxName + "-pod"
			runtimeClassName := "gvisor-ttl-wait"

			createRuntimeClassHelper(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassHelper(ctx, runtimeClassName)

			createPod(ctx, podName, testNamespace, runtimeClassName, "test-node",
				[]corev1.Container{{Name: "main", Image: "busybox"}},
				[]corev1.ContainerStatus{{Name: "main", ContainerID: "containerd://wait123", Ready: true}},
			)
			defer deletePodHelper(ctx, podName, testNamespace)

			snap := &sandboxv1alpha1.RootfsSnapshot{
				ObjectMeta: metav1.ObjectMeta{Name: snapName, Namespace: testNamespace},
				Spec: sandboxv1alpha1.RootfsSnapshotSpec{
					SandboxName:             sandboxName,
					ContainerNames:          []string{"main"},
					TTLSecondsAfterFinished: func() *int32 { v := int32(60); return &v }(),
				},
			}
			Expect(k8sClient.Create(ctx, snap)).To(Succeed())
			defer deleteRootfsSnapshotHelper(ctx, snapName, testNamespace)

			jobName := snapName + "-main"
			defer deleteJobHelper(ctx, jobName, testNamespace)

			// Reconcile to create and complete job
			_, _ = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			setJobComplete(ctx, jobName, testNamespace)
			_, _ = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})

			// Advance clock but not past TTL
			fakeClock.Advance(30 * time.Second)

			// Reconcile should requeue, not delete
			result, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeNumerically(">", 0))

			// Should still exist
			snap = getRootfsSnapshotHelper(ctx, snapName, testNamespace)
			Expect(snap).NotTo(BeNil())
			Expect(snap.DeletionTimestamp.IsZero()).To(BeTrue())
		})
	})

	Context("Finalizer Cleanup", func() {
		It("should delete jobs when RootfsSnapshot is deleted", func() {
			snapName := "snap-cleanup"
			sandboxName := "sandbox-cleanup"
			podName := sandboxName + "-pod"
			runtimeClassName := "gvisor-cleanup"

			createRuntimeClassHelper(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassHelper(ctx, runtimeClassName)

			createPod(ctx, podName, testNamespace, runtimeClassName, "test-node",
				[]corev1.Container{{Name: "main", Image: "busybox"}},
				[]corev1.ContainerStatus{{Name: "main", ContainerID: "containerd://cleanup123", Ready: true}},
			)
			defer deletePodHelper(ctx, podName, testNamespace)

			createRootfsSnapshot(ctx, snapName, testNamespace, sandboxName, []string{"main"})

			jobName := snapName + "-main"
			defer deleteJobHelper(ctx, jobName, testNamespace)

			// Reconcile to create job
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify job exists
			job := getJobHelper(ctx, jobName, testNamespace)
			Expect(job).NotTo(BeNil())

			// Delete RootfsSnapshot
			snap := getRootfsSnapshotHelper(ctx, snapName, testNamespace)
			Expect(k8sClient.Delete(ctx, snap)).To(Succeed())

			// Reconcile during deletion
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify RootfsSnapshot is gone
			Eventually(func() bool {
				snap := getRootfsSnapshotHelper(ctx, snapName, testNamespace)
				return snap == nil
			}, testTimeout, testInterval).Should(BeTrue())
		})
	})

	Context("ActiveDeadlineSeconds", func() {
		It("should use spec activeDeadlineSeconds for jobs", func() {
			snapName := "snap-deadline"
			sandboxName := "sandbox-deadline"
			podName := sandboxName + "-pod"
			runtimeClassName := "gvisor-deadline"

			createRuntimeClassHelper(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassHelper(ctx, runtimeClassName)

			createPod(ctx, podName, testNamespace, runtimeClassName, "test-node",
				[]corev1.Container{{Name: "main", Image: "busybox"}},
				[]corev1.ContainerStatus{{Name: "main", ContainerID: "containerd://deadline123", Ready: true}},
			)
			defer deletePodHelper(ctx, podName, testNamespace)

			customDeadline := int64(120)
			snap := &sandboxv1alpha1.RootfsSnapshot{
				ObjectMeta: metav1.ObjectMeta{Name: snapName, Namespace: testNamespace},
				Spec: sandboxv1alpha1.RootfsSnapshotSpec{
					SandboxName:           sandboxName,
					ContainerNames:        []string{"main"},
					ActiveDeadlineSeconds: &customDeadline,
				},
			}
			Expect(k8sClient.Create(ctx, snap)).To(Succeed())
			defer deleteRootfsSnapshotHelper(ctx, snapName, testNamespace)

			jobName := snapName + "-main"
			defer deleteJobHelper(ctx, jobName, testNamespace)

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			job := getJobHelper(ctx, jobName, testNamespace)
			Expect(job).NotTo(BeNil())
			Expect(job.Spec.ActiveDeadlineSeconds).NotTo(BeNil())
			Expect(*job.Spec.ActiveDeadlineSeconds).To(Equal(customDeadline))
		})

		It("should use default activeDeadlineSeconds when not specified", func() {
			snapName := "rfs-snap-default-deadline"
			sandboxName := "rfs-sandbox-default-deadline"
			podName := sandboxName + "-pod"
			runtimeClassName := "rfs-gvisor-default-deadline"

			createRuntimeClassHelper(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassHelper(ctx, runtimeClassName)

			createPod(ctx, podName, testNamespace, runtimeClassName, "test-node",
				[]corev1.Container{{Name: "main", Image: "busybox"}},
				[]corev1.ContainerStatus{{Name: "main", ContainerID: "containerd://default123", Ready: true}},
			)
			defer deletePodHelper(ctx, podName, testNamespace)

			createRootfsSnapshot(ctx, snapName, testNamespace, sandboxName, []string{"main"})
			defer deleteRootfsSnapshotHelper(ctx, snapName, testNamespace)

			jobName := snapName + "-main"
			defer deleteJobHelper(ctx, jobName, testNamespace)

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			job := getJobHelper(ctx, jobName, testNamespace)
			Expect(job).NotTo(BeNil())
			Expect(job.Spec.ActiveDeadlineSeconds).NotTo(BeNil())
			Expect(*job.Spec.ActiveDeadlineSeconds).To(Equal(defaultActiveDeadlineSecondsSnapshot))
		})
	})
})
