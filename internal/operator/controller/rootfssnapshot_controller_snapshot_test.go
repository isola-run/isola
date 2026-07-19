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
	"k8s.io/apimachinery/pkg/api/resource"
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

	Context("Single Container Snapshot", func() {
		It("should create job for single container with two-container pattern", func() {
			snapName := "snap-single"
			sandboxName := "sandbox-single"
			podName := sandboxName + "-pod"
			runtimeClassName := "gvisor-single"

			createRuntimeClassForSnapshot(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassForSnapshot(ctx, runtimeClassName)

			createSnapshotPod(ctx, podName, runtimeClassName,
				[]corev1.Container{{Name: "sandbox", Image: "busybox"}},
				[]corev1.ContainerStatus{{Name: "sandbox", ContainerID: "containerd://abc123", Ready: true}},
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

			// Verify job was created with two-container pattern
			job := getSnapshotJob(ctx, jobName)
			Expect(job).NotTo(BeNil())

			// Init container should be snapshotter (runs runsc tar)
			Expect(job.Spec.Template.Spec.InitContainers).To(HaveLen(1))
			Expect(job.Spec.Template.Spec.InitContainers[0].Name).To(Equal("snapshotter"))

			// Main container should be uploader (uploads to bucket)
			Expect(job.Spec.Template.Spec.Containers).To(HaveLen(1))
			Expect(job.Spec.Template.Spec.Containers[0].Name).To(Equal("snapshot-uploader"))
			Expect(job.Spec.Template.Spec.Containers[0].Image).To(Equal("isola-snapshot-uploader:test"))

			// Verify uploader ImagePullPolicy propagates from reconciler config
			reconciler.UploaderImagePullPolicy = corev1.PullAlways
			defer func() { reconciler.UploaderImagePullPolicy = "" }()

			snapName2 := "snap-pullpolicy"
			createRootfsSnapshotCR(ctx, snapName2, sandboxName)
			defer deleteRootfsSnapshotCR(ctx, snapName2)

			jobName2 := snapName2 + "-job"
			defer deleteSnapshotJob(ctx, jobName2)

			_, err2 := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName2, Namespace: testNamespace},
			})
			Expect(err2).NotTo(HaveOccurred())

			job2 := getSnapshotJob(ctx, jobName2)
			Expect(job2.Spec.Template.Spec.Containers[0].ImagePullPolicy).To(Equal(corev1.PullAlways))

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

			// Verify snapshot status (SnapshotKey not set until job completes)
			snap := getRootfsSnapshotCR(ctx, snapName)
			Expect(snap).NotTo(BeNil())
			Expect(snap.Status.ContainerID).To(Equal("abc123"))
			// SnapshotKey is empty until job completes with termination message
			Expect(snap.Status.SnapshotKey).To(BeEmpty())
		})

		It("should mark complete when job succeeds", func() {
			snapName := "snap-success"
			sandboxName := "sandbox-success"
			podName := sandboxName + "-pod"
			runtimeClassName := "gvisor-success"

			createRuntimeClassForSnapshot(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassForSnapshot(ctx, runtimeClassName)

			createSnapshotPod(ctx, podName, runtimeClassName,
				[]corev1.Container{{Name: "main", Image: "busybox"}},
				[]corev1.ContainerStatus{{Name: "main", ContainerID: "containerd://xyz789", Ready: true}},
			)
			defer deleteSnapshotPod(ctx, podName)

			createRootfsSnapshotCR(ctx, snapName, sandboxName)
			defer deleteRootfsSnapshotCR(ctx, snapName)

			jobName := snapName + "-job"
			defer deleteSnapshotJob(ctx, jobName)
			defer deleteSnapshotJobPod(ctx, jobName)

			// First reconcile - creates job
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Create job pod with termination message and mark job complete
			createSnapshotJobPodWithTerminationMessage(ctx, jobName, &snapshotpkg.UploadResult{
				SnapshotKey:  "rootfssnapshots/" + testNamespace + "/" + sandboxName + ".tar",
				BytesWritten: 1024,
			})
			setSnapshotJobComplete(ctx, jobName)

			// Second reconcile - updates status
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			snap := getRootfsSnapshotCR(ctx, snapName)
			Expect(snap).NotTo(BeNil())

			readyCond := meta.FindStatusCondition(snap.Status.Conditions, sandboxv1alpha1.RootfsSnapshotSucceededCondition)
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(readyCond.Reason).To(Equal(sandboxv1alpha1.ReasonRootfsSnapshotSucceeded))

			Expect(snap.Status.CompletionTime).NotTo(BeNil())
		})

		It("should mark failed when job fails", func() {
			snapName := "snap-fail"
			sandboxName := "sandbox-fail"
			podName := sandboxName + "-pod"
			runtimeClassName := "gvisor-fail"

			createRuntimeClassForSnapshot(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassForSnapshot(ctx, runtimeClassName)

			createSnapshotPod(ctx, podName, runtimeClassName,
				[]corev1.Container{{Name: "main", Image: "busybox"}},
				[]corev1.ContainerStatus{{Name: "main", ContainerID: "containerd://fail123", Ready: true}},
			)
			defer deleteSnapshotPod(ctx, podName)

			createRootfsSnapshotCR(ctx, snapName, sandboxName)
			defer deleteRootfsSnapshotCR(ctx, snapName)

			jobName := snapName + "-job"
			defer deleteSnapshotJob(ctx, jobName)

			// First reconcile - creates job
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Mark job failed
			setSnapshotJobFailed(ctx, jobName, "Container not found")

			// Second reconcile - updates status
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			snap := getRootfsSnapshotCR(ctx, snapName)
			Expect(snap).NotTo(BeNil())

			failedCond := meta.FindStatusCondition(snap.Status.Conditions, sandboxv1alpha1.RootfsSnapshotSucceededCondition)
			Expect(failedCond).NotTo(BeNil())
			Expect(failedCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(failedCond.Reason).To(Equal(sandboxv1alpha1.ReasonRootfsSnapshotFailed))

			Expect(snap.Status.CompletionTime).NotTo(BeNil())
		})
	})

	Context("Job Configuration", func() {
		It("should include credential secret EnvFrom when configured", func() {
			snapName := "snap-with-creds"
			sandboxName := "sandbox-with-creds"
			podName := sandboxName + "-pod"
			runtimeClassName := "gvisor-with-creds"

			createRuntimeClassForSnapshot(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassForSnapshot(ctx, runtimeClassName)

			createSnapshotPod(ctx, podName, runtimeClassName,
				[]corev1.Container{{Name: "main", Image: "busybox"}},
				[]corev1.ContainerStatus{{Name: "main", ContainerID: "containerd://creds123", Ready: true}},
			)
			defer deleteSnapshotPod(ctx, podName)

			createRootfsSnapshotCR(ctx, snapName, sandboxName)
			defer deleteRootfsSnapshotCR(ctx, snapName)

			jobName := snapName + "-job"
			defer deleteSnapshotJob(ctx, jobName)

			// Use reconciler with credential secret configured
			credsReconciler := &RootfsSnapshotReconciler{
				Client:                 k8sClient,
				Scheme:                 k8sClient.Scheme(),
				Recorder:               recorder,
				Clock:                  fakeClock,
				BucketURL:              "s3://test-bucket?region=us-east-1",
				UploaderImage:          "isola-snapshot-uploader:test",
				CredentialSecretName:   "cloud-credentials",
				SnapshotServiceAccount: "test-snapshot-sa",
				Enabled:                true,
				GvisorRunscPath:        "/usr/local/bin/runsc",
				GvisorRunscRoot:        "/run/containerd/runsc/k8s.io",
			}

			_, err := credsReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			job := getSnapshotJob(ctx, jobName)
			Expect(job).NotTo(BeNil())

			// Verify uploader container has EnvFrom with secret reference
			uploaderContainer := job.Spec.Template.Spec.Containers[0]
			Expect(uploaderContainer.Name).To(Equal("snapshot-uploader"))
			Expect(uploaderContainer.EnvFrom).To(HaveLen(1))
			Expect(uploaderContainer.EnvFrom[0].SecretRef).NotTo(BeNil())
			Expect(uploaderContainer.EnvFrom[0].SecretRef.Name).To(Equal("cloud-credentials"))
		})

		It("should not include credential EnvFrom when not configured", func() {
			snapName := "snap-no-creds"
			sandboxName := "sandbox-no-creds"
			podName := sandboxName + "-pod"
			runtimeClassName := "gvisor-no-creds"

			createRuntimeClassForSnapshot(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassForSnapshot(ctx, runtimeClassName)

			createSnapshotPod(ctx, podName, runtimeClassName,
				[]corev1.Container{{Name: "main", Image: "busybox"}},
				[]corev1.ContainerStatus{{Name: "main", ContainerID: "containerd://nocreds123", Ready: true}},
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

			// Verify uploader container has no EnvFrom
			uploaderContainer := job.Spec.Template.Spec.Containers[0]
			Expect(uploaderContainer.EnvFrom).To(BeEmpty())
		})

		It("should use container ephemeral storage limit for snapshot volume", func() {
			snapName := "snap-storage-limit"
			sandboxName := "sandbox-storage-limit"
			podName := sandboxName + "-pod"
			runtimeClassName := "gvisor-storage-limit"

			createRuntimeClassForSnapshot(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassForSnapshot(ctx, runtimeClassName)

			// Create pod with ephemeral storage limit on the container
			storageLimit := resource.MustParse("2Gi")
			createSnapshotPod(ctx, podName, runtimeClassName,
				[]corev1.Container{{
					Name:  "main",
					Image: "busybox",
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceEphemeralStorage: storageLimit,
						},
					},
				}},
				[]corev1.ContainerStatus{{Name: "main", ContainerID: "containerd://storage123", Ready: true}},
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

			// Find snapshot-data volume and verify its size limit matches container's ephemeral storage limit
			var snapshotDataVolume *corev1.Volume
			for i := range job.Spec.Template.Spec.Volumes {
				if job.Spec.Template.Spec.Volumes[i].Name == "snapshot-data" {
					snapshotDataVolume = &job.Spec.Template.Spec.Volumes[i]
					break
				}
			}
			Expect(snapshotDataVolume).NotTo(BeNil())
			Expect(snapshotDataVolume.EmptyDir).NotTo(BeNil())
			Expect(snapshotDataVolume.EmptyDir.SizeLimit).NotTo(BeNil())
			Expect(snapshotDataVolume.EmptyDir.SizeLimit.String()).To(Equal("2Gi"))
		})

		It("should use default size limit when container has no ephemeral storage limit", func() {
			snapName := "snap-default-limit"
			sandboxName := "sandbox-default-limit"
			podName := sandboxName + "-pod"
			runtimeClassName := "gvisor-default-limit"

			createRuntimeClassForSnapshot(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassForSnapshot(ctx, runtimeClassName)

			// Create pod WITHOUT ephemeral storage limit
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

			// Find snapshot-data volume and verify it uses the default limit (1Gi)
			var snapshotDataVolume *corev1.Volume
			for i := range job.Spec.Template.Spec.Volumes {
				if job.Spec.Template.Spec.Volumes[i].Name == "snapshot-data" {
					snapshotDataVolume = &job.Spec.Template.Spec.Volumes[i]
					break
				}
			}
			Expect(snapshotDataVolume).NotTo(BeNil())
			Expect(snapshotDataVolume.EmptyDir).NotTo(BeNil())
			Expect(snapshotDataVolume.EmptyDir.SizeLimit).NotTo(BeNil())
			Expect(snapshotDataVolume.EmptyDir.SizeLimit.String()).To(Equal("1Gi"))
		})

		It("should be idempotent when job already exists", func() {
			snapName := "snap-idempotent"
			sandboxName := "sandbox-idempotent"
			podName := sandboxName + "-pod"
			runtimeClassName := "gvisor-idempotent"

			createRuntimeClassForSnapshot(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassForSnapshot(ctx, runtimeClassName)

			createSnapshotPod(ctx, podName, runtimeClassName,
				[]corev1.Container{{Name: "main", Image: "busybox"}},
				[]corev1.ContainerStatus{{Name: "main", ContainerID: "containerd://idem123", Ready: true}},
			)
			defer deleteSnapshotPod(ctx, podName)

			createRootfsSnapshotCR(ctx, snapName, sandboxName)
			defer deleteRootfsSnapshotCR(ctx, snapName)

			jobName := snapName + "-job"
			defer deleteSnapshotJob(ctx, jobName)

			// First reconcile - creates job
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			job := getSnapshotJob(ctx, jobName)
			Expect(job).NotTo(BeNil())
			originalUID := job.UID

			// Second reconcile - job already exists, should not error
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify same job exists (not recreated)
			job = getSnapshotJob(ctx, jobName)
			Expect(job).NotTo(BeNil())
			Expect(job.UID).To(Equal(originalUID))
		})
	})

	Context("Upload Result Error Handling", func() {
		It("should fail when job pod has no termination message", func() {
			snapName := "snap-no-term-msg"
			sandboxName := "sandbox-no-term-msg"
			podName := sandboxName + "-pod"
			runtimeClassName := "gvisor-no-term-msg"

			createRuntimeClassForSnapshot(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassForSnapshot(ctx, runtimeClassName)

			createSnapshotPod(ctx, podName, runtimeClassName,
				[]corev1.Container{{Name: "main", Image: "busybox"}},
				[]corev1.ContainerStatus{{Name: "main", ContainerID: "containerd://noterm123", Ready: true}},
			)
			defer deleteSnapshotPod(ctx, podName)

			createRootfsSnapshotCR(ctx, snapName, sandboxName)
			defer deleteRootfsSnapshotCR(ctx, snapName)

			jobName := snapName + "-job"
			defer deleteSnapshotJob(ctx, jobName)

			// First reconcile - creates job
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Create job pod WITHOUT termination message (pass nil)
			createSnapshotJobPodWithTerminationMessage(ctx, jobName, nil)
			defer deleteSnapshotJobPod(ctx, jobName)
			setSnapshotJobComplete(ctx, jobName)

			// Second reconcile - should fail because no termination message
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			snap := getRootfsSnapshotCR(ctx, snapName)
			Expect(snap).NotTo(BeNil())

			failedCond := meta.FindStatusCondition(snap.Status.Conditions, sandboxv1alpha1.RootfsSnapshotSucceededCondition)
			Expect(failedCond).NotTo(BeNil())
			Expect(failedCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(failedCond.Reason).To(Equal(sandboxv1alpha1.ReasonRootfsSnapshotFailed))
			Expect(failedCond.Message).To(ContainSubstring("termination message"))
		})

		It("should fail when termination message has invalid JSON", func() {
			snapName := "snap-bad-json"
			sandboxName := "sandbox-bad-json"
			podName := sandboxName + "-pod"
			runtimeClassName := "gvisor-bad-json"

			createRuntimeClassForSnapshot(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassForSnapshot(ctx, runtimeClassName)

			createSnapshotPod(ctx, podName, runtimeClassName,
				[]corev1.Container{{Name: "main", Image: "busybox"}},
				[]corev1.ContainerStatus{{Name: "main", ContainerID: "containerd://badjson123", Ready: true}},
			)
			defer deleteSnapshotPod(ctx, podName)

			createRootfsSnapshotCR(ctx, snapName, sandboxName)
			defer deleteRootfsSnapshotCR(ctx, snapName)

			jobName := snapName + "-job"
			defer deleteSnapshotJob(ctx, jobName)

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
					Containers:    []corev1.Container{{Name: "snapshot-uploader", Image: "test"}},
					RestartPolicy: corev1.RestartPolicyNever,
				},
			}
			Expect(k8sClient.Create(ctx, jobPod)).To(Succeed())
			defer deleteSnapshotJobPod(ctx, jobName)

			jobPod.Status.Phase = corev1.PodSucceeded
			jobPod.Status.ContainerStatuses = []corev1.ContainerStatus{
				{
					Name: "snapshot-uploader",
					State: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{
							ExitCode: 0,
							Message:  "this is not valid json {{{",
						},
					},
				},
			}
			Expect(k8sClient.Status().Update(ctx, jobPod)).To(Succeed())

			setSnapshotJobComplete(ctx, jobName)

			// Second reconcile - should fail because invalid JSON
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			snap := getRootfsSnapshotCR(ctx, snapName)
			Expect(snap).NotTo(BeNil())

			failedCond := meta.FindStatusCondition(snap.Status.Conditions, sandboxv1alpha1.RootfsSnapshotSucceededCondition)
			Expect(failedCond).NotTo(BeNil())
			Expect(failedCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(failedCond.Reason).To(Equal(sandboxv1alpha1.ReasonRootfsSnapshotFailed))
			Expect(failedCond.Message).To(ContainSubstring("parse termination message"))
		})

		It("should requeue (not fail) when no job pod is observed yet", func() {
			// A completed job whose pod has not yet propagated to the informer
			// cache, or was GC'd after a node scale-down, must not be treated as
			// a permanent failure: the tar is already uploaded. The controller
			// should requeue and retry rather than marking the snapshot failed.
			snapName := "snap-no-job-pod"
			sandboxName := "sandbox-no-job-pod"
			podName := sandboxName + "-pod"
			runtimeClassName := "gvisor-no-job-pod"

			createRuntimeClassForSnapshot(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassForSnapshot(ctx, runtimeClassName)

			createSnapshotPod(ctx, podName, runtimeClassName,
				[]corev1.Container{{Name: "main", Image: "busybox"}},
				[]corev1.ContainerStatus{{Name: "main", ContainerID: "containerd://nojobpod123", Ready: true}},
			)
			defer deleteSnapshotPod(ctx, podName)

			createRootfsSnapshotCR(ctx, snapName, sandboxName)
			defer deleteRootfsSnapshotCR(ctx, snapName)

			jobName := snapName + "-job"
			defer deleteSnapshotJob(ctx, jobName)

			// First reconcile - creates job
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Mark job complete WITHOUT creating a job pod
			setSnapshotJobComplete(ctx, jobName)

			// Second reconcile - should requeue, not fail
			result, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeNumerically(">", 0))

			snap := getRootfsSnapshotCR(ctx, snapName)
			Expect(snap).NotTo(BeNil())
			Expect(snap.Status.CompletionTime).To(BeNil())
			Expect(meta.FindStatusCondition(snap.Status.Conditions, sandboxv1alpha1.RootfsSnapshotSucceededCondition)).To(BeNil())
		})

		It("should requeue (not fail) when the uploader container is not terminated yet", func() {
			// The job's completion condition can be visible in the cache before
			// the job pod's container status reflects termination. Reading the
			// termination message here is a transient miss, not a failure.
			snapName := "snap-not-terminated"
			sandboxName := "sandbox-not-terminated"
			podName := sandboxName + "-pod"
			runtimeClassName := "gvisor-not-terminated"

			createRuntimeClassForSnapshot(ctx, runtimeClassName, "runsc")
			defer deleteRuntimeClassForSnapshot(ctx, runtimeClassName)

			createSnapshotPod(ctx, podName, runtimeClassName,
				[]corev1.Container{{Name: "main", Image: "busybox"}},
				[]corev1.ContainerStatus{{Name: "main", ContainerID: "containerd://notterm123", Ready: true}},
			)
			defer deleteSnapshotPod(ctx, podName)

			createRootfsSnapshotCR(ctx, snapName, sandboxName)
			defer deleteRootfsSnapshotCR(ctx, snapName)

			jobName := snapName + "-job"
			defer deleteSnapshotJob(ctx, jobName)

			// First reconcile - creates job
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Create job pod whose uploader container is still running (not terminated)
			jobPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      jobName + "-pod",
					Namespace: testNamespace,
					Labels:    map[string]string{"job-name": jobName},
				},
				Spec: corev1.PodSpec{
					Containers:    []corev1.Container{{Name: "snapshot-uploader", Image: "test"}},
					RestartPolicy: corev1.RestartPolicyNever,
				},
			}
			Expect(k8sClient.Create(ctx, jobPod)).To(Succeed())
			defer deleteSnapshotJobPod(ctx, jobName)

			jobPod.Status.Phase = corev1.PodRunning
			jobPod.Status.ContainerStatuses = []corev1.ContainerStatus{
				{
					Name:  "snapshot-uploader",
					State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
				},
			}
			Expect(k8sClient.Status().Update(ctx, jobPod)).To(Succeed())

			setSnapshotJobComplete(ctx, jobName)

			// Second reconcile - should requeue, not fail
			result, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: snapName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeNumerically(">", 0))

			snap := getRootfsSnapshotCR(ctx, snapName)
			Expect(snap).NotTo(BeNil())
			Expect(snap.Status.CompletionTime).To(BeNil())
			Expect(meta.FindStatusCondition(snap.Status.Conditions, sandboxv1alpha1.RootfsSnapshotSucceededCondition)).To(BeNil())
		})
	})
})
