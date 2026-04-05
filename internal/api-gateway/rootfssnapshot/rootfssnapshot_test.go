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

package rootfssnapshot

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	sandboxv1alpha1 "github.com/isola-run/isola/api/v1alpha1"
	apigateway "github.com/isola-run/isola/internal/api-gateway"
)

var _ = Describe("RootfsSnapshot Endpoints", func() {
	Describe("POST /rootfs-snapshots", func() {
		It("creates a rootfs snapshot with minimal request", func() {
			resp := testAPI.Post("/v1/rootfs-snapshots", strings.NewReader(`{"sandboxId":"test-sb","snapshotName":"snap1"}`))
			Expect(resp.Code).To(Equal(201))

			var body RootfsSnapshotResponse
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.ID).To(HaveLen(22))
			Expect(body.ID).To(MatchRegexp(`^[a-z][a-z0-9]{21}$`))
			Expect(body.SandboxID).To(Equal("test-sb"))
			Expect(body.SnapshotName).To(Equal("snap1"))
			Expect(body.ContainerName).To(BeEmpty())
			Expect(body.Status).To(Equal(apigateway.StatusPending))
			_, err := time.Parse(time.RFC3339, body.CreationTimestamp)
			Expect(err).NotTo(HaveOccurred())

			// CRD defaults applied by API server
			Expect(body.TimeoutSeconds).NotTo(BeNil())
			Expect(*body.TimeoutSeconds).To(Equal(int64(300)))
			Expect(body.TTLSecondsAfterFinished).NotTo(BeNil())
			Expect(*body.TTLSecondsAfterFinished).To(Equal(int32(300)))
		})

		It("creates a rootfs snapshot with all fields", func() {
			reqBody := `{
				"sandboxId": "test-sb-full",
				"snapshotName": "snap-full",
				"containerName": "mycontainer",
				"timeoutSeconds": 600,
				"ttlSecondsAfterFinished": 120
			}`

			resp := testAPI.Post("/v1/rootfs-snapshots", strings.NewReader(reqBody))
			Expect(resp.Code).To(Equal(201))

			var body RootfsSnapshotResponse
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.SandboxID).To(Equal("test-sb-full"))
			Expect(body.SnapshotName).To(Equal("snap-full"))
			Expect(body.ContainerName).To(Equal("mycontainer"))
			Expect(*body.TimeoutSeconds).To(Equal(int64(600)))
			Expect(*body.TTLSecondsAfterFinished).To(Equal(int32(120)))
		})

		It("accepts ttlSecondsAfterFinished of 0", func() {
			reqBody := `{"sandboxId":"test-sb-ttl0","snapshotName":"snap-ttl0","ttlSecondsAfterFinished":0}`
			resp := testAPI.Post("/v1/rootfs-snapshots", strings.NewReader(reqBody))
			Expect(resp.Code).To(Equal(201))

			var body RootfsSnapshotResponse
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(*body.TTLSecondsAfterFinished).To(Equal(int32(0)))
		})

		It("verifies CRD was created in Kubernetes", func() {
			resp := testAPI.Post("/v1/rootfs-snapshots", strings.NewReader(`{"sandboxId":"test-sb-verify","snapshotName":"snap-verify"}`))
			Expect(resp.Code).To(Equal(201))

			var body RootfsSnapshotResponse
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())

			rs := &sandboxv1alpha1.RootfsSnapshot{}
			Eventually(func() error {
				return k8sClient.Get(ctx, keyFor(body.ID), rs)
			}).Should(Succeed())
			Expect(rs.Spec.SandboxName).To(Equal("test-sb-verify"))
			Expect(rs.Spec.SnapshotName).To(Equal("snap-verify"))
		})

		It("rejects missing sandboxId with 422", func() {
			resp := testAPI.Post("/v1/rootfs-snapshots", strings.NewReader(`{"snapshotName":"snap"}`))
			Expect(resp.Code).To(Equal(422))
		})

		It("rejects missing snapshotName with 422", func() {
			resp := testAPI.Post("/v1/rootfs-snapshots", strings.NewReader(`{"sandboxId":"sb"}`))
			Expect(resp.Code).To(Equal(422))
		})

		It("rejects empty sandboxId with 422", func() {
			resp := testAPI.Post("/v1/rootfs-snapshots", strings.NewReader(`{"sandboxId":"","snapshotName":"snap"}`))
			Expect(resp.Code).To(Equal(422))
		})

		It("rejects empty snapshotName with 422", func() {
			resp := testAPI.Post("/v1/rootfs-snapshots", strings.NewReader(`{"sandboxId":"sb","snapshotName":""}`))
			Expect(resp.Code).To(Equal(422))
		})

		It("rejects invalid snapshotName pattern with 422", func() {
			resp := testAPI.Post("/v1/rootfs-snapshots", strings.NewReader(`{"sandboxId":"sb","snapshotName":"UPPERCASE"}`))
			Expect(resp.Code).To(Equal(422))
		})

		It("rejects invalid containerName pattern with 422", func() {
			resp := testAPI.Post("/v1/rootfs-snapshots", strings.NewReader(`{"sandboxId":"sb","snapshotName":"snap","containerName":"NOT_VALID"}`))
			Expect(resp.Code).To(Equal(422))
		})

		It("rejects timeoutSeconds of 0 with 422", func() {
			resp := testAPI.Post("/v1/rootfs-snapshots", strings.NewReader(`{"sandboxId":"sb","snapshotName":"snap","timeoutSeconds":0}`))
			Expect(resp.Code).To(Equal(422))
		})

		It("rejects negative ttlSecondsAfterFinished with 422", func() {
			resp := testAPI.Post("/v1/rootfs-snapshots", strings.NewReader(`{"sandboxId":"sb","snapshotName":"snap","ttlSecondsAfterFinished":-1}`))
			Expect(resp.Code).To(Equal(422))
		})
	})

	Describe("GET /rootfs-snapshots/{id}", func() {
		It("returns rootfs snapshot details", func() {
			createResp := testAPI.Post("/v1/rootfs-snapshots", strings.NewReader(`{"sandboxId":"test-sb-get","snapshotName":"snap-get"}`))
			Expect(createResp.Code).To(Equal(201))

			var created RootfsSnapshotResponse
			Expect(json.NewDecoder(createResp.Body).Decode(&created)).To(Succeed())

			Eventually(func() int {
				return testAPI.Get(fmt.Sprintf("/v1/rootfs-snapshots/%s", created.ID)).Code
			}).Should(Equal(200))

			getResp := testAPI.Get(fmt.Sprintf("/v1/rootfs-snapshots/%s", created.ID))
			var got RootfsSnapshotResponse
			Expect(json.NewDecoder(getResp.Body).Decode(&got)).To(Succeed())
			Expect(got.ID).To(Equal(created.ID))
			Expect(got.SandboxID).To(Equal("test-sb-get"))
			Expect(got.SnapshotName).To(Equal("snap-get"))
		})

		It("returns 404 for nonexistent rootfs snapshot", func() {
			resp := testAPI.Get("/v1/rootfs-snapshots/nonexistent")
			Expect(resp.Code).To(Equal(404))
		})
	})

	Describe("Status mapping", func() {
		now := &metav1.Time{Time: time.Now()}

		It("maps nil startTime and no conditions to Pending", func() {
			Expect(snapshotStatus(nil, nil)).To(Equal(apigateway.StatusPending))
		})

		It("maps nil startTime and empty conditions to Pending", func() {
			Expect(snapshotStatus(nil, []metav1.Condition{})).To(Equal(apigateway.StatusPending))
		})

		It("maps set startTime and no Succeeded condition to Running", func() {
			Expect(snapshotStatus(now, nil)).To(Equal(apigateway.StatusRunning))
		})

		It("maps Succeeded=True to Succeeded", func() {
			conditions := []metav1.Condition{
				{Type: sandboxv1alpha1.RootfsSnapshotSucceededCondition, Status: metav1.ConditionTrue, Reason: sandboxv1alpha1.ReasonRootfsSnapshotSucceeded},
			}
			Expect(snapshotStatus(now, conditions)).To(Equal(apigateway.StatusSucceeded))
		})

		It("maps Succeeded=False to Failed", func() {
			conditions := []metav1.Condition{
				{Type: sandboxv1alpha1.RootfsSnapshotSucceededCondition, Status: metav1.ConditionFalse, Reason: sandboxv1alpha1.ReasonRootfsSnapshotFailed},
			}
			Expect(snapshotStatus(now, conditions)).To(Equal(apigateway.StatusFailed))
		})

		It("maps startTime set with no Succeeded condition to Running", func() {
			Expect(snapshotStatus(now, []metav1.Condition{})).To(Equal(apigateway.StatusRunning))
		})
	})
})

func keyFor(id string) client.ObjectKey {
	return client.ObjectKey{Name: id, Namespace: testNamespace}
}
