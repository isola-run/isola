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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	sandboxv1alpha1 "github.com/isola-ai/isola/api/v1alpha1"
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
			UploaderImage:          "isola-uploader:test",
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
	})
})
