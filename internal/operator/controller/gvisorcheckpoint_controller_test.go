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

// Package controller contains tests for the GvisorCheckpoint controller.
// Tests are split across multiple files for maintainability:
//   - gvisorcheckpoint_controller_helpers_test.go: Helper functions
//   - gvisorcheckpoint_controller_test.go: Basic operations and runtime validation tests
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

	sandboxv1alpha1 "github.com/isola-ai/isola-sb/api/v1alpha1"
	snapshotpkg "github.com/isola-ai/isola-sb/internal/snapshot"
)

var _ = Describe("GvisorCheckpoint Controller", func() {
	var (
		reconciler *GvisorCheckpointReconciler
		fakeClock  *FakeClock
		recorder   *events.FakeRecorder
	)

	BeforeEach(func() {
		fakeClock = NewFakeClock(time.Now())
		recorder = events.NewFakeRecorder(10)
		reconciler = &GvisorCheckpointReconciler{
			Client:                   k8sClient,
			Scheme:                   k8sClient.Scheme(),
			Recorder:                 recorder,
			Clock:                    fakeClock,
			BucketURL:                "s3://test-bucket?region=us-east-1",
			UploaderImage:            "checkpoint-uploader:test",
			CheckpointServiceAccount: "test-checkpoint-sa",
			Enabled:                  true,
			GvisorRunscPath:          "/usr/local/bin/runsc",
			GvisorRunscRoot:          "/run/containerd/runsc/k8s.io",
		}
	})

	Context("Basic Operations", func() {
		It("should fail when bucket URL is not configured", func() {
			chkptName := "chkpt-no-bucket"
			sandboxName := "sandbox-no-bucket"

			// Create reconciler without bucket URL
			noBucketReconciler := &GvisorCheckpointReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				Recorder:        recorder,
				Clock:           fakeClock,
				Enabled:         true,
				GvisorRunscPath: "/usr/local/bin/runsc",
				GvisorRunscRoot: "/run/containerd/runsc/k8s.io",
				// BucketURL is intentionally empty
			}

			createGvisorCheckpointCR(ctx, chkptName, sandboxName, "main")
			defer deleteGvisorCheckpointCR(ctx, chkptName)

			_, err := noBucketReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: chkptName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			chkpt := getGvisorCheckpointCR(ctx, chkptName)
			Expect(chkpt).NotTo(BeNil())

			readyCond := meta.FindStatusCondition(chkpt.Status.Conditions, string(sandboxv1alpha1.GvisorCheckpointComplete))
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCond.Reason).To(Equal(sandboxv1alpha1.ReasonGvisorCheckpointFailed))
			Expect(readyCond.Message).To(ContainSubstring("ISOLA_CHECKPOINT_BUCKET_URL"))
		})

		It("should create job on first reconcile", func() {
			chkptName := "chkpt-first"
			sandboxName := "sandbox-first-chkpt"
			podName := sandboxName + "-pod"
			runtimeClassName := "gvisor-first-chkpt"

			createRuntimeClassForSnapshot(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassForSnapshot(ctx, runtimeClassName)

			createCheckpointPod(ctx, podName, runtimeClassName,
				[]corev1.Container{{Name: "main", Image: "busybox"}},
				[]corev1.ContainerStatus{{Name: "main", ContainerID: "containerd://abc123", Ready: true}},
			)
			defer deleteCheckpointPod(ctx, podName)

			createGvisorCheckpointCR(ctx, chkptName, sandboxName, "main")
			defer deleteGvisorCheckpointCR(ctx, chkptName)
			defer deleteCheckpointJob(ctx, chkptName+"-checkpoint")

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: chkptName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify job was created
			job := getCheckpointJob(ctx, chkptName+"-checkpoint")
			Expect(job).NotTo(BeNil())
		})
	})

	Context("Runtime Validation", func() {
		It("should fail when pod does not exist", func() {
			chkptName := "chkpt-no-pod"
			sandboxName := "sandbox-no-pod-chkpt"

			createGvisorCheckpointCR(ctx, chkptName, sandboxName, "main")
			defer deleteGvisorCheckpointCR(ctx, chkptName)

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: chkptName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			chkpt := getGvisorCheckpointCR(ctx, chkptName)
			Expect(chkpt).NotTo(BeNil())

			readyCond := meta.FindStatusCondition(chkpt.Status.Conditions, string(sandboxv1alpha1.GvisorCheckpointComplete))
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCond.Reason).To(Equal(sandboxv1alpha1.ReasonGvisorCheckpointFailed))
		})

		It("should fail when runtime class is not gvisor", func() {
			chkptName := "chkpt-unsupported"
			sandboxName := "sandbox-unsupported-chkpt"
			podName := sandboxName + "-pod"
			runtimeClassName := "runc-unsupported-chkpt"

			createRuntimeClassForSnapshot(ctx, runtimeClassName, "runc")
			defer deleteRuntimeClassForSnapshot(ctx, runtimeClassName)

			createCheckpointPod(ctx, podName, runtimeClassName,
				[]corev1.Container{{Name: "main", Image: "busybox"}},
				[]corev1.ContainerStatus{{Name: "main", ContainerID: "containerd://abc123", Ready: true}},
			)
			defer deleteCheckpointPod(ctx, podName)

			createGvisorCheckpointCR(ctx, chkptName, sandboxName, "main")
			defer deleteGvisorCheckpointCR(ctx, chkptName)

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: chkptName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			chkpt := getGvisorCheckpointCR(ctx, chkptName)
			Expect(chkpt).NotTo(BeNil())

			readyCond := meta.FindStatusCondition(chkpt.Status.Conditions, string(sandboxv1alpha1.GvisorCheckpointComplete))
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCond.Reason).To(Equal(sandboxv1alpha1.ReasonGvisorCheckpointFailed))
			Expect(readyCond.Message).To(ContainSubstring("Runtime does not support"))
		})

		It("should fail when pod is not ready", func() {
			chkptName := "chkpt-pod-not-ready"
			sandboxName := "sandbox-pod-not-ready-chkpt"
			podName := sandboxName + "-pod"
			runtimeClassName := "gvisor-not-ready-chkpt"

			createRuntimeClassForSnapshot(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassForSnapshot(ctx, runtimeClassName)

			createCheckpointPodNotReady(ctx, podName, runtimeClassName,
				[]corev1.Container{{Name: "main", Image: "busybox"}},
			)
			defer deleteCheckpointPod(ctx, podName)

			createGvisorCheckpointCR(ctx, chkptName, sandboxName, "main")
			defer deleteGvisorCheckpointCR(ctx, chkptName)

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: chkptName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			chkpt := getGvisorCheckpointCR(ctx, chkptName)
			Expect(chkpt).NotTo(BeNil())

			readyCond := meta.FindStatusCondition(chkpt.Status.Conditions, string(sandboxv1alpha1.GvisorCheckpointComplete))
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCond.Reason).To(Equal(sandboxv1alpha1.ReasonGvisorCheckpointFailed))
			Expect(readyCond.Message).To(ContainSubstring("Sandbox pod is not ready"))
		})
	})

	Context("Job Completion", func() {
		It("should mark checkpoint as succeeded when job completes", func() {
			chkptName := "chkpt-complete"
			sandboxName := "sandbox-complete-chkpt"
			podName := sandboxName + "-pod"
			runtimeClassName := "gvisor-complete-chkpt"
			jobName := chkptName + "-checkpoint"

			createRuntimeClassForSnapshot(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassForSnapshot(ctx, runtimeClassName)

			createCheckpointPod(ctx, podName, runtimeClassName,
				[]corev1.Container{{Name: "main", Image: "busybox"}},
				[]corev1.ContainerStatus{{Name: "main", ContainerID: "containerd://abc123", Ready: true}},
			)
			defer deleteCheckpointPod(ctx, podName)

			createGvisorCheckpointCR(ctx, chkptName, sandboxName, "main")
			defer deleteGvisorCheckpointCR(ctx, chkptName)

			// First reconcile creates job
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: chkptName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Simulate job completion with termination message
			result := &snapshotpkg.UploadResult{
				SnapshotKey:  "checkpoints/test-sandbox/sandbox-complete-chkpt/rev-1/main/",
				Revision:     1,
				BytesWritten: 1024,
			}
			createCheckpointJobPodWithTerminationMessage(ctx, jobName, result)
			defer deleteCheckpointJobPod(ctx, jobName)
			setCheckpointJobComplete(ctx, jobName)

			// Second reconcile processes completed job
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: chkptName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			chkpt := getGvisorCheckpointCR(ctx, chkptName)
			Expect(chkpt).NotTo(BeNil())

			readyCond := meta.FindStatusCondition(chkpt.Status.Conditions, string(sandboxv1alpha1.GvisorCheckpointComplete))
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(readyCond.Reason).To(Equal(sandboxv1alpha1.ReasonGvisorCheckpointSucceeded))
			Expect(chkpt.Status.CheckpointKey).To(Equal(result.SnapshotKey))
			Expect(chkpt.Status.Revision).To(Equal(result.Revision))
		})

		It("should mark checkpoint as failed when job fails", func() {
			chkptName := "chkpt-failed"
			sandboxName := "sandbox-failed-chkpt"
			podName := sandboxName + "-pod"
			runtimeClassName := "gvisor-failed-chkpt"
			jobName := chkptName + "-checkpoint"

			createRuntimeClassForSnapshot(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassForSnapshot(ctx, runtimeClassName)

			createCheckpointPod(ctx, podName, runtimeClassName,
				[]corev1.Container{{Name: "main", Image: "busybox"}},
				[]corev1.ContainerStatus{{Name: "main", ContainerID: "containerd://abc123", Ready: true}},
			)
			defer deleteCheckpointPod(ctx, podName)

			createGvisorCheckpointCR(ctx, chkptName, sandboxName, "main")
			defer deleteGvisorCheckpointCR(ctx, chkptName)

			// First reconcile creates job
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: chkptName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Simulate job failure
			setCheckpointJobFailed(ctx, jobName, "checkpoint command failed")

			// Second reconcile processes failed job
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: chkptName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			chkpt := getGvisorCheckpointCR(ctx, chkptName)
			Expect(chkpt).NotTo(BeNil())

			readyCond := meta.FindStatusCondition(chkpt.Status.Conditions, string(sandboxv1alpha1.GvisorCheckpointComplete))
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCond.Reason).To(Equal(sandboxv1alpha1.ReasonGvisorCheckpointFailed))
			Expect(readyCond.Message).To(ContainSubstring("Checkpoint job failed"))
		})
	})

	Context("TTL Management", func() {
		It("should delete checkpoint after TTL expires", func() {
			chkptName := "chkpt-ttl"
			sandboxName := "sandbox-ttl-chkpt"
			podName := sandboxName + "-pod"
			runtimeClassName := "gvisor-ttl-chkpt"
			jobName := chkptName + "-checkpoint"

			createRuntimeClassForSnapshot(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassForSnapshot(ctx, runtimeClassName)

			createCheckpointPod(ctx, podName, runtimeClassName,
				[]corev1.Container{{Name: "main", Image: "busybox"}},
				[]corev1.ContainerStatus{{Name: "main", ContainerID: "containerd://abc123", Ready: true}},
			)
			defer deleteCheckpointPod(ctx, podName)

			createGvisorCheckpointCR(ctx, chkptName, sandboxName, "main")

			// First reconcile creates job
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: chkptName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Simulate job completion
			result := &snapshotpkg.UploadResult{
				SnapshotKey:  "checkpoints/test-sandbox/sandbox-ttl-chkpt/rev-1/main/",
				Revision:     1,
				BytesWritten: 1024,
			}
			createCheckpointJobPodWithTerminationMessage(ctx, jobName, result)
			defer deleteCheckpointJobPod(ctx, jobName)
			setCheckpointJobComplete(ctx, jobName)

			// Second reconcile processes completed job
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: chkptName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Advance clock past TTL (default 300s)
			fakeClock.Advance(301 * time.Second)

			// Third reconcile should delete the checkpoint
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: chkptName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Checkpoint should be deleted
			chkpt := getGvisorCheckpointCR(ctx, chkptName)
			Expect(chkpt).To(BeNil())
		})
	})
})
