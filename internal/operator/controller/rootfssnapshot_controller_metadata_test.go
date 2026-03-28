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
			UploaderImage:          "isola-uploader:test",
			SnapshotServiceAccount: "test-snapshot-sa",
			Enabled:                true,
			GvisorRunscPath:        "/usr/local/bin/runsc",
			GvisorRunscRoot:        "/run/containerd/runsc/k8s.io",
		}
	})

	Context("Snapshot Key Format", func() {
		It("should use namespace-prefixed key path", func() {
			sandboxName := "sandbox-keypath"
			podName := sandboxName + "-pod"
			runtimeClassName := "gvisor-keypath"

			createRuntimeClassForSnapshot(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassForSnapshot(ctx, runtimeClassName)

			createSnapshotPod(ctx, podName, runtimeClassName,
				[]corev1.Container{{Name: "main", Image: "busybox"}},
				[]corev1.ContainerStatus{{Name: "main", ContainerID: "containerd://key123", Ready: true}},
			)
			defer deleteSnapshotPod(ctx, podName)

			snapName := "snap-keypath"
			createRootfsSnapshotCR(ctx, snapName, sandboxName)
			defer deleteRootfsSnapshotCR(ctx, snapName)

			jobName := snapName + "-job"
			defer deleteSnapshotJob(ctx, jobName)
			defer deleteSnapshotJobPod(ctx, jobName)

			// First reconcile creates the job
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Create job pod with termination message and complete the job
			createSnapshotJobPodWithTerminationMessage(ctx, jobName, &snapshotpkg.UploadResult{
				SnapshotKey:  "rootfssnapshots/" + testNamespace + "/" + sandboxName + ".tar",
				BytesWritten: 1024,
			})
			setSnapshotJobComplete(ctx, jobName)

			// Second reconcile processes job completion
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			snap := getRootfsSnapshotCR(ctx, snapName)
			Expect(snap).NotTo(BeNil())
			Expect(snap.Status.SnapshotKey).To(Equal("rootfssnapshots/" + testNamespace + "/" + sandboxName + ".tar"))
		})

		It("should pass custom snapshotName to the uploader job", func() {
			sandboxName := "sandbox-custom-snapname"
			podName := sandboxName + "-pod"
			runtimeClassName := "gvisor-custom-snapname"

			createRuntimeClassForSnapshot(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassForSnapshot(ctx, runtimeClassName)

			createSnapshotPod(ctx, podName, runtimeClassName,
				[]corev1.Container{{Name: "main", Image: "busybox"}},
				[]corev1.ContainerStatus{{Name: "main", ContainerID: "containerd://custom123", Ready: true}},
			)
			defer deleteSnapshotPod(ctx, podName)

			snapName := "snap-custom-snapname"
			customSnapshotName := "my-custom-snapshot"
			snap := &sandboxv1alpha1.RootfsSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      snapName,
					Namespace: testNamespace,
				},
				Spec: sandboxv1alpha1.RootfsSnapshotSpec{
					SandboxName:  sandboxName,
					SnapshotName: customSnapshotName,
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

			// Verify the SNAPSHOT_NAME and SNAPSHOT_NAMESPACE env vars
			uploaderContainer := job.Spec.Template.Spec.Containers[0]
			Expect(uploaderContainer.Name).To(Equal("uploader"))
			var snapshotNameEnv, snapshotNamespaceEnv string
			for _, env := range uploaderContainer.Env {
				if env.Name == "SNAPSHOT_NAME" {
					snapshotNameEnv = env.Value
				}
				if env.Name == "SNAPSHOT_NAMESPACE" {
					snapshotNamespaceEnv = env.Value
				}
			}
			Expect(snapshotNameEnv).To(Equal(customSnapshotName))
			Expect(snapshotNamespaceEnv).To(Equal(testNamespace))
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
