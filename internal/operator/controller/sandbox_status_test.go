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
)

var _ = Describe("computeConditions", func() {
	const gen int64 = 1

	findCondition := func(conditions []metav1.Condition, condType string) *metav1.Condition {
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
			state := &reconcileState{
				TemplateResolved: true,
				NetworkApplied:   true,
			}

			conditions := computeConditions(state, gen)

			templateCond := findCondition(conditions, SandboxTemplateReadyCondition)
			Expect(templateCond).NotTo(BeNil())
			Expect(templateCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(templateCond.Reason).To(Equal(CondReasonTemplateResolved))
		})

		It("should set TemplateReady=False and Stalled=True when template is not found", func() {
			state := &reconcileState{
				TemplateError: "SandboxTemplate \"foo\" not found",
			}

			conditions := computeConditions(state, gen)

			templateCond := findCondition(conditions, SandboxTemplateReadyCondition)
			Expect(templateCond).NotTo(BeNil())
			Expect(templateCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(templateCond.Reason).To(Equal(CondReasonTemplateNotFound))

			stalledCond := findCondition(conditions, SandboxStalledCondition)
			Expect(stalledCond).NotTo(BeNil())
			Expect(stalledCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(stalledCond.Reason).To(Equal(CondReasonTemplateNotFound))

			readyCond := findCondition(conditions, SandboxReadyCondition)
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCond.Reason).To(Equal(CondReasonTemplateNotFound))
		})

		It("should not emit TemplateReady condition when template not yet checked", func() {
			state := &reconcileState{}

			conditions := computeConditions(state, gen)

			templateCond := findCondition(conditions, SandboxTemplateReadyCondition)
			Expect(templateCond).To(BeNil())
		})
	})

	// ============================================
	// Network State Tests
	// ============================================
	Context("Network state", func() {
		It("should set NetworkConfigured=True when network is applied", func() {
			state := &reconcileState{
				TemplateResolved: true,
				NetworkApplied:   true,
			}

			conditions := computeConditions(state, gen)

			networkCond := findCondition(conditions, SandboxNetworkReadyCondition)
			Expect(networkCond).NotTo(BeNil())
			Expect(networkCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(networkCond.Reason).To(Equal(CondReasonNetworkPolicyApplied))
		})

		It("should set NetworkConfigured=False and Stalled=True when network fails", func() {
			state := &reconcileState{
				TemplateResolved: true,
				NetworkError:     "invalid CIDR",
			}

			conditions := computeConditions(state, gen)

			networkCond := findCondition(conditions, SandboxNetworkReadyCondition)
			Expect(networkCond).NotTo(BeNil())
			Expect(networkCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(networkCond.Reason).To(Equal(CondReasonNetworkPolicyFailed))

			stalledCond := findCondition(conditions, SandboxStalledCondition)
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
			state := &reconcileState{
				TemplateResolved: true,
				NetworkApplied:   true,
				PodExists:        true,
				PodPhase:         corev1.PodRunning,
				PodReady:         true,
			}

			conditions := computeConditions(state, gen)

			podCond := findCondition(conditions, SandboxPodReadyCondition)
			Expect(podCond).NotTo(BeNil())
			Expect(podCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(podCond.Reason).To(Equal(CondReasonPodRunning))

			readyCond := findCondition(conditions, SandboxReadyCondition)
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(readyCond.Reason).To(Equal(CondReasonPodRunning))

			// No Reconciling or Stalled when healthy
			Expect(findCondition(conditions, SandboxReconcilingCondition)).To(BeNil())
			Expect(findCondition(conditions, SandboxStalledCondition)).To(BeNil())
		})

		It("should set PodReady=False and Reconciling=True when pod is pending", func() {
			state := &reconcileState{
				TemplateResolved: true,
				NetworkApplied:   true,
				PodExists:        true,
				PodPhase:         corev1.PodPending,
				PodReady:         false,
			}

			conditions := computeConditions(state, gen)

			podCond := findCondition(conditions, SandboxPodReadyCondition)
			Expect(podCond).NotTo(BeNil())
			Expect(podCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(podCond.Reason).To(Equal(CondReasonPodPending))

			reconcilingCond := findCondition(conditions, SandboxReconcilingCondition)
			Expect(reconcilingCond).NotTo(BeNil())
			Expect(reconcilingCond.Status).To(Equal(metav1.ConditionTrue))

			// No Stalled when pending
			Expect(findCondition(conditions, SandboxStalledCondition)).To(BeNil())
		})

		It("should set PodReady=False and Stalled=True when pod has failed", func() {
			state := &reconcileState{
				TemplateResolved: true,
				NetworkApplied:   true,
				PodExists:        true,
				PodPhase:         corev1.PodFailed,
				PodReady:         false,
			}

			conditions := computeConditions(state, gen)

			podCond := findCondition(conditions, SandboxPodReadyCondition)
			Expect(podCond).NotTo(BeNil())
			Expect(podCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(podCond.Reason).To(Equal(CondReasonPodFailed))

			stalledCond := findCondition(conditions, SandboxStalledCondition)
			Expect(stalledCond).NotTo(BeNil())
			Expect(stalledCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(stalledCond.Reason).To(Equal(CondReasonPodFailed))

			readyCond := findCondition(conditions, SandboxReadyCondition)
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCond.Reason).To(Equal(CondReasonPodFailed))
		})

		It("should set PodReady=False and Stalled=True when pod has succeeded unexpectedly", func() {
			state := &reconcileState{
				TemplateResolved: true,
				NetworkApplied:   true,
				PodExists:        true,
				PodPhase:         corev1.PodSucceeded,
				PodReady:         false,
			}

			conditions := computeConditions(state, gen)

			podCond := findCondition(conditions, SandboxPodReadyCondition)
			Expect(podCond).NotTo(BeNil())
			Expect(podCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(podCond.Reason).To(Equal(CondReasonPodSucceeded))

			stalledCond := findCondition(conditions, SandboxStalledCondition)
			Expect(stalledCond).NotTo(BeNil())
			Expect(stalledCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(stalledCond.Reason).To(Equal(CondReasonPodSucceeded))

			readyCond := findCondition(conditions, SandboxReadyCondition)
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCond.Reason).To(Equal(CondReasonPodSucceeded))
		})

		It("should set Reconciling=True when pod does not exist but template is resolved", func() {
			state := &reconcileState{
				TemplateResolved: true,
				NetworkApplied:   true,
				PodExists:        false,
			}

			conditions := computeConditions(state, gen)

			reconcilingCond := findCondition(conditions, SandboxReconcilingCondition)
			Expect(reconcilingCond).NotTo(BeNil())
			Expect(reconcilingCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(reconcilingCond.Reason).To(Equal(CondReasonPodCreating))

			// No PodReady condition when pod doesn't exist
			Expect(findCondition(conditions, SandboxPodReadyCondition)).To(BeNil())
		})
	})

	// ============================================
	// Fatal Error Tests
	// ============================================
	Context("Fatal errors", func() {
		It("should set Stalled=True when sidecar injection fails", func() {
			state := &reconcileState{
				TemplateResolved: true,
				NetworkApplied:   true,
				FatalError:       "sandbox pod must have exactly one container",
				FatalReason:      CondReasonSidecarInjectionFail,
			}

			conditions := computeConditions(state, gen)

			podCond := findCondition(conditions, SandboxPodReadyCondition)
			Expect(podCond).NotTo(BeNil())
			Expect(podCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(podCond.Reason).To(Equal(CondReasonSidecarInjectionFail))

			stalledCond := findCondition(conditions, SandboxStalledCondition)
			Expect(stalledCond).NotTo(BeNil())
			Expect(stalledCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(stalledCond.Reason).To(Equal(CondReasonSidecarInjectionFail))

			readyCond := findCondition(conditions, SandboxReadyCondition)
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCond.Reason).To(Equal(CondReasonSidecarInjectionFail))
		})

		It("should set Stalled=True when pod creation fails", func() {
			state := &reconcileState{
				TemplateResolved: true,
				NetworkApplied:   true,
				FatalError:       "pods is forbidden",
				FatalReason:      CondReasonPodCreationFailed,
			}

			conditions := computeConditions(state, gen)

			podCond := findCondition(conditions, SandboxPodReadyCondition)
			Expect(podCond).NotTo(BeNil())
			Expect(podCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(podCond.Reason).To(Equal(CondReasonPodCreationFailed))

			stalledCond := findCondition(conditions, SandboxStalledCondition)
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
			state := &reconcileState{
				TemplateResolved: true,
				NetworkApplied:   true,
				PodExists:        true,
				PodPhase:         corev1.PodRunning,
				PodReady:         true,
				IsDeleting:       true,
			}

			conditions := computeConditions(state, gen)

			readyCond := findCondition(conditions, SandboxReadyCondition)
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCond.Reason).To(Equal(CondReasonDeleting))

			// Pod still shows as ready
			podCond := findCondition(conditions, SandboxPodReadyCondition)
			Expect(podCond).NotTo(BeNil())
			Expect(podCond.Status).To(Equal(metav1.ConditionTrue))
		})

		It("should set Reconciling=True when snapshotting is in progress", func() {
			state := &reconcileState{
				TemplateResolved: true,
				NetworkApplied:   true,
				PodExists:        true,
				PodPhase:         corev1.PodRunning,
				PodReady:         true,
				IsDeleting:       true,
				IsSnapshotting:   true,
			}

			conditions := computeConditions(state, gen)

			reconcilingCond := findCondition(conditions, SandboxReconcilingCondition)
			Expect(reconcilingCond).NotTo(BeNil())
			Expect(reconcilingCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(reconcilingCond.Reason).To(Equal(CondReasonSnapshottingInProgress))

			// No Stalled when snapshotting (even if there was an error before)
			Expect(findCondition(conditions, SandboxStalledCondition)).To(BeNil())
		})

		It("should override Stalled with Reconciling when snapshotting", func() {
			// Edge case: pod failed, but snapshot is happening
			state := &reconcileState{
				TemplateResolved: true,
				NetworkApplied:   true,
				PodExists:        true,
				PodPhase:         corev1.PodFailed,
				PodReady:         false,
				IsDeleting:       true,
				IsSnapshotting:   true,
			}

			conditions := computeConditions(state, gen)

			// Snapshotting overrides the stalled state
			reconcilingCond := findCondition(conditions, SandboxReconcilingCondition)
			Expect(reconcilingCond).NotTo(BeNil())
			Expect(reconcilingCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(reconcilingCond.Reason).To(Equal(CondReasonSnapshottingInProgress))

			// Stalled should NOT be present when snapshotting
			Expect(findCondition(conditions, SandboxStalledCondition)).To(BeNil())
		})
	})

	// ============================================
	// ObservedGeneration Tests
	// ============================================
	Context("ObservedGeneration", func() {
		It("should set ObservedGeneration on all conditions", func() {
			state := &reconcileState{
				TemplateResolved: true,
				NetworkApplied:   true,
				PodExists:        true,
				PodPhase:         corev1.PodPending,
				PodReady:         false,
			}

			conditions := computeConditions(state, gen)

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
			state := &reconcileState{
				TemplateResolved: true,
				NetworkApplied:   true,
				PodExists:        true,
				PodPhase:         corev1.PodRunning,
				PodReady:         true,
			}

			conditions := computeConditions(state, gen)

			Expect(findCondition(conditions, SandboxReconcilingCondition)).To(BeNil(),
				"Reconciling should be absent when healthy")
			Expect(findCondition(conditions, SandboxStalledCondition)).To(BeNil(),
				"Stalled should be absent when healthy")
		})

		It("should have Reconciling=True but no Stalled when pod is pending", func() {
			state := &reconcileState{
				TemplateResolved: true,
				NetworkApplied:   true,
				PodExists:        true,
				PodPhase:         corev1.PodPending,
				PodReady:         false,
			}

			conditions := computeConditions(state, gen)

			reconcilingCond := findCondition(conditions, SandboxReconcilingCondition)
			Expect(reconcilingCond).NotTo(BeNil())
			Expect(reconcilingCond.Status).To(Equal(metav1.ConditionTrue))

			Expect(findCondition(conditions, SandboxStalledCondition)).To(BeNil())
		})

		It("should have Stalled=True but no Reconciling when pod has failed", func() {
			state := &reconcileState{
				TemplateResolved: true,
				NetworkApplied:   true,
				PodExists:        true,
				PodPhase:         corev1.PodFailed,
				PodReady:         false,
			}

			conditions := computeConditions(state, gen)

			stalledCond := findCondition(conditions, SandboxStalledCondition)
			Expect(stalledCond).NotTo(BeNil())
			Expect(stalledCond.Status).To(Equal(metav1.ConditionTrue))

			Expect(findCondition(conditions, SandboxReconcilingCondition)).To(BeNil())
		})
	})
})

var _ = Describe("updateFromPod", func() {
	It("should set pod state from a running ready pod", func() {
		state := &reconcileState{}
		pod := &corev1.Pod{
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				Conditions: []corev1.PodCondition{
					{Type: corev1.PodReady, Status: corev1.ConditionTrue},
				},
			},
		}

		state.updateFromPod(pod)

		Expect(state.PodExists).To(BeTrue())
		Expect(state.PodPhase).To(Equal(corev1.PodRunning))
		Expect(state.PodReady).To(BeTrue())
	})

	It("should set pod state from a pending pod", func() {
		state := &reconcileState{}
		pod := &corev1.Pod{
			Status: corev1.PodStatus{
				Phase: corev1.PodPending,
			},
		}

		state.updateFromPod(pod)

		Expect(state.PodExists).To(BeTrue())
		Expect(state.PodPhase).To(Equal(corev1.PodPending))
		Expect(state.PodReady).To(BeFalse())
	})

	It("should handle nil pod gracefully", func() {
		state := &reconcileState{}

		state.updateFromPod(nil)

		Expect(state.PodExists).To(BeFalse())
		Expect(state.PodPhase).To(BeEmpty())
		Expect(state.PodReady).To(BeFalse())
	})

	It("should set pod state from a failed pod", func() {
		state := &reconcileState{}
		pod := &corev1.Pod{
			Status: corev1.PodStatus{
				Phase: corev1.PodFailed,
			},
		}

		state.updateFromPod(pod)

		Expect(state.PodExists).To(BeTrue())
		Expect(state.PodPhase).To(Equal(corev1.PodFailed))
		Expect(state.PodReady).To(BeFalse())
	})
})
