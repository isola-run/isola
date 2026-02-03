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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	sandboxv1alpha1 "github.com/isola-ai/isola-sb/api/v1alpha1"
)

// newTestSandbox creates a sandbox for testing.
func newTestSandbox(podIP string) *sandboxv1alpha1.Sandbox {
	return &sandboxv1alpha1.Sandbox{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-sandbox",
			Namespace:  "test-ns",
			Generation: 1,
		},
		Status: sandboxv1alpha1.SandboxStatus{
			PodIP: podIP,
		},
	}
}

// newReadyPod creates a pod that appears ready.
func newReadyPod() *corev1.Pod {
	return &corev1.Pod{
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			},
		},
	}
}

// newPendingPod creates a pod in pending state.
func newPendingPod() *corev1.Pod {
	return &corev1.Pod{
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
		},
	}
}

// newFailedPod creates a pod in failed state.
func newFailedPod() *corev1.Pod {
	return &corev1.Pod{
		Status: corev1.PodStatus{
			Phase: corev1.PodFailed,
		},
	}
}

// newSucceededPod creates a pod in succeeded state.
func newSucceededPod() *corev1.Pod {
	return &corev1.Pod{
		Status: corev1.PodStatus{
			Phase: corev1.PodSucceeded,
		},
	}
}

