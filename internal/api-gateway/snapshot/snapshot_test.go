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

package snapshot

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	sandboxv1alpha1 "github.com/isola-ai/isola/api/v1alpha1"
)

// createRunningSandboxCR creates a Sandbox CR and updates its status to simulate a running pod.
func createRunningSandboxCR(name string) {
	sb := &sandboxv1alpha1.Sandbox{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: sandboxv1alpha1.SandboxSpec{
			PodTemplate: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "sandbox", Image: "alpine:latest"},
					},
				},
			},
		},
	}
	ExpectWithOffset(1, k8sClient.Create(ctx, sb)).To(Succeed())

	Eventually(func() error {
		return k8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: testNamespace}, sb)
	}).Should(Succeed())

	sb.Status.PodIP = "127.0.0.1"
	sb.Status.Conditions = []metav1.Condition{
		{
			Type:               "Ready",
			Status:             metav1.ConditionTrue,
			Reason:             "PodRunning",
			LastTransitionTime: metav1.Now(),
		},
	}
	ExpectWithOffset(1, k8sClient.Status().Update(ctx, sb)).To(Succeed())

	Eventually(func() string {
		got := &sandboxv1alpha1.Sandbox{}
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: testNamespace}, got); err != nil {
			return ""
		}
		return got.Status.PodIP
	}).ShouldNot(BeEmpty())
}

func keyFor(id string) client.ObjectKey {
	return client.ObjectKey{Name: id, Namespace: testNamespace}
}

