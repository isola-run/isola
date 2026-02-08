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

// Package controller contains tests for the RootfsSnapshot controller.
// Tests are split across multiple files for maintainability:
//   - rootfssnapshot_controller_helpers_test.go: Helper functions
//   - rootfssnapshot_controller_test.go: Basic operations and runtime validation tests
//   - rootfssnapshot_controller_snapshot_test.go: Single container snapshot and error handling tests
//   - rootfssnapshot_controller_metadata_test.go: Label, revision, and container selection tests
//   - rootfssnapshot_controller_lifecycle_test.go: TTL, finalizer, and deadline tests
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

	Context("Basic Operations", func() {
		It("should fail when bucket URL is not configured", func() {
			snapName := "snap-no-bucket"
			sandboxName := "sandbox-no-bucket"

			// Create reconciler without bucket URL
			noBucketReconciler := &RootfsSnapshotReconciler{
				Client:          k8sClient,
				Scheme:          k8sClient.Scheme(),
				Recorder:        recorder,
				Clock:           fakeClock,
				Enabled:         true,
				GvisorRunscPath: "/usr/local/bin/runsc",
				GvisorRunscRoot: "/run/containerd/runsc/k8s.io",
				// BucketURL is intentionally empty
			}

			createRootfsSnapshotCR(ctx, snapName, sandboxName, nil)
			defer deleteRootfsSnapshotCR(ctx, snapName)

			_, err := noBucketReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			snap := getRootfsSnapshotCR(ctx, snapName)
			Expect(snap).NotTo(BeNil())

			readyCond := meta.FindStatusCondition(snap.Status.Conditions, string(sandboxv1alpha1.RootfsSnapshotFailed))
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(readyCond.Reason).To(Equal(sandboxv1alpha1.ReasonRootfsSnapshotFailed))
			Expect(readyCond.Message).To(ContainSubstring("ISOLA_ROOTFSSNAPSHOT_BUCKET_URL"))
		})

		It("should create job on first reconcile", func() {
			snapName := "snap-first"
			sandboxName := "sandbox-first"
			podName := sandboxName + "-pod"
			runtimeClassName := "gvisor-first"

			createRuntimeClassForSnapshot(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassForSnapshot(ctx, runtimeClassName)

			createSnapshotPod(ctx, podName, runtimeClassName,
				[]corev1.Container{{Name: "main", Image: "busybox"}},
				[]corev1.ContainerStatus{{Name: "main", ContainerID: "containerd://abc123", Ready: true}},
			)
			defer deleteSnapshotPod(ctx, podName)

			createRootfsSnapshotCR(ctx, snapName, sandboxName, []string{"main"})
			defer deleteRootfsSnapshotCR(ctx, snapName)
			defer deleteSnapshotJob(ctx, snapName+"-main")

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify job was created
			job := getSnapshotJob(ctx, snapName+"-main")
			Expect(job).NotTo(BeNil())

			// Verify standard labels on Job metadata
			expectedLabels := map[string]string{
				"app.kubernetes.io/name":       "isola-sandbox",
				"app.kubernetes.io/instance":   snapName,
				"app.kubernetes.io/component":  "rootfssnapshot",
				"app.kubernetes.io/part-of":    "isola",
				"app.kubernetes.io/managed-by": "isola-operator",
			}
			for k, v := range expectedLabels {
				Expect(job.Labels).To(HaveKeyWithValue(k, v))
			}

			// Verify same labels on pod template
			for k, v := range expectedLabels {
				Expect(job.Spec.Template.Labels).To(HaveKeyWithValue(k, v))
			}
		})
	})

	Context("Runtime Validation", func() {
		It("should fail when pod does not exist", func() {
			snapName := "snap-no-pod"
			sandboxName := "sandbox-no-pod"

			createRootfsSnapshotCR(ctx, snapName, sandboxName, nil)
			defer deleteRootfsSnapshotCR(ctx, snapName)

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			snap := getRootfsSnapshotCR(ctx, snapName)
			Expect(snap).NotTo(BeNil())

			readyCond := meta.FindStatusCondition(snap.Status.Conditions, string(sandboxv1alpha1.RootfsSnapshotFailed))
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(readyCond.Reason).To(Equal(sandboxv1alpha1.ReasonRootfsSnapshotFailed))
		})

		It("should fail when runtime class is not gvisor", func() {
			snapName := "snap-unsupported"
			sandboxName := "sandbox-unsupported"
			podName := sandboxName + "-pod"
			runtimeClassName := "runc-unsupported"

			createRuntimeClassForSnapshot(ctx, runtimeClassName, "runc")
			defer deleteRuntimeClassForSnapshot(ctx, runtimeClassName)

			createSnapshotPod(ctx, podName, runtimeClassName,
				[]corev1.Container{{Name: "main", Image: "busybox"}},
				[]corev1.ContainerStatus{{Name: "main", ContainerID: "containerd://abc123", Ready: true}},
			)
			defer deleteSnapshotPod(ctx, podName)

			createRootfsSnapshotCR(ctx, snapName, sandboxName, nil)
			defer deleteRootfsSnapshotCR(ctx, snapName)

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			snap := getRootfsSnapshotCR(ctx, snapName)
			Expect(snap).NotTo(BeNil())

			readyCond := meta.FindStatusCondition(snap.Status.Conditions, string(sandboxv1alpha1.RootfsSnapshotFailed))
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(readyCond.Reason).To(Equal(sandboxv1alpha1.ReasonRootfsSnapshotFailed))
			Expect(readyCond.Message).To(ContainSubstring("Runtime does not support"))
		})

		It("should fail when pod is not ready", func() {
			snapName := "snap-pod-not-ready"
			sandboxName := "sandbox-pod-not-ready"
			podName := sandboxName + "-pod"
			runtimeClassName := "gvisor-not-ready"

			createRuntimeClassForSnapshot(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassForSnapshot(ctx, runtimeClassName)

			createSnapshotPodNotReady(ctx, podName, runtimeClassName,
				[]corev1.Container{{Name: "main", Image: "busybox"}},
			)
			defer deleteSnapshotPod(ctx, podName)

			createRootfsSnapshotCR(ctx, snapName, sandboxName, []string{"main"})
			defer deleteRootfsSnapshotCR(ctx, snapName)

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			snap := getRootfsSnapshotCR(ctx, snapName)
			Expect(snap).NotTo(BeNil())

			readyCond := meta.FindStatusCondition(snap.Status.Conditions, string(sandboxv1alpha1.RootfsSnapshotFailed))
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(readyCond.Reason).To(Equal(sandboxv1alpha1.ReasonRootfsSnapshotFailed))
			Expect(readyCond.Message).To(ContainSubstring("Sandbox pod is not ready"))
		})
	})
})
