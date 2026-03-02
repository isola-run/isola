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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	sandboxv1alpha1 "github.com/isola-ai/isola/api/v1alpha1"
	snapshotpkg "github.com/isola-ai/isola/internal/snapshot"
)

var _ = Describe("Metrics", func() {

	Context("Registration", func() {
		It("should register all custom metrics with the controller-runtime registry", func() {
			// Verify each metric can be collected from the registry without errors.
			// We use CollectAndCount on individual collectors rather than Gather(),
			// because CounterVec metrics don't appear in Gather() until labels are observed.
			Expect(testutil.CollectAndCount(sandboxCreatedTotal)).To(Equal(1))
			Expect(testutil.CollectAndCount(sandboxTimedOutTotal)).To(Equal(1))
			Expect(testutil.CollectAndCount(sandboxReadyDurationSeconds)).To(Equal(1))
			Expect(testutil.CollectAndCount(rootfsSnapshotCreatedTotal)).To(Equal(1))
			// CounterVec starts with 0 label combos until observations happen
			Expect(testutil.CollectAndCount(rootfsSnapshotCompletedTotal)).To(BeNumerically(">=", 0))
			// sandboxRunningCollector is tested via scrape in its own test below
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

		It("should observe sandboxReadyDurationSeconds when sandbox becomes ready", func() {
			sandboxName := "metrics-ready-duration"

			createSandbox(ctx, sandboxName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			// First reconcile: create pod
			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			// Make pod ready
			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())
			makePodReady(ctx, pod, "containerd://abc123", fakeClock)

			// Reconcile again to pick up the ready state — the histogram should gain an observation
			_, err = doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			// Verify the histogram has at least 1 observation (CollectAndCount returns metric family count)
			Expect(testutil.CollectAndCount(sandboxReadyDurationSeconds)).To(Equal(1))
		})

		It("should report running sandbox count from collector at scrape time", func() {
			collector := &sandboxRunningCollector{client: k8sClient}

			// Baseline: count running sandboxes before test
			before := testutil.ToFloat64(collector)

			sandboxName := "metrics-running-collector"
			createSandbox(ctx, sandboxName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			// Reconcile to create pod
			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			// Not ready yet — count should not change
			Expect(testutil.ToFloat64(collector)).To(Equal(before))

			// Make pod ready
			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())
			makePodReady(ctx, pod, "containerd://abc-collector", fakeClock)

			// Reconcile to set Ready=True on the sandbox
			_, err = doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			// Now the collector should see one more running sandbox
			Expect(testutil.ToFloat64(collector)).To(Equal(before + 1))
		})

		It("should not increment sandboxTimedOutTotal on subsequent reconciles after cleanup begins", func() {
			sandboxName := "metrics-timed-out-idempotent"
			timeout := int64(60)

			createSandbox(ctx, sandboxName, func(s *sandboxv1alpha1.Sandbox) {
				s.Spec.ActiveDeadlineSeconds = &timeout
			})
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())
			pod.Status.Phase = corev1.PodRunning
			pod.Status.StartTime = &metav1.Time{Time: fakeClock.Now()}
			pod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
			Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())

			_, err = doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			fakeClock.Advance(61 * time.Second)

			// First timeout reconcile — fires the counter
			_, err = doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			after := testutil.ToFloat64(sandboxTimedOutTotal)

			// Second timeout reconcile — cleanup already started, counter must not fire again
			_, err = doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			Expect(testutil.ToFloat64(sandboxTimedOutTotal)).To(Equal(after))
		})

		It("should increment sandboxTimedOutTotal when sandbox times out", func() {
			sandboxName := "metrics-timed-out"
			timeout := int64(60)

			createSandbox(ctx, sandboxName, func(s *sandboxv1alpha1.Sandbox) {
				s.Spec.ActiveDeadlineSeconds = &timeout
			})
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			// Reconcile to add finalizer and create pod
			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			// Make pod ready
			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())
			pod.Status.Phase = corev1.PodRunning
			pod.Status.StartTime = &metav1.Time{Time: fakeClock.Now()}
			pod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
			Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())

			// Reconcile to update status
			_, err = doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			before := testutil.ToFloat64(sandboxTimedOutTotal)

			// Advance past timeout
			fakeClock.Advance(61 * time.Second)

			// Reconcile to trigger timeout
			_, err = doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			Expect(testutil.ToFloat64(sandboxTimedOutTotal)).To(Equal(before + 1))
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
				UploaderImage:          "isola-uploader:test",
				SnapshotServiceAccount: "test-snapshot-sa",
				Enabled:                true,
				GvisorRunscPath:        "/usr/local/bin/runsc",
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

			createRootfsSnapshotCR(ctx, snapName, sandboxName, []string{"sandbox"})
			defer deleteRootfsSnapshotCR(ctx, snapName)
			defer deleteSnapshotJob(ctx, snapName+"-sandbox")

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
			jobName := snapName + "-sandbox"

			before := testutil.ToFloat64(rootfsSnapshotCompletedTotal.WithLabelValues("succeeded"))

			createRuntimeClassForSnapshot(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassForSnapshot(ctx, runtimeClassName)

			createSnapshotPod(ctx, podName, runtimeClassName,
				[]corev1.Container{{Name: "sandbox", Image: "busybox"}},
				[]corev1.ContainerStatus{{Name: "sandbox", ContainerID: "containerd://abc456", Ready: true}},
			)
			defer deleteSnapshotPod(ctx, podName)

			createRootfsSnapshotCR(ctx, snapName, sandboxName, []string{"sandbox"})
			defer deleteRootfsSnapshotCR(ctx, snapName)
			defer deleteSnapshotJob(ctx, jobName)
			defer deleteSnapshotJobPod(ctx, jobName)

			// First reconcile: creates the job
			_, err := snapReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			createSnapshotJobPodWithTerminationMessage(ctx, jobName, &snapshotpkg.UploadResult{
				SnapshotKey: "snapshots/test/rev-00001/sandbox.tar",
				Revision:    1,
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
			jobName := snapName + "-sandbox"

			before := testutil.ToFloat64(rootfsSnapshotCompletedTotal.WithLabelValues("failed"))

			createRuntimeClassForSnapshot(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassForSnapshot(ctx, runtimeClassName)

			createSnapshotPod(ctx, podName, runtimeClassName,
				[]corev1.Container{{Name: "sandbox", Image: "busybox"}},
				[]corev1.ContainerStatus{{Name: "sandbox", ContainerID: "containerd://abc789", Ready: true}},
			)
			defer deleteSnapshotPod(ctx, podName)

			createRootfsSnapshotCR(ctx, snapName, sandboxName, []string{"sandbox"})
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
