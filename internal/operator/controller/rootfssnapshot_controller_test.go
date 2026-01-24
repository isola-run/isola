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
	"context"
	"encoding/json"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	nodev1 "k8s.io/api/node/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	sandboxv1alpha1 "github.com/isola-ai/isola-sb/api/v1alpha1"
	snapshotpkg "github.com/isola-ai/isola-sb/internal/snapshot"
)

var _ = Describe("RootfsSnapshot Controller", func() {
	var (
		reconciler *RootfsSnapshotReconciler
		fakeClock  *FakeClock
		recorder   *record.FakeRecorder
	)

	// Helper functions
	createRuntimeClassHelper := func(ctx context.Context, name, handler string) {
		rc := &nodev1.RuntimeClass{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Handler:    handler,
		}
		err := k8sClient.Create(ctx, rc)
		if !errors.IsAlreadyExists(err) {
			Expect(err).NotTo(HaveOccurred())
		}
	}

	deleteRuntimeClassHelper := func(ctx context.Context, name string) {
		rc := &nodev1.RuntimeClass{}
		err := k8sClient.Get(ctx, types.NamespacedName{Name: name}, rc)
		if errors.IsNotFound(err) {
			return // Already deleted
		}
		Expect(err).NotTo(HaveOccurred())
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, rc))).NotTo(HaveOccurred())
	}

	createPod := func(ctx context.Context, name, namespace, runtimeClassName, nodeName string, containers []corev1.Container, containerStatuses []corev1.ContainerStatus) *corev1.Pod {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
			Spec: corev1.PodSpec{
				RuntimeClassName: &runtimeClassName,
				NodeName:         nodeName,
				Containers:       containers,
			},
		}
		Expect(k8sClient.Create(ctx, pod)).To(Succeed())
		pod.Status.Phase = corev1.PodRunning
		pod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
		pod.Status.ContainerStatuses = containerStatuses
		Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())
		return pod
	}

	createPodNotReady := func(ctx context.Context, name, namespace, runtimeClassName, nodeName string, containers []corev1.Container) *corev1.Pod {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
			Spec: corev1.PodSpec{
				RuntimeClassName: &runtimeClassName,
				NodeName:         nodeName,
				Containers:       containers,
			},
		}
		Expect(k8sClient.Create(ctx, pod)).To(Succeed())
		pod.Status.Phase = corev1.PodPending
		pod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionFalse, Reason: "ContainersNotReady"}}
		Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())
		return pod
	}

	deletePodHelper := func(ctx context.Context, name, namespace string) {
		pod := &corev1.Pod{}
		err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, pod)
		if errors.IsNotFound(err) {
			return // Already deleted
		}
		Expect(err).NotTo(HaveOccurred())
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, pod))).NotTo(HaveOccurred())
	}

	createRootfsSnapshot := func(ctx context.Context, name, namespace, sandboxName string, containerNames []string) *sandboxv1alpha1.RootfsSnapshot {
		snap := &sandboxv1alpha1.RootfsSnapshot{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				Labels:    map[string]string{LabelSandboxName: sandboxName},
			},
			Spec: sandboxv1alpha1.RootfsSnapshotSpec{
				SandboxName:    sandboxName,
				ContainerNames: containerNames,
			},
		}
		Expect(k8sClient.Create(ctx, snap)).To(Succeed())
		return snap
	}

	deleteRootfsSnapshotHelper := func(ctx context.Context, name, namespace string) {
		snap := &sandboxv1alpha1.RootfsSnapshot{}
		err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, snap)
		if errors.IsNotFound(err) {
			return // Already deleted
		}
		Expect(err).NotTo(HaveOccurred())
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, snap))).NotTo(HaveOccurred())
	}

	getRootfsSnapshotHelper := func(ctx context.Context, name, namespace string) *sandboxv1alpha1.RootfsSnapshot {
		snap := &sandboxv1alpha1.RootfsSnapshot{}
		err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, snap)
		if err != nil {
			return nil
		}
		return snap
	}

	getJobHelper := func(ctx context.Context, name, namespace string) *batchv1.Job {
		job := &batchv1.Job{}
		err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, job)
		if err != nil {
			return nil
		}
		return job
	}

	deleteJobHelper := func(ctx context.Context, name, namespace string) {
		job := &batchv1.Job{}
		err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, job)
		if errors.IsNotFound(err) {
			return // Already deleted
		}
		Expect(err).NotTo(HaveOccurred())
		propagationPolicy := metav1.DeletePropagationBackground
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, job, &client.DeleteOptions{PropagationPolicy: &propagationPolicy}))).NotTo(HaveOccurred())
	}

	setJobComplete := func(ctx context.Context, name, namespace string) {
		job := getJobHelper(ctx, name, namespace)
		if job == nil {
			return
		}
		now := metav1.Now()
		job.Status.StartTime = &now
		job.Status.CompletionTime = &now
		job.Status.Succeeded = 1
		job.Status.Conditions = []batchv1.JobCondition{
			{Type: batchv1.JobSuccessCriteriaMet, Status: corev1.ConditionTrue},
			{Type: batchv1.JobComplete, Status: corev1.ConditionTrue},
		}
		ExpectWithOffset(1, k8sClient.Status().Update(ctx, job)).To(Succeed())
	}

	setJobFailed := func(ctx context.Context, name, namespace, message string) {
		job := getJobHelper(ctx, name, namespace)
		if job == nil {
			return
		}
		now := metav1.Now()
		job.Status.StartTime = &now
		job.Status.Failed = 1
		job.Status.Conditions = []batchv1.JobCondition{
			{Type: batchv1.JobFailureTarget, Status: corev1.ConditionTrue},
			{Type: batchv1.JobFailed, Status: corev1.ConditionTrue, Message: message},
		}
		ExpectWithOffset(1, k8sClient.Status().Update(ctx, job)).To(Succeed())
	}

	// createJobPodWithTerminationMessage creates a pod for the job with a termination message
	// containing the upload result (simulating what the uploader writes)
	createJobPodWithTerminationMessage := func(ctx context.Context, jobName, namespace string, result *snapshotpkg.UploadResult) {
		// Create termination message JSON
		var terminationMessage string
		if result != nil {
			data, err := json.Marshal(result)
			Expect(err).NotTo(HaveOccurred())
			terminationMessage = string(data)
		}

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      jobName + "-pod",
				Namespace: namespace,
				Labels:    map[string]string{"job-name": jobName},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "uploader", Image: "test"},
				},
				RestartPolicy: corev1.RestartPolicyNever,
			},
		}
		Expect(k8sClient.Create(ctx, pod)).To(Succeed())

		// Update status with termination message
		pod.Status.Phase = corev1.PodSucceeded
		pod.Status.ContainerStatuses = []corev1.ContainerStatus{
			{
				Name: "uploader",
				State: corev1.ContainerState{
					Terminated: &corev1.ContainerStateTerminated{
						ExitCode: 0,
						Message:  terminationMessage,
					},
				},
			},
		}
		Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())
	}

	deleteJobPodHelper := func(ctx context.Context, jobName, namespace string) {
		pod := &corev1.Pod{}
		err := k8sClient.Get(ctx, types.NamespacedName{Name: jobName + "-pod", Namespace: namespace}, pod)
		if errors.IsNotFound(err) {
			return // Already deleted
		}
		Expect(err).NotTo(HaveOccurred())
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, pod))).NotTo(HaveOccurred())
	}

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

	Context("Basic Operations", func() {
		It("should fail when bucket URL is not configured", func() {
			snapName := "snap-no-bucket"
			sandboxName := "sandbox-no-bucket"

			// Create reconciler without bucket URL
			noBucketReconciler := &RootfsSnapshotReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: recorder,
				Clock:    fakeClock,
				// BucketURL is intentionally empty
			}

			createRootfsSnapshot(ctx, snapName, testNamespace, sandboxName, nil)
			defer deleteRootfsSnapshotHelper(ctx, snapName, testNamespace)

			_, err := noBucketReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			snap := getRootfsSnapshotHelper(ctx, snapName, testNamespace)
			Expect(snap).NotTo(BeNil())

			readyCond := meta.FindStatusCondition(snap.Status.Conditions, string(sandboxv1alpha1.RootfsSnapshotComplete))
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCond.Reason).To(Equal(sandboxv1alpha1.ReasonRootfsSnapshotFailed))
			Expect(readyCond.Message).To(ContainSubstring("ISOLA_SNAPSHOT_BUCKET_URL"))
		})

		It("should create job on first reconcile", func() {
			snapName := "snap-first"
			sandboxName := "sandbox-first"
			podName := sandboxName + "-pod"
			runtimeClassName := "gvisor-first"

			createRuntimeClassHelper(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassHelper(ctx, runtimeClassName)

			createPod(ctx, podName, testNamespace, runtimeClassName, "test-node",
				[]corev1.Container{{Name: "main", Image: "busybox"}},
				[]corev1.ContainerStatus{{Name: "main", ContainerID: "containerd://abc123", Ready: true}},
			)
			defer deletePodHelper(ctx, podName, testNamespace)

			createRootfsSnapshot(ctx, snapName, testNamespace, sandboxName, []string{"main"})
			defer deleteRootfsSnapshotHelper(ctx, snapName, testNamespace)
			defer deleteJobHelper(ctx, snapName+"-main", testNamespace)

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify job was created
			job := getJobHelper(ctx, snapName+"-main", testNamespace)
			Expect(job).NotTo(BeNil())
		})
	})

	Context("Runtime Validation", func() {
		It("should fail when pod does not exist", func() {
			snapName := "snap-no-pod"
			sandboxName := "sandbox-no-pod"

			createRootfsSnapshot(ctx, snapName, testNamespace, sandboxName, nil)
			defer deleteRootfsSnapshotHelper(ctx, snapName, testNamespace)

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			snap := getRootfsSnapshotHelper(ctx, snapName, testNamespace)
			Expect(snap).NotTo(BeNil())

			readyCond := meta.FindStatusCondition(snap.Status.Conditions, string(sandboxv1alpha1.RootfsSnapshotComplete))
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCond.Reason).To(Equal(sandboxv1alpha1.ReasonRootfsSnapshotFailed))
		})

		It("should fail when runtime class is not gvisor", func() {
			snapName := "snap-unsupported"
			sandboxName := "sandbox-unsupported"
			podName := sandboxName + "-pod"
			runtimeClassName := "runc-unsupported"

			createRuntimeClassHelper(ctx, runtimeClassName, "runc")
			defer deleteRuntimeClassHelper(ctx, runtimeClassName)

			createPod(ctx, podName, testNamespace, runtimeClassName, "test-node",
				[]corev1.Container{{Name: "main", Image: "busybox"}},
				[]corev1.ContainerStatus{{Name: "main", ContainerID: "containerd://abc123", Ready: true}},
			)
			defer deletePodHelper(ctx, podName, testNamespace)

			createRootfsSnapshot(ctx, snapName, testNamespace, sandboxName, nil)
			defer deleteRootfsSnapshotHelper(ctx, snapName, testNamespace)

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			snap := getRootfsSnapshotHelper(ctx, snapName, testNamespace)
			Expect(snap).NotTo(BeNil())

			readyCond := meta.FindStatusCondition(snap.Status.Conditions, string(sandboxv1alpha1.RootfsSnapshotComplete))
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCond.Reason).To(Equal(sandboxv1alpha1.ReasonRootfsSnapshotFailed))
			Expect(readyCond.Message).To(ContainSubstring("Runtime does not support"))
		})

		It("should fail when pod is not ready", func() {
			snapName := "snap-pod-not-ready"
			sandboxName := "sandbox-pod-not-ready"
			podName := sandboxName + "-pod"
			runtimeClassName := "gvisor-not-ready"

			createRuntimeClassHelper(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassHelper(ctx, runtimeClassName)

			createPodNotReady(ctx, podName, testNamespace, runtimeClassName, "test-node",
				[]corev1.Container{{Name: "main", Image: "busybox"}},
			)
			defer deletePodHelper(ctx, podName, testNamespace)

			createRootfsSnapshot(ctx, snapName, testNamespace, sandboxName, []string{"main"})
			defer deleteRootfsSnapshotHelper(ctx, snapName, testNamespace)

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			snap := getRootfsSnapshotHelper(ctx, snapName, testNamespace)
			Expect(snap).NotTo(BeNil())

			readyCond := meta.FindStatusCondition(snap.Status.Conditions, string(sandboxv1alpha1.RootfsSnapshotComplete))
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCond.Reason).To(Equal(sandboxv1alpha1.ReasonRootfsSnapshotFailed))
			Expect(readyCond.Message).To(ContainSubstring("Sandbox pod is not ready"))
		})
	})

	Context("Single Container Snapshot", func() {
		It("should create job for single container with two-container pattern", func() {
			snapName := "snap-single"
			sandboxName := "sandbox-single"
			podName := sandboxName + "-pod"
			runtimeClassName := "gvisor-single"

			createRuntimeClassHelper(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassHelper(ctx, runtimeClassName)

			createPod(ctx, podName, testNamespace, runtimeClassName, "test-node",
				[]corev1.Container{{Name: "sandbox", Image: "busybox"}},
				[]corev1.ContainerStatus{{Name: "sandbox", ContainerID: "containerd://abc123", Ready: true}},
			)
			defer deletePodHelper(ctx, podName, testNamespace)

			createRootfsSnapshot(ctx, snapName, testNamespace, sandboxName, []string{"sandbox"})
			defer deleteRootfsSnapshotHelper(ctx, snapName, testNamespace)

			jobName := snapName + "-sandbox"
			defer deleteJobHelper(ctx, jobName, testNamespace)

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify job was created with two-container pattern
			job := getJobHelper(ctx, jobName, testNamespace)
			Expect(job).NotTo(BeNil())

			// Init container should be snapshotter (runs runsc tar)
			Expect(job.Spec.Template.Spec.InitContainers).To(HaveLen(1))
			Expect(job.Spec.Template.Spec.InitContainers[0].Name).To(Equal("snapshotter"))

			// Main container should be uploader (uploads to bucket)
			Expect(job.Spec.Template.Spec.Containers).To(HaveLen(1))
			Expect(job.Spec.Template.Spec.Containers[0].Name).To(Equal("uploader"))
			Expect(job.Spec.Template.Spec.Containers[0].Image).To(Equal("isola-uploader:test"))

			// Verify emptyDir volume for snapshot data
			var snapshotDataVolume *corev1.Volume
			for i := range job.Spec.Template.Spec.Volumes {
				if job.Spec.Template.Spec.Volumes[i].Name == "snapshot-data" {
					snapshotDataVolume = &job.Spec.Template.Spec.Volumes[i]
					break
				}
			}
			Expect(snapshotDataVolume).NotTo(BeNil())
			Expect(snapshotDataVolume.EmptyDir).NotTo(BeNil())

			// Verify container snapshot status (SnapshotKey/URI not set until job completes)
			snap := getRootfsSnapshotHelper(ctx, snapName, testNamespace)
			Expect(snap).NotTo(BeNil())
			Expect(snap.Status.ContainerSnapshots).To(HaveLen(1))
			Expect(snap.Status.ContainerSnapshots[0].ContainerName).To(Equal("sandbox"))
			Expect(snap.Status.ContainerSnapshots[0].ContainerID).To(Equal("abc123"))
			// SnapshotKey and URI are empty until job completes with termination message
			Expect(snap.Status.ContainerSnapshots[0].SnapshotKey).To(BeEmpty())
			Expect(snap.Status.ContainerSnapshots[0].SnapshotURI).To(BeEmpty())
			// Revision is 0 until job completes and reports actual revision
			Expect(snap.Status.Revision).To(Equal(int32(0)))
		})

		It("should mark complete when job succeeds", func() {
			snapName := "snap-success"
			sandboxName := "sandbox-success"
			podName := sandboxName + "-pod"
			runtimeClassName := "gvisor-success"

			createRuntimeClassHelper(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassHelper(ctx, runtimeClassName)

			createPod(ctx, podName, testNamespace, runtimeClassName, "test-node",
				[]corev1.Container{{Name: "main", Image: "busybox"}},
				[]corev1.ContainerStatus{{Name: "main", ContainerID: "containerd://xyz789", Ready: true}},
			)
			defer deletePodHelper(ctx, podName, testNamespace)

			createRootfsSnapshot(ctx, snapName, testNamespace, sandboxName, []string{"main"})
			defer deleteRootfsSnapshotHelper(ctx, snapName, testNamespace)

			jobName := snapName + "-main"
			defer deleteJobHelper(ctx, jobName, testNamespace)
			defer deleteJobPodHelper(ctx, jobName, testNamespace)

			// First reconcile - creates job
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Create job pod with termination message and mark job complete
			createJobPodWithTerminationMessage(ctx, jobName, testNamespace, &snapshotpkg.UploadResult{
				SnapshotKey:  "snapshots/" + testNamespace + "/" + sandboxName + "/rev-00001/main.tar",
				Revision:     1,
				BytesWritten: 1024,
			})
			setJobComplete(ctx, jobName, testNamespace)

			// Second reconcile - updates status
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			snap := getRootfsSnapshotHelper(ctx, snapName, testNamespace)
			Expect(snap).NotTo(BeNil())

			readyCond := meta.FindStatusCondition(snap.Status.Conditions, string(sandboxv1alpha1.RootfsSnapshotComplete))
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(readyCond.Reason).To(Equal(sandboxv1alpha1.ReasonRootfsSnapshotSucceeded))

			Expect(snap.Status.CompletedAt).NotTo(BeNil())
		})

		It("should mark failed when job fails", func() {
			snapName := "snap-fail"
			sandboxName := "sandbox-fail"
			podName := sandboxName + "-pod"
			runtimeClassName := "gvisor-fail"

			createRuntimeClassHelper(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassHelper(ctx, runtimeClassName)

			createPod(ctx, podName, testNamespace, runtimeClassName, "test-node",
				[]corev1.Container{{Name: "main", Image: "busybox"}},
				[]corev1.ContainerStatus{{Name: "main", ContainerID: "containerd://fail123", Ready: true}},
			)
			defer deletePodHelper(ctx, podName, testNamespace)

			createRootfsSnapshot(ctx, snapName, testNamespace, sandboxName, []string{"main"})
			defer deleteRootfsSnapshotHelper(ctx, snapName, testNamespace)

			jobName := snapName + "-main"
			defer deleteJobHelper(ctx, jobName, testNamespace)

			// First reconcile - creates job
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Mark job failed
			setJobFailed(ctx, jobName, testNamespace, "Container not found")

			// Second reconcile - updates status
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			snap := getRootfsSnapshotHelper(ctx, snapName, testNamespace)
			Expect(snap).NotTo(BeNil())

			readyCond := meta.FindStatusCondition(snap.Status.Conditions, string(sandboxv1alpha1.RootfsSnapshotComplete))
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCond.Reason).To(Equal(sandboxv1alpha1.ReasonRootfsSnapshotFailed))

			Expect(snap.Status.CompletedAt).NotTo(BeNil())
		})
	})

	Context("Upload Result Error Handling", func() {
		It("should fail when job pod has no termination message", func() {
			snapName := "snap-no-term-msg"
			sandboxName := "sandbox-no-term-msg"
			podName := sandboxName + "-pod"
			runtimeClassName := "gvisor-no-term-msg"

			createRuntimeClassHelper(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassHelper(ctx, runtimeClassName)

			createPod(ctx, podName, testNamespace, runtimeClassName, "test-node",
				[]corev1.Container{{Name: "main", Image: "busybox"}},
				[]corev1.ContainerStatus{{Name: "main", ContainerID: "containerd://noterm123", Ready: true}},
			)
			defer deletePodHelper(ctx, podName, testNamespace)

			createRootfsSnapshot(ctx, snapName, testNamespace, sandboxName, []string{"main"})
			defer deleteRootfsSnapshotHelper(ctx, snapName, testNamespace)

			jobName := snapName + "-main"
			defer deleteJobHelper(ctx, jobName, testNamespace)

			// First reconcile - creates job
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Create job pod WITHOUT termination message (pass nil)
			createJobPodWithTerminationMessage(ctx, jobName, testNamespace, nil)
			defer deleteJobPodHelper(ctx, jobName, testNamespace)
			setJobComplete(ctx, jobName, testNamespace)

			// Second reconcile - should fail because no termination message
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			snap := getRootfsSnapshotHelper(ctx, snapName, testNamespace)
			Expect(snap).NotTo(BeNil())

			readyCond := meta.FindStatusCondition(snap.Status.Conditions, string(sandboxv1alpha1.RootfsSnapshotComplete))
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCond.Reason).To(Equal(sandboxv1alpha1.ReasonRootfsSnapshotFailed))
			Expect(readyCond.Message).To(ContainSubstring("termination message"))
		})

		It("should fail when termination message has invalid JSON", func() {
			snapName := "snap-bad-json"
			sandboxName := "sandbox-bad-json"
			podName := sandboxName + "-pod"
			runtimeClassName := "gvisor-bad-json"

			createRuntimeClassHelper(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassHelper(ctx, runtimeClassName)

			createPod(ctx, podName, testNamespace, runtimeClassName, "test-node",
				[]corev1.Container{{Name: "main", Image: "busybox"}},
				[]corev1.ContainerStatus{{Name: "main", ContainerID: "containerd://badjson123", Ready: true}},
			)
			defer deletePodHelper(ctx, podName, testNamespace)

			createRootfsSnapshot(ctx, snapName, testNamespace, sandboxName, []string{"main"})
			defer deleteRootfsSnapshotHelper(ctx, snapName, testNamespace)

			jobName := snapName + "-main"
			defer deleteJobHelper(ctx, jobName, testNamespace)

			// First reconcile - creates job
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Create job pod with invalid JSON termination message
			jobPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      jobName + "-pod",
					Namespace: testNamespace,
					Labels:    map[string]string{"job-name": jobName},
				},
				Spec: corev1.PodSpec{
					Containers:    []corev1.Container{{Name: "uploader", Image: "test"}},
					RestartPolicy: corev1.RestartPolicyNever,
				},
			}
			Expect(k8sClient.Create(ctx, jobPod)).To(Succeed())
			defer deleteJobPodHelper(ctx, jobName, testNamespace)

			jobPod.Status.Phase = corev1.PodSucceeded
			jobPod.Status.ContainerStatuses = []corev1.ContainerStatus{
				{
					Name: "uploader",
					State: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{
							ExitCode: 0,
							Message:  "this is not valid json {{{",
						},
					},
				},
			}
			Expect(k8sClient.Status().Update(ctx, jobPod)).To(Succeed())

			setJobComplete(ctx, jobName, testNamespace)

			// Second reconcile - should fail because invalid JSON
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			snap := getRootfsSnapshotHelper(ctx, snapName, testNamespace)
			Expect(snap).NotTo(BeNil())

			readyCond := meta.FindStatusCondition(snap.Status.Conditions, string(sandboxv1alpha1.RootfsSnapshotComplete))
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCond.Reason).To(Equal(sandboxv1alpha1.ReasonRootfsSnapshotFailed))
			Expect(readyCond.Message).To(ContainSubstring("parse termination message"))
		})

		It("should fail when no job pod exists", func() {
			snapName := "snap-no-job-pod"
			sandboxName := "sandbox-no-job-pod"
			podName := sandboxName + "-pod"
			runtimeClassName := "gvisor-no-job-pod"

			createRuntimeClassHelper(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassHelper(ctx, runtimeClassName)

			createPod(ctx, podName, testNamespace, runtimeClassName, "test-node",
				[]corev1.Container{{Name: "main", Image: "busybox"}},
				[]corev1.ContainerStatus{{Name: "main", ContainerID: "containerd://nojobpod123", Ready: true}},
			)
			defer deletePodHelper(ctx, podName, testNamespace)

			createRootfsSnapshot(ctx, snapName, testNamespace, sandboxName, []string{"main"})
			defer deleteRootfsSnapshotHelper(ctx, snapName, testNamespace)

			jobName := snapName + "-main"
			defer deleteJobHelper(ctx, jobName, testNamespace)

			// First reconcile - creates job
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Mark job complete WITHOUT creating a job pod
			setJobComplete(ctx, jobName, testNamespace)

			// Second reconcile - should fail because no job pod
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			snap := getRootfsSnapshotHelper(ctx, snapName, testNamespace)
			Expect(snap).NotTo(BeNil())

			readyCond := meta.FindStatusCondition(snap.Status.Conditions, string(sandboxv1alpha1.RootfsSnapshotComplete))
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCond.Reason).To(Equal(sandboxv1alpha1.ReasonRootfsSnapshotFailed))
			Expect(readyCond.Message).To(ContainSubstring("no pods found"))
		})
	})

	Context("Label Management", func() {
		It("should add sandbox label when snapshot is created without it", func() {
			snapName := "snap-no-label"
			sandboxName := "sandbox-no-label"
			podName := sandboxName + "-pod"
			runtimeClassName := "gvisor-no-label"

			createRuntimeClassHelper(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassHelper(ctx, runtimeClassName)

			createPod(ctx, podName, testNamespace, runtimeClassName, "test-node",
				[]corev1.Container{{Name: "main", Image: "busybox"}},
				[]corev1.ContainerStatus{{Name: "main", ContainerID: "containerd://nolabel123", Ready: true}},
			)
			defer deletePodHelper(ctx, podName, testNamespace)

			// Create snapshot WITHOUT the sandbox label (bypassing helper)
			snap := &sandboxv1alpha1.RootfsSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      snapName,
					Namespace: testNamespace,
					// No labels set
				},
				Spec: sandboxv1alpha1.RootfsSnapshotSpec{
					SandboxName:    sandboxName,
					ContainerNames: []string{"main"},
				},
			}
			Expect(k8sClient.Create(ctx, snap)).To(Succeed())
			defer deleteRootfsSnapshotHelper(ctx, snapName, testNamespace)
			defer deleteJobHelper(ctx, snapName+"-main", testNamespace)

			// Verify label is missing
			snap = getRootfsSnapshotHelper(ctx, snapName, testNamespace)
			Expect(snap.Labels).To(BeNil())

			// Reconcile should add the label
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify label was added
			snap = getRootfsSnapshotHelper(ctx, snapName, testNamespace)
			Expect(snap).NotTo(BeNil())
			Expect(snap.Labels).NotTo(BeNil())
			Expect(snap.Labels[LabelSandboxName]).To(Equal(sandboxName))
		})

		It("should correct sandbox label when it has wrong value", func() {
			snapName := "snap-wrong-label"
			sandboxName := "sandbox-wrong-label"
			podName := sandboxName + "-pod"
			runtimeClassName := "gvisor-wrong-label"

			createRuntimeClassHelper(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassHelper(ctx, runtimeClassName)

			createPod(ctx, podName, testNamespace, runtimeClassName, "test-node",
				[]corev1.Container{{Name: "main", Image: "busybox"}},
				[]corev1.ContainerStatus{{Name: "main", ContainerID: "containerd://wronglabel123", Ready: true}},
			)
			defer deletePodHelper(ctx, podName, testNamespace)

			// Create snapshot with WRONG sandbox label
			snap := &sandboxv1alpha1.RootfsSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      snapName,
					Namespace: testNamespace,
					Labels: map[string]string{
						LabelSandboxName: "wrong-sandbox-name",
					},
				},
				Spec: sandboxv1alpha1.RootfsSnapshotSpec{
					SandboxName:    sandboxName,
					ContainerNames: []string{"main"},
				},
			}
			Expect(k8sClient.Create(ctx, snap)).To(Succeed())
			defer deleteRootfsSnapshotHelper(ctx, snapName, testNamespace)
			defer deleteJobHelper(ctx, snapName+"-main", testNamespace)

			// Verify label has wrong value
			snap = getRootfsSnapshotHelper(ctx, snapName, testNamespace)
			Expect(snap.Labels[LabelSandboxName]).To(Equal("wrong-sandbox-name"))

			// Reconcile should correct the label
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify label was corrected
			snap = getRootfsSnapshotHelper(ctx, snapName, testNamespace)
			Expect(snap).NotTo(BeNil())
			Expect(snap.Labels[LabelSandboxName]).To(Equal(sandboxName))
		})
	})

	Context("Revision Management", func() {
		It("should increment revision for each snapshot of the same sandbox", func() {
			sandboxName := "sandbox-revision"
			podName := sandboxName + "-pod"
			runtimeClassName := "gvisor-revision"

			createRuntimeClassHelper(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassHelper(ctx, runtimeClassName)

			createPod(ctx, podName, testNamespace, runtimeClassName, "test-node",
				[]corev1.Container{{Name: "main", Image: "busybox"}},
				[]corev1.ContainerStatus{{Name: "main", ContainerID: "containerd://rev123", Ready: true}},
			)
			defer deletePodHelper(ctx, podName, testNamespace)

			// First snapshot
			snap1Name := "snap-rev-1"
			createRootfsSnapshot(ctx, snap1Name, testNamespace, sandboxName, []string{"main"})
			defer deleteRootfsSnapshotHelper(ctx, snap1Name, testNamespace)

			job1Name := snap1Name + "-main"
			defer deleteJobHelper(ctx, job1Name, testNamespace)
			defer deleteJobPodHelper(ctx, job1Name, testNamespace)

			// First reconcile creates the job
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snap1Name, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Create job pod with termination message and complete the job
			createJobPodWithTerminationMessage(ctx, job1Name, testNamespace, &snapshotpkg.UploadResult{
				SnapshotKey:  "snapshots/" + testNamespace + "/" + sandboxName + "/rev-00001/main.tar",
				Revision:     1,
				BytesWritten: 1024,
			})
			setJobComplete(ctx, job1Name, testNamespace)

			// Second reconcile processes job completion
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snap1Name, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			snap1 := getRootfsSnapshotHelper(ctx, snap1Name, testNamespace)
			Expect(snap1).NotTo(BeNil())
			Expect(snap1.Status.Revision).To(Equal(int32(1)))
			Expect(snap1.Status.ContainerSnapshots[0].SnapshotKey).To(ContainSubstring("rev-00001"))

			// Second snapshot of same sandbox
			snap2Name := "snap-rev-2"
			createRootfsSnapshot(ctx, snap2Name, testNamespace, sandboxName, []string{"main"})
			defer deleteRootfsSnapshotHelper(ctx, snap2Name, testNamespace)

			job2Name := snap2Name + "-main"
			defer deleteJobHelper(ctx, job2Name, testNamespace)
			defer deleteJobPodHelper(ctx, job2Name, testNamespace)

			// First reconcile creates the job
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snap2Name, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Create job pod with termination message and complete the job
			createJobPodWithTerminationMessage(ctx, job2Name, testNamespace, &snapshotpkg.UploadResult{
				SnapshotKey:  "snapshots/" + testNamespace + "/" + sandboxName + "/rev-00002/main.tar",
				Revision:     2,
				BytesWritten: 2048,
			})
			setJobComplete(ctx, job2Name, testNamespace)

			// Second reconcile processes job completion
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snap2Name, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			snap2 := getRootfsSnapshotHelper(ctx, snap2Name, testNamespace)
			Expect(snap2).NotTo(BeNil())
			Expect(snap2.Status.Revision).To(Equal(int32(2)))
			Expect(snap2.Status.ContainerSnapshots[0].SnapshotKey).To(ContainSubstring("rev-00002"))
		})
	})

	Context("Container Selection", func() {
		It("should fail when containerNames is empty", func() {
			snapName := "snap-auto"
			sandboxName := "sandbox-auto"
			podName := sandboxName + "-pod"
			runtimeClassName := "gvisor-auto"

			createRuntimeClassHelper(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassHelper(ctx, runtimeClassName)

			createPod(ctx, podName, testNamespace, runtimeClassName, "test-node",
				[]corev1.Container{
					{Name: "app", Image: "busybox"},
					{Name: "sidecar", Image: "busybox"},
				},
				[]corev1.ContainerStatus{
					{Name: "app", ContainerID: "containerd://app123", Ready: true},
					{Name: "sidecar", ContainerID: "containerd://sidecar456", Ready: true},
				},
			)
			defer deletePodHelper(ctx, podName, testNamespace)

			createRootfsSnapshot(ctx, snapName, testNamespace, sandboxName, nil) // nil = no containers specified
			defer deleteRootfsSnapshotHelper(ctx, snapName, testNamespace)

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			snap := getRootfsSnapshotHelper(ctx, snapName, testNamespace)
			Expect(snap).NotTo(BeNil())

			readyCond := meta.FindStatusCondition(snap.Status.Conditions, string(sandboxv1alpha1.RootfsSnapshotComplete))
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCond.Reason).To(Equal(sandboxv1alpha1.ReasonRootfsSnapshotFailed))
			Expect(readyCond.Message).To(ContainSubstring("No containers found"))
		})

		It("should use first specified container when containerNames is provided", func() {
			snapName := "snap-specified"
			sandboxName := "sandbox-specified"
			podName := sandboxName + "-pod"
			runtimeClassName := "gvisor-specified"

			createRuntimeClassHelper(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassHelper(ctx, runtimeClassName)

			createPod(ctx, podName, testNamespace, runtimeClassName, "test-node",
				[]corev1.Container{
					{Name: "a", Image: "busybox"},
					{Name: "b", Image: "busybox"},
				},
				[]corev1.ContainerStatus{
					{Name: "a", ContainerID: "containerd://a111", Ready: true},
					{Name: "b", ContainerID: "containerd://b222", Ready: true},
				},
			)
			defer deletePodHelper(ctx, podName, testNamespace)

			// Request specific containers - controller uses first one
			createRootfsSnapshot(ctx, snapName, testNamespace, sandboxName, []string{"b", "a"})
			defer deleteRootfsSnapshotHelper(ctx, snapName, testNamespace)
			defer deleteJobHelper(ctx, snapName+"-b", testNamespace)

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			snap := getRootfsSnapshotHelper(ctx, snapName, testNamespace)
			Expect(snap).NotTo(BeNil())
			Expect(snap.Status.ContainerSnapshots).To(HaveLen(1))
			Expect(snap.Status.ContainerSnapshots[0].ContainerName).To(Equal("b"))
		})
	})

	Context("TTL Cleanup", func() {
		It("should delete RootfsSnapshot after TTL expires", func() {
			snapName := "snap-ttl"
			sandboxName := "sandbox-ttl"
			podName := sandboxName + "-pod"
			runtimeClassName := "gvisor-ttl"

			createRuntimeClassHelper(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassHelper(ctx, runtimeClassName)

			createPod(ctx, podName, testNamespace, runtimeClassName, "test-node",
				[]corev1.Container{{Name: "main", Image: "busybox"}},
				[]corev1.ContainerStatus{{Name: "main", ContainerID: "containerd://ttl123", Ready: true}},
			)
			defer deletePodHelper(ctx, podName, testNamespace)

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
			defer deleteRootfsSnapshotHelper(ctx, snapName, testNamespace)

			jobName := snapName + "-main"
			defer deleteJobHelper(ctx, jobName, testNamespace)

			// First reconcile - creates job
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Mark job complete
			setJobComplete(ctx, jobName, testNamespace)

			// Second reconcile - marks complete
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify completion
			snap = getRootfsSnapshotHelper(ctx, snapName, testNamespace)
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
				snap := getRootfsSnapshotHelper(ctx, snapName, testNamespace)
				return snap == nil || !snap.DeletionTimestamp.IsZero()
			}, testTimeout, testInterval).Should(BeTrue())
		})

		It("should not delete before TTL expires", func() {
			snapName := "snap-ttl-wait"
			sandboxName := "sandbox-ttl-wait"
			podName := sandboxName + "-pod"
			runtimeClassName := "gvisor-ttl-wait"

			createRuntimeClassHelper(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassHelper(ctx, runtimeClassName)

			createPod(ctx, podName, testNamespace, runtimeClassName, "test-node",
				[]corev1.Container{{Name: "main", Image: "busybox"}},
				[]corev1.ContainerStatus{{Name: "main", ContainerID: "containerd://wait123", Ready: true}},
			)
			defer deletePodHelper(ctx, podName, testNamespace)

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
			defer deleteRootfsSnapshotHelper(ctx, snapName, testNamespace)

			jobName := snapName + "-main"
			defer deleteJobHelper(ctx, jobName, testNamespace)

			// Reconcile to create and complete job
			_, _ = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			setJobComplete(ctx, jobName, testNamespace)
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
			snap = getRootfsSnapshotHelper(ctx, snapName, testNamespace)
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

			createRuntimeClassHelper(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassHelper(ctx, runtimeClassName)

			createPod(ctx, podName, testNamespace, runtimeClassName, "test-node",
				[]corev1.Container{{Name: "main", Image: "busybox"}},
				[]corev1.ContainerStatus{{Name: "main", ContainerID: "containerd://cleanup123", Ready: true}},
			)
			defer deletePodHelper(ctx, podName, testNamespace)

			createRootfsSnapshot(ctx, snapName, testNamespace, sandboxName, []string{"main"})

			jobName := snapName + "-main"
			defer deleteJobHelper(ctx, jobName, testNamespace)

			// Reconcile to create job
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify job exists
			job := getJobHelper(ctx, jobName, testNamespace)
			Expect(job).NotTo(BeNil())

			// Verify owner reference is set correctly - this ensures K8s GC will clean up the job
			// when the RootfsSnapshot is deleted (defer cleanup only needed because EnvTest lacks GC)
			Expect(job.OwnerReferences).To(HaveLen(1))
			Expect(job.OwnerReferences[0].Kind).To(Equal("RootfsSnapshot"))
			Expect(job.OwnerReferences[0].Name).To(Equal(snapName))
			Expect(job.OwnerReferences[0].Controller).NotTo(BeNil())
			Expect(*job.OwnerReferences[0].Controller).To(BeTrue())

			// Delete RootfsSnapshot
			snap := getRootfsSnapshotHelper(ctx, snapName, testNamespace)
			Expect(k8sClient.Delete(ctx, snap)).To(Succeed())

			// Reconcile during deletion
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify RootfsSnapshot is gone
			Eventually(func() bool {
				snap := getRootfsSnapshotHelper(ctx, snapName, testNamespace)
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

			createRuntimeClassHelper(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassHelper(ctx, runtimeClassName)

			createPod(ctx, podName, testNamespace, runtimeClassName, "test-node",
				[]corev1.Container{{Name: "main", Image: "busybox"}},
				[]corev1.ContainerStatus{{Name: "main", ContainerID: "containerd://deadline123", Ready: true}},
			)
			defer deletePodHelper(ctx, podName, testNamespace)

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
			defer deleteRootfsSnapshotHelper(ctx, snapName, testNamespace)

			jobName := snapName + "-main"
			defer deleteJobHelper(ctx, jobName, testNamespace)

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			job := getJobHelper(ctx, jobName, testNamespace)
			Expect(job).NotTo(BeNil())
			Expect(job.Spec.ActiveDeadlineSeconds).NotTo(BeNil())
			Expect(*job.Spec.ActiveDeadlineSeconds).To(Equal(customDeadline))
		})

		It("should use default activeDeadlineSeconds when not specified", func() {
			snapName := "rfs-snap-default-deadline"
			sandboxName := "rfs-sandbox-default-deadline"
			podName := sandboxName + "-pod"
			runtimeClassName := "rfs-gvisor-default-deadline"

			createRuntimeClassHelper(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassHelper(ctx, runtimeClassName)

			createPod(ctx, podName, testNamespace, runtimeClassName, "test-node",
				[]corev1.Container{{Name: "main", Image: "busybox"}},
				[]corev1.ContainerStatus{{Name: "main", ContainerID: "containerd://default123", Ready: true}},
			)
			defer deletePodHelper(ctx, podName, testNamespace)

			createRootfsSnapshot(ctx, snapName, testNamespace, sandboxName, []string{"main"})
			defer deleteRootfsSnapshotHelper(ctx, snapName, testNamespace)

			jobName := snapName + "-main"
			defer deleteJobHelper(ctx, jobName, testNamespace)

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			job := getJobHelper(ctx, jobName, testNamespace)
			Expect(job).NotTo(BeNil())
			Expect(job.Spec.ActiveDeadlineSeconds).NotTo(BeNil())
			Expect(*job.Spec.ActiveDeadlineSeconds).To(Equal(defaultActiveDeadlineSecondsSnapshot))
		})
	})
})
