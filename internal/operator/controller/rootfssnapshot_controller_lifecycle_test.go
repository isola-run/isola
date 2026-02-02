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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	sandboxv1alpha1 "github.com/isola-ai/isola-sb/api/v1alpha1"
)

var _ = Describe("RootfsSnapshot Controller", func() {
	var (
		reconciler *RootfsSnapshotReconciler
		fakeClock  *FakeClock
		recorder   *record.FakeRecorder
	)

	BeforeEach(func() {
		fakeClock = NewFakeClock(time.Now())
		recorder = record.NewFakeRecorder(10)
		reconciler = &RootfsSnapshotReconciler{
			Client:                 k8sClient,
			Scheme:                 k8sClient.Scheme(),
			Recorder:               recorder,
			Clock:                  fakeClock,
			BucketURL:              "s3://test-bucket?region=us-east-1",
			UploaderImage:          "isola-uploader:test",
			SnapshotServiceAccount: "test-snapshot-sa",
		}
	})

	Context("TTL Cleanup", func() {
		It("should delete RootfsSnapshot after TTL expires", func() {
			snapName := "snap-ttl"
			sandboxName := "sandbox-ttl"
			podName := sandboxName + "-pod"
			runtimeClassName := "gvisor-ttl"

			createRuntimeClassForSnapshot(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassForSnapshot(ctx, runtimeClassName)

			createSnapshotPod(ctx, podName, runtimeClassName,
				[]corev1.Container{{Name: "main", Image: "busybox"}},
				[]corev1.ContainerStatus{{Name: "main", ContainerID: "containerd://ttl123", Ready: true}},
			)
			defer deleteSnapshotPod(ctx, podName)

			snap := &sandboxv1alpha1.RootfsSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      snapName,
					Namespace: testNamespace,
					Labels:    map[string]string{LabelSandboxName: sandboxName},
				},
				Spec: sandboxv1alpha1.RootfsSnapshotSpec{
					SandboxName:             sandboxName,
					ContainerNames:          []string{"main"},
					TTLSecondsAfterFinished: func() *int32 { v := int32(10); return &v }(),
				},
			}
			Expect(k8sClient.Create(ctx, snap)).To(Succeed())
			defer deleteRootfsSnapshotCR(ctx, snapName)

			jobName := snapName + "-main"
			defer deleteSnapshotJob(ctx, jobName)

			// First reconcile - creates job
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Mark job complete
			setSnapshotJobComplete(ctx, jobName)

			// Second reconcile - marks complete
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify completion
			snap = getRootfsSnapshotCR(ctx, snapName)
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
				snap := getRootfsSnapshotCR(ctx, snapName)
				return snap == nil || !snap.DeletionTimestamp.IsZero()
			}, testTimeout, testInterval).Should(BeTrue())
		})

		It("should not delete before TTL expires", func() {
			snapName := "snap-ttl-wait"
			sandboxName := "sandbox-ttl-wait"
			podName := sandboxName + "-pod"
			runtimeClassName := "gvisor-ttl-wait"

			createRuntimeClassForSnapshot(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassForSnapshot(ctx, runtimeClassName)

			createSnapshotPod(ctx, podName, runtimeClassName,
				[]corev1.Container{{Name: "main", Image: "busybox"}},
				[]corev1.ContainerStatus{{Name: "main", ContainerID: "containerd://wait123", Ready: true}},
			)
			defer deleteSnapshotPod(ctx, podName)

			snap := &sandboxv1alpha1.RootfsSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      snapName,
					Namespace: testNamespace,
					Labels:    map[string]string{LabelSandboxName: sandboxName},
				},
				Spec: sandboxv1alpha1.RootfsSnapshotSpec{
					SandboxName:             sandboxName,
					ContainerNames:          []string{"main"},
					TTLSecondsAfterFinished: func() *int32 { v := int32(60); return &v }(),
				},
			}
			Expect(k8sClient.Create(ctx, snap)).To(Succeed())
			defer deleteRootfsSnapshotCR(ctx, snapName)

			jobName := snapName + "-main"
			defer deleteSnapshotJob(ctx, jobName)

			// Reconcile to create and complete job
			_, _ = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			setSnapshotJobComplete(ctx, jobName)
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
			snap = getRootfsSnapshotCR(ctx, snapName)
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

			createRuntimeClassForSnapshot(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassForSnapshot(ctx, runtimeClassName)

			createSnapshotPod(ctx, podName, runtimeClassName,
				[]corev1.Container{{Name: "main", Image: "busybox"}},
				[]corev1.ContainerStatus{{Name: "main", ContainerID: "containerd://cleanup123", Ready: true}},
			)
			defer deleteSnapshotPod(ctx, podName)

			createRootfsSnapshotCR(ctx, snapName, sandboxName, []string{"main"})

			jobName := snapName + "-main"
			defer deleteSnapshotJob(ctx, jobName)

			// Reconcile to create job
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify job exists
			job := getSnapshotJob(ctx, jobName)
			Expect(job).NotTo(BeNil())

			// Verify owner reference is set correctly - this ensures K8s GC will clean up the job
			// when the RootfsSnapshot is deleted (defer cleanup only needed because EnvTest lacks GC)
			Expect(job.OwnerReferences).To(HaveLen(1))
			Expect(job.OwnerReferences[0].Kind).To(Equal("RootfsSnapshot"))
			Expect(job.OwnerReferences[0].Name).To(Equal(snapName))
			Expect(job.OwnerReferences[0].Controller).NotTo(BeNil())
			Expect(*job.OwnerReferences[0].Controller).To(BeTrue())

			// Delete RootfsSnapshot
			snap := getRootfsSnapshotCR(ctx, snapName)
			Expect(k8sClient.Delete(ctx, snap)).To(Succeed())

			// Reconcile during deletion
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify RootfsSnapshot is gone
			Eventually(func() bool {
				snap := getRootfsSnapshotCR(ctx, snapName)
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

			createRuntimeClassForSnapshot(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassForSnapshot(ctx, runtimeClassName)

			createSnapshotPod(ctx, podName, runtimeClassName,
				[]corev1.Container{{Name: "main", Image: "busybox"}},
				[]corev1.ContainerStatus{{Name: "main", ContainerID: "containerd://deadline123", Ready: true}},
			)
			defer deleteSnapshotPod(ctx, podName)

			customDeadline := int64(120)
			snap := &sandboxv1alpha1.RootfsSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      snapName,
					Namespace: testNamespace,
					Labels:    map[string]string{LabelSandboxName: sandboxName},
				},
				Spec: sandboxv1alpha1.RootfsSnapshotSpec{
					SandboxName:           sandboxName,
					ContainerNames:        []string{"main"},
					ActiveDeadlineSeconds: &customDeadline,
				},
			}
			Expect(k8sClient.Create(ctx, snap)).To(Succeed())
			defer deleteRootfsSnapshotCR(ctx, snapName)

			jobName := snapName + "-main"
			defer deleteSnapshotJob(ctx, jobName)

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			job := getSnapshotJob(ctx, jobName)
			Expect(job).NotTo(BeNil())
			Expect(job.Spec.ActiveDeadlineSeconds).NotTo(BeNil())
			Expect(*job.Spec.ActiveDeadlineSeconds).To(Equal(customDeadline))
		})

		It("should use default activeDeadlineSeconds when not specified", func() {
			snapName := "rfs-snap-default-deadline"
			sandboxName := "rfs-sandbox-default-deadline"
			podName := sandboxName + "-pod"
			runtimeClassName := "rfs-gvisor-default-deadline"

			createRuntimeClassForSnapshot(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassForSnapshot(ctx, runtimeClassName)

			createSnapshotPod(ctx, podName, runtimeClassName,
				[]corev1.Container{{Name: "main", Image: "busybox"}},
				[]corev1.ContainerStatus{{Name: "main", ContainerID: "containerd://default123", Ready: true}},
			)
			defer deleteSnapshotPod(ctx, podName)

			createRootfsSnapshotCR(ctx, snapName, sandboxName, []string{"main"})
			defer deleteRootfsSnapshotCR(ctx, snapName)

			jobName := snapName + "-main"
			defer deleteSnapshotJob(ctx, jobName)

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			job := getSnapshotJob(ctx, jobName)
			Expect(job).NotTo(BeNil())
			Expect(job.Spec.ActiveDeadlineSeconds).NotTo(BeNil())
			Expect(*job.Spec.ActiveDeadlineSeconds).To(Equal(defaultActiveDeadlineSecondsSnapshot))
		})

		It("should set DeadlineExceeded when job exceeds activeDeadlineSeconds", func() {
			snapName := "snap-deadline-exceeded"
			sandboxName := "sandbox-deadline-exceeded"
			podName := sandboxName + "-pod"
			runtimeClassName := "gvisor-deadline-exceeded"

			createRuntimeClassForSnapshot(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassForSnapshot(ctx, runtimeClassName)

			createSnapshotPod(ctx, podName, runtimeClassName,
				[]corev1.Container{{Name: "main", Image: "busybox"}},
				[]corev1.ContainerStatus{{Name: "main", ContainerID: "containerd://exceeded123", Ready: true}},
			)
			defer deleteSnapshotPod(ctx, podName)

			snap := &sandboxv1alpha1.RootfsSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      snapName,
					Namespace: testNamespace,
					Labels:    map[string]string{LabelSandboxName: sandboxName},
				},
				Spec: sandboxv1alpha1.RootfsSnapshotSpec{
					SandboxName:    sandboxName,
					ContainerNames: []string{"main"},
				},
			}
			Expect(k8sClient.Create(ctx, snap)).To(Succeed())
			defer deleteRootfsSnapshotCR(ctx, snapName)

			jobName := snapName + "-main"
			defer deleteSnapshotJob(ctx, jobName)

			// First reconcile - creates job and sets InProgress
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify job was created
			job := getSnapshotJob(ctx, jobName)
			Expect(job).NotTo(BeNil())

			// Simulate K8s marking job as failed due to deadline exceeded
			setSnapshotJobDeadlineExceeded(ctx, jobName)

			// Reconcile again - should detect deadline exceeded from job status
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify snapshot has DeadlineExceeded status
			snap = getRootfsSnapshotCR(ctx, snapName)
			Expect(snap).NotTo(BeNil())
			Expect(snap.Status.CompletedAt).NotTo(BeNil())

			cond := meta.FindStatusCondition(snap.Status.Conditions, string(sandboxv1alpha1.RootfsSnapshotComplete))
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Reason).To(Equal(sandboxv1alpha1.ReasonRootfsSnapshotDeadlineExceeded))
		})

		It("should stay InProgress while job is still running", func() {
			snapName := "snap-deadline-ok"
			sandboxName := "sandbox-deadline-ok"
			podName := sandboxName + "-pod"
			runtimeClassName := "gvisor-deadline-ok"

			createRuntimeClassForSnapshot(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassForSnapshot(ctx, runtimeClassName)

			createSnapshotPod(ctx, podName, runtimeClassName,
				[]corev1.Container{{Name: "main", Image: "busybox"}},
				[]corev1.ContainerStatus{{Name: "main", ContainerID: "containerd://ok123", Ready: true}},
			)
			defer deleteSnapshotPod(ctx, podName)

			snap := &sandboxv1alpha1.RootfsSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      snapName,
					Namespace: testNamespace,
					Labels:    map[string]string{LabelSandboxName: sandboxName},
				},
				Spec: sandboxv1alpha1.RootfsSnapshotSpec{
					SandboxName:    sandboxName,
					ContainerNames: []string{"main"},
				},
			}
			Expect(k8sClient.Create(ctx, snap)).To(Succeed())
			defer deleteRootfsSnapshotCR(ctx, snapName)

			jobName := snapName + "-main"
			defer deleteSnapshotJob(ctx, jobName)

			// First reconcile - creates job
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Reconcile again while job is still running (no status change)
			result, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(time.Duration(0)), "Should not requeue - relies on job watch")

			// Verify snapshot is still in progress
			snap = getRootfsSnapshotCR(ctx, snapName)
			Expect(snap).NotTo(BeNil())
			Expect(snap.Status.CompletedAt).To(BeNil(), "Should not be completed yet")

			cond := meta.FindStatusCondition(snap.Status.Conditions, string(sandboxv1alpha1.RootfsSnapshotComplete))
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Reason).To(Equal(sandboxv1alpha1.ReasonRootfsSnapshotInProgress))
		})

		It("should delete the job when deadline exceeded", func() {
			snapName := "snap-deadline-delete-job"
			sandboxName := "sandbox-deadline-delete-job"
			podName := sandboxName + "-pod"
			runtimeClassName := "gvisor-deadline-delete-job"

			createRuntimeClassForSnapshot(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassForSnapshot(ctx, runtimeClassName)

			createSnapshotPod(ctx, podName, runtimeClassName,
				[]corev1.Container{{Name: "main", Image: "busybox"}},
				[]corev1.ContainerStatus{{Name: "main", ContainerID: "containerd://deljob123", Ready: true}},
			)
			defer deleteSnapshotPod(ctx, podName)

			snap := &sandboxv1alpha1.RootfsSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      snapName,
					Namespace: testNamespace,
					Labels:    map[string]string{LabelSandboxName: sandboxName},
				},
				Spec: sandboxv1alpha1.RootfsSnapshotSpec{
					SandboxName:    sandboxName,
					ContainerNames: []string{"main"},
				},
			}
			Expect(k8sClient.Create(ctx, snap)).To(Succeed())
			defer deleteRootfsSnapshotCR(ctx, snapName)

			jobName := snapName + "-main"
			defer deleteSnapshotJob(ctx, jobName)

			// First reconcile - creates job
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify job exists
			job := getSnapshotJob(ctx, jobName)
			Expect(job).NotTo(BeNil())

			// Simulate K8s marking job as failed due to deadline exceeded
			setSnapshotJobDeadlineExceeded(ctx, jobName)

			// Reconcile - should detect deadline exceeded and delete job
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify job was deleted
			Eventually(func() bool {
				job := getSnapshotJob(ctx, jobName)
				return job == nil || !job.DeletionTimestamp.IsZero()
			}, testTimeout, testInterval).Should(BeTrue(), "Job should be deleted after deadline exceeded")
		})
	})
})
