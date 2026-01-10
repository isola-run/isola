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
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	sandboxv1alpha1 "github.com/omereli/dev-isola/services/isola-operator/api/v1alpha1"
)

var _ = Describe("FilesystemSnapshot Controller", func() {
	var (
		testClock   *FakeClock
		reconciler  *FilesystemSnapshotReconciler
		snapshotKey types.NamespacedName
	)

	BeforeEach(func() {
		testClock = NewFakeClock(time.Now())
	})

	AfterEach(func() {
		// Clean up created resources
		if snapshotKey.Name != "" {
			snapshot := &sandboxv1alpha1.FilesystemSnapshot{}
			if err := k8sClient.Get(ctx, snapshotKey, snapshot); err == nil {
				// Remove finalizer to allow deletion
				snapshot.Finalizers = nil
				_ = k8sClient.Update(ctx, snapshot)
				_ = k8sClient.Delete(ctx, snapshot)
			}
		}
	})

	Context("Basic Reconciliation", func() {
		It("should create snapshotter job when FilesystemSnapshot is created", func() {
			reconciler = newTestFilesystemSnapshotReconciler(testClock)

			snapshotName := "test-snapshot-basic"
			snapshotKey = types.NamespacedName{Name: snapshotName, Namespace: testNamespace}

			snapshot := &sandboxv1alpha1.FilesystemSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      snapshotName,
					Namespace: testNamespace,
				},
				Spec: sandboxv1alpha1.FilesystemSnapshotSpec{
					SandboxRef: corev1.LocalObjectReference{
						Name: "test-sandbox",
					},
					PodName:               "test-pod",
					ContainerID:           "abc123",
					NodeName:              "test-node",
					SnapshotPath:          "/tmp/snapshot.tar",
					ActiveDeadlineSeconds: 300,
				},
			}

			Expect(k8sClient.Create(ctx, snapshot)).To(Succeed())

			// First reconcile adds finalizer
			_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: snapshotKey})
			Expect(err).NotTo(HaveOccurred())

			// Verify finalizer was added
			Expect(k8sClient.Get(ctx, snapshotKey, snapshot)).To(Succeed())
			Expect(snapshot.Finalizers).To(ContainElement(FilesystemSnapshotFinalizer))

			// Second reconcile creates job
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: snapshotKey})
			Expect(err).NotTo(HaveOccurred())

			// Verify job was created
			job := &batchv1.Job{}
			jobKey := types.NamespacedName{Name: snapshot.GetSnapshotterJobName(), Namespace: testNamespace}
			Expect(k8sClient.Get(ctx, jobKey, job)).To(Succeed())

			// Verify job spec
			Expect(job.Spec.Template.Spec.NodeSelector).To(HaveKeyWithValue("kubernetes.io/hostname", "test-node"))
			Expect(job.Spec.Template.Spec.Containers).To(HaveLen(1))
			Expect(job.Spec.Template.Spec.Containers[0].Name).To(Equal("snapshotter"))

			// Verify status was updated
			Expect(k8sClient.Get(ctx, snapshotKey, snapshot)).To(Succeed())
			Expect(snapshot.Status.JobName).To(Equal(snapshot.GetSnapshotterJobName()))
			Expect(snapshot.Status.Phase).To(Equal(sandboxv1alpha1.FilesystemSnapshotPhaseRunning))

			// Clean up job
			Expect(k8sClient.Delete(ctx, job)).To(Succeed())
		})

		It("should mark snapshot as succeeded when job completes", func() {
			reconciler = newTestFilesystemSnapshotReconciler(testClock)

			snapshotName := "test-snapshot-success"
			snapshotKey = types.NamespacedName{Name: snapshotName, Namespace: testNamespace}

			snapshot := &sandboxv1alpha1.FilesystemSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      snapshotName,
					Namespace: testNamespace,
				},
				Spec: sandboxv1alpha1.FilesystemSnapshotSpec{
					SandboxRef: corev1.LocalObjectReference{
						Name: "test-sandbox",
					},
					PodName:               "test-pod",
					ContainerID:           "abc123",
					NodeName:              "test-node",
					SnapshotPath:          "/tmp/snapshot.tar",
					ActiveDeadlineSeconds: 300,
				},
			}

			Expect(k8sClient.Create(ctx, snapshot)).To(Succeed())

			// Reconcile to add finalizer and create job
			_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: snapshotKey})
			Expect(err).NotTo(HaveOccurred())
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: snapshotKey})
			Expect(err).NotTo(HaveOccurred())

			// Mark job as complete
			job := &batchv1.Job{}
			jobKey := types.NamespacedName{Name: snapshot.GetSnapshotterJobName(), Namespace: testNamespace}
			Expect(k8sClient.Get(ctx, jobKey, job)).To(Succeed())

			job.Status.Conditions = []batchv1.JobCondition{
				{
					Type:   batchv1.JobComplete,
					Status: corev1.ConditionTrue,
				},
			}
			Expect(k8sClient.Status().Update(ctx, job)).To(Succeed())

			// Reconcile again
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: snapshotKey})
			Expect(err).NotTo(HaveOccurred())

			// Verify status
			Expect(k8sClient.Get(ctx, snapshotKey, snapshot)).To(Succeed())
			Expect(snapshot.Status.Phase).To(Equal(sandboxv1alpha1.FilesystemSnapshotPhaseSucceeded))
			Expect(snapshot.Status.CompletionTime).NotTo(BeNil())

			// Clean up job
			Expect(k8sClient.Delete(ctx, job)).To(Succeed())
		})

		It("should mark snapshot as failed when job fails", func() {
			reconciler = newTestFilesystemSnapshotReconciler(testClock)

			snapshotName := "test-snapshot-fail"
			snapshotKey = types.NamespacedName{Name: snapshotName, Namespace: testNamespace}

			snapshot := &sandboxv1alpha1.FilesystemSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      snapshotName,
					Namespace: testNamespace,
				},
				Spec: sandboxv1alpha1.FilesystemSnapshotSpec{
					SandboxRef: corev1.LocalObjectReference{
						Name: "test-sandbox",
					},
					PodName:               "test-pod",
					ContainerID:           "abc123",
					NodeName:              "test-node",
					SnapshotPath:          "/tmp/snapshot.tar",
					ActiveDeadlineSeconds: 300,
				},
			}

			Expect(k8sClient.Create(ctx, snapshot)).To(Succeed())

			// Reconcile to add finalizer and create job
			_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: snapshotKey})
			Expect(err).NotTo(HaveOccurred())
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: snapshotKey})
			Expect(err).NotTo(HaveOccurred())

			// Mark job as failed
			job := &batchv1.Job{}
			jobKey := types.NamespacedName{Name: snapshot.GetSnapshotterJobName(), Namespace: testNamespace}
			Expect(k8sClient.Get(ctx, jobKey, job)).To(Succeed())

			job.Status.Conditions = []batchv1.JobCondition{
				{
					Type:    batchv1.JobFailed,
					Status:  corev1.ConditionTrue,
					Message: "Container exited with error",
				},
			}
			Expect(k8sClient.Status().Update(ctx, job)).To(Succeed())

			// Reconcile again
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: snapshotKey})
			Expect(err).NotTo(HaveOccurred())

			// Verify status
			Expect(k8sClient.Get(ctx, snapshotKey, snapshot)).To(Succeed())
			Expect(snapshot.Status.Phase).To(Equal(sandboxv1alpha1.FilesystemSnapshotPhaseFailed))
			Expect(snapshot.Status.CompletionTime).NotTo(BeNil())
			Expect(snapshot.Status.Message).To(ContainSubstring("failed"))

			// Clean up job
			Expect(k8sClient.Delete(ctx, job)).To(Succeed())
		})
	})

	Context("Event Recording", func() {
		It("should record JobCreated event when job is created", func() {
			recorder := record.NewFakeRecorder(100)
			reconciler = newTestFilesystemSnapshotReconcilerWithRecorder(testClock, recorder)

			snapshotName := "test-snapshot-event-created"
			snapshotKey = types.NamespacedName{Name: snapshotName, Namespace: testNamespace}

			snapshot := &sandboxv1alpha1.FilesystemSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      snapshotName,
					Namespace: testNamespace,
				},
				Spec: sandboxv1alpha1.FilesystemSnapshotSpec{
					SandboxRef: corev1.LocalObjectReference{
						Name: "test-sandbox",
					},
					PodName:               "test-pod",
					ContainerID:           "abc123",
					NodeName:              "test-node",
					SnapshotPath:          "/tmp/snapshot.tar",
					ActiveDeadlineSeconds: 300,
				},
			}

			Expect(k8sClient.Create(ctx, snapshot)).To(Succeed())

			// Reconcile to add finalizer and create job
			_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: snapshotKey})
			Expect(err).NotTo(HaveOccurred())
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: snapshotKey})
			Expect(err).NotTo(HaveOccurred())

			// Check for JobCreated event
			Eventually(func() bool {
				select {
				case event := <-recorder.Events:
					return event != "" && (event == "Normal JobCreated Snapshotter job created" ||
						strings.Contains(event, "JobCreated"))
				default:
					return false
				}
			}, testTimeout, testInterval).Should(BeTrue())

			// Clean up
			job := &batchv1.Job{}
			jobKey := types.NamespacedName{Name: snapshot.GetSnapshotterJobName(), Namespace: testNamespace}
			if err := k8sClient.Get(ctx, jobKey, job); err == nil {
				Expect(k8sClient.Delete(ctx, job)).To(Succeed())
			}
		})

		It("should record Succeeded event when snapshot completes", func() {
			recorder := record.NewFakeRecorder(100)
			reconciler = newTestFilesystemSnapshotReconcilerWithRecorder(testClock, recorder)

			snapshotName := "test-snapshot-event-succeeded"
			snapshotKey = types.NamespacedName{Name: snapshotName, Namespace: testNamespace}

			snapshot := &sandboxv1alpha1.FilesystemSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      snapshotName,
					Namespace: testNamespace,
				},
				Spec: sandboxv1alpha1.FilesystemSnapshotSpec{
					SandboxRef: corev1.LocalObjectReference{
						Name: "test-sandbox",
					},
					PodName:               "test-pod",
					ContainerID:           "abc123",
					NodeName:              "test-node",
					SnapshotPath:          "/tmp/snapshot.tar",
					ActiveDeadlineSeconds: 300,
				},
			}

			Expect(k8sClient.Create(ctx, snapshot)).To(Succeed())

			// Reconcile to add finalizer and create job
			_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: snapshotKey})
			Expect(err).NotTo(HaveOccurred())
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: snapshotKey})
			Expect(err).NotTo(HaveOccurred())

			// Drain events from job creation
			drainEvents(recorder)

			// Mark job as complete
			job := &batchv1.Job{}
			jobKey := types.NamespacedName{Name: snapshot.GetSnapshotterJobName(), Namespace: testNamespace}
			Expect(k8sClient.Get(ctx, jobKey, job)).To(Succeed())

			job.Status.Conditions = []batchv1.JobCondition{
				{
					Type:   batchv1.JobComplete,
					Status: corev1.ConditionTrue,
				},
			}
			Expect(k8sClient.Status().Update(ctx, job)).To(Succeed())

			// Reconcile again
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: snapshotKey})
			Expect(err).NotTo(HaveOccurred())

			// Check for Succeeded event
			Eventually(func() bool {
				select {
				case event := <-recorder.Events:
					return event != "" && strings.Contains(event, "Succeeded")
				default:
					return false
				}
			}, testTimeout, testInterval).Should(BeTrue())

			// Clean up
			Expect(k8sClient.Delete(ctx, job)).To(Succeed())
		})
	})

	Context("Deletion Handling", func() {
		It("should remove finalizer on deletion", func() {
			reconciler = newTestFilesystemSnapshotReconciler(testClock)

			snapshotName := "test-snapshot-delete"
			snapshotKey = types.NamespacedName{Name: snapshotName, Namespace: testNamespace}

			snapshot := &sandboxv1alpha1.FilesystemSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      snapshotName,
					Namespace: testNamespace,
				},
				Spec: sandboxv1alpha1.FilesystemSnapshotSpec{
					SandboxRef: corev1.LocalObjectReference{
						Name: "test-sandbox",
					},
					PodName:               "test-pod",
					ContainerID:           "abc123",
					NodeName:              "test-node",
					SnapshotPath:          "/tmp/snapshot.tar",
					ActiveDeadlineSeconds: 300,
				},
			}

			Expect(k8sClient.Create(ctx, snapshot)).To(Succeed())

			// Reconcile to add finalizer
			_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: snapshotKey})
			Expect(err).NotTo(HaveOccurred())

			// Verify finalizer was added
			Expect(k8sClient.Get(ctx, snapshotKey, snapshot)).To(Succeed())
			Expect(snapshot.Finalizers).To(ContainElement(FilesystemSnapshotFinalizer))

			// Delete the snapshot
			Expect(k8sClient.Delete(ctx, snapshot)).To(Succeed())

			// Reconcile to process deletion
			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: snapshotKey})
			Expect(err).NotTo(HaveOccurred())

			// Verify snapshot is gone
			err = k8sClient.Get(ctx, snapshotKey, snapshot)
			Expect(client.IgnoreNotFound(err)).To(Succeed())

			// Reset snapshotKey to prevent AfterEach from trying to clean up
			snapshotKey = types.NamespacedName{}
		})
	})
})

// drainEvents drains all events from the recorder
func drainEvents(recorder *record.FakeRecorder) {
	for {
		select {
		case <-recorder.Events:
		default:
			return
		}
	}
}
