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
	"github.com/prometheus/client_golang/prometheus/testutil"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	snapshotpkg "github.com/isola-run/isola/internal/snapshot"
)

var _ = Describe("Metrics", func() {

	Context("Registration", func() {
		It("should register all custom metrics with the controller-runtime registry", func() {
			// Verify each metric can be collected from the registry without errors.
			// We use CollectAndCount on individual collectors rather than Gather(),
			// because CounterVec metrics don't appear in Gather() until labels are observed.
			Expect(testutil.CollectAndCount(sandboxCreatedTotal)).To(Equal(1))
			Expect(testutil.CollectAndCount(rootfsSnapshotCreatedTotal)).To(Equal(1))
			// CounterVec starts with 0 label combos until observations happen
			Expect(testutil.CollectAndCount(rootfsSnapshotCompletedTotal)).To(BeNumerically(">=", 0))
		})
	})

	Context("Sandbox lifecycle", func() {
		var (
			reconciler *SandboxReconciler
			fakeClock  *FakeClock
		)

		BeforeEach(func() {
			fakeClock = NewFakeClock(time.Now())
			reconciler = newTestReconciler(fakeClock)
		})

		It("should increment sandboxCreatedTotal on pod creation", func() {
			sandboxName := "metrics-created"
			before := testutil.ToFloat64(sandboxCreatedTotal)

			createSandbox(ctx, sandboxName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			Expect(testutil.ToFloat64(sandboxCreatedTotal)).To(Equal(before + 1))
		})

	})

	Context("Snapshot lifecycle", func() {
		var (
			snapReconciler *RootfsSnapshotReconciler
			recorder       *events.FakeRecorder
		)

		BeforeEach(func() {
			recorder = events.NewFakeRecorder(10)
			snapReconciler = &RootfsSnapshotReconciler{
				Client:                 k8sClient,
				Scheme:                 k8sClient.Scheme(),
				Recorder:               recorder,
				Clock:                  NewFakeClock(time.Now()),
				BucketURL:              "s3://test-bucket?region=us-east-1",
				UploaderImage:          "isola-snapshot-uploader:test",
				SnapshotServiceAccount: "test-snapshot-sa",
				Enabled:                true,
				GvisorRunscPath:        "/usr/local/bin/runsc",
				GvisorInstallDir:       "/opt/isola/bin",
				ContainerdStateDir:     "/run/containerd",
				GvisorRunscRoot:        "/run/containerd/runsc/k8s.io",
			}
		})

		It("should increment rootfsSnapshotCreatedTotal when a snapshot job is created", func() {
			snapName := "metrics-snap-created"
			sandboxName := "metrics-sandbox-snap-created"
			podName := sandboxName + "-pod"
			runtimeClassName := "gvisor-metrics-snap-created"

			before := testutil.ToFloat64(rootfsSnapshotCreatedTotal)

			createRuntimeClassForSnapshot(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassForSnapshot(ctx, runtimeClassName)

			createSnapshotPod(ctx, podName, runtimeClassName,
				[]corev1.Container{{Name: "sandbox", Image: "busybox"}},
				[]corev1.ContainerStatus{{Name: "sandbox", ContainerID: "containerd://abc123", Ready: true}},
			)
			defer deleteSnapshotPod(ctx, podName)

			createRootfsSnapshotCR(ctx, snapName, sandboxName)
			defer deleteRootfsSnapshotCR(ctx, snapName)
			defer deleteSnapshotJob(ctx, snapName+"-job")

			_, err := snapReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			Expect(testutil.ToFloat64(rootfsSnapshotCreatedTotal)).To(Equal(before + 1))
		})

		It("should increment rootfsSnapshotCompletedTotal{result=succeeded} when job succeeds", func() {
			snapName := "metrics-snap-succeeded"
			sandboxName := "metrics-sandbox-snap-succeeded"
			podName := sandboxName + "-pod"
			runtimeClassName := "gvisor-metrics-snap-succeeded"
			jobName := snapName + "-job"

			before := testutil.ToFloat64(rootfsSnapshotCompletedTotal.WithLabelValues("succeeded"))

			createRuntimeClassForSnapshot(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassForSnapshot(ctx, runtimeClassName)

			createSnapshotPod(ctx, podName, runtimeClassName,
				[]corev1.Container{{Name: "sandbox", Image: "busybox"}},
				[]corev1.ContainerStatus{{Name: "sandbox", ContainerID: "containerd://abc456", Ready: true}},
			)
			defer deleteSnapshotPod(ctx, podName)

			createRootfsSnapshotCR(ctx, snapName, sandboxName)
			defer deleteRootfsSnapshotCR(ctx, snapName)
			defer deleteSnapshotJob(ctx, jobName)
			defer deleteSnapshotJobPod(ctx, jobName)

			// First reconcile: creates the job
			_, err := snapReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			createSnapshotJobPodWithTerminationMessage(ctx, jobName, &snapshotpkg.UploadResult{
				SnapshotKey: "rootfssnapshots/" + testNamespace + "/sandbox-metrics-success.tar",
			})
			setSnapshotJobComplete(ctx, jobName)

			// Second reconcile: processes the completed job
			_, err = snapReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			Expect(testutil.ToFloat64(rootfsSnapshotCompletedTotal.WithLabelValues("succeeded"))).To(Equal(before + 1))
		})

		It("should increment rootfsSnapshotCompletedTotal{result=failed} when job fails", func() {
			snapName := "metrics-snap-failed"
			sandboxName := "metrics-sandbox-snap-failed"
			podName := sandboxName + "-pod"
			runtimeClassName := "gvisor-metrics-snap-failed"
			jobName := snapName + "-job"

			before := testutil.ToFloat64(rootfsSnapshotCompletedTotal.WithLabelValues("failed"))

			createRuntimeClassForSnapshot(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassForSnapshot(ctx, runtimeClassName)

			createSnapshotPod(ctx, podName, runtimeClassName,
				[]corev1.Container{{Name: "sandbox", Image: "busybox"}},
				[]corev1.ContainerStatus{{Name: "sandbox", ContainerID: "containerd://abc789", Ready: true}},
			)
			defer deleteSnapshotPod(ctx, podName)

			createRootfsSnapshotCR(ctx, snapName, sandboxName)
			defer deleteRootfsSnapshotCR(ctx, snapName)
			defer deleteSnapshotJob(ctx, jobName)

			// First reconcile: creates the job
			_, err := snapReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			setSnapshotJobFailed(ctx, jobName, "upload failed: connection refused")

			// Second reconcile: processes the failed job
			_, err = snapReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			Expect(testutil.ToFloat64(rootfsSnapshotCompletedTotal.WithLabelValues("failed"))).To(Equal(before + 1))
		})
	})
})
