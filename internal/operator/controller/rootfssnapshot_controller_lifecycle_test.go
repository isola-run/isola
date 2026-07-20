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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	sandboxv1alpha1 "github.com/isola-run/isola/api/v1alpha1"
	snapshotpkg "github.com/isola-run/isola/internal/snapshot"
)

var _ = Describe("RootfsSnapshot Controller", func() {
	var (
		reconciler *RootfsSnapshotReconciler
		fakeClock  *FakeClock
		recorder   *events.FakeRecorder
	)

	BeforeEach(func() {
		fakeClock = NewFakeClock(time.Now())
		recorder = events.NewFakeRecorder(10)
		reconciler = &RootfsSnapshotReconciler{
			Client:                 k8sClient,
			Scheme:                 k8sClient.Scheme(),
			Recorder:               recorder,
			Clock:                  fakeClock,
			BucketURL:              "s3://test-bucket?region=us-east-1",
			UploaderImage:          "isola-snapshot-uploader:test",
			SnapshotServiceAccount: "test-snapshot-sa",
			Enabled:                true,
			GvisorRunscPath:        "/usr/local/bin/runsc",
			GvisorRunscRoot:        "/run/containerd/runsc/k8s.io",
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
				},
				Spec: sandboxv1alpha1.RootfsSnapshotSpec{
					SandboxName:             sandboxName,
					SnapshotName:            snapName,
					TTLSecondsAfterFinished: func() *int32 { v := int32(10); return &v }(),
				},
			}
			Expect(k8sClient.Create(ctx, snap)).To(Succeed())
			defer deleteRootfsSnapshotCR(ctx, snapName)

			jobName := snapName + "-job"
			defer deleteSnapshotJob(ctx, jobName)
			defer deleteSnapshotJobPod(ctx, jobName)

			// First reconcile - creates job
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			createSnapshotJobPodWithTerminationMessage(ctx, jobName, &snapshotpkg.UploadResult{
				SnapshotKey:  "rootfssnapshots/" + testNamespace + "/" + snapName + ".tar",
				BytesWritten: 1024,
			})
			setSnapshotJobComplete(ctx, jobName)

			// Second reconcile - marks complete
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify completion
			snap = getRootfsSnapshotCR(ctx, snapName)
			Expect(snap).NotTo(BeNil())
			Expect(snap.Status.CompletionTime).NotTo(BeNil())

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
				},
				Spec: sandboxv1alpha1.RootfsSnapshotSpec{
					SandboxName:             sandboxName,
					SnapshotName:            snapName,
					TTLSecondsAfterFinished: func() *int32 { v := int32(60); return &v }(),
				},
			}
			Expect(k8sClient.Create(ctx, snap)).To(Succeed())
			defer deleteRootfsSnapshotCR(ctx, snapName)

			jobName := snapName + "-job"
			defer deleteSnapshotJob(ctx, jobName)
			defer deleteSnapshotJobPod(ctx, jobName)

			// Reconcile to create and complete job
			_, _ = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			createSnapshotJobPodWithTerminationMessage(ctx, jobName, &snapshotpkg.UploadResult{
				SnapshotKey:  "rootfssnapshots/" + testNamespace + "/" + snapName + ".tar",
				BytesWritten: 1024,
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

		It("should clean up jobs for completed snapshots before TTL expires", func() {
			snapName := "snap-complete-job-cleanup"
			sandboxName := "sandbox-complete-job-cleanup"

			createRootfsSnapshotCR(ctx, snapName, sandboxName)
			defer deleteRootfsSnapshotCR(ctx, snapName)

			snap := getRootfsSnapshotCR(ctx, snapName)
			Expect(snap).NotTo(BeNil())

			now := metav1.NewTime(fakeClock.Now())
			snap.Status.CompletionTime = &now
			snap.Status.SnapshotKey = "rootfssnapshots/" + testNamespace + "/" + snapName + ".tar"
			meta.SetStatusCondition(&snap.Status.Conditions, metav1.Condition{
				Type:               sandboxv1alpha1.RootfsSnapshotSucceededCondition,
				Status:             metav1.ConditionTrue,
				Reason:             sandboxv1alpha1.ReasonRootfsSnapshotSucceeded,
				Message:            "Snapshot completed successfully",
				ObservedGeneration: snap.Generation,
			})
			Expect(k8sClient.Status().Update(ctx, snap)).To(Succeed())

			jobName := snapName + "-job"
			job := &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      jobName,
					Namespace: testNamespace,
				},
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers:    []corev1.Container{{Name: "snapshot-uploader", Image: "test"}},
							RestartPolicy: corev1.RestartPolicyNever,
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, job)).To(Succeed())
			defer deleteSnapshotJob(ctx, jobName)
			setSnapshotJobFailed(ctx, jobName, "container state already removed")

			result, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeNumerically(">", 0))

			Eventually(func() bool {
				job := getSnapshotJob(ctx, jobName)
				return job == nil || !job.DeletionTimestamp.IsZero()
			}, testTimeout, testInterval).Should(BeTrue())

			snap = getRootfsSnapshotCR(ctx, snapName)
			Expect(snap).NotTo(BeNil())
			readyCond := meta.FindStatusCondition(snap.Status.Conditions, sandboxv1alpha1.RootfsSnapshotSucceededCondition)
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(snap.Status.SnapshotKey).To(Equal("rootfssnapshots/" + testNamespace + "/" + snapName + ".tar"))
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

			createRootfsSnapshotCR(ctx, snapName, sandboxName)

			jobName := snapName + "-job"
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

	Context("TimeoutSeconds", func() {
		It("should use spec timeoutSeconds for jobs", func() {
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
				},
				Spec: sandboxv1alpha1.RootfsSnapshotSpec{
					SandboxName:    sandboxName,
					SnapshotName:   snapName,
					TimeoutSeconds: &customDeadline,
				},
			}
			Expect(k8sClient.Create(ctx, snap)).To(Succeed())
			defer deleteRootfsSnapshotCR(ctx, snapName)

			jobName := snapName + "-job"
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

		It("should use default timeoutSeconds when not specified", func() {
			snapName := "snap-default-deadline"
			sandboxName := "sandbox-default-deadline"
			podName := sandboxName + "-pod"
			runtimeClassName := "gvisor-default-deadline"

			createRuntimeClassForSnapshot(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassForSnapshot(ctx, runtimeClassName)

			createSnapshotPod(ctx, podName, runtimeClassName,
				[]corev1.Container{{Name: "main", Image: "busybox"}},
				[]corev1.ContainerStatus{{Name: "main", ContainerID: "containerd://default123", Ready: true}},
			)
			defer deleteSnapshotPod(ctx, podName)

			createRootfsSnapshotCR(ctx, snapName, sandboxName)
			defer deleteRootfsSnapshotCR(ctx, snapName)

			jobName := snapName + "-job"
			defer deleteSnapshotJob(ctx, jobName)

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			job := getSnapshotJob(ctx, jobName)
			Expect(job).NotTo(BeNil())
			Expect(job.Spec.ActiveDeadlineSeconds).NotTo(BeNil())
			Expect(*job.Spec.ActiveDeadlineSeconds).To(Equal(defaultTimeoutSecondsSnapshot))
		})
	})
})
