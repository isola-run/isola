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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"

	sandboxv1alpha1 "github.com/isola-run/isola/api/v1alpha1"
	"github.com/isola-run/isola/internal/identity"
)

func newTestReconcilerWithIdentity(clock Clock) *SandboxReconciler {
	rec := events.NewFakeRecorder(100)
	return &SandboxReconciler{
		Client:                k8sClient,
		Scheme:                scheme.Scheme,
		Recorder:              rec,
		SandboxSidecarImage:   "sandbox-sidecar:test",
		Clock:                 clock,
		IdentitySignerURL:     "http://identity-signer:8443",
		SandboxServiceAccount: "isola-sandbox",
	}
}

var _ = Describe("Sandbox Controller - Identity", func() {

	Context("Identity Bootstrap Injection", func() {
		var (
			reconciler *SandboxReconciler
			fakeClock  *FakeClock
		)

		BeforeEach(func() {
			fakeClock = NewFakeClock(time.Now())
			reconciler = newTestReconcilerWithIdentity(fakeClock)
		})

		It("should inject bootstrap init container before sidecar when identity enabled", func() {
			sandboxName := "sandbox-identity-bootstrap"

			createSandbox(ctx, sandboxName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())

			// Bootstrap must be first init container, sidecar second
			Expect(pod.Spec.InitContainers).To(HaveLen(2))
			Expect(pod.Spec.InitContainers[0].Name).To(Equal(identityBootstrapContainerName))
			Expect(pod.Spec.InitContainers[1].Name).To(Equal(sandboxSidecarContainerName))
		})

		It("should inject three identity volumes", func() {
			sandboxName := "sandbox-identity-volumes"

			createSandbox(ctx, sandboxName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())

			volumeNames := make([]string, len(pod.Spec.Volumes))
			for i, v := range pod.Spec.Volumes {
				volumeNames[i] = v.Name
			}
			Expect(volumeNames).To(ContainElement(identityTLSVolumeName))
			Expect(volumeNames).To(ContainElement(identityTokenVolumeName))
			Expect(volumeNames).To(ContainElement(identityPodMetaVolumeName))
		})

		It("should mount TLS volume into sidecar as read-only", func() {
			sandboxName := "sandbox-identity-sidecar-tls"

			createSandbox(ctx, sandboxName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())

			sidecar := pod.Spec.InitContainers[1]
			Expect(sidecar.Name).To(Equal(sandboxSidecarContainerName))

			var hasTLSMount bool
			for _, vm := range sidecar.VolumeMounts {
				if vm.Name == identityTLSVolumeName {
					hasTLSMount = true
					Expect(vm.ReadOnly).To(BeTrue())
				}
			}
			Expect(hasTLSMount).To(BeTrue(), "sidecar should have TLS volume mount")
		})

		It("should NOT mount token volume into sidecar", func() {
			sandboxName := "sandbox-identity-no-token-sidecar"

			createSandbox(ctx, sandboxName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())

			sidecar := pod.Spec.InitContainers[1]
			for _, vm := range sidecar.VolumeMounts {
				Expect(vm.Name).NotTo(Equal(identityTokenVolumeName), "sidecar should not have token volume mount")
			}
		})

		It("should NOT mount token or TLS volumes into user containers", func() {
			sandboxName := "sandbox-identity-no-user-mount"

			createSandbox(ctx, sandboxName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())

			for _, c := range pod.Spec.Containers {
				for _, vm := range c.VolumeMounts {
					Expect(vm.Name).NotTo(Equal(identityTokenVolumeName))
					Expect(vm.Name).NotTo(Equal(identityTLSVolumeName))
				}
			}
		})

		It("should set bootstrap container args and env", func() {
			sandboxName := "sandbox-identity-bootstrap-env"

			createSandbox(ctx, sandboxName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())

			bootstrap := pod.Spec.InitContainers[0]
			Expect(bootstrap.Args).To(ContainElement("--bootstrap-cert-only"))

			envNames := make([]string, len(bootstrap.Env))
			for i, e := range bootstrap.Env {
				envNames[i] = e.Name
			}
			Expect(envNames).To(ContainElement("ISOLA_SIGNER_URL"))
			Expect(envNames).To(ContainElement("ISOLA_SANDBOX_NAME"))
		})

		It("should use memory-backed emptyDir for TLS volume", func() {
			sandboxName := "sandbox-identity-tmpfs"

			createSandbox(ctx, sandboxName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())

			for _, v := range pod.Spec.Volumes {
				if v.Name == identityTLSVolumeName {
					Expect(v.EmptyDir).NotTo(BeNil())
					Expect(v.EmptyDir.Medium).To(Equal(corev1.StorageMediumMemory))
				}
			}
		})

		It("should enforce the dedicated ServiceAccount", func() {
			sandboxName := "sandbox-identity-sa"

			createSandbox(ctx, sandboxName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())
			Expect(pod.Spec.ServiceAccountName).To(Equal("isola-sandbox"))
		})

		It("should set projected token with correct audience", func() {
			sandboxName := "sandbox-identity-token-audience"

			createSandbox(ctx, sandboxName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())

			for _, v := range pod.Spec.Volumes {
				if v.Name == identityTokenVolumeName {
					Expect(v.Projected).NotTo(BeNil())
					Expect(v.Projected.Sources).To(HaveLen(1))
					sat := v.Projected.Sources[0].ServiceAccountToken
					Expect(sat).NotTo(BeNil())
					Expect(sat.Audience).To(Equal(identity.TokenAudience))
				}
			}
		})

		It("should add sandbox-name label to pod", func() {
			sandboxName := "sandbox-identity-label"

			createSandbox(ctx, sandboxName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())
			Expect(pod.Labels[LabelSandboxName]).To(Equal(sandboxName))
		})
	})

	Context("Status PodName and PodUID", func() {
		var (
			reconciler *SandboxReconciler
			fakeClock  *FakeClock
		)

		BeforeEach(func() {
			fakeClock = NewFakeClock(time.Now())
			reconciler = newTestReconciler(fakeClock)
		})

		It("should persist PodName and PodUID in sandbox status", func() {
			sandboxName := "sandbox-status-pod-identity"

			createSandbox(ctx, sandboxName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			// First reconcile creates the pod
			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())

			// Simulate pod becoming ready
			pod.Status.Phase = corev1.PodRunning
			pod.Status.PodIP = "10.0.0.1"
			pod.Status.Conditions = []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
				{Type: corev1.ContainersReady, Status: corev1.ConditionTrue},
			}
			pod.Status.StartTime = &metav1.Time{Time: fakeClock.Now()}
			Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())

			// Second reconcile updates status
			_, err = doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			sb := &sandboxv1alpha1.Sandbox{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: sandboxName, Namespace: testNamespace}, sb)).To(Succeed())
			Expect(sb.Status.PodName).To(Equal(podName))
			Expect(sb.Status.PodUID).NotTo(BeEmpty())
		})
	})

	Context("No Identity (disabled)", func() {
		var (
			reconciler *SandboxReconciler
			fakeClock  *FakeClock
		)

		BeforeEach(func() {
			fakeClock = NewFakeClock(time.Now())
			reconciler = newTestReconciler(fakeClock) // no IdentitySignerURL
		})

		It("should not inject bootstrap init container when identity disabled", func() {
			sandboxName := "sandbox-no-identity"

			createSandbox(ctx, sandboxName)
			defer deleteSandbox(ctx, sandboxName)

			podName := sandboxName + "-pod"
			defer deletePod(ctx, podName)

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, podName)
			Expect(pod).NotTo(BeNil())

			// Only sidecar init container, no bootstrap
			Expect(pod.Spec.InitContainers).To(HaveLen(1))
			Expect(pod.Spec.InitContainers[0].Name).To(Equal(sandboxSidecarContainerName))
		})
	})
})