var _ = Describe("computeConditions", func() {
	const gen int64 = 1

	findCond := func(conditions []metav1.Condition, condType string) *metav1.Condition {
		for i := range conditions {
			if conditions[i].Type == condType {
				return &conditions[i]
			}
		}
		return nil
	}

	// ============================================
	// Template State Tests
	// ============================================
	Context("Template state", func() {
		It("should set TemplateReady=True when template is resolved", func() {
			state := &sandboxState{
				Template:       &sandboxv1alpha1.SandboxTemplate{},
				NetworkApplied: true,
			}
			sandbox := newTestSandbox("")

			conditions := computeConditions(state, sandbox)

			templateCond := findCond(conditions, SandboxTemplateReadyCondition)
			Expect(templateCond).NotTo(BeNil())
			Expect(templateCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(templateCond.Reason).To(Equal(CondReasonTemplateResolved))
		})

		It("should set TemplateReady=False and Stalled=True when template is not found", func() {
			state := &sandboxState{
				TemplateNotFound: true,
			}
			sandbox := newTestSandbox("")

			conditions := computeConditions(state, sandbox)

			templateCond := findCond(conditions, SandboxTemplateReadyCondition)
			Expect(templateCond).NotTo(BeNil())
			Expect(templateCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(templateCond.Reason).To(Equal(CondReasonTemplateNotFound))

			stalledCond := findCond(conditions, SandboxStalledCondition)
			Expect(stalledCond).NotTo(BeNil())
			Expect(stalledCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(stalledCond.Reason).To(Equal(CondReasonTemplateNotFound))

			readyCond := findCond(conditions, SandboxReadyCondition)
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCond.Reason).To(Equal(CondReasonTemplateNotFound))
		})

		It("should not emit TemplateReady condition when template not yet checked", func() {
			state := &sandboxState{}
			sandbox := newTestSandbox("")

			conditions := computeConditions(state, sandbox)

			templateCond := findCond(conditions, SandboxTemplateReadyCondition)
			Expect(templateCond).To(BeNil())
		})
	})

	// ============================================
	// Network State Tests
	// ============================================
	Context("Network state", func() {
		It("should set NetworkConfigured=True when network is applied", func() {
			state := &sandboxState{
				Template:       &sandboxv1alpha1.SandboxTemplate{},
				NetworkApplied: true,
			}
			sandbox := newTestSandbox("")

			conditions := computeConditions(state, sandbox)

			networkCond := findCond(conditions, SandboxNetworkReadyCondition)
			Expect(networkCond).NotTo(BeNil())
			Expect(networkCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(networkCond.Reason).To(Equal(CondReasonNetworkPolicyApplied))
		})

		It("should set NetworkConfigured=False and Stalled=True when network fails", func() {
			state := &sandboxState{
				Template:     &sandboxv1alpha1.SandboxTemplate{},
				NetworkError: "invalid CIDR",
			}
			sandbox := newTestSandbox("")

			conditions := computeConditions(state, sandbox)

			networkCond := findCond(conditions, SandboxNetworkReadyCondition)
			Expect(networkCond).NotTo(BeNil())
			Expect(networkCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(networkCond.Reason).To(Equal(CondReasonNetworkPolicyFailed))

			stalledCond := findCond(conditions, SandboxStalledCondition)
			Expect(stalledCond).NotTo(BeNil())
			Expect(stalledCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(stalledCond.Reason).To(Equal(CondReasonNetworkPolicyFailed))
		})
	})

	// ============================================
	// Pod State Tests
	// ============================================
	Context("Pod state", func() {
		It("should set PodReady=True and Ready=True when pod is ready", func() {
			state := &sandboxState{
				Template:       &sandboxv1alpha1.SandboxTemplate{},
				NetworkApplied: true,
				Pod:            newReadyPod(),
			}
			sandbox := newTestSandbox("")

			conditions := computeConditions(state, sandbox)

			podCond := findCond(conditions, SandboxPodReadyCondition)
			Expect(podCond).NotTo(BeNil())
			Expect(podCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(podCond.Reason).To(Equal(CondReasonPodRunning))

			readyCond := findCond(conditions, SandboxReadyCondition)
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(readyCond.Reason).To(Equal(CondReasonPodRunning))

			// No Reconciling or Stalled when healthy
			Expect(findCond(conditions, SandboxReconcilingCondition)).To(BeNil())
			Expect(findCond(conditions, SandboxStalledCondition)).To(BeNil())
		})

		It("should set PodReady=False and Reconciling=True when pod is pending", func() {
			state := &sandboxState{
				Template:       &sandboxv1alpha1.SandboxTemplate{},
				NetworkApplied: true,
				Pod:            newPendingPod(),
			}
			sandbox := newTestSandbox("")

			conditions := computeConditions(state, sandbox)

			podCond := findCond(conditions, SandboxPodReadyCondition)
			Expect(podCond).NotTo(BeNil())
			Expect(podCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(podCond.Reason).To(Equal(CondReasonPodPending))

			reconcilingCond := findCond(conditions, SandboxReconcilingCondition)
			Expect(reconcilingCond).NotTo(BeNil())
			Expect(reconcilingCond.Status).To(Equal(metav1.ConditionTrue))

			// No Stalled when pending
			Expect(findCond(conditions, SandboxStalledCondition)).To(BeNil())
		})

		It("should set PodReady=False and Stalled=True when pod has failed", func() {
			state := &sandboxState{
				Template:       &sandboxv1alpha1.SandboxTemplate{},
				NetworkApplied: true,
				Pod:            newFailedPod(),
			}
			sandbox := newTestSandbox("")

			conditions := computeConditions(state, sandbox)

			podCond := findCond(conditions, SandboxPodReadyCondition)
			Expect(podCond).NotTo(BeNil())
			Expect(podCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(podCond.Reason).To(Equal(CondReasonPodFailed))

			stalledCond := findCond(conditions, SandboxStalledCondition)
			Expect(stalledCond).NotTo(BeNil())
			Expect(stalledCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(stalledCond.Reason).To(Equal(CondReasonPodFailed))

			readyCond := findCond(conditions, SandboxReadyCondition)
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCond.Reason).To(Equal(CondReasonPodFailed))
		})

		It("should set PodReady=False and Stalled=True when pod has succeeded unexpectedly", func() {
			state := &sandboxState{
				Template:       &sandboxv1alpha1.SandboxTemplate{},
				NetworkApplied: true,
				Pod:            newSucceededPod(),
			}
			sandbox := newTestSandbox("")

			conditions := computeConditions(state, sandbox)

			podCond := findCond(conditions, SandboxPodReadyCondition)
			Expect(podCond).NotTo(BeNil())
			Expect(podCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(podCond.Reason).To(Equal(CondReasonPodSucceeded))

			stalledCond := findCond(conditions, SandboxStalledCondition)
			Expect(stalledCond).NotTo(BeNil())
			Expect(stalledCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(stalledCond.Reason).To(Equal(CondReasonPodSucceeded))

			readyCond := findCond(conditions, SandboxReadyCondition)
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCond.Reason).To(Equal(CondReasonPodSucceeded))
		})

		It("should set Reconciling=True when pod does not exist but template is resolved", func() {
			state := &sandboxState{
				Template:       &sandboxv1alpha1.SandboxTemplate{},
				NetworkApplied: true,
				// Pod is nil - not yet created
			}
			sandbox := newTestSandbox("")

			conditions := computeConditions(state, sandbox)

			reconcilingCond := findCond(conditions, SandboxReconcilingCondition)
			Expect(reconcilingCond).NotTo(BeNil())
			Expect(reconcilingCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(reconcilingCond.Reason).To(Equal(CondReasonPodCreating))

			// No PodReady condition when pod doesn't exist
			Expect(findCond(conditions, SandboxPodReadyCondition)).To(BeNil())
		})

		It("should detect pod deletion and report PodDeleted", func() {
			state := &sandboxState{
				Template:       &sandboxv1alpha1.SandboxTemplate{},
				NetworkApplied: true,
				// Pod is nil - deleted
			}
			// Sandbox HAD a pod (PodIP is set) - pod was deleted
			sandbox := newTestSandbox("10.0.0.1")

			conditions := computeConditions(state, sandbox)

			podCond := findCond(conditions, SandboxPodReadyCondition)
			Expect(podCond).NotTo(BeNil())
			Expect(podCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(podCond.Reason).To(Equal(CondReasonPodDeleted)) // Detected deletion!

			stalledCond := findCond(conditions, SandboxStalledCondition)
			Expect(stalledCond).NotTo(BeNil())
			Expect(stalledCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(stalledCond.Reason).To(Equal(CondReasonPodDeleted))

			readyCond := findCond(conditions, SandboxReadyCondition)
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCond.Reason).To(Equal(CondReasonPodDeleted))
		})
	})

	// ============================================
	// Fatal Error Tests
	// ============================================
	Context("Fatal errors", func() {
		It("should set Stalled=True when sidecar injection fails", func() {
			state := &sandboxState{
				Template:       &sandboxv1alpha1.SandboxTemplate{},
				NetworkApplied: true,
				FatalError:     "sandbox pod must have exactly one container",
				FatalReason:    CondReasonSidecarInjectionFail,
			}
			sandbox := newTestSandbox("")

			conditions := computeConditions(state, sandbox)

			podCond := findCond(conditions, SandboxPodReadyCondition)
			Expect(podCond).NotTo(BeNil())
			Expect(podCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(podCond.Reason).To(Equal(CondReasonSidecarInjectionFail))

			stalledCond := findCond(conditions, SandboxStalledCondition)
			Expect(stalledCond).NotTo(BeNil())
			Expect(stalledCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(stalledCond.Reason).To(Equal(CondReasonSidecarInjectionFail))

			readyCond := findCond(conditions, SandboxReadyCondition)
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCond.Reason).To(Equal(CondReasonSidecarInjectionFail))
		})

		It("should set Stalled=True when pod creation fails", func() {
			state := &sandboxState{
				Template:       &sandboxv1alpha1.SandboxTemplate{},
				NetworkApplied: true,
				FatalError:     "pods is forbidden",
				FatalReason:    CondReasonPodCreationFailed,
			}
			sandbox := newTestSandbox("")

			conditions := computeConditions(state, sandbox)

			podCond := findCond(conditions, SandboxPodReadyCondition)
			Expect(podCond).NotTo(BeNil())
			Expect(podCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(podCond.Reason).To(Equal(CondReasonPodCreationFailed))

			stalledCond := findCond(conditions, SandboxStalledCondition)
			Expect(stalledCond).NotTo(BeNil())
			Expect(stalledCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(stalledCond.Reason).To(Equal(CondReasonPodCreationFailed))
		})
	})

	// ============================================
	// Lifecycle State Tests
	// ============================================
	Context("Lifecycle state", func() {
		It("should set Ready=False with Deleting reason when sandbox is being deleted", func() {
			state := &sandboxState{
				Template:       &sandboxv1alpha1.SandboxTemplate{},
				NetworkApplied: true,
				Pod:            newReadyPod(),
				IsDeleting:     true,
			}
			sandbox := newTestSandbox("")

			conditions := computeConditions(state, sandbox)

			readyCond := findCond(conditions, SandboxReadyCondition)
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCond.Reason).To(Equal(CondReasonDeleting))

			// Pod still shows as ready
			podCond := findCond(conditions, SandboxPodReadyCondition)
			Expect(podCond).NotTo(BeNil())
			Expect(podCond.Status).To(Equal(metav1.ConditionTrue))
		})

		It("should set Reconciling=True when snapshotting is in progress", func() {
			state := &sandboxState{
				Template:       &sandboxv1alpha1.SandboxTemplate{},
				NetworkApplied: true,
				Pod:            newReadyPod(),
				IsDeleting:     true,
				IsSnapshotting: true,
			}
			sandbox := newTestSandbox("")

			conditions := computeConditions(state, sandbox)

			reconcilingCond := findCond(conditions, SandboxReconcilingCondition)
			Expect(reconcilingCond).NotTo(BeNil())
			Expect(reconcilingCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(reconcilingCond.Reason).To(Equal(CondReasonSnapshottingInProgress))

			// No Stalled when snapshotting (even if there was an error before)
			Expect(findCond(conditions, SandboxStalledCondition)).To(BeNil())
		})

		It("should override Stalled with Reconciling when snapshotting", func() {
			// Edge case: pod failed, but snapshot is happening
			state := &sandboxState{
				Template:       &sandboxv1alpha1.SandboxTemplate{},
				NetworkApplied: true,
				Pod:            newFailedPod(),
				IsDeleting:     true,
				IsSnapshotting: true,
			}
			sandbox := newTestSandbox("")

			conditions := computeConditions(state, sandbox)

			// Snapshotting overrides the stalled state
			reconcilingCond := findCond(conditions, SandboxReconcilingCondition)
			Expect(reconcilingCond).NotTo(BeNil())
			Expect(reconcilingCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(reconcilingCond.Reason).To(Equal(CondReasonSnapshottingInProgress))

			// Stalled should NOT be present when snapshotting
			Expect(findCond(conditions, SandboxStalledCondition)).To(BeNil())
		})
	})

	// ============================================
	// ObservedGeneration Tests
	// ============================================
	Context("ObservedGeneration", func() {
		It("should set ObservedGeneration on all conditions", func() {
			state := &sandboxState{
				Template:       &sandboxv1alpha1.SandboxTemplate{},
				NetworkApplied: true,
				Pod:            newPendingPod(),
			}
			sandbox := newTestSandbox("")

			conditions := computeConditions(state, sandbox)

			for _, cond := range conditions {
				Expect(cond.ObservedGeneration).To(Equal(gen),
					"Condition %s should have ObservedGeneration set", cond.Type)
			}
		})
	})

	// ============================================
	// Abnormal-True Pattern Tests
	// ============================================
	Context("Abnormal-true pattern", func() {
		It("should not include Reconciling or Stalled when pod is healthy", func() {
			state := &sandboxState{
				Template:       &sandboxv1alpha1.SandboxTemplate{},
				NetworkApplied: true,
				Pod:            newReadyPod(),
			}
			sandbox := newTestSandbox("")

			conditions := computeConditions(state, sandbox)

			Expect(findCond(conditions, SandboxReconcilingCondition)).To(BeNil(),
				"Reconciling should be absent when healthy")
			Expect(findCond(conditions, SandboxStalledCondition)).To(BeNil(),
				"Stalled should be absent when healthy")
		})

		It("should have Reconciling=True but no Stalled when pod is pending", func() {
			state := &sandboxState{
				Template:       &sandboxv1alpha1.SandboxTemplate{},
				NetworkApplied: true,
				Pod:            newPendingPod(),
			}
			sandbox := newTestSandbox("")

			conditions := computeConditions(state, sandbox)

			reconcilingCond := findCond(conditions, SandboxReconcilingCondition)
			Expect(reconcilingCond).NotTo(BeNil())
			Expect(reconcilingCond.Status).To(Equal(metav1.ConditionTrue))

			Expect(findCond(conditions, SandboxStalledCondition)).To(BeNil())
		})

		It("should have Stalled=True but no Reconciling when pod has failed", func() {
			state := &sandboxState{
				Template:       &sandboxv1alpha1.SandboxTemplate{},
				NetworkApplied: true,
				Pod:            newFailedPod(),
			}
			sandbox := newTestSandbox("")

			conditions := computeConditions(state, sandbox)

			stalledCond := findCond(conditions, SandboxStalledCondition)
			Expect(stalledCond).NotTo(BeNil())
			Expect(stalledCond.Status).To(Equal(metav1.ConditionTrue))

			Expect(findCond(conditions, SandboxReconcilingCondition)).To(BeNil())
		})
	})
})
