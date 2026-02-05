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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	sandboxv1alpha1 "github.com/isola-ai/isola-sb/api/v1alpha1"
)

var _ = Describe("Sandbox Controller - Restore", func() {
	var (
		ctx        context.Context
		reconciler *SandboxReconciler
	)

	BeforeEach(func() {
		ctx = context.Background()
		reconciler = &SandboxReconciler{
			Client:               k8sClient,
			Scheme:               k8sClient.Scheme(),
			SandboxSidecarImage:  "test-sidecar:latest",
			RestorerImage:        "test-restorer:latest",
			BucketURL:            "s3://test-bucket?region=us-east-1",
			CredentialSecretName: "test-credentials",
		}
	})

	Describe("resolveSnapshotKey", func() {
		It("should return direct snapshotKey when provided", func() {
			restoreFrom := &sandboxv1alpha1.RestoreFromSnapshot{
				SnapshotKey:   "snapshots/ns/sandbox/rev-00001/main.tar",
				ContainerName: "main",
			}

			key, err := reconciler.resolveSnapshotKey(ctx, testNamespace, restoreFrom)
			Expect(err).NotTo(HaveOccurred())
			Expect(key).To(Equal("snapshots/ns/sandbox/rev-00001/main.tar"))
		})

		It("should resolve snapshotKey from RootfsSnapshot CR", func() {
			// Create a completed RootfsSnapshot
			snap := &sandboxv1alpha1.RootfsSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-snapshot",
					Namespace: testNamespace,
				},
				Spec: sandboxv1alpha1.RootfsSnapshotSpec{
					SandboxName:    "test-sandbox",
					ContainerNames: []string{"main"},
				},
			}
			Expect(k8sClient.Create(ctx, snap)).To(Succeed())

			// Set it to complete with a snapshot key
			snap.Status.Conditions = []metav1.Condition{
				{
					Type:   string(sandboxv1alpha1.RootfsSnapshotComplete),
					Status: metav1.ConditionTrue,
					Reason: sandboxv1alpha1.ReasonRootfsSnapshotSucceeded,
				},
			}
			snap.Status.ContainerSnapshots = []sandboxv1alpha1.ContainerSnapshotStatus{
				{
					ContainerName: "main",
					SnapshotKey:   "snapshots/test-ns/test-sandbox/rev-00001/main.tar",
				},
			}
			Expect(k8sClient.Status().Update(ctx, snap)).To(Succeed())

			restoreFrom := &sandboxv1alpha1.RestoreFromSnapshot{
				SnapshotName:  "test-snapshot",
				ContainerName: "main",
			}

			key, err := reconciler.resolveSnapshotKey(ctx, testNamespace, restoreFrom)
			Expect(err).NotTo(HaveOccurred())
			Expect(key).To(Equal("snapshots/test-ns/test-sandbox/rev-00001/main.tar"))

			// Cleanup
			Expect(k8sClient.Delete(ctx, snap)).To(Succeed())
		})

		It("should fail when RootfsSnapshot is not complete", func() {
			// Create an incomplete RootfsSnapshot
			snap := &sandboxv1alpha1.RootfsSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "incomplete-snapshot",
					Namespace: testNamespace,
				},
				Spec: sandboxv1alpha1.RootfsSnapshotSpec{
					SandboxName:    "test-sandbox",
					ContainerNames: []string{"main"},
				},
			}
			Expect(k8sClient.Create(ctx, snap)).To(Succeed())

			restoreFrom := &sandboxv1alpha1.RestoreFromSnapshot{
				SnapshotName:  "incomplete-snapshot",
				ContainerName: "main",
			}

			_, err := reconciler.resolveSnapshotKey(ctx, testNamespace, restoreFrom)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not complete"))

			// Cleanup
			Expect(k8sClient.Delete(ctx, snap)).To(Succeed())
		})

		It("should fail when container not found in snapshot", func() {
			// Create a completed RootfsSnapshot without the requested container
			snap := &sandboxv1alpha1.RootfsSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "wrong-container-snapshot",
					Namespace: testNamespace,
				},
				Spec: sandboxv1alpha1.RootfsSnapshotSpec{
					SandboxName:    "test-sandbox",
					ContainerNames: []string{"other"},
				},
			}
			Expect(k8sClient.Create(ctx, snap)).To(Succeed())

			snap.Status.Conditions = []metav1.Condition{
				{
					Type:   string(sandboxv1alpha1.RootfsSnapshotComplete),
					Status: metav1.ConditionTrue,
					Reason: sandboxv1alpha1.ReasonRootfsSnapshotSucceeded,
				},
			}
			snap.Status.ContainerSnapshots = []sandboxv1alpha1.ContainerSnapshotStatus{
				{
					ContainerName: "other",
					SnapshotKey:   "snapshots/test-ns/test-sandbox/rev-00001/other.tar",
				},
			}
			Expect(k8sClient.Status().Update(ctx, snap)).To(Succeed())

			restoreFrom := &sandboxv1alpha1.RestoreFromSnapshot{
				SnapshotName:  "wrong-container-snapshot",
				ContainerName: "main",
			}

			_, err := reconciler.resolveSnapshotKey(ctx, testNamespace, restoreFrom)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no snapshot for container"))

			// Cleanup
			Expect(k8sClient.Delete(ctx, snap)).To(Succeed())
		})
	})

	Describe("buildRestorerContainer", func() {
		It("should create restorer container with correct configuration", func() {
			container := reconciler.buildRestorerContainer("snapshots/ns/sandbox/rev-00001/main.tar")

			Expect(container.Name).To(Equal(restorerContainerName))
			Expect(container.Image).To(Equal("test-restorer:latest"))

			// Check environment variables
			var bucketURL, snapshotKey, restoreFile string
			for _, env := range container.Env {
				switch env.Name {
				case "ISOLA_BUCKET_URL":
					bucketURL = env.Value
				case "RESTORE_SNAPSHOT_KEY":
					snapshotKey = env.Value
				case "RESTORE_FILE":
					restoreFile = env.Value
				}
			}
			Expect(bucketURL).To(Equal("s3://test-bucket?region=us-east-1"))
			Expect(snapshotKey).To(Equal("snapshots/ns/sandbox/rev-00001/main.tar"))
			Expect(restoreFile).To(Equal(restoreTarPath))

			// Check credential secret reference
			Expect(container.EnvFrom).To(HaveLen(1))
			Expect(container.EnvFrom[0].SecretRef.Name).To(Equal("test-credentials"))

			// Check security context
			Expect(*container.SecurityContext.RunAsNonRoot).To(BeTrue())
			Expect(*container.SecurityContext.ReadOnlyRootFilesystem).To(BeTrue())
			Expect(*container.SecurityContext.AllowPrivilegeEscalation).To(BeFalse())
		})

		It("should not include credential secret when not configured", func() {
			r := &SandboxReconciler{
				RestorerImage:        "test-restorer:latest",
				BucketURL:            "s3://test-bucket",
				CredentialSecretName: "", // Empty
			}

			container := r.buildRestorerContainer("snapshots/ns/sandbox/rev-00001/main.tar")
			Expect(container.EnvFrom).To(BeEmpty())
		})
	})

	Describe("configureContainerForRestore", func() {
		It("should wrap command when container has explicit command", func() {
			container := &corev1.Container{
				Name:    "main",
				Command: []string{"/bin/app"},
				Args:    []string{"--flag", "value"},
			}

			configureContainerForRestore(container)

			Expect(container.Command).To(Equal([]string{"/bin/sh", "-c"}))
			Expect(container.Args).To(HaveLen(1))
			Expect(container.Args[0]).To(ContainSubstring("tar xf"))
			Expect(container.Args[0]).To(ContainSubstring("/bin/app"))
			Expect(container.Args[0]).To(ContainSubstring("--flag"))

			// Check volume mount was added
			Expect(container.VolumeMounts).To(ContainElement(
				corev1.VolumeMount{
					Name:      restoreVolumeName,
					MountPath: "/restore-data",
					ReadOnly:  true,
				},
			))
		})

		It("should add lifecycle hook when container has no explicit command", func() {
			container := &corev1.Container{
				Name:  "main",
				Image: "nginx:latest",
				// No Command set
			}

			configureContainerForRestore(container)

			Expect(container.Command).To(BeEmpty())
			Expect(container.Lifecycle).NotTo(BeNil())
			Expect(container.Lifecycle.PostStart).NotTo(BeNil())
			Expect(container.Lifecycle.PostStart.Exec.Command).To(ContainElement(ContainSubstring("tar xf")))

			// Check volume mount was added
			Expect(container.VolumeMounts).To(ContainElement(
				corev1.VolumeMount{
					Name:      restoreVolumeName,
					MountPath: "/restore-data",
					ReadOnly:  true,
				},
			))
		})

		It("should handle commands with special characters", func() {
			container := &corev1.Container{
				Name:    "main",
				Command: []string{"/bin/sh", "-c"},
				Args:    []string{"echo 'hello world' && sleep infinity"},
			}

			configureContainerForRestore(container)

			// The original command should be preserved in escaped form
			Expect(container.Args[0]).To(ContainSubstring("echo"))
		})
	})

	Describe("injectRestorer", func() {
		It("should inject restorer init container and configure target container", func() {
			pod := &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:    "main",
							Command: []string{"/app"},
						},
						{
							Name:  "sidecar",
							Image: "sidecar:latest",
						},
					},
				},
			}

			restoreFrom := &sandboxv1alpha1.RestoreFromSnapshot{
				ContainerName: "main",
			}

			err := reconciler.injectRestorer(pod, restoreFrom, "snapshots/ns/sandbox/rev-00001/main.tar")
			Expect(err).NotTo(HaveOccurred())

			// Check restorer init container was added
			Expect(pod.Spec.InitContainers).To(HaveLen(1))
			Expect(pod.Spec.InitContainers[0].Name).To(Equal(restorerContainerName))

			// Check volume was added
			var hasRestoreVolume bool
			for _, vol := range pod.Spec.Volumes {
				if vol.Name == restoreVolumeName {
					hasRestoreVolume = true
					Expect(vol.EmptyDir).NotTo(BeNil())
				}
			}
			Expect(hasRestoreVolume).To(BeTrue())

			// Check target container was configured
			mainContainer := pod.Spec.Containers[0]
			Expect(mainContainer.Command).To(Equal([]string{"/bin/sh", "-c"}))
			Expect(mainContainer.Args[0]).To(ContainSubstring("tar xf"))
			Expect(mainContainer.Args[0]).To(ContainSubstring("/app"))

			// Check sidecar container was NOT modified
			sidecarContainer := pod.Spec.Containers[1]
			Expect(sidecarContainer.Command).To(BeEmpty())
		})

		It("should fail when target container not found", func() {
			pod := &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "other"},
					},
				},
			}

			restoreFrom := &sandboxv1alpha1.RestoreFromSnapshot{
				ContainerName: "main",
			}

			err := reconciler.injectRestorer(pod, restoreFrom, "snapshots/ns/sandbox/rev-00001/main.tar")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not found"))
		})
	})

	Describe("buildCommandString", func() {
		It("should properly quote simple commands", func() {
			result := buildCommandString([]string{"/bin/app"}, []string{"arg1", "arg2"})
			Expect(result).To(Equal("'/bin/app' 'arg1' 'arg2'"))
		})

		It("should escape single quotes in arguments", func() {
			result := buildCommandString([]string{"echo"}, []string{"hello 'world'"})
			Expect(result).To(Equal("'echo' 'hello '\\''world'\\'''"))
		})

		It("should handle empty args", func() {
			result := buildCommandString([]string{"/bin/app"}, nil)
			Expect(result).To(Equal("'/bin/app'"))
		})
	})
})

// Unit tests for helper functions without needing a real k8s client
var _ = Describe("Sandbox Controller Restore Helpers", func() {
	Describe("escapeShellSingleQuote", func() {
		It("should escape single quotes", func() {
			Expect(escapeShellSingleQuote("hello")).To(Equal("hello"))
			Expect(escapeShellSingleQuote("hello'world")).To(Equal("hello'\\''world"))
			Expect(escapeShellSingleQuote("'")).To(Equal("'\\''"))
			Expect(escapeShellSingleQuote("a'b'c")).To(Equal("a'\\''b'\\''c"))
		})
	})
})
