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
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	sandboxv1alpha1 "github.com/isola-ai/isola/api/v1alpha1"

	snapshotpkg "github.com/isola-ai/isola/internal/snapshot"
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

	Context("Revision Management", func() {
		It("should increment revision for each snapshot of the same sandbox", func() {
			sandboxName := "sandbox-revision"
			podName := sandboxName + "-pod"
			runtimeClassName := "gvisor-revision"

			createRuntimeClassForSnapshot(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassForSnapshot(ctx, runtimeClassName)

			createSnapshotPod(ctx, podName, runtimeClassName,
				[]corev1.Container{{Name: "main", Image: "busybox"}},
				[]corev1.ContainerStatus{{Name: "main", ContainerID: "containerd://rev123", Ready: true}},
			)
			defer deleteSnapshotPod(ctx, podName)

			// First snapshot
			snap1Name := "snap-rev-1"
			createRootfsSnapshotCR(ctx, snap1Name, sandboxName)
			defer deleteRootfsSnapshotCR(ctx, snap1Name)

			job1Name := snap1Name + "-job"
			defer deleteSnapshotJob(ctx, job1Name)
			defer deleteSnapshotJobPod(ctx, job1Name)

			// First reconcile creates the job
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snap1Name, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Create job pod with termination message and complete the job
			createSnapshotJobPodWithTerminationMessage(ctx, job1Name, &snapshotpkg.UploadResult{
				SnapshotKey:  "snapshots/" + testNamespace + "/" + sandboxName + "/rev-00001/main.tar",
				Revision:     1,
				BytesWritten: 1024,
			})
			setSnapshotJobComplete(ctx, job1Name)

			// Second reconcile processes job completion
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snap1Name, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			snap1 := getRootfsSnapshotCR(ctx, snap1Name)
			Expect(snap1).NotTo(BeNil())
			Expect(snap1.Status.Revision).To(Equal(int32(1)))
			Expect(snap1.Status.SnapshotKey).To(ContainSubstring("rev-00001"))

			// Second snapshot of same sandbox
			snap2Name := "snap-rev-2"
			createRootfsSnapshotCR(ctx, snap2Name, sandboxName)
			defer deleteRootfsSnapshotCR(ctx, snap2Name)

			job2Name := snap2Name + "-job"
			defer deleteSnapshotJob(ctx, job2Name)
			defer deleteSnapshotJobPod(ctx, job2Name)

			// First reconcile creates the job
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snap2Name, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Create job pod with termination message and complete the job
			createSnapshotJobPodWithTerminationMessage(ctx, job2Name, &snapshotpkg.UploadResult{
				SnapshotKey:  "snapshots/" + testNamespace + "/" + sandboxName + "/rev-00002/main.tar",
				Revision:     2,
				BytesWritten: 2048,
			})
			setSnapshotJobComplete(ctx, job2Name)

			// Second reconcile processes job completion
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snap2Name, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			snap2 := getRootfsSnapshotCR(ctx, snap2Name)
			Expect(snap2).NotTo(BeNil())
			Expect(snap2.Status.Revision).To(Equal(int32(2)))
			Expect(snap2.Status.SnapshotKey).To(ContainSubstring("rev-00002"))
		})
	})

	Context("Container Selection", func() {
		It("should use the first container from the pod", func() {
			snapName := "snap-specified"
			sandboxName := "sandbox-specified"
			podName := sandboxName + "-pod"
			runtimeClassName := "gvisor-specified"

			createRuntimeClassForSnapshot(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassForSnapshot(ctx, runtimeClassName)

			createSnapshotPod(ctx, podName, runtimeClassName,
				[]corev1.Container{
					{Name: "app", Image: "busybox"},
					{Name: "sidecar", Image: "busybox"},
				},
				[]corev1.ContainerStatus{
					{Name: "app", ContainerID: "containerd://app123", Ready: true},
					{Name: "sidecar", ContainerID: "containerd://sidecar456", Ready: true},
				},
			)
			defer deleteSnapshotPod(ctx, podName)

			createRootfsSnapshotCR(ctx, snapName, sandboxName)
			defer deleteRootfsSnapshotCR(ctx, snapName)
			defer deleteSnapshotJob(ctx, snapName+"-job")

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			snap := getRootfsSnapshotCR(ctx, snapName)
			Expect(snap).NotTo(BeNil())
			Expect(snap.Status.ContainerID).To(Equal("app123"))
		})

		It("should use the explicitly specified container", func() {
			snapName := "snap-explicit"
			sandboxName := "sandbox-explicit"
			podName := sandboxName + "-pod"
			runtimeClassName := "gvisor-explicit"

			createRuntimeClassForSnapshot(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassForSnapshot(ctx, runtimeClassName)

			createSnapshotPod(ctx, podName, runtimeClassName,
				[]corev1.Container{
					{Name: "app", Image: "busybox"},
					{Name: "sidecar", Image: "busybox"},
				},
				[]corev1.ContainerStatus{
					{Name: "app", ContainerID: "containerd://app123", Ready: true},
					{Name: "sidecar", ContainerID: "containerd://sidecar456", Ready: true},
				},
			)
			defer deleteSnapshotPod(ctx, podName)

			createRootfsSnapshotCRWithContainer(ctx, snapName, sandboxName, "sidecar")
			defer deleteRootfsSnapshotCR(ctx, snapName)
			defer deleteSnapshotJob(ctx, snapName+"-job")

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			snap := getRootfsSnapshotCR(ctx, snapName)
			Expect(snap).NotTo(BeNil())
			Expect(snap.Status.ContainerID).To(Equal("sidecar456"))
		})

		It("should fail when specified container does not exist", func() {
			snapName := "snap-badcontainer"
			sandboxName := "sandbox-badcontainer"
			podName := sandboxName + "-pod"
			runtimeClassName := "gvisor-badcontainer"

			createRuntimeClassForSnapshot(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassForSnapshot(ctx, runtimeClassName)

			createSnapshotPod(ctx, podName, runtimeClassName,
				[]corev1.Container{
					{Name: "app", Image: "busybox"},
				},
				[]corev1.ContainerStatus{
					{Name: "app", ContainerID: "containerd://app123", Ready: true},
				},
			)
			defer deleteSnapshotPod(ctx, podName)

			createRootfsSnapshotCRWithContainer(ctx, snapName, sandboxName, "nonexistent")
			defer deleteRootfsSnapshotCR(ctx, snapName)

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			snap := getRootfsSnapshotCR(ctx, snapName)
			Expect(snap).NotTo(BeNil())
			failedCond := meta.FindStatusCondition(snap.Status.Conditions, string(sandboxv1alpha1.RootfsSnapshotFailed))
			Expect(failedCond).NotTo(BeNil())
			Expect(failedCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(failedCond.Message).To(ContainSubstring(`Container "nonexistent" not found`))
		})
	})
})