var _ = Describe("Snapshot Endpoints", func() {
	Describe("POST /sandboxes/{id}/rootfssnapshots", func() {
		It("creates a snapshot with minimal fields", func() {
			sbName := "snap-post-minimal"
			createRunningSandboxCR(sbName)

			resp := testAPI.Post(fmt.Sprintf("/sandboxes/%s/rootfssnapshots", sbName),
				strings.NewReader(`{"snapshotName":"my-snap"}`))
			Expect(resp.Code).To(Equal(201))

			var body RootfsSnapshotResponse
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.ID).NotTo(BeEmpty())
			Expect(body.SandboxID).To(Equal(sbName))
			Expect(body.SnapshotName).To(Equal("my-snap"))
			Expect(body.Status).To(Equal("pending"))
			_, err := time.Parse(time.RFC3339, body.CreationTimestamp)
			Expect(err).NotTo(HaveOccurred())
		})

		It("creates a snapshot with all optional fields and round-trips via GET", func() {
			sbName := "snap-post-all"
			createRunningSandboxCR(sbName)

			reqBody := `{
				"snapshotName": "full-snap",
				"container": "worker",
				"activeDeadlineSeconds": 600,
				"ttlSecondsAfterFinished": 120
			}`
			resp := testAPI.Post(fmt.Sprintf("/sandboxes/%s/rootfssnapshots", sbName),
				strings.NewReader(reqBody))
			Expect(resp.Code).To(Equal(201))

			var body RootfsSnapshotResponse
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.Container).To(Equal("worker"))
			Expect(*body.ActiveDeadlineSeconds).To(Equal(int64(600)))
			Expect(*body.TTLSecondsAfterFinished).To(Equal(int32(120)))

			// GET read-back
			Eventually(func() int {
				return testAPI.Get(fmt.Sprintf("/sandboxes/%s/rootfssnapshots/%s", sbName, body.ID)).Code
			}).Should(Equal(200))

			getResp := testAPI.Get(fmt.Sprintf("/sandboxes/%s/rootfssnapshots/%s", sbName, body.ID))
			var got RootfsSnapshotResponse
			Expect(json.NewDecoder(getResp.Body).Decode(&got)).To(Succeed())
			Expect(got.ID).To(Equal(body.ID))
			Expect(got.SandboxID).To(Equal(sbName))
			Expect(got.SnapshotName).To(Equal("full-snap"))
			Expect(got.Container).To(Equal("worker"))
			Expect(*got.ActiveDeadlineSeconds).To(Equal(int64(600)))
			Expect(*got.TTLSecondsAfterFinished).To(Equal(int32(120)))
		})

		It("applies CRD defaults when optional fields are omitted", func() {
			sbName := "snap-post-defaults"
			createRunningSandboxCR(sbName)

			resp := testAPI.Post(fmt.Sprintf("/sandboxes/%s/rootfssnapshots", sbName),
				strings.NewReader(`{"snapshotName":"default-snap"}`))
			Expect(resp.Code).To(Equal(201))

			var body RootfsSnapshotResponse
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())

			// Verify CRD kubebuilder defaults were applied (activeDeadlineSeconds=300, ttlSecondsAfterFinished=300)
			snap := &sandboxv1alpha1.RootfsSnapshot{}
			Eventually(func() error {
				return k8sClient.Get(ctx, keyFor(body.ID), snap)
			}).Should(Succeed())
			Expect(*snap.Spec.ActiveDeadlineSeconds).To(Equal(int64(300)))
			Expect(*snap.Spec.TTLSecondsAfterFinished).To(Equal(int32(300)))
		})

		It("returns nil for snapshotKey, startTime, completionTime, failureMessage when pending", func() {
			sbName := "snap-post-nil-fields"
			createRunningSandboxCR(sbName)

			resp := testAPI.Post(fmt.Sprintf("/sandboxes/%s/rootfssnapshots", sbName),
				strings.NewReader(`{"snapshotName":"nil-fields"}`))
			Expect(resp.Code).To(Equal(201))

			var body RootfsSnapshotResponse
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.SnapshotKey).To(BeNil())
			Expect(body.StartTime).To(BeNil())
			Expect(body.CompletionTime).To(BeNil())
			Expect(body.FailureMessage).To(BeNil())
		})

		It("succeeds with max-length snapshotName (63 chars)", func() {
			sbName := "snap-post-maxname"
			createRunningSandboxCR(sbName)

			longName := strings.Repeat("a", 63)
			resp := testAPI.Post(fmt.Sprintf("/sandboxes/%s/rootfssnapshots", sbName),
				strings.NewReader(fmt.Sprintf(`{"snapshotName":"%s"}`, longName)))
			Expect(resp.Code).To(Equal(201))

			var body RootfsSnapshotResponse
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.SnapshotName).To(Equal(longName))
			Expect(body.ID).NotTo(BeEmpty())
		})

		It("returns 404 for nonexistent sandbox", func() {
			resp := testAPI.Post("/sandboxes/nonexistent/rootfssnapshots",
				strings.NewReader(`{"snapshotName":"test"}`))
			Expect(resp.Code).To(Equal(404))
		})

		It("returns 409 for non-running sandbox", func() {
			// Create sandbox but don't set it to running
			sb := &sandboxv1alpha1.Sandbox{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "snap-not-ready",
					Namespace: testNamespace,
				},
				Spec: sandboxv1alpha1.SandboxSpec{
					PodTemplate: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "sandbox", Image: "alpine:latest"},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, sb)).To(Succeed())

			Eventually(func() error {
				return k8sClient.Get(ctx, client.ObjectKey{Name: "snap-not-ready", Namespace: testNamespace}, sb)
			}).Should(Succeed())

			resp := testAPI.Post("/sandboxes/snap-not-ready/rootfssnapshots",
				strings.NewReader(`{"snapshotName":"test"}`))
			Expect(resp.Code).To(Equal(409))
		})

		It("rejects missing snapshotName with 422", func() {
			sbName := "snap-post-422-missing"
			createRunningSandboxCR(sbName)

			resp := testAPI.Post(fmt.Sprintf("/sandboxes/%s/rootfssnapshots", sbName),
				strings.NewReader(`{}`))
			Expect(resp.Code).To(Equal(422))
		})

		It("rejects invalid snapshotName pattern with 422", func() {
			sbName := "snap-post-422-pattern"
			createRunningSandboxCR(sbName)

			resp := testAPI.Post(fmt.Sprintf("/sandboxes/%s/rootfssnapshots", sbName),
				strings.NewReader(`{"snapshotName":"Invalid-Name"}`))
			Expect(resp.Code).To(Equal(422))
		})

		It("rejects activeDeadlineSeconds of 0 with 422", func() {
			sbName := "snap-post-422-deadline"
			createRunningSandboxCR(sbName)

			resp := testAPI.Post(fmt.Sprintf("/sandboxes/%s/rootfssnapshots", sbName),
				strings.NewReader(`{"snapshotName":"test","activeDeadlineSeconds":0}`))
			Expect(resp.Code).To(Equal(422))
		})
	})

	Describe("GET /sandboxes/{id}/rootfssnapshots/{snapId}", func() {
		It("returns snapshot details", func() {
			sbName := "snap-get-details"
			createRunningSandboxCR(sbName)

			createResp := testAPI.Post(fmt.Sprintf("/sandboxes/%s/rootfssnapshots", sbName),
				strings.NewReader(`{"snapshotName":"get-test"}`))
			Expect(createResp.Code).To(Equal(201))

			var created RootfsSnapshotResponse
			Expect(json.NewDecoder(createResp.Body).Decode(&created)).To(Succeed())

			Eventually(func() int {
				return testAPI.Get(fmt.Sprintf("/sandboxes/%s/rootfssnapshots/%s", sbName, created.ID)).Code
			}).Should(Equal(200))

			getResp := testAPI.Get(fmt.Sprintf("/sandboxes/%s/rootfssnapshots/%s", sbName, created.ID))
			var got RootfsSnapshotResponse
			Expect(json.NewDecoder(getResp.Body).Decode(&got)).To(Succeed())
			Expect(got.ID).To(Equal(created.ID))
			Expect(got.SandboxID).To(Equal(sbName))
		})

		It("returns 404 for nonexistent snapshot", func() {
			resp := testAPI.Get("/sandboxes/any-sandbox/rootfssnapshots/nonexistent")
			Expect(resp.Code).To(Equal(404))
		})

		It("returns 404 for snapshot belonging to a different sandbox", func() {
			sbName := "snap-get-wrong-sb"
			createRunningSandboxCR(sbName)

			createResp := testAPI.Post(fmt.Sprintf("/sandboxes/%s/rootfssnapshots", sbName),
				strings.NewReader(`{"snapshotName":"cross-sb"}`))
			Expect(createResp.Code).To(Equal(201))

			var created RootfsSnapshotResponse
			Expect(json.NewDecoder(createResp.Body).Decode(&created)).To(Succeed())

			// Try to GET with a different sandbox ID
			Eventually(func() int {
				return testAPI.Get(fmt.Sprintf("/sandboxes/other-sandbox/rootfssnapshots/%s", created.ID)).Code
			}).Should(Equal(404))
		})
	})

	Describe("Status mapping", func() {
		It("maps no conditions and no startTime to pending", func() {
			snap := &sandboxv1alpha1.RootfsSnapshot{}
			Expect(rootfsSnapshotStatus(snap)).To(Equal("pending"))
		})

		It("maps startTime with no terminal condition to inProgress", func() {
			now := metav1.Now()
			snap := &sandboxv1alpha1.RootfsSnapshot{
				Status: sandboxv1alpha1.RootfsSnapshotStatus{
					StartTime: &now,
				},
			}
			Expect(rootfsSnapshotStatus(snap)).To(Equal("inProgress"))
		})

		It("maps Complete=True to complete", func() {
			snap := &sandboxv1alpha1.RootfsSnapshot{
				Status: sandboxv1alpha1.RootfsSnapshotStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(sandboxv1alpha1.RootfsSnapshotComplete),
							Status: metav1.ConditionTrue,
						},
					},
				},
			}
			Expect(rootfsSnapshotStatus(snap)).To(Equal("complete"))
		})

		It("maps Failed=True to failed", func() {
			snap := &sandboxv1alpha1.RootfsSnapshot{
				Status: sandboxv1alpha1.RootfsSnapshotStatus{
					Conditions: []metav1.Condition{
						{
							Type:    string(sandboxv1alpha1.RootfsSnapshotFailed),
							Status:  metav1.ConditionTrue,
							Message: "Pod not found",
						},
					},
				},
			}
			Expect(rootfsSnapshotStatus(snap)).To(Equal("failed"))
			msg := rootfsSnapshotFailureMessage(snap)
			Expect(msg).NotTo(BeNil())
			Expect(*msg).To(Equal("Pod not found"))
		})

		It("returns nil failureMessage when not failed", func() {
			snap := &sandboxv1alpha1.RootfsSnapshot{}
			Expect(rootfsSnapshotFailureMessage(snap)).To(BeNil())
		})

		It("prioritizes Failed over Complete defensively", func() {
			snap := &sandboxv1alpha1.RootfsSnapshot{
				Status: sandboxv1alpha1.RootfsSnapshotStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(sandboxv1alpha1.RootfsSnapshotComplete),
							Status: metav1.ConditionTrue,
						},
						{
							Type:   string(sandboxv1alpha1.RootfsSnapshotFailed),
							Status: metav1.ConditionTrue,
						},
					},
				},
			}
			Expect(rootfsSnapshotStatus(snap)).To(Equal("failed"))
		})
	})
})
